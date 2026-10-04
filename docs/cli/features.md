---
title: features
parent: CLI Reference
nav_order: 1
---

# `nemo features`

Prints the feature-set matrix: which filesystems (`ntfs`, `apfs`, `ext4`) support which techniques (`named-stream`, `slack-space`, `timestomp`). One row per filesystem/technique pair, with a supported/unsupported indicator.

```
nemo features
```

Useful for checking capabilities before running `hide` against a given image, without consulting the docs. The matrix is built from the same `internal/filesystem` and `internal/technique` registrations used at runtime, so it can't drift from actual behavior. See [Filesystem Interfaces](../architecture/filesystem.html#technique-selection-and-the-features-matrix) for how it's assembled, and [Techniques](../techniques/#support-matrix-current) for the current matrix.
