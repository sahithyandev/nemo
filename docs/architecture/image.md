---
title: Image and Custody
parent: Architecture
nav_order: 1
---

# Image and custody

## `internal/image.Image`

```go
type Image interface {
    ReadAt(p []byte, off int64) (int, error)
    WriteAt(p []byte, off int64) (int, error)
    Size() int64
    Path() string
}
```

[`rawimage.go`]({{ site.src }}/internal/image/rawimage.go) implements this over `os.File`. [`Open`]({{ site.src }}/internal/image/rawimage.go#L20) opens read-write. [`OpenReadOnly`]({{ site.src }}/internal/image/rawimage.go#L26) is for `detect`.

[`readonly.go`]({{ site.src }}/internal/image/readonly.go) adds a [`ReadOnly`]({{ site.src }}/internal/image/readonly.go#L11) wrapper. Its [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) always returns [`ErrReadOnly`]({{ site.src }}/internal/image/readonly.go#L7). `detect` wraps its image in that as a second guard.

Live mode, once built, will use the same interface backed by a single target file. Filesystem and technique code never branches on mode. See [Modes](modes.html).

## `internal/custody`

Every write goes through `internal/custody`.

[`custody.Wrap(img)`]({{ site.src }}/internal/custody/custody.go#L35) returns a [`Recorder`]({{ site.src }}/internal/custody/custody.go#L18). That is the [`Image`]({{ site.src }}/internal/image/image.go#L4) interface plus [`EventsSnapshot`]({{ site.src }}/internal/custody/custody.go#L20). It wraps a real [`Image`]({{ site.src }}/internal/image/image.go#L4), SHA-256-hashes and records each [`WriteAt`]({{ site.src }}/internal/image/image.go#L6), then passes it on.

Callers wrap once and pass the result everywhere. Nothing downstream needs to know custody exists.

[`log.go`]({{ site.src }}/internal/custody/log.go) combines a recorded event with the command's context into a [`Record`]({{ site.src }}/internal/custody/log.go#L15), then appends it to the custody log.

Because custody is a decorator over [`Image`]({{ site.src }}/internal/image/image.go#L4), recording is structural. Every [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) on the wrapped image is hashed and recorded, whoever calls it. There is no flag to skip it.

## What the user sees

Every successful `hide` and `clear` writes an entry to the chain-of-custody log, in both modes. There is no flag to turn it off. `detect` is read-only. It never writes to the target and never touches the log.

On success, `hide` and `clear` also print their custody record as one JSON object on standard output. It includes the technique, technique-specific detail, and the affected byte count.

If the output fails after the filesystem change, the command reports the failure. It does not claim the change was rolled back.
