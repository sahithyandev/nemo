---
title: APFS
parent: File Systems
nav_order: 2
---

# Nemo's APFS parser

This is about `internal/filesystem/apfs`. It covers detection, mounting,
the B-tree code, the in-place write path, and every layout the parser
refuses instead of misreading.

For what named streams are and how detection works at the technique level,
see [Named-Stream Hiding](../techniques/named-streams.html). For
where this package sits in the overall layering, see
[Architecture](../architecture/).

## Detection

`sniff` decides if an image is APFS. It gets at most the first 4096 bytes.

It checks two things.

- Does the image start with the `NXSB` magic at byte 32? That means a bare
  container.
- Does it have a GPT header at offset 512, with an `Apple_APFS` partition
  entry inside it? That means a GPT-wrapped container.

Either match is enough. `sniff` must handle a short or empty slice without
panicking, since some images are smaller than 4096 bytes.

## Mounting

`New(img)` is read-only. It walks from raw bytes to an open filesystem tree
in six steps.

**1. Find the container.**

Read block 0. If it starts with `NXSB`, the image is a bare container.

If not, look for a GPT header and an `Apple_APFS` partition entry, matched
by type GUID. If found, wrap the image in a `section`. This is a small
struct that shifts every read and write by the partition's start offset. It
lets the rest of the parser treat the partition as if it started at byte 0.

The GPT partition entry table itself is bounds-checked before it's read.
`entrySize * numEntries` is computed in 64-bit arithmetic and capped at 1
MiB, so a crafted header can't claim a multi-gigabyte table and force a huge
allocation.

**2. Read the container superblock (`nx_superblock_t`).**

Block 0 gives the real `nx_block_size`. This gets checked again for sanity.
It must be a power of two, between 4 KiB and 64 KiB. Nothing else is read
at that size until the check passes, so a crafted block size can't drive an
oversized read.

The checkpoint descriptor area is then scanned block by block. Every
checksum-valid `NXSB` block found there is a candidate checkpoint. The one
with the highest transaction id (`xid`) wins. Checkpoint-map blocks don't
carry the `NXSB` magic, so they get skipped automatically.

A tree-form checkpoint descriptor area (the high bit of
`nx_xp_desc_blocks` set) is not supported. Only the flat array form is.

**3. Resolve the container object map (omap).**

This is a B-tree keyed by `(oid, xid)`. It maps virtual object ids to
physical block addresses. Nearly everything else in APFS is addressed
through an omap, not by a raw physical address.

**4. Resolve a volume.**

The chosen checkpoint's `nx_fs_oid` array lists up to `nx_max_file_systems`
volume object ids. Nemo resolves each one through the container omap, in
order, and mounts the first one that parses cleanly.

Encrypted volumes are skipped here. See Limitations below.

**5. Read the volume superblock (`apfs_superblock_t`).**

This gives the volume's own omap, and the root object id of its filesystem
tree.

**6. Resolve the volume omap.**

Then resolve the filesystem tree's root through it, and open that as a
`tree` (see the next section). `FS.fsTree` is this tree. `FS.Root()` is the
fixed object id `2`, the volume's root directory.

Every object read along the way, whether a superblock or a B-tree node,
goes through `readObject`. That function checks the object's Fletcher-64
checksum before anything looks at the bytes. A checksum mismatch is always
a hard error. Nothing downstream ever sees unverified bytes.

## The B-tree layer (`btree.go`)

APFS uses one on-disk B-tree format, `btree_node_phys_t`. The container
omap, each volume's omap, and each volume's filesystem tree all use it.

Only the key and value shape, and the comparator, differ between them.
`tree` is a single generic implementation. Three things parameterize it.

- `resolve(oid) (paddr, error)` turns a child pointer into a physical block
  address. The omap's own tree uses `omapResolveIdentity`, since an omap
  can't be indirected through another omap. Every other tree resolves
  through that volume's omap, at the mounted `xid`.
