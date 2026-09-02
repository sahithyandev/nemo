# Nemo: Architecture

## Goal

Adding a filesystem (e.g. FAT32 later) or a technique should mean writing a new package that satisfies existing interfaces, not touching code that already works. This lets contributors own a filesystem or technique independently and ship it incrementally, without stepping on each other or on `cmd_hide.go`/`cmd_detect.go`/`cmd_clear.go`.

## File structure

```
nemo/
  main.go
  cmd/
    root.go
    VERSION
  internal/
    image/
      image.go          // Image interface: ReadAt, WriteAt, Size, Path
      rawimage.go        // concrete implementation over os.File
    binutil/
      binutil.go         // shared bitfield extraction, endian helpers, fixed-width string reads
    filesystem/
      filesystem.go       // FileSystem interface, Entry interface, Type enum
      registry.go          // signature-based detection + factory
      ntfs/
        ntfs.go
        mft.go
        namedstream.go     // ADS (image-mode Entry)
        slack.go
        timestomp.go
        live_windows.go    // live-mode Entry: direct syscalls, no MFT parsing
        live_stub.go        // "unsupported on this OS" on non-Windows builds
      apfs/
        apfs.go
        btree.go
        namedstream.go     // xattr + resource fork (image-mode Entry)
        slack.go
        timestomp.go
        live_darwin.go     // live-mode Entry: direct syscalls, no B-tree parsing
        live_stub.go        // "unsupported on this OS" on non-macOS builds
      ext4/
        ext4.go
        inode.go
        namedstream.go     // xattr (image-mode Entry)
        slack.go
        timestomp.go
        live_linux.go      // live-mode Entry: direct syscalls, no inode parsing
        live_stub.go        // "unsupported on this OS" on non-Linux builds
    technique/
      technique.go         // NamedStreamTechnique, SlackSpaceTechnique, TimestompTechnique interfaces, Finding/Result types
    custody/
      custody.go           // decorator wrapping Image, logs + SHA-256 hashes every write
      log.go
    tskcheck/
      tskcheck.go           // cgo adapter over libtsk, isolated so only this package needs cgo
    validate/
      harness.go            // runs against fkie-cad/hide-and-seek-dataset
  cmd_hide.go / cmd_detect.go / cmd_clear.go   // cobra command definitions, one file per mode
  go.mod
```

## Layers

```
cmd_hide.go / cmd_detect.go / cmd_clear.go   (Cobra commands)
            |
      internal/technique                     (Finding/Result, per-technique interfaces)
            |
      internal/filesystem                    (FileSystem/Entry interfaces, registry)
        ntfs/ apfs/ ext4/                     (one implementation package per FS)
            |
      internal/custody                       (Image decorator: hash + log every write)
            |
      internal/image                         (Image interface: ReadAt/WriteAt/Size/Path)
```

Each layer only depends on the interfaces of the layer below it, never on a concrete sibling. `internal/technique` code never imports `internal/filesystem/ntfs` directly — it calls through the `FileSystem` interface. This is what makes filesystems and techniques independently pluggable.

## Live mode vs. image mode

Image mode and live mode do not share one code path per filesystem — they split by *technique*, not by a mode flag on a single `Entry`:

- **Named-stream and timestomp** never need volume-level parsing. An NTFS ADS is `os.OpenFile("target:stream")`, an ext4/APFS xattr is a syscall, timestomping is a `SetFileTime`/`utimes`-family call. None of that touches an MFT, a B-tree, or an inode table.
- **Slack-space** is inherently volume-level: the unused tail bytes of an allocated cluster/block aren't exposed by any normal file API on any OS. Doing it live means opening the raw block device (`\\.\PhysicalDriveN`, `/dev/diskN`) and running the *same* parser image mode uses — which requires admin/root, unlike the other two techniques. This is in scope (per `docs/overall-plan.md`), so `hide`/`clear`/`detect` with `--technique slack-space` and no `--image` must detect a missing-privilege condition and fail with a clear error rather than silently degrading.

Concretely, each filesystem package ships **two `Entry` implementations**, not two modes bolted onto one:

- **image-mode `Entry`**: backed by a parsed `Image` (a disk-image file or an opened raw device), implements all three capability interfaces, including `SlackSpaceCapable`.
- **live-mode `Entry`**: backed by a single OS path, implements `NamedStreamCapable`/`TimestompCapable` via direct syscalls, and additionally implements `SlackSpaceCapable` only when constructed against an opened raw device (i.e. running elevated).

