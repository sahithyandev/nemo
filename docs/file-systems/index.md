---
title: File Systems
nav_order: 5
has_children: true
---

# File Systems

Per-filesystem parser docs: detection, mounting, on-disk structures, and the in-place write path each `internal/filesystem/<fs>` package implements. For the contract every parser satisfies, see [Filesystem Interfaces](../architecture/filesystem.html); for what each capability means at the technique level, see [Techniques](../techniques/).

- [NTFS](ntfs.html) covers `internal/filesystem/ntfs`.
- [APFS](apfs.html) covers `internal/filesystem/apfs`.
- [ext4](ext4.html) covers `internal/filesystem/ext4`.

## Status

| Filesystem | detection | traversal | named streams | slack space | timestomp |
| ---------- | --------- | --------- | -------------- | ----------- | --------- |
| NTFS | done | done | done, resident and non-resident | not built | not built |
| APFS | done | done | done, embedded and stream-backed | not built | not built |
| ext4 | done | done | done, in-inode and external block | not built | done |

Each parser refuses any on-disk layout it doesn't fully understand with a clear error, rather than guessing. See each page's Limitations section for the specific refusals.