- `cmp(a, b []byte) int` is the key comparator. It's `omapKeyCompare` for
  `{oid, xid}` pairs, and `fsKeyCompare` for filesystem-tree records.
- `fixedKeySize`/`fixedValSize` only matter when a node's own
  `BTNODE_FIXED_KV_SIZE` flag is set. That's true for the omap tree, where
  keys and leaf values are a fixed size. It's always `0, 0` for the
  filesystem tree, since names vary in length.

### Reading a node

`decodeNode` reads one block and produces parallel `keys` and `vals`
slices. These alias the block buffer directly. There's no copying on the
read path.

It handles both on-disk layouts.

- Fixed-kv nodes use `kvoff_t` table-of-contents entries.
- Variable-kv nodes use `kvloc_t` entries. Keys pack forward from the end
  of the table. Values pack backward from the end of the block.

A non-leaf node's value is always treated as an 8-byte child oid, no matter
what the table of contents claims. Trusting a bad length there would panic
later, deep in `binary.LittleEndian.Uint64`. Checking it up front turns
that into a clean bounds error instead.

### Walking the tree

Traversal happens through a `cursor`.

`seek(key)` walks root to leaf. At each level it picks a child with
`lastLE`, the last key less than or equal to the target. A non-leaf key is
only a lower bound on its child subtree. It's not necessarily an exact copy
of the child's smallest key.

`next()` moves the cursor forward, one record at a time, in key order.

APFS nodes have no sibling pointers. So `cursor` keeps the whole descent
stack, from root down to the current leaf. When a leaf runs out of
records, `next()` pops back up the stack and re-descends into the next
subtree, instead of following a next-leaf pointer that doesn't exist.

A hard cap, `maxBtreeDepth` (64), bounds every descent. Real APFS trees are
maybe ten levels deep, so this only ever fires on a corrupted or cyclic
pointer chain. Without the cap, that kind of image could make a descent
loop forever.

### The object map lookup

`omap.resolve(oid, atXid)` looks up the physical address for an object id,
as of a given transaction id.

An object can have several versions in the omap, each a separate `(oid,
xid)` entry. `resolve` seeks to `(oid, 0)`, the smallest possible key for
that oid, then scans forward through every entry for that oid. It keeps the
one with the largest `xid` that's still less than or equal to `atXid`.

It has to scan every version this way, rather than seeking straight to
`(oid, atXid)`. A forward-only cursor seeking directly there would skip
past any entry with a smaller xid. That's the common case, since it's rare
for an entry's xid to exactly equal the xid being looked up.

### Filesystem-tree keys

`j_key_t` is one little-endian `u64`. The low 60 bits are the object id.
The high 4 bits are the record type. `encodeJKey` and `decodeJKey` handle
the packing.

`fsKeyCompare` only orders by `(oid, type)`. It doesn't model the per-type
sub-key, things like a directory entry's name hash, an xattr's name, or a
file extent's logical offset.

Because of that, a seek only lands on the first record for a given `(oid,
type)`. Callers like `Children`, `xattrRecords`, and `extentsOf` then scan
forward from there by hand, stopping at the first key whose `(oid, type)`
no longer matches.

The record types this parser reads are `INODE` (3), `XATTR` (4),
`DSTREAM_ID` (6), `FILE_EXTENT` (8), and `DIR_REC` (9).

Directory entry names come from `decodeDrecKey`. APFS has two key layouts
for directory records.

- A "hashed" layout (`j_drec_hashed_key_t`), used by case-insensitive or
  normalization-sensitive volumes.
- A plain layout (`j_drec_key_t`).

Rather than read the volume's incompatible-features flag to know which one
is in use, `decodeDrecKey` just tries the hashed layout first. If the
result doesn't look like a real, printable name, it falls back to the
plain layout.

## In-place B-tree writes (`btree_write.go`)

Real APFS is copy-on-write. A proper update allocates new blocks, rewrites
the whole root-to-leaf path, updates the omap, and commits a new
checkpoint.

