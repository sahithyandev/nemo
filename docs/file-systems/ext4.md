---
title: ext4
parent: File Systems
nav_order: 3
---

# Nemo's ext4 parser

This is about `internal/filesystem/ext4`. It covers detection, superblock
and inode parsing, extent-tree traversal, extended attributes, and
timestomp.

For the on-disk xattr format this parser reads and writes, see
[Named-Stream Hiding](../techniques/named-streams.html#ext4-extended-attributes);
this page covers the parser's own behavior and what it refuses. For where
this package sits in the overall layering, see
[Architecture](../architecture/).

## Detection

`Sniff` (`ext4.go`) checks for the ext4 superblock magic `0xEF53` at byte
`1024 + 0x38`, the fixed superblock location on every ext2/3/4 filesystem.

## Mounting

`New(img)` reads the 1024-byte superblock at offset 1024 and validates it
before anything else touches the image:

- The magic must match, and the block-size shift (`log_block_size`) must be
  0 through 6, giving a 1 KiB to 64 KiB block size.
- Incompatible feature flags (`s_feature_incompat`) are checked against an
  allowlist (`filetype`, `extents`, `64bit`, `flex_bg`, `csum_seed`); any
  other bit set is refused. `extents` must be set, since this parser only
  understands extent-mapped inodes, not the older indirect-block scheme.
- Read-only-compatible `bigalloc` is refused explicitly, since it changes
  the allocation unit away from the raw block size everywhere this parser
  assumes blocks.
- `validateGeometry` cross-checks `inodesCount`, `blocksCount`,
  `blocksPerGroup`, and `inodesPerGroup` for zero values, checks
  `blocksPerGroup` fits the block-bitmap capacity (`blockSize * 8`), checks
  `inodeSize` is at least 128 bytes and a multiple of 4, and checks the
  filesystem's claimed size against the actual image size.
- The group descriptor table's location and size are bounds-checked against
  the image before any group descriptor is read.
- `New` finishes by reading inode 2 (the root) and requiring it to be a
  directory, so a superblock that parses but points at garbage fails
  immediately rather than later, mid-traversal.

`64bit` support means block and inode-table numbers, and the group
descriptor size itself, can carry a high 32-bit half read from an extended
region of the descriptor; the parser reads that half only when the feature
bit is set.

## Inodes (`inode.go`)

`readRawInode` locates an inode's raw bytes: group number and in-group
index from `(inodeNumber - 1) / inodesPerGroup` and `% inodesPerGroup`,
then the group's descriptor (at `gdtOffset + group * descSize`) gives the
inode table's starting block, and from there a direct offset calculation
gives the inode's bytes. `readInode` then decodes just the fields the
parser needs: `mode`, a 64-bit `size` (low 32 bits at offset 4, high 32
bits at offset 108, the standard ext4 large-file split), `flags`, and the
60-byte `i_block` array that holds either the extent tree or (unsupported
here) indirect block pointers.

## Extents (`inode.go`)

`extentBlocks` walks an inode's extent tree, refusing three cases up
front: `inlineData` (the file's content lives in the inode itself, no
blocks to walk), `encrypted` inodes, and any inode without the `extents`
flag set (the older indirect-block layout).

`walkExtentNode` recurses through internal extent-tree nodes (checking the
`0xF30A` extent-header magic, that the node's claimed depth matches what
its parent expected, and a hard cap of depth 5) down to leaf nodes, where
each 12-byte extent record gives a logical block, a length, and a physical
block. A length above `0x8000` marks an unwritten (preallocated but
zero-reading) extent, which this parser treats as unsupported rather than
guessing at its content. Visited internal-node block numbers are tracked so
a cyclic or duplicated child pointer can't loop the walk forever. The
resulting blocks are sorted by logical number and checked for overlap.

Directory traversal (`Children` in `ext4.go`) reads linear and extent-backed
HTree directories. Indexed traversal follows validated root/index pointers,
checks count/limit fields, block bounds, hash ordering and unique references,
then parses only leaf blocks as directory records. Holes, unreferenced blocks
and duplicate names are rejected. Hash versions 0–5 and up to two index levels
are supported; incompatible feature flags remain subject to the mount allowlist.

## Extended attributes (`xattr.go`)

`readXattrRegions` reads the up-to-two places ext4 stores xattrs for one
inode: the in-inode gap starting at `128 + i_extra_isize`, when it opens
with the `0xEA020000` magic, and the external block named by `i_file_acl`
(low 32 bits at inode offset 104, high 16 bits at offset 118 when the inode
is large enough), when that block number is nonzero. The external block's
own header is validated (magic, a nonzero `refcount`, and a block count of
exactly 1) before its entries are trusted.

`parseXattrs` walks the shared entry-list format both regions use: 16-byte
fixed entries (name length, namespace index, value offset, a value-inode
field this parser rejects if nonzero, since ea-inode values need extra
inode lookups it doesn't do, value length, and a per-entry hash used only
in the external-block format) packed from the front, terminated by a
4-byte zero, with values packed from the back and 4-byte aligned. Value
ranges are checked for overlap with each other and with the entry list
before any value bytes are trusted.

**Writing.** `writeXattr` tries four placements in order: update the
attribute in place if the name already exists in either region; otherwise
pack it into the in-inode region alongside what's already there, if it
fits; otherwise take over the in-inode region on its own if that's not
already used; otherwise fall into the external block, if one exists.
An occupied in-inode region is never replaced with only the new attribute
when the combined set does not fit. Existing attributes stay in their
original region; the new attribute goes into an allocated external block
or the write fails. Capacity and shared-block checks finish before any
image write, so these rejected operations leave the image unchanged.
Creating a *new* external block, when none is allocated and no existing
region has room, is refused. `storeXattrRegion` also refuses to mutate an
external block whose `refcount` is above 1, since two inodes share it and
an in-place edit would corrupt the other inode's attributes; that needs
copy-on-write block allocation this parser doesn't do. `deleteXattr`
refuses to remove the last attribute from an external block, since that
would need to free the block, not just its content.

When `metadata_csum` is enabled, both an inode-body write
(`updateInodeChecksum`) and an external-block write recompute and store the
correct checksum, so a mutated inode or block doesn't fail `fsck` on that
account alone.

## Timestomp (`timestomp.go`)

`Entry` implements `filesystem.TimestompCapable`. `SetTimestamp` reads the
whole raw inode, computes the field's byte layout, writes the new value
into that copy, refreshes the inode checksum when `metadata_csum` is
enabled, and writes the complete inode back in one `WriteAt`, so a custody
decorator wrapping the image sees one mutation event covering the whole
inode rather than a sub-inode partial write.

`timestampLayout` maps `modified` and `accessed` to the fixed offsets every
inode has (`i_mtime` at 16, `i_atime` at 8) and `created` to `i_crtime` at
offset 144, available only when the inode's extra area (past the 128-byte
"good old" inode, sized by `i_extra_isize`) reaches that far;
`inodeExtraEnd` computes exactly how far that area extends, validating
`i_extra_isize` itself along the way. A request for `created` on an inode
too small to carry `i_crtime` is a clear, named error rather than silently
writing the wrong field.

