# APFS Named Streams Branch Review

Critical issues found across commits `dd5cc8c..858325f` (apfs branch).

## 1. Stream-backed xattr write is not atomic

**File:** `internal/filesystem/apfs/namedstream.go:332-342`

For stream-backed xattrs, `writeXattr` writes the extent data first, *then* updates the B-tree record:

```go
if err := f.writeExtents(r.stream.objID, r.stream.allocedSize, data); err != nil {
    return err
}
// ... then:
return f.replaceXattr(oid, name, encodeStreamXattrVal(ds))
```

If `writeExtents` succeeds but `replaceXattr` fails (e.g. "node full" from `encodeLeaf`, or the key vanishes due to a crash), the extent blocks on disk now hold new data but the B-tree's `j_xattr_dstream_t` still records the old `size`. A subsequent `readXattr` would read `old_size` bytes from the now-modified extents, returning truncated/corrupted content.

The doc comment on `btree_write.go` acknowledges this is not a real CoW transaction, but this specific two-phase gap is a data-integrity hazard that is not called out. At minimum the doc comment on `writeXattr` or `writeExtents` should warn that a crash between the two writes leaves the stream inconsistent.

## 2. `writeExtents` zeroes all allocated extents on every write

**File:** `internal/filesystem/apfs/namedstream.go:293-297`

When replacing a stream-backed value with a shorter one, `writeExtents` zero-fills the entire allocated extent space:

```go
chunk := make([]byte, ext.length)
if written < uint64(len(data)) {
    written += uint64(copy(chunk, data[written:]))
}
```

For an 8000-byte stream being replaced with 100 bytes, this writes 7900 bytes of zeros through the custody recorder. This is correct for anti-forensics (no residual old data in the tail), but it means every write to a stream-backed xattr rewrites *all* allocated extent blocks even when only the first few bytes changed. For large allocations (e.g., 128 KB resource forks) this is needlessly expensive. Not a correctness bug, but the tradeoff should be documented.

## 3. `readExtents` can OOM on corrupted dstream metadata

**File:** `internal/filesystem/apfs/namedstream.go:267`

```go
out := make([]byte, 0, size)
```

If the extent metadata says `size = 8000` but the actual extents only cover 7000 bytes, this pre-allocates 8000 bytes of capacity, reads 7000, then the `< size` check returns an error. The over-allocation is small in this case, but for pathological `size` values (e.g., a corrupted `j_xattr_dstream_t` claiming a 4 GB size), this would OOM before the bounds check fires. The capacity should be capped, or the total extent coverage should be used as the capacity instead.

## 4. `descendToLeaf` has no cycle or depth guard

**File:** `internal/filesystem/apfs/btree.go:187-206`

`descendToLeaf` loops from root to leaf with no depth limit:

```go
paddr := t.rootPaddr
for {
    n, raw, err := t.readNodeRaw(paddr)
    ...
```

A corrupted object map or B-tree node could form a cycle (node A points to child B, which points back to A). Since checksums are verified per-node, cycles between *valid* nodes are possible if the omap resolves to a legitimate physical address that is also an ancestor. The loop would spin forever. A depth cap of e.g. 64 (APFS max tree depth is ~10) would turn this into a clean error instead of a hang.

## 5. `insertXattr` rejects inserting at position 0 of a non-root leaf

**File:** `internal/filesystem/apfs/namedstream.go:414-415`

```go
if idx == 0 && !leafIsRoot {
    return nil, 0, errors.New("apfs: inserting before the first record of a non-root leaf is unsupported")
}
```

This rejects any xattr insert where the new key sorts before every existing key in the leaf. If a file's OID sorts before all other OIDs whose xattrs share the same leaf, this rejects a valid insert. The `lastLE` descent logic guarantees the search *arrives* at this leaf, so the insert position is correct; the only concern is that the leaf's minimum key would change, making the parent's separator stale. But since `lastLE` only requires the separator to be a lower bound (not exact), this is actually safe. The rejection is overly conservative and will silently fail for users in certain OID orderings.

