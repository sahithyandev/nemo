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

`rawimage.go` implements this over `os.File`, with `Open` for read-write and `OpenReadOnly` for `detect`. `readonly.go` adds a `ReadOnly` wrapper whose `WriteAt` always returns `ErrReadOnly`; `detect` wraps its image in that as a second guard. Live mode, once built, will use the same interface backed by a single target file, so filesystem and technique code never branches on mode. See [Live Mode](live-mode.html).

### `internal/custody`

Every write goes through `internal/custody`. `custody.Wrap(img)` returns a `Recorder` (the `Image` interface plus `EventsSnapshot`): it wraps a real `Image`, SHA-256-hashes and records each `WriteAt`, then delegates. Callers wrap once and pass the result everywhere; nothing downstream needs to know custody recording exists. `log.go` turns a recorded event plus the command's semantic context into a `Record` and appends it to the custody log.

This is what makes custody recording structural rather than a convention a technique could forget to follow: because it is a decorator over `Image`, every `WriteAt` on the wrapped image is hashed and recorded regardless of the caller, with no flag to skip it. See [CLI Reference](../cli/#custody-logging) for the user-visible effect.
