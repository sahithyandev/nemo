---
title: Filesystem Interfaces
parent: Architecture
nav_order: 2
---

# Filesystem Interfaces

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

## Technique selection and the `features` matrix

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

`features` reads `Techniques` off every registered `Detector` and prints the matrix: no image, no reflection, and no way to drift from `registry.go` because it reads the same struct detection uses. When ext4 gains slack-space support, that is a one-line change to its `Detector.Techniques`. See [Techniques](../techniques/#support-matrix-current) for the matrix as it stands.
