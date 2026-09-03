# Nemo: Architecture

## Goal

Adding a filesystem (FAT32 later, say) or a technique should mean writing a new package that satisfies existing interfaces, not touching code that already works. This lets contributors own a filesystem or technique independently and ship it incrementally, without stepping on each other or on the `cmd/` command files.

## File structure

What exists today is marked "built"; the rest is the planned layout. `nemo clear`, live mode, `internal/tskcheck`, and `internal/validate` are not written yet.

```
nemo/
  main.go                  // built: calls cmd.Execute
  cmd/                      // package cmd, one file per command
    root.go                // built: root command + "nemo version" (embeds VERSION)
    VERSION                // built
    hide.go                // built
    detect.go              // built
    features.go            // built: "nemo features" support matrix
    filesystems.go         // built: blank-imports the filesystem packages so they register
    clear.go               // planned
  internal/
    image/
      image.go             // built: Image interface (ReadAt, WriteAt, Size, Path)
      rawimage.go          // built: os.File backend, Open + OpenReadOnly
      readonly.go          // built: ReadOnly wrapper, fails every write (detect's guard)
    binutil/
      binutil.go           // built: Uint/Int, Bits, String/UTF16String
      reader.go            // built: sequential cursor with a sticky first-error
      doc.go               // built
    filesystem/
      filesystem.go        // built: FileSystem, Entry, Type, the three capability interfaces
      registry.go          // built: Detector, Register, Detectors, signature-based Open
      fakefs/
        fakefs.go          // built: in-memory FileSystem/Entry with all three capabilities
        image.go           // built: in-memory Image, for tests
      ntfs/                // planned
        ntfs.go
        mft.go
        namedstream.go     // ADS
        slack.go
        timestomp.go
        live_windows.go    // live-mode Entry: direct syscalls, no MFT parsing
        live_stub.go       // "unsupported on this OS" on non-Windows builds
      apfs/
        apfs.go            // built: parser + Detector (no techniques wired yet)
        btree.go           // built
        namedstream.go     // planned: xattr + resource fork
        slack.go           // planned
        timestomp.go       // planned
        live_darwin.go     // planned
        live_stub.go       // planned
      ext4/
        ext4.go            // built: parser + Detector (named-stream, timestomp)
        inode.go           // built
        xattr.go           // built: named streams via xattr
        timestomp.go       // built
        slack.go           // planned
        live_linux.go      // planned
        live_stub.go       // planned
    technique/
      technique.go         // built: Technique interface, Finding/Result/Backup/Request, Get, ErrUnsupported
      slackframe.go        // built: slack frame encode/decode
      manifest.go          // built: JSON Lines backup manifest for clear
    custody/
      custody.go           // built: Wrap decorator, SHA-256 + record per WriteAt
      log.go               // built: Record type, append to the custody log
    tskcheck/              // planned
      tskcheck.go          // cgo adapter over libtsk, isolated so only this package needs cgo
    validate/             // planned
      harness.go           // runs against fkie-cad/hide-and-seek-dataset
  go.mod
```

## Layers

```
cmd/ (hide.go, detect.go, features.go, clear.go)   Cobra commands
            |
      internal/technique                     Finding/Result, the Technique interface
            |
      internal/filesystem                    FileSystem/Entry interfaces, registry
        ntfs/ apfs/ ext4/                     one implementation package per FS
            |
      internal/custody                       Image decorator: hash + record every write
            |
      internal/image                         Image interface: ReadAt/WriteAt/Size/Path
```

Each layer depends only on the interfaces of the layer below it, never on a concrete sibling. `internal/technique` code never imports `internal/filesystem/ext4` directly; it calls through the `FileSystem` interface. That is what makes filesystems and techniques independently pluggable.

## Live mode vs. image mode (planned)

Only image mode is built today. Everything in this section is design intent for live mode.

Image mode and live mode will not share one code path per filesystem. They split by technique, not by a mode flag on a single `Entry`:

- Named-stream and timestomp never need volume-level parsing. An NTFS ADS is `os.OpenFile("target:stream")`, an ext4/APFS xattr is a syscall, timestomping is a `SetFileTime` or `utimes`-family call. None of that touches an MFT, a B-tree, or an inode table.
- Slack-space is inherently volume-level. The unused tail bytes of an allocated cluster or block are not exposed by any normal file API on any OS. Doing it live means opening the raw block device (`\\.\PhysicalDriveN`, `/dev/diskN`) and running the same parser image mode uses, which needs admin or root, unlike the other two techniques. This is in scope (per `docs/overall-plan.md`), so `hide`/`clear`/`detect` with `--technique slack-space` and no `--image` must detect a missing-privilege condition and fail with a clear error rather than silently degrading.

