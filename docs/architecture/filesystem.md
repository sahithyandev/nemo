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

[`Children()`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L34) backs `detect` with no target: the command calls [`Root()`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L26) then walks [`Children()`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L34) recursively (the shape of `io/fs.WalkDir`), running the technique's [`Detect`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L95) against every [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) it reaches. `hide`/`clear` always take an explicit target, so they only call [`Open(path)`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go#L62) and never walk. `internal/filesystem/fakefs` is an in-memory [`FileSystem`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L24)/[`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) implementing all three capabilities plus an in-memory [`Image`](https://github.com/sahithyandev/nemo/blob/main/internal/image/image.go#L4); the command and technique tests run against it.

[`registry.go`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go) holds a signature-based lookup (a `[]Detector`, each a byte-pattern sniff plus a constructor) and returns the right [`FileSystem`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L24) for an image. Adding a filesystem means registering a [`Detector`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go#L14) (from that package's `init`) plus a new `internal/filesystem/<fs>/` package; nothing in `internal/technique` or the commands changes.

[`Register`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go#L31) panics on an invalid [`Detector`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go#L14) (empty [`Type`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L14), nil `Sniff`, or nil `New`) or on a [`Type`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L14) already registered. Registration happens at package init, so these are startup-time programmer errors, not conditions to recover from. [`Open`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go#L62) reads up to the first 4096 bytes and sniffs every registered detector rather than stopping at the first hit: no match is an error naming the registered filesystems, more than one match is an "ambiguous image format" error naming every candidate.

Not every filesystem supports every capability the same way, so rather than force all three [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) implementations to satisfy a bloated interface, capabilities are split into optional interfaces an [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) may also implement:

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
    Timestamp(field TimeField) (time.Time, error)
    SupportsTimestamp(field TimeField) (bool, error)
    SetTimestamp(field TimeField, t time.Time) error
}
```

`internal/technique` type-asserts the [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) it is given against the capability it needs and returns an "unsupported on this filesystem" error if the assertion fails. ext4 image entries implement all three capabilities. A capability can still return [`ErrUnsupported`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L11) for an entry type it cannot handle, such as slack space on a directory; default scans skip that technique for the entry and continue walking.

## Technique selection and the `features` matrix

The commands pick which technique kind purely from the `--technique` flag string, through a switch in `internal/technique`, no filesystem involved:

```go
func Get(name string) (Technique, error) // "named-stream" | "slack-space" | "timestomp"
```

Whether the selected technique works against the given target is answered separately, at call time, by the type assertion above: the [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) implements the required capability or it does not. So filesystem support is never a lookup table keyed by `(FileSystem type, technique)`; it falls out of whether a given [`ntfs.Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/ntfs/ntfs.go#L92)/[`apfs.Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/apfs/apfs.go#L731)/[`ext4.Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/ext4/ext4.go#L235) implements the capability.

`nemo features` needs that matrix without an image loaded, so it cannot run the type assertion against a live [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31). Instead each filesystem package declares its supported technique names once, in the [`Detector`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go#L14) it registers:

```go
type Detector struct {
    Type       Type
    Sniff      func([]byte) bool
    New        func(image.Image) (FileSystem, error)
    Techniques []string // ext4: []string{"named-stream", "slack-space", "timestomp"}
}
```

`features` reads `Techniques` off every registered [`Detector`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go#L14) and prints the matrix: no image, no reflection, and no way to drift from [`registry.go`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go) because it reads the same struct detection uses. Filesystem implementations must keep their declared techniques consistent with their capabilities. See [features](../cli/features.html#support-matrix) for the matrix.
