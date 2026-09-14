# Nemo's APFS parser

This is about `internal/filesystem/apfs`. It covers how mounting works, how
the B-tree code works, what the in-place write path can and can't do, and
every layout the parser refuses instead of misreading. For what named
streams are and how detection works, see
[docs/techniques/named-streams.md](techniques/named-streams.md). For where
this package sits in the overall layering, see
[docs/architecture.md](architecture.md).

## Mounting

`New(img)` is read-only. It walks from raw bytes to an open filesystem tree
in six steps.

1. **Find the container.** Read block 0. If it starts with the `NXSB` magic,
   the image is a bare container. If not, look for a GPT header at offset
   512 and an `Apple_APFS` partition entry (matched by type GUID). If one is
   found, wrap the image in a `section` that shifts every read and write, so
   the rest of the parser sees the partition as starting at byte 0.
2. **Read the container superblock (`nx_superblock_t`).** Block 0 gives the
   real `nx_block_size`, checked again for sanity (power of two, 4 KiB to 64
   KiB) before anything else is read at that size. The checkpoint descriptor
   area is scanned block by block. Every checksum-valid `NXSB` block found
   there is a candidate checkpoint, and the one with the highest transaction
   id (`xid`) wins. Checkpoint-map blocks get skipped, since they don't
   carry the `NXSB` magic.
3. **Resolve the container object map (omap).** This is a B-tree keyed by
   `(oid, xid)` that maps virtual object ids to physical block addresses.
4. **Resolve a volume.** The chosen checkpoint's `nx_fs_oid` array lists up
   to `nx_max_file_systems` volume object ids. Nemo resolves each one
   through the container omap and mounts the first that parses cleanly.
   Encrypted volumes are skipped; see Limitations.
5. **Read the volume superblock (`apfs_superblock_t`).** This gives the
   volume's own omap and the root object id of its filesystem tree.
6. **Resolve the volume omap**, then resolve the filesystem tree's root
   through it, and open that as a `tree` (see below). `FS.fsTree` is this
   tree. `FS.Root()` is the fixed object id `2`, the volume's root
   directory.

Every object read along the way, whether a superblock or a B-tree node,
goes through `readObject`. That function checks the object's Fletcher-64
checksum before anything looks at the bytes.

## The B-tree layer (`btree.go`)

APFS uses one on-disk B-tree format (`btree_node_phys_t`) for the container
omap, each volume's omap, and each volume's filesystem tree. Only the
key/value shape and the comparator differ between them. `tree` is a single
generic implementation. Three things parameterize it.

- `resolve(oid) (paddr, error)` turns a child pointer into a physical block
  address. The omap's own tree uses `omapResolveIdentity`, since an omap
  can't be indirected through another omap. Every other tree resolves
  through that volume's omap at the mounted `xid`.
- `cmp(a, b []byte) int` is the key comparator. It's `omapKeyCompare` for
  `{oid, xid}` pairs, and `fsKeyCompare` for filesystem-tree records.
- `fixedKeySize`/`fixedValSize` only matter when a node's own
  `BTNODE_FIXED_KV_SIZE` flag is set. That's true for the omap tree, where
  keys and leaf values are a fixed size. It's always `0, 0` for the
  filesystem tree, since names vary in length.

`decodeNode` reads one block and produces parallel `keys`/`vals` slices.
These alias the block buffer directly, so there's no copying on the read
path. It handles both layouts, fixed-kv (`kvoff_t` table-of-contents
entries) and variable-kv (`kvloc_t` entries, keys packed forward from the
end of the table, values packed backward from the end of the block). A
non-leaf node's value is always treated as an 8-byte child oid, no matter
what the table of contents claims. Trusting a bad length there would panic
later. This way it fails as a clean bounds error instead.

