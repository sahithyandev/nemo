---
title: Adjacent techniques
parent: How techniques work
nav_order: 4
---

# Adjacent techniques nemo does not do

These techniques sit next to the three nemo implements, but each is out of scope. Knowing them helps you avoid reaching for one by accident, and shows where nemo's guarantees stop. The short scope summary is on [Home](../#scope).

**Unallocated-space hiding and file carving.** Writing a payload into blocks the filesystem marks free, or recovering files from there. The next allocation overwrites the payload. Recovery is a data-recovery problem, not a filesystem-parsing one. Nemo only touches space the filesystem considers allocated.

**Reserved metadata regions.** NTFS keeps about 16 MFT records reserved (`$MFT` through `$Extend`), and the tail of `$Boot` is padding. ext4 reserves inodes 1 through 10 and pads its group descriptors. Data parked there survives normal use. But `fsck` or `chkdsk` will flag or overwrite it, and writing there risks corrupting the volume.

**`$BadClus` and bad-block-list marking.** Telling NTFS (`$BadClus`) or ext4 (the bad-block inode) that a good cluster is bad, so the filesystem avoids it, then reading it directly. Every modern forensic suite checks for this first.

**Deleted-inode and deleted-MFT-record reuse.** Writing into the record of a just-deleted file before the filesystem reuses it. It is racy and short-lived.

**Steganography inside file formats.** Hiding a payload in the unused bits of a JPEG, PNG, or PDF. It has no filesystem component and is a large field of its own.

**Sparse-file holes.** A sparse file's unwritten ranges read as zeros and take no blocks. There is nothing to hide in. You can only notice a mismatch between size and allocation.

**Snapshot and VSS stashing.** Putting data in a Volume Shadow Copy or an APFS snapshot, then deleting the live copy. This is "hide a normal file where most tools do not look". It is not a filesystem-structure technique, and it relies on snapshot retention you do not control.
