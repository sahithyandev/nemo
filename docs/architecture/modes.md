---
title: Mode implementation
parent: Architecture
nav_order: 4
---

# Mode implementation

For what image and live mode mean to a user, and what each platform supports, see [Modes](../modes.html). This page covers how the split works in the code.

APFS follows the native-operation design described below. NTFS is read-only for now, so it shares the image parser for every technique.

## Why live mode differs per technique

- **Named-stream and timestomp** never need volume-level parsing. An NTFS ADS is `os.OpenFile("target:stream")`. An ext4 or APFS xattr is a syscall. Timestomping is a `SetFileTime` or `utimes`-family call. None of these touch an MFT, a B-tree, or an inode table.
- **Slack-space** is inherently volume-level. No normal file API exposes the unused tail of a cluster or block. Doing it live means opening the raw block device (`\\.\PhysicalDriveN`, `/dev/diskN`) and running the same parser as image mode. That needs admin or root, unlike the other two techniques.

Live slack-space is in scope (see [Home](../#scope)). So `hide`, `clear`, and `detect` with `--technique slack-space` and no `--image` must notice a missing privilege and fail with a clear error. They must not quietly degrade. See [slack-space privilege](../techniques/slack-space.html#privilege).

## Two `Entry` implementations

Each filesystem package has two [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) implementations. They are not two modes bolted onto one.

- **Image-mode `Entry`:** backed by a parsed [`Image`]({{ site.src }}/internal/image/image.go#L4), either a disk-image file or an opened raw device. It implements all three capability interfaces, including [`SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52).
- **Live-mode `Entry`:** backed by a single OS path. It implements [`NamedStreamCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L39) and [`TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81) through direct syscalls. It implements [`SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52) only when built against an opened raw device, which needs elevation.

## Live mode per platform

**macOS (APFS).** The code is in `internal/filesystem/apfs/live_darwin.go`. See [APFS](../file-systems/apfs.html#live-mode-macos) for its raw-device limit. Pointing live mode at a mounted NTFS, ext4, or other non-APFS volume is covered in [Only APFS, nothing else, even on macOS](../file-systems/apfs.html#only-apfs-nothing-else-even-on-macos).

**Windows (NTFS).** The code is in `internal/filesystem/ntfs/live_windows.go`. It gives read-only volume access through the existing image parser.

- Every technique uses that volume reader and needs administrator access.
- Live writes are refused.
- It follows APFS's wrapping and cleanup pattern.
- Native path-backed NTFS changes are not supported.

See [NTFS](../file-systems/ntfs.html#live-mode-windows).

**Linux (ext4).**

- Live mode supports user xattrs and accessed and modified timestamps on verified ext4 mounts.
- Explicit slack detection and extraction use verified read-only block-device access.
- Mounted slack writes are refused.

## Choosing the filesystem

[`registry.go`]({{ site.src }}/internal/filesystem/registry.go) detects the filesystem by signature. That applies to image mode only.

The live router picks by platform: NTFS on Windows, APFS on macOS, ext4 on Linux. Other platforms get build-tagged unsupported stubs.

Each live opener checks that the volume is really its filesystem.

- **NTFS:** validates the boot sector through `New`. It does not assume every Windows volume is NTFS.
- **Linux:** checks the mounted filesystem with `fstatfs`, rejects final symlinks, and binds metadata operations to an opened file descriptor.