Nemo skips all of that. There's no space manager and no checkpoint writer
here. Instead, `rewriteLeaf` rewrites exactly one leaf, in place, at its
existing address.

1. Descend to the leaf that owns the key (`descendToLeaf`).
2. Hand the leaf's records to a `mutate` closure. It inserts, replaces, or
   deletes one record.
3. `encodeLeaf` repacks all the records. Keys go forward from the
   table-of-contents. Values go backward from the end of the block, 8-byte
   aligned. It zeroes the rest of the block first, so no stale bytes from a
   shrunk record are left behind for a forensic reader to find.
4. `writeNodeBlock` recomputes the Fletcher-64 checksum and writes the
   block back.
5. If the leaf wasn't the root, `bumpRootKeyCount` updates the root's
   trailing `btree_info_t`. That means the key count, and the
   longest-key/longest-val hints. The hints only ever grow, never shrink,
   since the root's own encode pass never sees a non-root leaf's change and
   can't know if some other, untouched leaf still holds the true maximum.

None of this touches an object id, transaction id, omap entry, or
checkpoint. The volume stays checksum-valid and mountable. But it isn't an
APFS transaction. A real driver doing anything else on the volume at the
same time, or a snapshot pointing at the old version of this leaf, would
see it as broken.

**Inserting or deleting at index 0 is safe, without touching the parent.**

A non-leaf key is only a lower bound on its child subtree. It doesn't have
to match the child's exact minimum. By the time an insert reaches a leaf,
descent has already confirmed `parent_key[i] ≤ new_key`. Landing the new
key below the leaf's old minimum doesn't break that bound.

A delete only raises a leaf's true minimum. That only widens the gap above
the parent's separator key. It never invalidates it. See the doc comment at
the top of `btree_write.go` for the full argument.

**What it can't do.**

It can't grow a node past its existing free space. `encodeLeaf` returns
"node full" instead of splitting it. And it can't touch anything above the
leaf besides the root's summary fields. Splitting a leaf, allocating a new
block, and updating a non-root ancestor are all unimplemented.

## Named streams (`namedstream.go`)

APFS extended attributes, and the resource fork, are `XATTR`-type records
in the filesystem tree. The resource fork is just the xattr named
`com.apple.ResourceFork`, nothing special. Every xattr record is keyed by
`(file object id, name)`.

A record's value is one of two kinds.

- **Embedded.** The bytes sit inline in the record, up to
  `XATTR_MAX_EMBEDDED_SIZE`, which is 3804 bytes.
- **Stream-backed.** A `j_xattr_dstream_t` points at a `DSTREAM_ID` object.
  Its data lives in `FILE_EXTENT` records. These must be contiguous and
  start at logical offset 0. `extentsOf` rejects anything sparse or
  out-of-order as unsupported.

Reading a stream-backed value calls `readExtents`, which walks the file's
extents and concatenates their bytes. It caps the read at the bytes the
extents actually cover, not at whatever size the (possibly corrupted)
`j_xattr_dstream_t` claims. That stops a bogus multi-gigabyte size field
from causing an out-of-memory read before the real check runs.

Writes only go through the in-place leaf rewriter above. That limits what
they can do.

| Operation | Supported | Not supported |
|---|---|---|
| Replace an embedded value | any value up to 3804 bytes | growing past 3804 bytes without switching to a stream |
| Replace a stream-backed value | any value that fits the stream's existing allocation | growing past that allocation (needs block allocation) |
| Create a new xattr | embedded only, up to 3804 bytes | a new stream-backed xattr (needs block allocation) |
| Delete an xattr | drops the record | freeing a stream-backed xattr's extents (needs the space manager; the blocks stay allocated but orphaned) |

A filesystem-owned xattr, one with the `XATTR_FILE_SYSTEM_OWNED` flag set,
can't be changed or deleted at all.