Each filesystem package will ship two `Entry` implementations, not two modes bolted onto one:

- image-mode `Entry`: backed by a parsed `Image` (a disk-image file or an opened raw device), implements all three capability interfaces, including `SlackSpaceCapable`.
- live-mode `Entry`: backed by a single OS path, implements `NamedStreamCapable` and `TimestompCapable` via direct syscalls, and implements `SlackSpaceCapable` only when constructed against an opened raw device (running elevated).

`registry.go`'s signature-based `Sniff` detection only applies to image mode; it identifies a filesystem from bytes. Live mode never sniffs. "Which filesystem" is just "which OS this binary is running on," so live mode picks its `Entry` implementation via a Go build-tag file per package (`ntfs/live_windows.go`, `apfs/live_darwin.go`, `ext4/live_linux.go`), each an "unsupported on this OS" stub on the other two platforms so the binary still builds and runs cross-platform.

## Contracts

### `internal/image.Image`

```go
type Image interface {
    ReadAt(p []byte, off int64) (int, error)
    WriteAt(p []byte, off int64) (int, error)
    Size() int64
    Path() string
}
```

`rawimage.go` implements this over `os.File`, with `Open` for read-write and `OpenReadOnly` for `detect`. `readonly.go` adds a `ReadOnly` wrapper whose `WriteAt` always returns `ErrReadOnly`; `detect` wraps its image in that as a second guard. Live mode, once built, will use the same interface backed by a single target file, so filesystem and technique code never branches on mode.

Every write goes through `internal/custody`. `custody.Wrap(img)` returns a `Recorder` (the `Image` interface plus `EventsSnapshot`): it wraps a real `Image`, SHA-256-hashes and records each `WriteAt`, then delegates. Callers wrap once and pass the result everywhere; nothing downstream needs to know custody recording exists. `log.go` turns a recorded event plus the command's semantic context into a `Record` and appends it to the custody log.

### `internal/filesystem.FileSystem` and `Entry`

```go
type FileSystem interface {
    Type() Type
    Root() Entry
    Open(path string) (Entry, error)
}

type Entry interface {
    Path() string
    IsDir() bool
    Children() ([]Entry, error) // empty for non-directories
    NamedStreams() ([]string, error)
    // slack-space and timestomp access are exposed via optional
    // interfaces (below), not required on every Entry.
}
```

`Children()` backs `detect` with no target: the command calls `Root()` then walks `Children()` recursively (the shape of `io/fs.WalkDir`), running the technique's `Detect` against every `Entry` it reaches. `hide`/`clear` always take an explicit target, so they only call `Open(path)` and never walk. `internal/filesystem/fakefs` is an in-memory `FileSystem`/`Entry` implementing all three capabilities plus an in-memory `Image`; the command and technique tests run against it.