## 6. `encodeLeaf` does not clear bytes between offset 56 and `tocStart`

**File:** `internal/filesystem/apfs/btree_write.go:116-118`

```go
for i := tocStart; i < valEnd; i++ {
    raw[i] = 0
}
```

If `tsOff > 0` (the table space starts later than byte 56), bytes `[56, tocStart)` are not zeroed. These bytes could contain stale data from the previous encoding. While `decodeNode` also starts reading at `tocStart`, leaving stale bytes in a node that gets checksum-verified is a forensic cleanliness issue -- an analyst could recover old key/value fragments from the gap. In practice `tsOff` is almost always 0, but it is not validated.

## 7. `writeXattr` does not update `totalBytesRead` on stream-backed xattrs

**File:** `internal/filesystem/apfs/namedstream.go:339-341`

```go
ds := *r.stream
ds.size = uint64(len(data))
ds.totalBytesWritten = uint64(len(data))
```

`ds.totalBytesRead` retains the old value from the original record. After a write, `totalBytesRead` no longer matches `size`. While APFS does not strictly enforce this consistency, a forensic tool comparing old and new dstream metadata would see a stale `totalBytesRead`. It should be reset to match `size`, or at least documented as intentionally left alone.

## 8. `updateBtreeInfo` only grows, never shrinks longest-key/longest-val

**File:** `internal/filesystem/apfs/btree_write.go:144-155`

```go
func updateBtreeInfo(raw []byte, keyCount, longestKey, longestVal int) {
    ...
    if uint32(longestKey) > binary.LittleEndian.Uint32(info[btInfoLongestKey:]) {
        binary.LittleEndian.PutUint32(info[btInfoLongestKey:], uint32(longestKey))
    }
```

If you replace a long value with a short one, `longestVal` in the new record set is smaller, but the `btree_info_t` retains the old maximum. This is not incorrect (overestimating is safe for allocation), but `bt_longest_key`/`bt_longest_val` monotonically grow and never contract, even when the shrink would be accurate. Over many hide/clear cycles this drifts the metadata.

## 9. `hide.go` close function bypasses the custody recorder

**File:** `cmd/hide.go:56-62`

```go
recorder := custody.Wrap(img)
fs, err := filesystem.Open(recorder)
...
return openedTarget{filesystem: fs, image: recorder, close: img.Close}, nil
```

The `close` is `img.Close` (the raw `*os.File`), not `recorder.Close`. This is fine because the recorder does not need closing, but the `image` field holds the recorder while `close` closes the underlying file. If someone later adds a `Close` method to the recorder (e.g., to flush custody logs), this would silently skip it. The `close` should be a closure that closes both, or the recorder should embed the close.

## 10. `TestXattrNodeFull` depends on fixture layout details

**File:** `internal/filesystem/apfs/namedstream_test.go:210-221`

```go
err := e.WriteStream("user.nemo.toobig", make([]byte, xattrMaxEmbeddedSize))
if err == nil || !strings.Contains(err.Error(), "node full") {
```

This test inserts a 3804-byte embedded xattr into a node that already has `user.nemo.test`. Whether this triggers "node full" depends on the existing node's `tsLen` (table space) and remaining key+value space. If the fixture is regenerated with a larger block size or different node layout, this test could silently start succeeding instead of failing as intended. The test should verify the exact available space in the leaf and compute the oversize value accordingly.

## 11. `readExtents`/`writeExtents` use `int64(phys) * int64(blockSize)` which can overflow

**File:** `internal/filesystem/apfs/namedstream.go:269, 298`

`ext.phys` is `uint64`, `f.blockSize` is `uint32`. The cast to `int64` before multiplication means that for `phys` values above `math.MaxInt64 / blockSize`, the product wraps negative, and the subsequent `ReadAt`/`WriteAt` call would use a negative offset. APFS physical addresses are bounded by image size, so this cannot happen with a valid image, but a crafted image with a large `phys` in an extent record would trigger it. The `readObject` function already validates `paddr < 0` for B-tree nodes; extent reads do not have an equivalent guard.