Overwriting a stream-backed value isn't atomic. The extent data gets
written first. Then the record's size field gets updated. If something
interrupts the write in between, a reader sees a prefix of the new value,
possibly zero-padded. Not the full old value, and not the full new one
either. A true copy-on-write update would avoid this gap. Nemo's in-place
path doesn't have one.

Inserting a new xattr keeps the leaf's records sorted by name.
`xattrKeyAfter` compares the new name against each existing record's name to
find the right insertion point, since `fsKeyCompare` alone only orders by
`(oid, type)` and doesn't look at the name.

## Entries and paths (`apfs.go`)

`FS.Open(path)` walks the path one component at a time, starting from the
root. At each step it calls `Children()` on the current entry and looks for
a name match. There's no shortcut lookup by full path. Every `Open` call
costs one tree scan per path component.

`Entry.Children()` seeks to the first `DIR_REC` record for the entry's
object id, then scans forward, decoding one child per record until the
`(oid, type)` no longer matches.

An `apfs.Entry` implements `filesystem.NamedStreamCapable`,
`filesystem.TimestompCapable` (`timestomp.go`), and
`filesystem.SlackSpaceCapable` (`slack.go`).

## Timestomping (`timestomp.go`)

The four timestamps live at fixed offsets in the `j_inode_val_t` INODE record:
`create_time` (16), `mod_time` (24), `change_time` (32), `access_time` (40), each a
`uint64` nanosecond count since the Unix epoch. `SetTimestamp` finds that record
through `fsTree.rewriteLeaf` (the same in-place leaf rewrite `namedstream.go` uses)
and overwrites 8 of its bytes, leaving the rest of the record, including `xfields`,
untouched. The record's length never changes, so this is the same same-size-replace
case `replaceXattr` already covers: no B-tree rebalance, no `btree_info_t` update
beyond what `bumpRootKeyCount` already does for a zero-delta mutation.

`change_time` maps to `filesystem.TimeChanged`, the fourth `TimeField`. It is the
one MACB timestamp no live userland API can set; writing it through the image
gives a stomp with no ctime/mtime mismatch to give it away.

## Slack space (`slack.go`)

`Entry.SlackRegions` reports the unused tail bytes past a file's logical
size, as absolute byte offsets into the caller's `image.Image` (not into
`FS.img`, which for a GPT-wrapped container is a partition-relative
`section`; `slack.go` adds the container's own byte offset, `FS.base`, back
in). Nemo's technique layer (`internal/technique`) does the actual
hide/detect/clear against those regions: this package only has to say where
they are.

A file's content is a data stream described by a `j_dstream_t`, which isn't
a fixed-offset field of `j_inode_val_t` like the timestamps are. It's an
extended field (xfield), `INO_EXT_TYPE_DSTREAM`, appended after the
92-byte fixed part. `inodeDStream` walks the xfield table (`xf_blob_t` +
`x_field_t[]` + values, each value padded up to the next 8-byte boundary) to
find it. The stream's actual blocks are then read the same way a
stream-backed xattr's are, via `extentsOf`, keyed by the inode's
`private_id` (offset 8) rather than the inode's own object id.

Slack is computed per extent, not once across the whole file: extents are
not necessarily contiguous on disk, so a single region spanning two of them
could point at unrelated blocks. Since every extent's length is a whole
number of blocks, this also means a region never crosses a block boundary
without meaning to.

Two layouts make the dstream's logical size meaningless, so `SlackRegions`
refuses them with an error wrapping `filesystem.ErrUnsupported` rather than
silently reporting a wrong (or simply absent) region:

- **HFS-style compression.** A `com.apple.decmpfs` xattr means the
  dstream's bytes are compressed, or that the real content lives in the
  xattr or resource fork instead.
- **Per-file encryption.** A non-zero `default_crypto_id` is refused
  outright, rather than special-casing any particular value as "not really
  encrypted": this parser has no key material to confirm that against.

A directory, an empty file, or an inode with no `DSTREAM` xfield at all
(e.g. a symlink) reports no regions and no error: hide then fails through
the ordinary "insufficient slack space" path instead of turning a
whole-image `detect` scan into a hard stop on the first file it can't use.

