---
title: features
parent: CLI Reference
nav_order: 1
---

# `nemo features`

Prints the feature-set matrix: which filesystems (`ntfs`, `apfs`, `ext4`) support which techniques (`named-stream`, `slack-space`, `timestomp`). One row per filesystem/technique pair, with a supported/unsupported indicator.

```
nemo features
nemo features --mode image
nemo features --mode live
```

Useful for checking capabilities before running `hide` against a given image, without consulting the docs. The matrix is built from the same `internal/filesystem` and `internal/technique` registrations used at runtime, so it can't drift from actual behavior. See [Filesystem Interfaces](../architecture/filesystem.html#technique-selection-and-the-features-matrix) for how it's assembled, and [Techniques](../techniques/#support-matrix-current) for the current matrix.

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