`encodeExt4Timestamp`/`decodeExt4Timestamp` implement the split encoding:
32-bit seconds since 1970 in the fixed field, and, when the inode's extra
area also carries the `_extra` field for that timestamp, nanoseconds in the
upper 30 bits plus a 2-bit epoch extension in the low bits that pushes the
representable range past 2038. Without an extra field, sub-second precision
or an out-of-32-bit-range value is rejected outright rather than truncated.

`Entry` implements the shared `filesystem.TimestompCapable` methods `Timestamp`
(a reader), `SupportsTimestamp`, and `SetTimestamp`. `SupportsTimestamp` checks
whether the inode contains the requested field. The technique layer does not yet
compare timestamp values against a historical baseline or automatically save
original timestamps. See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp)
for the resulting detection and restoration limits.

## Limitations

- **Filesystems without the `extents` incompatible feature**, i.e. the
  older indirect-block mapping scheme.
- **`bigalloc` filesystems.**
- **Unsupported incompatible feature flags** outside the allowlist checked
  at mount.
- **Inline-data inodes**, for extent/content access; a tiny file whose
  content lives entirely in the inode has no blocks to walk.
- **Encrypted inodes.**
- **Unwritten (preallocated) extents.**
- **ea-inode xattr values** (`e_value_inum != 0`).
- **A new external xattr block**, when none exists and no existing region
  has room for a new attribute.
