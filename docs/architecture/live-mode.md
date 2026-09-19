---
title: Live Mode
parent: Architecture
nav_order: 4
---

# Live mode vs. image mode

APFS live mode is built on macOS (`internal/filesystem/apfs/live_darwin.go`); every other
combination of filesystem and OS is still design intent. See
[APFS](../file-systems/apfs.html#live-mode-macos) for what it supports and its raw-device
limitation.

Image mode and live mode will not share one code path per filesystem. They split by technique, not by a mode flag on a single `Entry`:

- Named-stream and timestomp never need volume-level parsing. An NTFS ADS is `os.OpenFile("target:stream")`, an ext4/APFS xattr is a syscall, timestomping is a `SetFileTime` or `utimes`-family call. None of that touches an MFT, a B-tree, or an inode table.
- Slack-space is inherently volume-level. The unused tail bytes of an allocated cluster or block are not exposed by any normal file API on any OS. Doing it live means opening the raw block device (`\\.\PhysicalDriveN`, `/dev/diskN`) and running the same parser image mode uses, which needs admin or root, unlike the other two techniques. This is in scope (per [Overview](../overview.html)), so `hide`/`clear`/`detect` with `--technique slack-space` and no `--image` must detect a missing-privilege condition and fail with a clear error rather than silently degrading. See [Slack-Space Hiding](../techniques/slack-space.html#privilege).

Each filesystem package will ship two `Entry` implementations, not two modes bolted onto one:

- image-mode `Entry`: backed by a parsed `Image` (a disk-image file or an opened raw device), implements all three capability interfaces, including `SlackSpaceCapable`.
- live-mode `Entry`: backed by a single OS path, implements `NamedStreamCapable` and `TimestompCapable` via direct syscalls, and implements `SlackSpaceCapable` only when constructed against an opened raw device (running elevated).

`registry.go`'s signature-based `Sniff` detection only applies to image mode; it identifies a filesystem from bytes. Live mode never sniffs. "Which filesystem" is just "which OS this binary is running on," so live mode picks its `Entry` implementation via a Go build-tag file per package (`ntfs/live_windows.go`, `apfs/live_darwin.go`, `ext4/live_linux.go`), each an "unsupported on this OS" stub on the other two platforms so the binary still builds and runs cross-platform. `apfs/live_darwin.go` is the one built so far; `apfs/live_unsupported.go` (`//go:build !darwin`) covers the rest, and `ntfs`/`ext4` have no live file yet at all, so `cmd`'s live-mode router only ever reaches into `apfs`.
