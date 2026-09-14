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

Directory traversal (`Children` in `ext4.go`) requires a non-indexed
directory (`inodeFlagIndex` clear); an indexed (`htree`) directory is
refused rather than parsed as if it were linear. For a plain directory it
walks the inode's extent-mapped blocks as a flat, logically-contiguous
sequence (a hole at any logical block number is an error) and parses each
block's linear directory-record list.

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

`Entry` also exposes `Timestamp` (a reader) and `SupportsTimestamp`,
implemented here for ext4's own use, but neither is part of the shared
`filesystem.TimestompCapable` interface yet, so `internal/technique` can't
call them. See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp)
for why that matters for `detect`.

## Limitations

- **Filesystems without the `extents` incompatible feature**, i.e. the
  older indirect-block mapping scheme.
- **`bigalloc` filesystems.**
- **Unsupported incompatible feature flags** outside the allowlist checked
  at mount.
- **Inline-data inodes**, for extent/content access; a tiny file whose
  content lives entirely in the inode has no blocks to walk.
- **Encrypted inodes.**
- **Indexed (`htree`) directories.** Only linear directories are read.
- **Unwritten (preallocated) extents.**
- **ea-inode xattr values** (`e_value_inum != 0`).
- **A new external xattr block**, when none exists and no existing region
  has room for a new attribute.
- **Mutating a shared external xattr block** (`refcount > 1`).
- **Emptying an external xattr block via delete.**
- **Slack-space access and live mode for ext4.** Not built yet. See
  [Roadmap](../roadmap.html).

## Testing

`ext4_test.go` and `timestomp_test.go` build synthetic superblocks, group
descriptors, inodes, and extent trees byte by byte, so tests don't depend
on an image produced by a real Linux install and pin down exact on-disk
layout assumptions: extent-header validation, xattr entry packing in both
the in-inode and external-block layouts, and the split second/nanosecond
timestamp encoding.