Traversal happens through a `cursor`. `seek(key)` walks root to leaf,
picking children with `lastLE`, the last key less than or equal to the
target. (A non-leaf key is only a lower bound on its child subtree, not
necessarily an exact copy of the child's smallest key.) `next()` then moves
forward in key order. APFS nodes have no sibling pointers, so `cursor` keeps
the whole descent stack and re-descends into the next subtree once a leaf
runs out, instead of following a next-leaf pointer that doesn't exist. A
hard cap, `maxBtreeDepth` (64), stops every descent, so a corrupted or
cyclic pointer chain fails instead of looping forever.

### Filesystem-tree keys

`j_key_t` is one little-endian `u64`. The low 60 bits are the object id, the
high 4 bits are the record type (`encodeJKey`/`decodeJKey`). `fsKeyCompare`
only orders by `(oid, type)`. It doesn't model the per-type sub-key, things
like a directory entry's name hash, an xattr's name, or a file extent's
logical offset. A seek lands on the first record for a given `(oid, type)`,
and callers (`Children`, `xattrRecords`, `extentsOf`) scan forward from
there, stopping at the first key whose `(oid, type)` no longer matches. The
record types this parser reads are `INODE` (3), `XATTR` (4), `DSTREAM_ID`
(6), `FILE_EXTENT` (8), and `DIR_REC` (9).

Directory entry names come from `decodeDrecKey`. APFS has two key layouts
for directory records, a "hashed" one (`j_drec_hashed_key_t`) used by
case-insensitive or normalization-sensitive volumes, and a plain one
(`j_drec_key_t`). Rather than read the volume's incompatible-features flag
to know which is in use, `decodeDrecKey` just tries the hashed layout first
and falls back to the plain one if the result doesn't look like a real,
printable name.

## In-place B-tree writes (`btree_write.go`)

Real APFS is copy-on-write. A proper update allocates new blocks, rewrites
the whole root-to-leaf path, updates the omap, and commits a new checkpoint.
Nemo skips all of that. There's no space manager and no checkpoint writer
here. Instead, `rewriteLeaf` rewrites exactly one leaf, in place, at its
existing address.

1. Descend to the leaf that owns the key (`descendToLeaf`).
2. Hand the leaf's records to a `mutate` closure, which inserts, replaces,
   or deletes one record.
3. `encodeLeaf` repacks all the records, keys forward from the
   table-of-contents, values backward from the end of the block, 8-byte
   aligned. It zeroes the rest of the block first, so no stale bytes from a
   shrunk record are left for a forensic reader to find.
4. `writeNodeBlock` recomputes the Fletcher-64 checksum and writes the block
   back.
5. If the leaf wasn't the root, `bumpRootKeyCount` updates the root's
   trailing `btree_info_t`, both the key count and the longest-key/
   longest-val hints (which only ever grow, never shrink). The root's own
   encode pass never sees a non-root leaf's changes, so this step exists to
   keep those fields honest.

None of this touches an object id, transaction id, omap entry, or
checkpoint. The volume stays checksum-valid and mountable. But it isn't an
APFS transaction. A real driver doing anything else at the same time, or a
snapshot pointing at the old version of this leaf, would see it as broken.

**Inserting or deleting at index 0 is safe, without touching the parent.**
A non-leaf key is only a lower bound on its child subtree, not required to
match the child's exact minimum. By the time an insert reaches a leaf,
descent has already confirmed `parent_key[i] ≤ new_key`. Landing the new key
below the leaf's old minimum doesn't break that. A delete only raises a
leaf's true minimum, which only widens the gap above the parent's separator
key. It never invalidates it. See the doc comment at the top of
`btree_write.go` for the full argument.

**What it can't do.** It can't grow a node past its existing free space
(`encodeLeaf` returns "node full" instead of splitting it), and it can't
touch anything above the leaf besides the root's summary fields. Splitting
a leaf, allocating a new block, and updating a non-root ancestor are all
unimplemented.

## Named streams (`namedstream.go`)

APFS extended attributes, and the resource fork (just the xattr named
`com.apple.ResourceFork`), are `XATTR`-type records in the filesystem tree,
keyed by `(file object id, name)`. A record's value is one of two kinds.

- **embedded** means the bytes sit inline in the record, up to
  `XATTR_MAX_EMBEDDED_SIZE` (3804 bytes).
- **stream-backed** means a `j_xattr_dstream_t` points at a `DSTREAM_ID`
  object, whose data lives in `FILE_EXTENT` records. These must be
  contiguous and start at logical offset 0. `extentsOf` rejects anything
  sparse or out-of-order.

Writes only go through the in-place leaf rewriter above, which limits what
they can do.

| Operation | Supported | Not supported |
|---|---|---|
| Replace an embedded value | any value up to 3804 bytes | growing past 3804 bytes without switching to a stream |
| Replace a stream-backed value | any value that fits the stream's existing allocation | growing past that allocation (needs block allocation) |
| Create a new xattr | embedded only, up to 3804 bytes | a new stream-backed xattr (needs block allocation) |
| Delete an xattr | drops the record | freeing a stream-backed xattr's extents (needs the space manager; the blocks stay allocated but orphaned) |

A filesystem-owned xattr (the `XATTR_FILE_SYSTEM_OWNED` flag) can't be
changed or deleted.

Overwriting a stream-backed value isn't atomic. The extent data gets
written first, then the record's size field. If something interrupts the
write in between, a reader sees a prefix of the new value, possibly
zero-padded, and neither the full old value nor the full new one. A true
copy-on-write update would avoid this gap; nemo's in-place path doesn't.

## Limitations

`New` refuses each of the layouts below with a clear error, rather than
return a partial or wrong result.

- **Encrypted volumes** (the `APFS_FS_UNENCRYPTED` bit clear in
  `apfs_fs_flags`). Reading content, and on an encrypted volume even the
  filenames, needs key material this parser doesn't have.
- **A tree-form checkpoint descriptor area** (`nx_xp_desc_blocks`' high bit
  set). Only the flat array form is supported.
- **No recognizable container.** No `NXSB` superblock at byte 0, and no GPT
  `Apple_APFS` partition entry either.
- **Any checksum mismatch**, on any superblock or B-tree node.

Some things aren't attempted at all, not even refused with an error.

- **Snapshots.** Only the current volume's live filesystem tree gets
  walked.
- **Hashed (`BTNODE_HASHED`) B-tree nodes.**
- **The space manager and reaper.** Nothing here allocates or frees blocks.
  That's why every write path above is capped to "fits in what's already
  allocated."
- **Slack-space access, timestomp, and live mode for APFS.** Not built yet;
  see `docs/work-breakdown.md` items 19c/20d/21e.

## Testing

`apfs_test.go`, `btree_test.go`, `btree_cursor_test.go`,
`btree_write_test.go`, and `namedstream_test.go` build synthetic
container/volume images byte by byte (`testimage_test.go`), rather than
relying on an image produced by a real Mac. That means tests run on any OS,
and they pin down exact on-disk layout assumptions, things like fixed vs.
variable kv, root vs. non-root leaf encoding, hashed vs. plain
directory-record keys, and embedded vs. stream xattr values.
