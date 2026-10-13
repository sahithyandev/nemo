---
title: Modes
parent: Architecture
nav_order: 4
---

# Modes

Nemo runs in one of two modes, chosen per command:

- **Image mode** (used when `--image` is given): operates offline against a raw disk image. Filesystem type is detected from the image, not specified by the user.
- **Live mode** (used when `--image` is omitted): operates directly on a file on the local, running machine, using the OS's native filesystem calls. This is the mode an everyday user reaches for to hide a file on their own machine.

Until a native filesystem implementation or image detector is registered, the corresponding mode fails with a clear unsupported/unrecognized error; nemo never falls back from one mode to the other.

## Live mode vs. image mode

APFS live mode runs on macOS (`internal/filesystem/apfs/live_darwin.go`).
Windows NTFS live mode (`internal/filesystem/ntfs/live_windows.go`)
provides read-only volume access using the existing image parser. All techniques
use that volume reader and require administrator access; live writes are refused.
It follows APFS's wrapping and cleanup pattern, while native path-backed NTFS
mutation is not supported. See [NTFS](../file-systems/ntfs.html#live-mode-windows).
Linux ext4 live mode supports user xattrs and accessed/modified timestamps on
verified ext4 mounts; explicit live slack detection/extraction uses verified
read-only block-device access. Mounted slack writes remain refused. See
[APFS](../file-systems/apfs.html#live-mode-macos) for what it supports and its raw-device
limitation, and [Only APFS, nothing else, even on
macOS](../file-systems/apfs.html#only-apfs-nothing-else-even-on-macos) for what happens
when live mode is pointed at a mounted NTFS, ext4, or other non-APFS volume there.

The intended native-operation architecture below is implemented by APFS. NTFS's
current read-only implementation instead shares the image parser for all techniques:

- Named-stream and timestomp never need volume-level parsing. An NTFS ADS is `os.OpenFile("target:stream")`, an ext4/APFS xattr is a syscall, timestomping is a `SetFileTime` or `utimes`-family call. None of that touches an MFT, a B-tree, or an inode table.
- Slack-space is inherently volume-level. The unused tail bytes of an allocated cluster or block are not exposed by any normal file API on any OS. Doing it live means opening the raw block device (`\\.\PhysicalDriveN`, `/dev/diskN`) and running the same parser image mode uses, which needs admin or root, unlike the other two techniques. This is in scope (per [Home](../#scope)), so `hide`/`clear`/`detect` with `--technique slack-space` and no `--image` must detect a missing-privilege condition and fail with a clear error rather than silently degrading. See [Slack-Space Hiding](../techniques/slack-space.html#privilege).

Each filesystem package has two [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) implementations, not two modes bolted onto one:

- image-mode [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31): backed by a parsed [`Image`](https://github.com/sahithyandev/nemo/blob/main/internal/image/image.go#L4) (a disk-image file or an opened raw device), implements all three capability interfaces, including [`SlackSpaceCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L52).
- live-mode [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31): backed by a single OS path, implements [`NamedStreamCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L39) and [`TimestompCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L81) via direct syscalls, and implements [`SlackSpaceCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L52) only when constructed against an opened raw device (running elevated).

[`registry.go`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/registry.go)'s signature-based detection applies to image mode. The live router
selects NTFS on Windows, APFS on macOS, and ext4 on Linux, with build-tagged
unsupported stubs elsewhere. The NTFS live opener additionally validates the volume's boot sector
through `New`; it does not assume every Windows volume is NTFS. The Linux opener checks the mounted filesystem with `fstatfs`, rejects final
symlinks, and binds metadata operations to an opened file descriptor.
