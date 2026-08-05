# Nemo: Architecture

## Goal

Adding a filesystem (e.g. FAT32 later) or a technique should mean writing a new package that satisfies existing interfaces, not touching code that already works. This lets contributors own a filesystem or technique independently and ship it incrementally, without stepping on each other or on `cmd_hide.go`/`cmd_detect.go`/`cmd_clear.go`.

## File structure

```
nemo/
  cmd/
    main.go
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
        namedstream.go     // ADS
        slack.go
        timestomp.go
      apfs/
        apfs.go
        btree.go
        namedstream.go     // xattr + resource fork
        slack.go
        timestomp.go
      ext4/
        ext4.go
        inode.go
        namedstream.go     // xattr
        slack.go
        timestomp.go
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
    NamedStreams() ([]string, error)
    // slack-space and timestomp access are exposed via optional
    // interfaces (below), not required on every Entry.
}
```

`registry.go` holds a signature-based lookup (`[]Detector` — byte pattern → constructor) and returns the right `FileSystem` for an image or file. Adding NTFS/APFS/ext4 support means registering a detector in `registry.go` plus a new `internal/filesystem/<fs>/` package; nothing in `internal/technique` or the Cobra commands changes.

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
type NamedStreamTechnique interface {
    Hide(e filesystem.NamedStreamCapable, streamName string, data []byte) (Result, error)
    Detect(e filesystem.NamedStreamCapable) ([]Finding, error)
    Clear(e filesystem.NamedStreamCapable, streamName string) (Result, error)
}
// SlashSpaceTechnique, TimestompTechnique mirror this shape over their
// respective capability interfaces.
```

`Finding` and `Result` are shared, technique-agnostic value types (used for `detect` output and `hide`/`clear` custody-log entries). Commands depend only on these interfaces, never on `ntfs.NamedStreamTechnique` etc. directly — the concrete technique is selected at runtime from the `FileSystem`'s type plus the `--technique` flag.

### `internal/tskcheck`

Isolated in its own package specifically because it's the only place cgo is required (libtsk binding). Nothing else in the tree may import it, so the rest of the codebase stays buildable with `CGO_ENABLED=0`. It's consumed only by `detect`, as an optional read-only cross-check, behind an interface (`Validator.CrossCheck(image.Image) ([]Finding, error)`) so `detect` doesn't hard-depend on cgo being available.

## Why this shape

- **Independent ownership**: one person can build `internal/filesystem/apfs` while another builds `internal/filesystem/ext4` — both only need to satisfy `FileSystem`/`Entry`, so neither blocks the other and neither touches shared code.
- **Incremental shipping**: a filesystem can land with only `NamedStreamCapable` implemented; `SlackSpaceCapable`/`TimestompCapable` are separate optional interfaces, so partial support isn't a partial/broken implementation of one big interface — it's just not implementing the pieces not yet built.
- **New filesystem = new package + one registry line**: no changes to `internal/technique`, `cmd_hide.go`, etc.
- **Custody logging is structural, not a convention**: because it's a decorator over `Image`, a technique can't accidentally skip it — every `WriteAt` on the wrapped image is hashed and logged regardless of which technique or filesystem is calling it.

## Non-goals

This mirrors `docs/overall-plan.md`'s scope: no plugin loading at runtime (dynamic `.so`/`.dll` loading), no config-driven technique selection beyond the `--technique` flag. "Pluggable" here means new Go packages behind existing interfaces, compiled in — not a runtime plugin system.
