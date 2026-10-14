---
title: features
parent: CLI Reference
nav_order: 1
---

# `nemo features`

Prints a matrix of which filesystems (`ntfs`, `apfs`, `ext4`) support which techniques (`named-stream`, `slack-space`, `timestomp`). Each row is one filesystem and technique pair, marked supported or unsupported.

```sh
nemo features
nemo features --mode image
nemo features --mode live
```

Run it before `hide` to check what a given image can do.

The matrix is built from the same registrations nemo uses at runtime, so it cannot drift from real behavior. [Filesystem interfaces](../architecture/filesystem.html#technique-selection-and-the-features-matrix) explains how it is assembled.

## Modes

- The default and `--mode image` report the techniques registered for image mode.
- `--mode live` reports what the current operating system supports natively. A `LIMITS` column marks partial timestamp support, read-only raw-device access, and unavailable backends. It does not open a target or check its permissions.

A `supported` row still depends on the target's format, file layout, and permissions.

## Live mode on Linux

- Named streams use `user.*` xattrs on verified ext4 mounts.
- Timestamp writes cover access and modification time. Creation and change time writes are not available.
- Slack detection and extraction are read-only. They need access to the verified backing block device.
- Slack writes to a mounted volume are refused.

## Support matrix

This table covers image mode. Filesystem layout restrictions still apply. For native platform limits, see [Modes](../modes.html).

| Filesystem | named-stream | slack-space | timestomp |
| ---------- | ------------ | ----------- | --------- |
| ext4       | `internal/filesystem/ext4/ext4.go` and `xattr.go` | bounded image layouts | `internal/filesystem/ext4/timestomp.go` |
| APFS       | `internal/filesystem/apfs/namedstream.go` (in-place B-tree leaf rewrite, no allocation) | bounded image layouts | yes |
| NTFS       | `internal/filesystem/ntfs/namedstream.go` (resident and non-resident, MFT-mirror aware) | bounded image layouts | yes |
| fakefs     | yes | yes | yes (test double, `internal/filesystem/fakefs`) |

`fakefs` is an in-memory filesystem that implements all three capabilities on a byte-slice image. It lets `internal/technique` be tested without a real NTFS, APFS, or ext4 image.

A real filesystem's [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) implements the same interfaces that [`fakefs.Entry`]({{ site.src }}/internal/filesystem/fakefs/fakefs.go#L18) already does.
