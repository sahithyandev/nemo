---
title: Filesystem Interfaces
parent: Architecture
nav_order: 2
---

# Filesystem interfaces

## `FileSystem` and `Entry`

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

### How commands use them

- `detect` with no target calls [`Root()`]({{ site.src }}/internal/filesystem/filesystem.go#L26) and walks [`Children()`]({{ site.src }}/internal/filesystem/filesystem.go#L34) recursively, like `io/fs.WalkDir`. It runs the technique's [`Detect`]({{ site.src }}/internal/technique/technique.go#L95) on every [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) it reaches.
- `hide` and `clear` always take an explicit target. They call [`Open(path)`]({{ site.src }}/internal/filesystem/registry.go#L62) and never walk.

[`Children()`]({{ site.src }}/internal/filesystem/filesystem.go#L34) is what makes the targetless `detect` possible.

### The test double

`internal/filesystem/fakefs` is an in-memory [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24) and [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31). It implements all three capabilities and includes an in-memory [`Image`]({{ site.src }}/internal/image/image.go#L4). The command and technique tests run against it.

## The registry

[`registry.go`]({{ site.src }}/internal/filesystem/registry.go) holds a signature-based lookup. It is a `[]Detector`, where each detector pairs a byte-pattern sniff with a constructor. It returns the right [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24) for an image.

To add a filesystem, create an `internal/filesystem/<fs>/` package and register a [`Detector`]({{ site.src }}/internal/filesystem/registry.go#L14) from its `init`. Nothing in `internal/technique` or the commands changes.

[`Register`]({{ site.src }}/internal/filesystem/registry.go#L31) panics on:

- an invalid [`Detector`]({{ site.src }}/internal/filesystem/registry.go#L14) (empty [`Type`]({{ site.src }}/internal/filesystem/filesystem.go#L14), nil `Sniff`, or nil `New`)
- a [`Type`]({{ site.src }}/internal/filesystem/filesystem.go#L14) that is already registered

Registration happens at package init, so these are programmer errors at startup. They are not conditions to recover from.

[`Open`]({{ site.src }}/internal/filesystem/registry.go#L62) reads up to the first 4096 bytes and tries every registered detector. It does not stop at the first hit.

- No match is an error that names the registered filesystems.
- More than one match is an "ambiguous image format" error that names every candidate.

## Capability interfaces

Filesystems differ in what they support. Rather than force every [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) to satisfy one bloated interface, capabilities are split into optional interfaces that an [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) may also implement:

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

`internal/technique` checks whether the [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) implements the capability it needs. If not, it returns an "unsupported on this filesystem" error.

ext4 image entries implement all three. A capability can still return [`ErrUnsupported`]({{ site.src }}/internal/filesystem/filesystem.go#L11) for an entry type it cannot handle, such as slack space on a directory. Default scans skip that technique for the entry and keep walking.

## Technique selection and the `features` matrix

Commands choose a technique only from the `--technique` string. A switch in `internal/technique` does the mapping, and no filesystem is involved:

```go
func Get(name string) (Technique, error) // "named-stream" | "slack-space" | "timestomp"
```

Whether that technique works on the target is answered later, at call time, by the capability check. The [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) either implements the capability or it does not.

So there is no lookup table keyed by `(FileSystem type, technique)`. Support follows from whether [`ntfs.Entry`]({{ site.src }}/internal/filesystem/ntfs/ntfs.go#L92), [`apfs.Entry`]({{ site.src }}/internal/filesystem/apfs/apfs.go#L731), or [`ext4.Entry`]({{ site.src }}/internal/filesystem/ext4/ext4.go#L235) implements the capability.

### How `features` works

`nemo features` needs the matrix without an image loaded. It cannot run the capability check on a live [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31). So each filesystem package declares its supported technique names once, in the [`Detector`]({{ site.src }}/internal/filesystem/registry.go#L14) it registers:

```go
type Detector struct {
    Type       Type
    Sniff      func([]byte) bool
    New        func(image.Image) (FileSystem, error)
    Techniques []string // ext4: []string{"named-stream", "slack-space", "timestomp"}
}
```

`features` reads `Techniques` from every registered [`Detector`]({{ site.src }}/internal/filesystem/registry.go#L14) and prints the matrix. It needs no image and no reflection. It cannot drift from [`registry.go`]({{ site.src }}/internal/filesystem/registry.go), because it reads the same struct detection uses.

Filesystem implementations must keep their declared techniques consistent with their capabilities. See [features](../cli/features.html#support-matrix) for the matrix.