`registry.go`'s signature-based `Sniff` detection only applies to image mode — it identifies a filesystem from bytes. Live mode never sniffs: "which filesystem" is just "which OS this binary is running on," so live mode picks its `Entry` implementation via `runtime.GOOS` (or a Go build-tag file per package, e.g. `ntfs/live_windows.go`, `apfs/live_darwin.go`, `ext4/live_linux.go`), each a no-op/"unsupported on this OS" stub on the other two platforms so the binary still builds and runs cross-platform.

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

`rawimage.go` implements this over `os.File` for image mode. Live mode uses the same interface backed by a single target file, so filesystem and technique code never branches on mode — the `Image` they're given is just a different concrete type.

Every write goes through `internal/custody`, a decorator implementing the same `Image` interface: it wraps a real `Image`, hashes and logs each `WriteAt`, then delegates. Callers construct `custody.Wrap(img)` once and pass the result everywhere; nothing downstream needs to know custody logging exists.

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

`Children()` is what backs `detect` with no target: the command calls `Root()` then walks `Children()` recursively (mirroring `io/fs.WalkDir`'s shape), running the technique's `Detect` against every `Entry` it reaches. `hide`/`clear` always take an explicit target, so they only ever call `Open(path)` directly and never need to walk.

`registry.go` holds a signature-based lookup (`[]Detector` — byte pattern → constructor) and returns the right `FileSystem` for an image or file. Adding NTFS/APFS/ext4 support means registering a detector in `registry.go` plus a new `internal/filesystem/<fs>/` package; nothing in `internal/technique` or the Cobra commands changes.

`Register` panics on an invalid `Detector` (empty `Type`, nil `Sniff`, or nil `New`) or on a `Type` already registered — registration happens at package init, so these are startup-time programmer errors, not conditions to recover from. `Open` sniffs against every registered detector rather than stopping at the first hit: no match is an actionable error naming the registered filesystems, and more than one match is an "ambiguous image format" error naming every candidate, rather than silently picking whichever detector happened to register first.

Not every filesystem supports every capability the same way, so instead of forcing all three `Entry` implementations to satisfy a bloated interface, capabilities are split into optional interfaces an `Entry` may additionally implement:

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

`internal/technique` type-asserts the `Entry` it's given against the capability it needs, and returns a clear "unsupported on this filesystem" error if the assertion fails. This is the mechanism that lets ext4 ship named-stream support before its slack-space support exists, without a partial/stub implementation of an oversized interface.

### `internal/technique`

```go
type Technique interface {
    Name() string
    Hide(filesystem.Entry, Request) (Result, error)
    Detect(filesystem.Entry, Request) ([]Finding, error)
    Clear(filesystem.Entry, Request) (Result, error)
}
```

There is one `Technique` interface, not one per capability: each of the three
concrete techniques takes a bare `filesystem.Entry` and type-asserts the single
capability it needs (`NamedStreamCapable` / `SlackSpaceCapable` /
`TimestompCapable`), returning the sentinel `ErrUnsupported` (wrapped, so
`errors.Is` matches and the message — `"unsupported on this filesystem"` — stays
stable) when the assertion fails. `Request` is the shared, mostly-optional input
bag (`Data`, `StreamName`, `Field`, `Timestamp`, `Image`, `Backup`); an operation
reads only the fields it needs.

**Slack framing.** Slack regions normally hold whatever residual bytes the
filesystem left behind, so a raw payload is indistinguishable from noise. Every
slack payload is wrapped in a 12-byte frame — `magic "NEMO"` (4) + `length`
uint32 LE (4) + `crc32` IEEE of the payload (4) — before it is written. `detect`
reports a slack finding only when the magic and CRC both validate; `clear` knows
exactly which bytes it wrote. Frame parsing goes through `internal/binutil` so a
crafted length never panics.

**Restoration / backup contract.** `Request.Backup func(Backup) error`, when set,
is called with the pre-write state (`Backup{Technique, Target, Location, Original
[]byte, Timestamp}`) *before* any destructive write; returning an error aborts
the operation. slack-space `Hide`/`Clear` emit the overwritten bytes this way.
`clear` for slack-space restores the caller-supplied original bytes (passed back
in `Request.Data`) when available, otherwise zero-fills the frame. The on-disk
manifest format that persists these records is `cmd_clear.go`'s concern, not this
package's. A nil `Backup` means the caller opted out of reversibility.

**Timestomp limitation.** `filesystem.TimestompCapable` exposes only
`SetTimestamp`, with no reader, so `timestomp.Detect` always returns no findings
and `timestomp.Clear` can only restore to a timestamp the caller supplies
explicitly (it errors on a zero value). Giving the capability a timestamp reader
is a follow-up against CORE-04.

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

`Finding` is what `detect` prints, one per line (technique, location, size — matching `docs/user-interface.md`'s output spec). `Result` is what `hide`/`clear` return on success; it does not carry a hash or timestamp itself — `internal/custody` already hashes and timestamps every `WriteAt` automatically at the `Image` layer. The command assembles the actual custody-log line from `Result` (semantic context: which technique, which target, what happened) plus the hash/timestamp the custody decorator already captured, so neither layer duplicates the other's job.

Commands depend only on these interfaces, never on a filesystem-specific technique type — because `Hide`/`Detect`/`Clear` only ever touch the capability interface (`NamedStreamCapable`, `SlackSpaceCapable`, `TimestompCapable`), the technique logic itself is filesystem-agnostic. There is one concrete implementation per technique kind — `namedStreamTechnique`, `slackSpaceTechnique`, `timestompTechnique` — not one per filesystem, and each lives in `internal/technique` and works against any `Entry` that satisfies the capability it needs.

### Technique selection and the `features` matrix

`cmd_hide.go`/`cmd_detect.go`/`cmd_clear.go` select *which* technique kind purely from the `--technique` flag string (`named-stream` → `namedStreamTechnique`, etc.) — a small switch or map in `internal/technique`, no filesystem involved:

```go
func Get(name string) (Technique, error) // "named-stream" | "slack-space" | "timestomp"
```

*Whether* the selected technique works against the given target is answered separately, at call time, by the type-assertion already described above (`Entry` implements the required capability or it doesn't). So filesystem support is never encoded as a lookup table keyed by `(FileSystem type, technique)` — it falls out of whether a given `ntfs.Entry`/`apfs.Entry`/`ext4.Entry` happens to implement `NamedStreamCapable`/`SlackSpaceCapable`/`TimestompCapable`.

`nemo features` still needs that support matrix without an image loaded, so it can't just run the type assertion against a live `Entry`. Instead, each filesystem package self-declares its supported technique names once, alongside its constructor in `registry.go`:

```go
type Detector struct {
    Type         Type
    Sniff        func([]byte) bool
    New          func(image.Image) (FileSystem, error)
    Techniques   []string // e.g. []string{"named-stream", "slack-space"} for ext4 today
}
```

`features` reads `Techniques` off every registered `Detector` and prints the matrix directly — no image, no reflection, and it can't drift from `registry.go` because it's reading the same struct `Open`/detection uses. When ext4 gains slack-space support, that's a one-line change to its `Detector.Techniques`, not a new lookup table to keep in sync.

### `internal/tskcheck`

Isolated in its own package specifically because it's the only place cgo is required (libtsk binding). Nothing else in the tree may import it, so the rest of the codebase stays buildable with `CGO_ENABLED=0`. It's consumed only by `detect`, as an optional read-only cross-check, behind an interface (`Validator.CrossCheck(image.Image) ([]Finding, error)`) so `detect` doesn't hard-depend on cgo being available.

## Why this shape

- **Independent ownership**: one person can build `internal/filesystem/apfs` while another builds `internal/filesystem/ext4` — both only need to satisfy `FileSystem`/`Entry`, so neither blocks the other and neither touches shared code.
- **Incremental shipping**: a filesystem can land with only `NamedStreamCapable` implemented; `SlackSpaceCapable`/`TimestompCapable` are separate optional interfaces, so partial support isn't a partial/broken implementation of one big interface — it's just not implementing the pieces not yet built.
- **New filesystem = new package + one registry line**: no changes to `internal/technique`, `cmd_hide.go`, etc.
- **Custody logging is structural, not a convention**: because it's a decorator over `Image`, a technique can't accidentally skip it — every `WriteAt` on the wrapped image is hashed and logged regardless of which technique or filesystem is calling it.

## Non-goals

This mirrors `docs/overall-plan.md`'s scope: no plugin loading at runtime (dynamic `.so`/`.dll` loading), no config-driven technique selection beyond the `--technique` flag. "Pluggable" here means new Go packages behind existing interfaces, compiled in — not a runtime plugin system.