- **Mutating a shared external xattr block** (`refcount > 1`).
- **Emptying an external xattr block via delete.**
- **Live slack writes** are unsupported; use a disposable unmounted image.
  Explicit detection/extraction supports read-only access to a verified ext4
  block device, subject to device permissions and kernel mount-ID support.
- **Live creation/change time writes** are unsupported. Linux live mode supports
  `user.*` xattrs and accessed/modified timestamps only on verified ext4 mounts.
  Explicit symlink targets and other mounted filesystem types are rejected.
  Recursive scans skip unsupported children with stderr warnings; permission
  and I/O errors remain fatal.

## Slack and Linux live mode

Image slack uses the unused tail of the final allocated regular-file block.
Sparse, overallocated, encrypted, inline-data, and unwritten-extent layouts are
refused with `filesystem.ErrUnsupported`, allowing automatic detection to
continue. Corrupt extent metadata remains fatal. Hide/detect/clear use the shared
CRC-checked Nemo frame and a backup
manifest for restoring overwritten residual bytes. Frames from the earlier
unmerged ext4 branch used a different format and must be recreated on disposable
fixtures; they are not detected by the shared frame reader.

Linux live mode uses descriptor-backed xattr and timestamp syscalls. Native
operations need ordinary file permissions. Live timestamp reads expose ctime
from the opened descriptor and birth time through `statx` when supplied by the
kernel/filesystem. Creation/change time writes return `filesystem.ErrUnsupported`;
access/modify writes use `UTIME_OMIT` to preserve the other field atomically.

Explicit live slack detection/extraction opens the backing block device read-only.
The target descriptor's mount ID and device number are checked against
`/proc/self/mountinfo`; the opened device must be a block device with matching
major/minor numbers. Bind mounts map through their filesystem root, escaped
mount paths are decoded, and sibling prefixes cannot escape the selected mount.
Read-only wrappers protect the parser and caller; the raw target inode must match
the mounted descriptor.

Raw reads need device read permission, often elevated privileges, a visible device
node and kernel `STATX_MNT_ID` support. Unflushed changes or concurrent activity
can make the mounted view stale: these reads are discovery, not a consistent
snapshot. Use a stable offline image for repeatable forensic results. Mounted
raw writes remain refused. Image
custody records contain write hashes; live operations record logical operation
metadata, not raw image write events.

## Testing

`ext4_test.go` and `timestomp_test.go` build synthetic superblocks, group
descriptors, inodes, and extent trees byte by byte, so tests don't depend
on an image produced by a real Linux install and pin down exact on-disk
layout assumptions: extent-header validation, xattr entry packing in both
the in-inode and external-block layouts, and the split second/nanosecond
timestamp encoding.

`xattr_capacity_test.go` covers a full inode-body region with and without
an external block, shared external-block rejection, and external capacity
failure. It checks existing values after external fallback and deletion,
and compares the whole image before and after rejected additions and
replacements to ensure no unrelated bytes change.

`indexed_test.go` generates disposable 1 KiB and 4 KiB ext4 images with
`mke2fs`, `debugfs` and `e2fsck -D`, and enumerates 700 indexed files with metadata
checksums enabled. Synthetic cases cover index levels and malformed metadata.
The e2fsprogs cases skip when those tools are unavailable.

`python3 scripts/linux-acceptance.py --work-parent <ext4-directory> --require-live`
requires native xattr/timestamp/extraction and skipped-child checks in addition
to image round trips. The dedicated Linux CI job uses this mode so missing native
coverage fails instead of silently skipping.
