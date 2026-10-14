---
title: Technique Interfaces
parent: Architecture
nav_order: 3
---

# Technique interfaces

[Techniques](../techniques/) covers what each technique does on disk: byte layouts, capacity, and detection. This page covers only the code shape.

```go
type Technique interface {
    Name() string
    Hide(filesystem.Entry, Request) (Result, error)
    Detect(filesystem.Entry, Request) ([]Finding, error)
    Clear(filesystem.Entry, Request) (Result, error)
    Extract(filesystem.Entry, Request) ([]byte, error)
}
```

## One interface, three techniques

There is one [`Technique`]({{ site.src }}/internal/technique/technique.go#L92) interface, not one per capability.

Each concrete technique takes a bare [`filesystem.Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) and checks for the one capability it needs: [`NamedStreamCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L39), [`SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52), or [`TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81).

If the check fails, it returns the [`ErrUnsupported`]({{ site.src }}/internal/technique/technique.go#L29) sentinel, wrapped as `technique %q: unsupported on this filesystem`. Match it with `errors.Is`, not by string.

[`Get(name)`]({{ site.src }}/internal/technique/technique.go#L105) maps a `--technique` string to the concrete technique.

[`Request`]({{ site.src }}/internal/technique/technique.go#L71) is the shared input. Most of its fields are optional, and an operation reads only the ones it needs:

- `Data`
- `StreamName`
- `Field`
- [`Timestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L82)
- [`Image`]({{ site.src }}/internal/image/image.go#L4)
- `Restore`
- [`Backup`]({{ site.src }}/internal/technique/technique.go#L59)

## Slack framing

New payloads use a 28-byte `NEM2` header with a random per-hide nonce. Older 12-byte `NEMO` frames stay readable.

The header tells a payload apart from leftover noise. The nonce stops identical payloads from reusing stale backup digests. Frame parsing checks the versioned header and payload bounds before slicing. The code is in [`slackframe.go`]({{ site.src }}/internal/technique/slackframe.go).

The exact byte layout is in [nemo's payload frame](../techniques/slack-space.html#nemos-payload-frame).

## Restoration and backup contract

For slack-space operations, `Request.Backup func(Backup) error` is called before any destructive write, when it is set. It receives the pre-write state: `Backup{Technique, Target, Location, Original []byte, Timestamp}`. If it returns an error, the operation aborts.

- Slack-space [`Hide`]({{ site.src }}/internal/technique/technique.go#L94) and [`Clear`]({{ site.src }}/internal/technique/technique.go#L96) pass the overwritten bytes this way.
- `clear` writes back the caller's original bytes ([`Request.Restore`]({{ site.src }}/internal/technique/technique.go#L81), separate from the hide payload in [`Request.Data`]({{ site.src }}/internal/technique/technique.go#L72)) when it has them. Otherwise it zero-fills the frame.
- [`Result.Restored`]({{ site.src }}/internal/technique/technique.go#L50) says which happened.
- A [`Request.Restore`]({{ site.src }}/internal/technique/technique.go#L81) whose length does not match the frame is rejected, not zero-filled.
- A nil [`Backup`]({{ site.src }}/internal/technique/technique.go#L59) means the caller opted out of reversibility.

### Persistence

The saving side is in [`manifest.go`]({{ site.src }}/internal/technique/manifest.go):

- [`AppendManifest(path, Backup)`]({{ site.src }}/internal/technique/manifest.go#L24)
- [`LoadManifest(path)`]({{ site.src }}/internal/technique/manifest.go#L41)
- [`LatestImageBackup(records, technique, target, location, identity)`]({{ site.src }}/internal/technique/manifest.go#L125)

The file is `nemo-manifest.jsonl` by default, and `--manifest` moves it. It is JSON Lines with one [`Backup`]({{ site.src }}/internal/technique/technique.go#L59) per line. `Original` is base64 and [`Timestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L82) is RFC 3339.

- `hide` passes a closure that calls [`AppendManifest`]({{ site.src }}/internal/technique/manifest.go#L24). If that write fails, the hide aborts before any byte is overwritten.
- `clear` reads it back with [`LoadManifest`]({{ site.src }}/internal/technique/manifest.go#L41) and [`LatestImageBackup`]({{ site.src }}/internal/technique/manifest.go#L125). It checks the image identity and exact frame digest before changing anything.
- Later matching records win. Re-hiding a target and then clearing it restores the last hide's bytes.

See [slack-space hiding](../techniques/slack-space.html) for what gets recorded.

## Extract

`extract` reads a hidden payload without changing anything.

- **named-stream:** calls `filesystem.NamedStreamCapable.ReadStream`.
- **slack-space:** walks the entry's [`SlackRegions`]({{ site.src }}/internal/filesystem/filesystem.go#L53) and returns the first valid framed payload. It uses the same CRC-checking frame parser as [`Hide`]({{ site.src }}/internal/technique/technique.go#L94) and [`Clear`]({{ site.src }}/internal/technique/technique.go#L96). If none decodes, it errors with `"no framed slack payload found to extract"`.
- **timestomp:** has no payload to retrieve and always errors.

## First-frame limitation

A target's slack space can hold more than one valid frame. Stale frames from an earlier, uncleared hide are one cause.

[`Extract`]({{ site.src }}/internal/technique/technique.go#L100) returns only the first frame. [`Detect`]({{ site.src }}/internal/technique/technique.go#L95) lists them all. So the two can disagree about which payload is the payload. [`Clear`]({{ site.src }}/internal/technique/technique.go#L96) also uses the first frame, so at least `extract` and `clear` agree.

There is no `--index` flag to pick a specific frame.

## Timestomp limitation

[`filesystem.TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81) exposes [`Timestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L82), [`SupportsTimestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L83), and [`SetTimestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L84).

Reading a current value does not show whether it was altered. There is no historical baseline and no anomaly detector, so `timestomp.Detect` returns no findings.

`timestomp.Hide` does not save the original timestamp to the manifest. `timestomp.Clear` needs the caller to supply the original, and it rejects a zero value. See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp).

## `Finding` and `Result`

[`Finding`]({{ site.src }}/internal/technique/technique.go#L32) and [`Result`]({{ site.src }}/internal/technique/technique.go#L39) are shared value types that do not depend on the technique:

```go
type Finding struct {
    Technique string // "named-stream" | "slack-space" | "timestomp"
    Location  string // stream name, slack region offset range, or timestamp field
    Size      int64  // bytes of hidden data recovered; 0 for timestomp findings
}

type Result struct {
    Technique string
    Target    string // path of the entry acted on
    Detail    string // stream name written, slack offset range, or "field=value" for timestomp
    Bytes     int64  // bytes written/removed; 0 for timestomp
}
```

[`Finding`]({{ site.src }}/internal/technique/technique.go#L32) is what `detect` prints, one per line (technique, location, size). It matches the output spec in the [CLI reference](../cli/detect.html).

[`Result`]({{ site.src }}/internal/technique/technique.go#L39) is what `hide` and `clear` return on success. It carries no hash or timestamp, because `internal/custody` already captures both for every [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) at the [`Image`]({{ site.src }}/internal/image/image.go#L4) layer. See [Image and custody](image.html).

The command builds the custody-log line from [`Result`]({{ site.src }}/internal/technique/technique.go#L39) (which technique, which target, what happened) plus the hash and timestamp the custody decorator recorded. Neither layer duplicates the other.

## Why commands stay simple

Commands depend only on these interfaces, never on a filesystem-specific technique type.

[`Hide`]({{ site.src }}/internal/technique/technique.go#L94), [`Detect`]({{ site.src }}/internal/technique/technique.go#L95), and [`Clear`]({{ site.src }}/internal/technique/technique.go#L96) only touch the capability interfaces. So the technique logic works on any filesystem. There is one implementation per technique, all in `internal/technique`: [`namedStreamTechnique`]({{ site.src }}/internal/technique/technique.go#L126), [`slackSpaceTechnique`]({{ site.src }}/internal/technique/technique.go#L197), and [`timestompTechnique`]({{ site.src }}/internal/technique/technique.go#L376). Each works with any [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) that has the capability it needs.
