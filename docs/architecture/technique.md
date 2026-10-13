---
title: Technique Interfaces
parent: Architecture
nav_order: 3
---

# Technique Interfaces

[Techniques](../techniques/) covers what each technique does on disk: byte layouts, capacity, and how it gets detected. This page covers only the code shape.

```go
type Technique interface {
    Name() string
    Hide(filesystem.Entry, Request) (Result, error)
    Detect(filesystem.Entry, Request) ([]Finding, error)
    Clear(filesystem.Entry, Request) (Result, error)
    Extract(filesystem.Entry, Request) ([]byte, error)
}
```

There is one [`Technique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L92) interface, not one per capability. Each of the three
concrete techniques takes a bare [`filesystem.Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) and type-asserts the single
capability it needs ([`NamedStreamCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L39), [`SlackSpaceCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L52), or
[`TimestompCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L81)), returning the [`ErrUnsupported`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L29) sentinel (wrapped as
`technique %q: unsupported on this filesystem`, so match it with `errors.Is`, not
by string) when the assertion fails. [`Get(name)`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L105) maps a `--technique` string to
the concrete technique. [`Request`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L71) is the shared, mostly-optional input bag
(`Data`, `StreamName`, `Field`, [`Timestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L82), [`Image`](https://github.com/sahithyandev/nemo/blob/main/internal/image/image.go#L4), `Restore`, [`Backup`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L59)); an
operation reads only the fields it needs.

**Slack framing.** New payloads use a 28-byte `NEM2` header with a random per-hide nonce; historical 12-byte `NEMO` frames remain readable. The header distinguishes payloads from residual noise and prevents identical payloads from reusing stale backup digests. See [Slack-Space Hiding](../techniques/slack-space.html#nemos-payload-frame) for the exact byte layout. Frame parsing checks the versioned header and payload bounds before slicing. This lives in [`slackframe.go`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/slackframe.go).

**Restoration and backup contract.** For slack-space operations,
`Request.Backup func(Backup) error`, when set, is called with the pre-write state (`Backup{Technique, Target, Location,
Original []byte, Timestamp}`) before any destructive write; returning an error
aborts the operation. slack-space [`Hide`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L94) and [`Clear`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L96) emit the overwritten bytes
this way. `clear` for slack-space writes back the caller-supplied original bytes
([`Request.Restore`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L81), distinct from the hide payload in [`Request.Data`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L72)) when
available, otherwise zero-fills the frame; [`Result.Restored`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L50) reports which
happened. A [`Request.Restore`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L81) whose length does not match the frame is rejected
rather than zero-filled. A nil [`Backup`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L59) means the caller opted out of
reversibility.

The persistence side lives in [`manifest.go`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/manifest.go): [`AppendManifest(path, Backup)`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/manifest.go#L24),
[`LoadManifest(path)`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/manifest.go#L41), [`LatestImageBackup(records, technique, target, location, identity)`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/manifest.go#L125). The
file (`nemo-manifest.jsonl` by default, `--manifest` to relocate) is JSON Lines,
one [`Backup`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L59) per line, `Original` as base64, [`Timestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L82) as RFC 3339. `hide`
passes a closure calling [`AppendManifest`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/manifest.go#L24); a write failure there aborts the hide
before any bytes are overwritten. `clear` replays it via
[`LoadManifest`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/manifest.go#L41) and [`LatestImageBackup`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/manifest.go#L125); image identity and exact frame digest are checked before mutation, and later matching records win, so re-hiding a target then
clearing restores the last hide's bytes. `--manifest` (default `nemo-manifest.jsonl`) names that file. `hide` appends to it before overwriting slack-space bytes, so a later `clear` can restore them. See [Slack-Space Hiding](../techniques/slack-space.html) for what gets recorded.

**Extract.** `extract` reads a hidden payload back out without mutating anything.
named-stream delegates to `filesystem.NamedStreamCapable.ReadStream`. slack-space
walks the entry's [`SlackRegions`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L53) and returns the payload of the first valid framed
slack payload it finds (CRC-checked via the same frame parser [`Hide`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L94)/[`Clear`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L96) use),
erroring `"no framed slack payload found to extract"` if none decode. timestomp has
no retrievable payload (see below) and always errors.

**First-frame limitation.** If a target's slack space holds more than one valid
framed payload (for example, stale frames left behind by an earlier hide that
was never cleared), [`Extract`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L100) returns only the first one it finds, while
[`Detect`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L95) lists every frame it finds. The two can disagree about which payload
is "the" payload on such a target. [`Clear`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L96) makes the same first-frame
assumption, so the behavior is at least consistent across all three. There is no
`--index` flag to pick a specific frame.

**Timestomp limitation.** [`filesystem.TimestompCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L81) exposes [`Timestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L82),
[`SupportsTimestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L83), and [`SetTimestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L84). Reading a current value does not establish
whether it was altered. No historical baseline comparison or anomaly detector is
implemented, so `timestomp.Detect` returns no findings. `timestomp.Hide` does not
automatically save original timestamps to the manifest; `timestomp.Clear` requires
the caller to supply the original value explicitly and rejects a zero value.
See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp).

[`Finding`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L32) and [`Result`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L39) are shared, technique-agnostic value types:

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

[`Finding`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L32) is what `detect` prints, one per line (technique, location, size), matching [CLI Reference](../cli/detect.html)'s output spec. [`Result`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L39) is what `hide` and `clear` return on success. It carries no hash or timestamp of its own, because `internal/custody` already captures both for every [`WriteAt`](https://github.com/sahithyandev/nemo/blob/main/internal/image/image.go#L6) at the [`Image`](https://github.com/sahithyandev/nemo/blob/main/internal/image/image.go#L4) layer, see [Image and Custody](image.html). The command builds the custody-log line from [`Result`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L39) (which technique, which target, what happened) plus the hash and timestamp the custody decorator recorded, so neither layer duplicates the other.

Commands depend only on these interfaces, never on a filesystem-specific technique type. Because [`Hide`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L94)/[`Detect`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L95)/[`Clear`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L96) only touch the capability interfaces, the technique logic is filesystem-agnostic: one concrete implementation per technique kind ([`namedStreamTechnique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L126), [`slackSpaceTechnique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L197), [`timestompTechnique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L376)), all in `internal/technique`, each working against any [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) that satisfies the capability it needs.