Slack space in a block a clone shares with another file isn't detected as
unsafe: nemo doesn't read `EXTENT_REF`/reference-count records, so writing
there could corrupt the clone's own content. Sparse (hole) extents are
already refused by `extentsOf`, before `slack.go` ever sees them.

## Limitations

`New` refuses each of the layouts below with a clear error, rather than
return a partial or wrong result.

- **Encrypted volumes.** This is the `APFS_FS_UNENCRYPTED` bit clear in
  `apfs_fs_flags`. Reading content, and on an encrypted volume even the
  filenames, needs key material this parser doesn't have.
- **A tree-form checkpoint descriptor area.** This is the high bit of
  `nx_xp_desc_blocks` set. Only the flat array form is supported.
- **No recognizable container.** No `NXSB` superblock at byte 0, and no
  GPT `Apple_APFS` partition entry either.
- **Any checksum mismatch**, on any superblock or B-tree node.
- **An implausible `nx_max_file_systems`** value. It's rejected if it's
  zero or over 100, since a real container never has that many volumes and
  a bogus value would otherwise drive an oversized allocation.

Some things aren't attempted at all, not even refused with an error.

- **Snapshots.** Only the current volume's live filesystem tree gets
  walked.
- **Hashed (`BTNODE_HASHED`) B-tree nodes.**
- **The space manager and reaper.** Nothing here allocates or frees blocks.
  That's why every write path above is capped to "fits in what's already
  allocated."
- **Clone-aware slack safety.** `slack.go` doesn't check whether a block is
  shared between clones before reporting it as slack.
- **Live mode for APFS.** Not built yet. See [Roadmap](../roadmap.html)
  item 21e.

## Safety against crafted images

A few checks exist specifically because the parser has to handle a hostile
or corrupted image, not just a well-formed one.

- Block size is validated before any block-sized read happens, so a
  crafted `nx_block_size` can't force a huge allocation.
- The GPT partition-entry table size is capped at 1 MiB, computed in 64-bit
  arithmetic so it can't wrap around a 32-bit multiplication.
- `maxBtreeDepth` stops a cyclic or corrupted child pointer chain from
  looping forever.
- A non-leaf node's value length is always treated as 8 bytes, regardless
  of what the node claims, so a short value can't reach a
  `binary.LittleEndian.Uint64` call and panic.
- A file extent's physical address is checked against `math.MaxInt64 /
  blockSize` before it's multiplied out, so a crafted extent can't turn
  into a negative byte offset.
- A stream's claimed size is capped at what its extents actually cover
  before any buffer gets allocated.

## Testing

`btree_test.go`, `btree_cursor_test.go`, and `btree_write_test.go` build
synthetic node buffers byte by byte, pinning down exact on-disk layout
assumptions (fixed vs. variable kv, root vs. non-root leaf encoding) without
needing a full image.

`apfs_test.go`, `namedstream_test.go`, `timestomp_test.go`, `slack_test.go`,
and `fixtures_test.go` instead run against real APFS images produced by a
real Mac: `testdata/apfs-{gpt,bare}.img.gz` and a handful of purpose-built
variants (`apfs-casesensitive`, `apfs-manyfiles`, `apfs-16k`,
`apfs-multivol`, `apfs-encrypted`), each exercising a parser path a plain
volume can't reach on its own. `testimage_test.go`'s `loadImage` decompresses
one into a private, writable temp copy per test. `testdata/mkapfs.sh`
(macOS-only, builtin `hdiutil`/`newfs_apfs`/`diskutil`) generates the whole
set; CI just consumes the committed `.gz` files and never regenerates them.
A couple of on-disk states — multiple non-contiguous extents on one stream,
a filesystem-owned xattr — turned out not to be reliably producible with
only builtin macOS tooling, so `fixtures_test.go` fabricates those directly
through the package's own unexported btree insert path instead of via a
fixture.
