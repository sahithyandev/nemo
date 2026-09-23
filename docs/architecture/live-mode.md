---
title: Live Mode
parent: Architecture
nav_order: 4
---

# Live mode vs. image mode

APFS live mode is built on macOS (`internal/filesystem/apfs/live_darwin.go`).
Windows NTFS live mode (`internal/filesystem/ntfs/live_windows.go`) currently
provides read-only volume access using the existing image parser. All techniques
use that volume reader and require administrator access; live writes are refused.
It follows APFS's wrapping and cleanup pattern, while native path-backed NTFS
mutation remains future work. See [NTFS](../file-systems/ntfs.html#live-mode-windows).
Other combinations remain design intent. See
[APFS](../file-systems/apfs.html#live-mode-macos) for what it supports and its raw-device
limitation, and [Only APFS, nothing else, even on
macOS](../file-systems/apfs.html#only-apfs-nothing-else-even-on-macos) for what happens
when live mode is pointed at a mounted NTFS, ext4, or other non-APFS volume there.

The intended native-operation architecture below is implemented by APFS. NTFS's
current read-only implementation instead shares the image parser for all techniques:

- Named-stream and timestomp never need volume-level parsing. An NTFS ADS is `os.OpenFile("target:stream")`, an ext4/APFS xattr is a syscall, timestomping is a `SetFileTime` or `utimes`-family call. None of that touches an MFT, a B-tree, or an inode table.
- Slack-space is inherently volume-level. The unused tail bytes of an allocated cluster or block are not exposed by any normal file API on any OS. Doing it live means opening the raw block device (`\\.\PhysicalDriveN`, `/dev/diskN`) and running the same parser image mode uses, which needs admin or root, unlike the other two techniques. This is in scope (per [Overview](../overview.html)), so `hide`/`clear`/`detect` with `--technique slack-space` and no `--image` must detect a missing-privilege condition and fail with a clear error rather than silently degrading. See [Slack-Space Hiding](../techniques/slack-space.html#privilege).

Each filesystem package will ship two `Entry` implementations, not two modes bolted onto one:

- image-mode `Entry`: backed by a parsed `Image` (a disk-image file or an opened raw device), implements all three capability interfaces, including `SlackSpaceCapable`.
- live-mode `Entry`: backed by a single OS path, implements `NamedStreamCapable` and `TimestompCapable` via direct syscalls, and implements `SlackSpaceCapable` only when constructed against an opened raw device (running elevated).

`registry.go`'s signature-based detection applies to image mode. The live router
selects NTFS on Windows and APFS on macOS, with build-tagged unsupported stubs
elsewhere. The NTFS live opener additionally validates the volume's boot sector
through `New`; it does not assume every Windows volume is NTFS. ext4 live mode
remains unimplemented.
