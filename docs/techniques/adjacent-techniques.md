---
title: Adjacent techniques
parent: How techniques work
nav_order: 4
---

# Adjacent techniques nemo does not do

These anti-forensic techniques sit next to the three nemo implements, but each
is out of scope. Recognise them so you do not reach for one by accident, and so
you know where nemo's guarantees stop. The short version of scope is on
[Home](../#scope).

**Unallocated-space hiding and file carving.** Writing a payload into blocks the
filesystem currently marks free, or recovering files from there. The next
allocation overwrites it, and recovering it is a data-recovery problem rather
than a filesystem-parsing one. nemo only touches space the filesystem considers
allocated.

**Reserved metadata regions.** NTFS keeps about 16 MFT records reserved
(`$MFT` through `$Extend`), and the `$Boot` file's tail is padding. ext4
reserves inodes 1 through 10 and pads its group descriptors. Data parked there
survives normal use, but any `fsck` or `chkdsk` flags or clobbers it, and
writing there risks corrupting the volume.

**`$BadClus` and bad-block-list marking.** Telling NTFS (`$BadClus`) or ext4 (the
bad-block inode) that a good cluster is bad so the filesystem routes around it,
then reading it directly. Every modern forensic suite checks for this first.

**Deleted-inode and deleted-MFT-record reuse.** Writing into the record of a
just-deleted file before the filesystem reuses it. Racy and short-lived.

**Steganography inside file formats.** Hiding a payload in the unused bits of a
JPEG, PNG, or PDF. This has no filesystem component and is a large field of its
own.

**Sparse-file holes.** A sparse file's unwritten ranges read as zeros and occupy
no blocks, so there is nothing to hide in, only a size-versus-allocation
mismatch to notice.

**Snapshot and VSS stashing.** Putting data in a Volume Shadow Copy or an APFS
snapshot and deleting the live copy. This is "hide a normal file where most tools
do not look," not a filesystem-structure technique, and it depends on snapshot
retention you do not control.
