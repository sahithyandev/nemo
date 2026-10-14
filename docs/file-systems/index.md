---
title: File Systems
nav_group: "Contributing"
nav_order: 2
has_children: true
---

# File systems

These pages document each filesystem parser: detection, mounting, on-disk structures, and the in-place write path. Each one covers an `internal/filesystem/<fs>` package.

- For the contract every parser satisfies, see [Filesystem interfaces](../architecture/filesystem.html).
- For what each capability means at the technique level, see [Techniques](../techniques/).

## Parsers

- [NTFS](ntfs.html): `internal/filesystem/ntfs`
- [APFS](apfs.html): `internal/filesystem/apfs`
- [ext4](ext4.html): `internal/filesystem/ext4`

## Support

| Filesystem | detection | traversal | named streams | slack space | timestomp |
| ---------- | --------- | --------- | -------------- | ----------- | --------- |
| NTFS | yes | yes | resident and non-resident | yes | yes |
| APFS | yes | yes | embedded and stream-backed | yes | yes |
| ext4 | yes | yes | in-inode and external block | yes | yes |

Each parser refuses any on-disk layout it does not fully understand, with a clear error. It never guesses. The Limitations section on each page lists the specific refusals.
