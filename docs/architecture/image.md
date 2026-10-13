---
title: Image and Custody
parent: Architecture
nav_order: 1
---

# Image and Custody

### `internal/image.Image`

```go
type Image interface {
    ReadAt(p []byte, off int64) (int, error)
    WriteAt(p []byte, off int64) (int, error)
    Size() int64
    Path() string
}
```

[`rawimage.go`]({{ site.src }}/internal/image/rawimage.go) implements this over `os.File`, with [`Open`]({{ site.src }}/internal/image/rawimage.go#L20) for read-write and [`OpenReadOnly`]({{ site.src }}/internal/image/rawimage.go#L26) for `detect`. [`readonly.go`]({{ site.src }}/internal/image/readonly.go) adds a [`ReadOnly`]({{ site.src }}/internal/image/readonly.go#L11) wrapper whose [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) always returns [`ErrReadOnly`]({{ site.src }}/internal/image/readonly.go#L7); `detect` wraps its image in that as a second guard. Live mode, once built, will use the same interface backed by a single target file, so filesystem and technique code never branches on mode. See [Modes](modes.html).

### `internal/custody`

Every write goes through `internal/custody`. [`custody.Wrap(img)`]({{ site.src }}/internal/custody/custody.go#L35) returns a [`Recorder`]({{ site.src }}/internal/custody/custody.go#L18) (the [`Image`]({{ site.src }}/internal/image/image.go#L4) interface plus [`EventsSnapshot`]({{ site.src }}/internal/custody/custody.go#L20)): it wraps a real [`Image`]({{ site.src }}/internal/image/image.go#L4), SHA-256-hashes and records each [`WriteAt`]({{ site.src }}/internal/image/image.go#L6), then delegates. Callers wrap once and pass the result everywhere; nothing downstream needs to know custody recording exists. [`log.go`]({{ site.src }}/internal/custody/log.go) turns a recorded event plus the command's semantic context into a [`Record`]({{ site.src }}/internal/custody/log.go#L15) and appends it to the custody log.

This is what makes custody recording structural rather than a convention a technique could forget to follow: because it is a decorator over [`Image`]({{ site.src }}/internal/image/image.go#L4), every [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) on the wrapped image is hashed and recorded regardless of the caller, with no flag to skip it. 

### What the user sees

Every successful `hide` and `clear` writes an entry to the chain-of-custody log automatically, in both modes, with no flag to disable it. `detect` is read-only: it never writes to the target and never touches the custody log.

On success, `hide` and `clear` emit their custody record as one JSON object on standard output, including the selected technique, technique-specific detail, and affected byte count. If the output sink fails after the filesystem mutation, the command reports the failure but does not imply that the mutation was rolled back.
