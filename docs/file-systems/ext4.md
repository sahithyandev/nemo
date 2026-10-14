---
title: ext4
parent: File Systems
nav_order: 3
---

# Nemo's ext4 parser

This page covers `internal/filesystem/ext4`: detection, superblock and inode parsing, extent-tree traversal, extended attributes, and timestomp.

- For the on-disk xattr format this parser reads and writes, see [named-stream hiding](../techniques/named-streams.html#ext4-extended-attributes). This page covers the parser's own behavior and what it refuses.
- For where this package sits in the layering, see [Architecture](../architecture/).

## At a glance

- Image mode supports named streams, slack space, and timestomp.
- Live mode (Linux) supports `user.*` xattrs and accessed and modified timestamps on verified mounts. Slack is read-only.
- It refuses `bigalloc`, inline data, encryption, and unwritten extents. See [Limitations](#limitations).

## Detection

[`Sniff`]({{ site.src }}/internal/filesystem/ext4/ext4.go#L51) in [`ext4.go`]({{ site.src }}/internal/filesystem/ext4/ext4.go) looks for the ext4 superblock magic `0xEF53` at byte `1024 + 0x38`. That is the fixed superblock location on every ext2, ext3, and ext4 filesystem.

## Mounting

[`New(img)`]({{ site.src }}/internal/filesystem/ext4/ext4.go#L82) reads the 1024-byte superblock at offset 1024 and validates it before anything else touches the image.

- The magic must match. The block-size shift (`log_block_size`) must be 0 through 6, which gives block sizes from 1 KiB to 64 KiB.
- Incompatible feature flags (`s_feature_incompat`) are checked against an allowlist: `filetype`, `extents`, `64bit`, `flex_bg`, and `csum_seed`. Any other bit is refused. `extents` must be set, because the parser only understands extent-mapped inodes, not the older indirect-block scheme.
- Read-only-compatible `bigalloc` is refused outright. It changes the allocation unit away from the raw block size, which the parser assumes everywhere.
- [`validateGeometry`]({{ site.src }}/internal/filesystem/ext4/ext4.go#L167) cross-checks the superblock:
  - `inodesCount`, `blocksCount`, `blocksPerGroup`, and `inodesPerGroup` must be non-zero.
  - `blocksPerGroup` must fit the block-bitmap capacity (`blockSize * 8`).
  - `inodeSize` must be at least 128 bytes and a multiple of 4.
  - The claimed filesystem size must fit the actual image size.
- The group descriptor table's location and size are bounds-checked before any descriptor is read.
- [`New`]({{ site.src }}/internal/filesystem/ext4/ext4.go#L82) ends by reading inode 2 (the root) and requiring it to be a directory. A superblock that parses but points at garbage fails here, not later mid-traversal.

With `64bit`, block and inode-table numbers and the group descriptor size can carry a high 32-bit half from an extended region of the descriptor. The parser reads that half only when the feature bit is set.

## Inodes (`inode.go`)

[`readRawInode`]({{ site.src }}/internal/filesystem/ext4/inode.go#L41) finds an inode's raw bytes in three steps.

1. Compute the group and in-group index: `(inodeNumber - 1) / inodesPerGroup` and `% inodesPerGroup`.
2. Read the group's descriptor (at `gdtOffset + group * descSize`) to get the inode table's starting block.
3. Calculate the inode's offset from there.

[`readInode`]({{ site.src }}/internal/filesystem/ext4/inode.go#L26) then decodes only the fields the parser needs:

- `mode`
- a 64-bit `size`: low 32 bits at offset 4, high 32 bits at offset 108 (the standard ext4 large-file split)
- `flags`
- the 60-byte `i_block` array, which holds the extent tree or, in unsupported cases, indirect block pointers

## Extents (`inode.go`)

[`extentBlocks`]({{ site.src }}/internal/filesystem/ext4/inode.go#L76) walks an inode's extent tree. It refuses three cases up front:

- `inlineData`: the content lives in the inode, so there are no blocks to walk.
- `encrypted` inodes.
- Any inode without the `extents` flag, which means the older indirect-block layout.

[`walkExtentNode`]({{ site.src }}/internal/filesystem/ext4/inode.go#L99) recurses through internal nodes down to the leaves. Along the way it checks:

- the `0xF30A` extent-header magic
- that each node's depth matches what its parent expected
- a hard depth cap of 5

Each 12-byte leaf extent gives a logical block, a length, and a physical block. A length above `0x8000` marks an unwritten extent: preallocated, but it reads as zeros. The parser treats that as unsupported instead of guessing at its content.

It tracks visited internal-node block numbers, so a cyclic or duplicated child pointer cannot loop the walk. The resulting blocks are sorted by logical number and checked for overlap.

### Directories

`Children` in [`ext4.go`]({{ site.src }}/internal/filesystem/ext4/ext4.go) reads linear directories and extent-backed HTree directories.

Indexed traversal follows validated root and index pointers. It checks count and limit fields, block bounds, hash ordering, and unique references. Only leaf blocks are parsed as directory records. Holes, unreferenced blocks, and duplicate names are rejected.

Hash versions 0 to 5 and up to two index levels are supported. The mount allowlist still applies to incompatible feature flags.

## Extended attributes (`xattr.go`)

### Reading

[`readXattrRegions`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L78) reads the up to two places where ext4 stores an inode's xattrs.

- **The in-inode gap** starts at `128 + i_extra_isize`, when it opens with the `0xEA020000` magic.
- **The external block** is named by `i_file_acl`: low 32 bits at inode offset 104, high 16 bits at offset 118 when the inode is large enough. It is read only when the block number is non-zero.

The external block's header is validated before its entries are trusted: the magic, a non-zero `refcount`, and a block count of exactly 1.

[`parseXattrs`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L130) walks the entry-list format both regions share.

- Entries are 16 bytes and fixed, packed from the front. Fields are name length, namespace index, value offset, a value-inode field, value length, and a per-entry hash used only in the external block.
- A non-zero value-inode field is rejected. These ea-inode values would need extra inode lookups that the parser does not do.
- A 4-byte zero ends the list.
- Values are packed from the back and 4-byte aligned.
- Value ranges are checked for overlap with each other and with the entry list before any value bytes are trusted.

### Writing

[`writeXattr`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L302) tries four placements in order.

1. Update the attribute in place if the name already exists in either region.
2. Otherwise pack it into the in-inode region next to what is already there, if it fits.
3. Otherwise take over the in-inode region on its own, if that region is unused.
4. Otherwise use the external block, if one exists.

An occupied in-inode region is never replaced with only the new attribute when the combined set does not fit. Existing attributes stay in their original region. The new attribute goes into an allocated external block, or the write fails.

Capacity and shared-block checks finish before any image write. A rejected operation leaves the image unchanged.

Three things are refused:

- **A new external block.** This applies when none is allocated and no existing region has room.
- **Changing a shared external block.** [`storeXattrRegion`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L380) rejects a block whose `refcount` is above 1. Two inodes share it, and an in-place edit would corrupt the other inode's attributes. Fixing that needs copy-on-write block allocation, which this parser does not do.
- **Deleting the last attribute in an external block.** [`deleteXattr`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L360) refuses, because that would mean freeing the block, not just its content.

With `metadata_csum` enabled, both an inode-body write ([`updateInodeChecksum`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L423)) and an external-block write recompute and store the right checksum. So the change does not fail `fsck` on that account.

## Timestomp (`timestomp.go`)

[`Entry`]({{ site.src }}/internal/filesystem/ext4/ext4.go#L235) implements [`filesystem.TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81): `Timestamp` (a reader), `SupportsTimestamp`, and `SetTimestamp`.

`SetTimestamp` works in five steps.

1. Read the whole raw inode.
2. Compute the field's byte layout.
3. Write the new value into that copy.
4. Refresh the inode checksum when `metadata_csum` is enabled.
5. Write the complete inode back in one [`WriteAt`]({{ site.src }}/internal/image/image.go#L6).

A custody decorator on the image therefore sees one change covering the whole inode, not a partial write.

### Field layout

[`timestampLayout`]({{ site.src }}/internal/filesystem/ext4/timestomp.go#L115) maps the fields to offsets:

- `modified` is `i_mtime` at 16.
- `accessed` is `i_atime` at 8.
- `created` is `i_crtime` at 144. It exists only when the inode's extra area reaches that far.

The extra area is the part past the 128-byte "good old" inode, sized by `i_extra_isize`. [`inodeExtraEnd`]({{ site.src }}/internal/filesystem/ext4/timestomp.go#L139) works out how far it extends and validates `i_extra_isize` on the way.

Asking for `created` on an inode too small to hold `i_crtime` gives a clear, named error. It never writes the wrong field.

### Encoding

[`encodeExt4Timestamp`]({{ site.src }}/internal/filesystem/ext4/timestomp.go#L162) and [`decodeExt4Timestamp`]({{ site.src }}/internal/filesystem/ext4/timestomp.go#L185) handle the split encoding.

- The fixed field holds 32-bit seconds since 1970.
- If the inode's extra area has the `_extra` field for that timestamp, it holds nanoseconds in the upper 30 bits and a 2-bit epoch extension in the low bits. The extension pushes the range past 2038.
- Without an extra field, a value with sub-second precision, or one outside the 32-bit range, is rejected. It is never truncated.

### What it does not do

`SupportsTimestamp` checks whether the inode contains the requested field. The technique layer does not compare timestamps against a historical baseline, and it does not save original timestamps. See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp) for the resulting detection and restoration limits.

## Limitations

Nemo refuses all of these:

- **Filesystems without the `extents` feature.** That is the older indirect-block mapping scheme.
- **`bigalloc` filesystems.**
- **Incompatible feature flags** outside the mount allowlist.
- **Inline-data inodes** for extent and content access. A tiny file whose content lives in the inode has no blocks to walk.
- **Encrypted inodes.**
- **Unwritten (preallocated) extents.**
- **ea-inode xattr values** (`e_value_inum != 0`).
- **A new external xattr block**, when none exists and no existing region has room.
- **Changing a shared external xattr block** (`refcount > 1`).
- **Emptying an external xattr block by delete.**
- **Live slack writes.** Use a disposable unmounted image. Explicit detection and extraction can read a verified ext4 block device read-only, subject to device permissions and kernel mount-ID support.
- **Live creation and change time writes.** Linux live mode supports `user.*` xattrs and accessed and modified timestamps only, and only on verified ext4 mounts. Explicit symlink targets and other mounted filesystem types are rejected. Recursive scans skip unsupported children with stderr warnings. Permission and I/O errors stay fatal.

## Slack and Linux live mode

### Image slack

Image slack uses the unused tail of the final allocated block of a regular file.

Nemo refuses these layouts with [`filesystem.ErrUnsupported`]({{ site.src }}/internal/filesystem/filesystem.go#L11), which lets automatic detection continue:

- sparse
- overallocated
- encrypted
- inline-data
- unwritten-extent

Corrupt extent metadata stays fatal.

Hide, detect, and clear use the shared CRC-checked nemo frame. A backup manifest restores the overwritten residual bytes. Frames from the earlier unmerged ext4 branch used a different format. The shared frame reader does not detect them, so recreate them on disposable fixtures.

### Native operations

Linux live mode uses descriptor-backed xattr and timestamp syscalls. Native operations need ordinary file permissions.

- Timestamp reads expose ctime from the opened descriptor. They expose birth time through `statx` when the kernel and filesystem supply it.
- Creation and change time writes return [`filesystem.ErrUnsupported`]({{ site.src }}/internal/filesystem/filesystem.go#L11).
- Access and modify writes use `UTIME_OMIT`, which keeps the other field intact atomically.

### Live slack detection and extraction

Explicit live slack detection and extraction open the backing block device read-only.

- The target descriptor's mount ID and device number are checked against `/proc/self/mountinfo`.
- The opened device must be a block device with matching major and minor numbers.
- Bind mounts map through their filesystem root, and escaped mount paths are decoded.
- Sibling prefixes cannot escape the selected mount.
- Read-only wrappers protect the parser and the caller.
- The raw target inode must match the mounted descriptor.

Raw reads need device read permission (often elevated), a visible device node, and kernel `STATX_MNT_ID` support.

Unflushed changes or concurrent activity can make the mounted view stale. These reads are for discovery, not a consistent snapshot. For repeatable forensic results, use a stable offline image. Mounted raw writes stay refused.

Image custody records hold write hashes. Live operations record logical operation metadata, not raw image write events.

## Testing

`ext4_test.go` and `timestomp_test.go` build synthetic superblocks, group descriptors, inodes, and extent trees byte by byte. The tests do not depend on an image from a real Linux install. They pin down the exact on-disk layout assumptions:

- extent-header validation
- xattr entry packing in the in-inode and external-block layouts
- the split second and nanosecond timestamp encoding

`xattr_capacity_test.go` covers:

- a full inode-body region, with and without an external block
- shared external-block rejection
- external capacity failure

It checks existing values after external fallback and after deletion. It also compares the whole image before and after rejected additions and replacements, to confirm no unrelated bytes change.

`indexed_test.go` generates disposable 1 KiB and 4 KiB ext4 images with `mke2fs`, `debugfs`, and `e2fsck -D`. It lists 700 indexed files with metadata checksums on. Synthetic cases cover index levels and malformed metadata. The e2fsprogs cases skip when those tools are missing.

`python3 scripts/linux-acceptance.py --work-parent <ext4-directory> --require-live` adds native xattr, timestamp, extraction, and skipped-child checks to the image round trips. The Linux CI job uses this mode, so missing native coverage fails instead of silently skipping.

## Further reading

- [ext4 documentation](https://docs.kernel.org/filesystems/ext4/index.html) in the kernel docs.
- [Superblock](https://docs.kernel.org/filesystems/ext4/super.html), [inodes](https://docs.kernel.org/filesystems/ext4/inodes.html), and [dynamic structures](https://docs.kernel.org/filesystems/ext4/dynamic.html) (extents).
- [Directory entries](https://docs.kernel.org/filesystems/ext4/directory.html) for HTree.
- [Extended attributes](https://docs.kernel.org/filesystems/ext4/attributes.html).
- [More in References](../references.html).
