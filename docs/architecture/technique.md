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
}
```

There is one `Technique` interface, not one per capability. Each of the three
concrete techniques takes a bare `filesystem.Entry` and type-asserts the single
capability it needs (`NamedStreamCapable`, `SlackSpaceCapable`, or
`TimestompCapable`), returning the `ErrUnsupported` sentinel (wrapped as
`technique %q: unsupported on this filesystem`, so match it with `errors.Is`, not
by string) when the assertion fails. `Get(name)` maps a `--technique` string to
the concrete technique. `Request` is the shared, mostly-optional input bag
(`Data`, `StreamName`, `Field`, `Timestamp`, `Image`, `Restore`, `Backup`); an
operation reads only the fields it needs.

**Slack framing.** Every slack payload is wrapped in a 12-byte frame before it is written, so a raw payload isn't indistinguishable from the filesystem's own residual noise. See [Slack-Space Hiding](../techniques/slack-space.html#nemos-payload-frame) for the exact byte layout. Frame parsing goes through `internal/binutil` so a crafted length never panics. This lives in `slackframe.go`.

**Restoration and backup contract.** `Request.Backup func(Backup) error`, when
set, is called with the pre-write state (`Backup{Technique, Target, Location,
Original []byte, Timestamp}`) before any destructive write; returning an error
aborts the operation. slack-space `Hide` and `Clear` emit the overwritten bytes
this way. `clear` for slack-space writes back the caller-supplied original bytes
(`Request.Restore`, distinct from the hide payload in `Request.Data`) when
available, otherwise zero-fills the frame; `Result.Restored` reports which
happened. A `Request.Restore` whose length does not match the frame is rejected
rather than zero-filled. A nil `Backup` means the caller opted out of
reversibility.

The persistence side lives in `manifest.go`: `AppendManifest(path, Backup)`,
`LoadManifest(path)`, `LatestBackup(records, technique, target, location)`. The
file (`nemo-manifest.jsonl` by default, `--manifest` to relocate) is JSON Lines,
one `Backup` per line, `Original` as base64, `Timestamp` as RFC 3339. `hide`
passes a closure calling `AppendManifest`; a write failure there aborts the hide
before any bytes are overwritten. `clear`, once built, replays it via
`LoadManifest` and `LatestBackup`; later records win, so re-hiding a target then
clearing restores the last hide's bytes. See [CLI Reference](../cli/#the-manifest) for the user-facing flag.

**Timestomp limitation.** `filesystem.TimestompCapable` exposes only
`SetTimestamp`, with no reader, so `timestomp.Detect` always returns no findings
and `timestomp.Clear` can only restore to a timestamp the caller supplies
explicitly (it errors on a zero value). See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp) for the full reasoning. An `ext4.Entry` happens to have a
`Timestamp` reader of its own, but the capability interface does not surface it;
widening the interface is a follow-up.

`Finding` and `Result` are shared, technique-agnostic value types:

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

`Finding` is what `detect` prints, one per line (technique, location, size), matching [CLI Reference](../cli/detect.html)'s output spec. `Result` is what `hide` and `clear` return on success. It carries no hash or timestamp of its own, because `internal/custody` already captures both for every `WriteAt` at the `Image` layer, see [Image and Custody](image.html). The command builds the custody-log line from `Result` (which technique, which target, what happened) plus the hash and timestamp the custody decorator recorded, so neither layer duplicates the other.

Commands depend only on these interfaces, never on a filesystem-specific technique type. Because `Hide`/`Detect`/`Clear` only touch the capability interfaces, the technique logic is filesystem-agnostic: one concrete implementation per technique kind (`namedStreamTechnique`, `slackSpaceTechnique`, `timestompTechnique`), all in `internal/technique`, each working against any `Entry` that satisfies the capability it needs.
