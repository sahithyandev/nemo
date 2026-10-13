---
title: features
parent: CLI Reference
nav_order: 1
---

# `nemo features`

Prints the feature-set matrix: which filesystems (`ntfs`, `apfs`, `ext4`) support which techniques (`named-stream`, `slack-space`, `timestomp`). One row per filesystem/technique pair, with a supported/unsupported indicator.

```sh
nemo features
nemo features --mode image
nemo features --mode live
```

Useful for checking capabilities before running `hide` against a given image, without consulting the docs. The matrix is built from the same `internal/filesystem` and `internal/technique` registrations used at runtime, so it can't drift from actual behavior. See [Filesystem Interfaces](../architecture/filesystem.html#technique-selection-and-the-features-matrix) for how it's assembled,.

The default and `--mode image` report registered image techniques. `--mode live`
reports native capabilities on the current operating system, with a `LIMITS`
column distinguishing partial timestamp support, read-only raw-device access,
and unavailable backends. It does not open a target or check its permissions.

On Linux, live named streams use `user.*` xattrs on verified ext4 mounts.
Timestamp writes support access/modification time; creation/change time writes
remain unavailable. Explicit live slack detection/extraction is read-only and
requires access to the verified backing block device. Mounted slack writes are
refused. A `supported` row still depends on the target's format, file layout and
permissions.

## Support matrix

This table covers image-mode capabilities; filesystem layout restrictions apply.
See [Modes](../architecture/modes.html) for native-platform limits.

| Filesystem | named-stream | slack-space | timestomp |
| ---------- | ------------ | ----------- | --------- |
| ext4       | `internal/filesystem/ext4/ext4.go` and `xattr.go` | bounded image layouts | `internal/filesystem/ext4/timestomp.go` |
| APFS       | `internal/filesystem/apfs/namedstream.go` (in-place B-tree leaf rewrite, no allocation) | bounded image layouts | yes |
| NTFS       | `internal/filesystem/ntfs/namedstream.go` (resident and non-resident, MFT-mirror aware) | bounded image layouts | yes |
| fakefs     | yes | yes | yes (test double, `internal/filesystem/fakefs`) |

`fakefs` is an in-memory filesystem that implements all three capabilities
against a byte-slice image. It is how `internal/technique` gets tested without a
real NTFS, APFS, or ext4 image. A real filesystem's [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) implements the same
interfaces [`fakefs.Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/fakefs/fakefs.go#L18) already does.