`registry.go` holds a signature-based lookup (a `[]Detector`, each a byte-pattern sniff plus a constructor) and returns the right `FileSystem` for an image. Adding a filesystem means registering a `Detector` (from that package's `init`) plus a new `internal/filesystem/<fs>/` package; nothing in `internal/technique` or the commands changes.

`Register` panics on an invalid `Detector` (empty `Type`, nil `Sniff`, or nil `New`) or on a `Type` already registered. Registration happens at package init, so these are startup-time programmer errors, not conditions to recover from. `Open` reads up to the first 4096 bytes and sniffs every registered detector rather than stopping at the first hit: no match is an error naming the registered filesystems, more than one match is an "ambiguous image format" error naming every candidate.

Not every filesystem supports every capability the same way, so rather than force all three `Entry` implementations to satisfy a bloated interface, capabilities are split into optional interfaces an `Entry` may also implement:

```go
type NamedStreamCapable interface {
    WriteStream(name string, data []byte) error
    ReadStream(name string) ([]byte, error)
    DeleteStream(name string) error
}

type SlackSpaceCapable interface {
    SlackRegions() ([]SlackRegion, error)
}

type TimestompCapable interface {
    SetTimestamp(field TimeField, t time.Time) error
}
```

`internal/technique` type-asserts the `Entry` it is given against the capability it needs and returns an "unsupported on this filesystem" error if the assertion fails. This is what lets ext4 ship named-stream and timestomp support while its slack-space support does not exist yet, without a stub implementation of an oversized interface.

### `internal/technique`

[docs/techniques/](techniques/overview.md) covers what each technique does on
disk: byte layouts, capacity, and how it gets detected. This section covers only
the code shape.

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

**Slack framing.** Slack regions normally hold whatever residual bytes the
filesystem left behind, so a raw payload is indistinguishable from noise. Every
slack payload is wrapped in a 12-byte frame before it is written: magic `NEMO`
(4 bytes), `length` as uint32 LE (4), `crc32` IEEE of the payload (4). `detect`
reports a slack finding only when the magic and CRC both validate; `clear` knows
exactly which bytes it wrote. Frame parsing goes through `internal/binutil` so a
crafted length never panics. This lives in `slackframe.go`.

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
clearing restores the last hide's bytes.

**Timestomp limitation.** `filesystem.TimestompCapable` exposes only
`SetTimestamp`, with no reader, so `timestomp.Detect` always returns no findings
and `timestomp.Clear` can only restore to a timestamp the caller supplies
explicitly (it errors on a zero value). An `ext4.Entry` happens to have a
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

`Finding` is what `detect` prints, one per line (technique, location, size), matching `docs/user-interface.md`'s output spec. `Result` is what `hide` and `clear` return on success. It carries no hash or timestamp of its own, because `internal/custody` already captures both for every `WriteAt` at the `Image` layer. The command builds the custody-log line from `Result` (which technique, which target, what happened) plus the hash and timestamp the custody decorator recorded, so neither layer duplicates the other.

Commands depend only on these interfaces, never on a filesystem-specific technique type. Because `Hide`/`Detect`/`Clear` only touch the capability interfaces, the technique logic is filesystem-agnostic: one concrete implementation per technique kind (`namedStreamTechnique`, `slackSpaceTechnique`, `timestompTechnique`), all in `internal/technique`, each working against any `Entry` that satisfies the capability it needs.

### Technique selection and the `features` matrix

The commands pick which technique kind purely from the `--technique` flag string, through a switch in `internal/technique`, no filesystem involved:

```go
func Get(name string) (Technique, error) // "named-stream" | "slack-space" | "timestomp"
```

Whether the selected technique works against the given target is answered separately, at call time, by the type assertion above: the `Entry` implements the required capability or it does not. So filesystem support is never a lookup table keyed by `(FileSystem type, technique)`; it falls out of whether a given `ntfs.Entry`/`apfs.Entry`/`ext4.Entry` implements the capability.

`nemo features` needs that matrix without an image loaded, so it cannot run the type assertion against a live `Entry`. Instead each filesystem package declares its supported technique names once, in the `Detector` it registers:

```go
type Detector struct {
    Type       Type
    Sniff      func([]byte) bool
    New        func(image.Image) (FileSystem, error)
    Techniques []string // ext4 today: []string{"named-stream", "timestomp"}
}
```

`features` reads `Techniques` off every registered `Detector` and prints the matrix: no image, no reflection, and no way to drift from `registry.go` because it reads the same struct detection uses. When ext4 gains slack-space support, that is a one-line change to its `Detector.Techniques`.

### `internal/tskcheck` (planned)

Isolated in its own package because it is the only place cgo is required (the libtsk binding). Nothing else in the tree may import it, so the rest of the codebase stays buildable with `CGO_ENABLED=0`. Only `detect` will consume it, as an optional read-only cross-check behind an interface (`Validator.CrossCheck(image.Image) ([]Finding, error)`), so `detect` does not hard-depend on cgo.

## Why this shape

- Independent ownership: one person builds `internal/filesystem/apfs` while another builds `internal/filesystem/ext4`. Both only need to satisfy `FileSystem`/`Entry`, so neither blocks the other and neither touches shared code.
- Incremental shipping: a filesystem can land with only `NamedStreamCapable`. `SlackSpaceCapable` and `TimestompCapable` are separate optional interfaces, so partial support is just the unbuilt pieces missing, not a broken implementation of one big interface.
- New filesystem equals a new package plus one registry line. No changes to `internal/technique` or the commands.
- Custody recording is structural, not a convention. Because it is a decorator over `Image`, a technique cannot skip it: every `WriteAt` on the wrapped image is hashed and recorded regardless of the caller.

## Non-goals

This mirrors `docs/overall-plan.md`'s scope: no runtime plugin loading (dynamic `.so`/`.dll`), no config-driven technique selection beyond the `--technique` flag. "Pluggable" here means new Go packages behind existing interfaces, compiled in.
