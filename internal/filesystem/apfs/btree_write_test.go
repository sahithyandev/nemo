package apfs

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestEncodeLeafRoundTrip encodes a record set into a leaf block and decodes
// it back, checking the header bookkeeping and a root node's btree_info_t.
func TestEncodeLeafRoundTrip(t *testing.T) {
	const bs = 4096
	raw := make([]byte, bs)
	binary.LittleEndian.PutUint16(raw[32:34], btnodeLeaf|btnodeRoot)
	binary.LittleEndian.PutUint16(raw[42:44], 64) // table space: room for 8 entries

	recs := []record{
		{key: []byte("aaa"), val: []byte("first value")},
		{key: []byte("bbbbbb"), val: []byte("v2")},
		{key: []byte("cc"), val: bytes.Repeat([]byte("x"), 40)},
	}
	if err := encodeLeaf(raw, bs, recs); err != nil {
		t.Fatalf("encodeLeaf: %v", err)
	}

	n, err := decodeNode(raw, bs, 0, 0)
	if err != nil {
		t.Fatalf("decodeNode: %v", err)
	}
	if len(n.keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(n.keys))
	}
	for i, r := range recs {
		if !bytes.Equal(n.keys[i], r.key) {
			t.Errorf("keys[%d] = %q, want %q", i, n.keys[i], r.key)
		}
		if !bytes.Equal(n.vals[i], r.val) {
			t.Errorf("vals[%d] = %q, want %q", i, n.vals[i], r.val)
		}
	}
	info := raw[bs-btreeInfoSize:]
	if kc := binary.LittleEndian.Uint64(info[btInfoKeyCount:]); kc != 3 {
		t.Errorf("bt_key_count = %d, want 3", kc)
	}
	if lk := binary.LittleEndian.Uint32(info[btInfoLongestKey:]); lk != 6 {
		t.Errorf("bt_longest_key = %d, want 6", lk)
	}
}

// TestEncodeLeafRootShrinksLongestHints confirms that for a root-as-leaf
// (single-node) tree, replacing the record set with smaller keys/values
// lowers bt_longest_key/bt_longest_val, not just raises them. This is exact
// for a single-node tree because the leaf being rewritten is the whole tree.
func TestEncodeLeafRootShrinksLongestHints(t *testing.T) {
	const bs = 4096
	raw := make([]byte, bs)
	binary.LittleEndian.PutUint16(raw[32:34], btnodeLeaf|btnodeRoot)
	binary.LittleEndian.PutUint16(raw[42:44], 64)

	if err := encodeLeaf(raw, bs, []record{{key: []byte("a"), val: bytes.Repeat([]byte("x"), 100)}}); err != nil {
		t.Fatalf("encodeLeaf (large): %v", err)
	}
	info := raw[bs-btreeInfoSize:]
	if lv := binary.LittleEndian.Uint32(info[btInfoLongestVal:]); lv != 100 {
		t.Fatalf("bt_longest_val after large write = %d, want 100", lv)
	}

	if err := encodeLeaf(raw, bs, []record{{key: []byte("a"), val: []byte("y")}}); err != nil {
		t.Fatalf("encodeLeaf (small): %v", err)
	}
	info = raw[bs-btreeInfoSize:]
	if lv := binary.LittleEndian.Uint32(info[btInfoLongestVal:]); lv != 1 {
		t.Fatalf("bt_longest_val after shrink = %d, want 1 (should shrink, not stay at the old max)", lv)
	}
	if kc := binary.LittleEndian.Uint64(info[btInfoKeyCount:]); kc != 1 {
		t.Fatalf("bt_key_count = %d, want 1", kc)
	}
}

func TestEncodeLeafNodeFull(t *testing.T) {
	const bs = 256
	raw := make([]byte, bs)
	binary.LittleEndian.PutUint16(raw[32:34], btnodeLeaf)
	binary.LittleEndian.PutUint16(raw[42:44], 16)

	recs := []record{{key: bytes.Repeat([]byte("k"), 120), val: bytes.Repeat([]byte("v"), 120)}}
	if err := encodeLeaf(raw, bs, recs); err == nil {
		t.Fatalf("encodeLeaf: expected a node-full error")
	}
}

// TestRewriteLeafNonRoot exercises descendToLeaf + rewriteLeaf +
// bumpRootKeyCount against the synthetic two-leaf tree: replace a value in a
// non-root leaf and confirm the tree still reads back correctly.
func TestRewriteLeafNonRoot(t *testing.T) {
	img, bs := buildSyntheticTwoLeafTree(t)
	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}

	err = tr.rewriteLeaf([]byte{3}, func(recs []record, _ bool) ([]record, int, error) {
		for i := range recs {
			if bytes.Equal(recs[i].key, []byte{3}) {
				recs[i].val = []byte{0xEE}
				return recs, 0, nil
			}
		}
		t.Fatalf("key [3] not found in leaf")
		return nil, 0, nil
	})
	if err != nil {
		t.Fatalf("rewriteLeaf: %v", err)
	}

	c, err := tr.seek([]byte{3})
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	if v := c.val(); len(v) != 1 || v[0] != 0xEE {
		t.Fatalf("val after rewrite = %v, want [0xEE]", v)
	}
	// The neighbouring record in the same leaf must be intact.
	if !c.next() || c.key()[0] != 4 || c.val()[0] != 0xDD {
		t.Fatalf("sibling record disturbed")
	}
}

// TestRewriteLeafReplaceGrowsRootLongestVal confirms an in-place replace
// (delta stays 0) in a non-root leaf still refreshes the root's
// bt_longest_val hint when the new value is longer than anything seen
// before. bumpRootKeyCount must run on a replace, not just an insert or
// delete, or a grown value's length never reaches the root.
func TestRewriteLeafReplaceGrowsRootLongestVal(t *testing.T) {
	img, bs := buildSyntheticTwoLeafTree(t)
	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}

	longVal := bytes.Repeat([]byte{0xEE}, 5)
	err = tr.rewriteLeaf([]byte{3}, func(recs []record, _ bool) ([]record, int, error) {
		for i := range recs {
			if bytes.Equal(recs[i].key, []byte{3}) {
				recs[i].val = longVal
				return recs, 0, nil // in-place replace: delta stays 0
			}
		}
		t.Fatalf("key [3] not found in leaf")
		return nil, 0, nil
	})
	if err != nil {
		t.Fatalf("rewriteLeaf: %v", err)
	}

	root, err := readObject(img, 0, bs)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	info := root[len(root)-btreeInfoSize:]
	if lv := binary.LittleEndian.Uint32(info[btInfoLongestVal:]); lv != uint32(len(longVal)) {
		t.Fatalf("root bt_longest_val = %d, want %d (a replace must still update it)", lv, len(longVal))
	}
}

// TestRewriteLeafFirstRecordDeleteThenInsert exercises the case the
// btree_write.go doc comment argues is safe: deleting the first record of a
// non-root leaf (leaving the parent's separator as a now-loose lower bound),
// then inserting a new key that lands below the leaf's current minimum but
// still above that stale separator. Both mutations touch only the leaf; if
// either broke the separator invariant, the full-tree walk below would skip
// or duplicate a record.
func TestRewriteLeafFirstRecordDeleteThenInsert(t *testing.T) {
	img, bs := buildSyntheticTwoLeafTree(t)
	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}

	// leaf1 (root separator key [3]) holds [3]=0xCC, [4]=0xDD. Delete [3],
	// its first record: leaf1's true minimum rises to [4], while the root's
	// separator for it stays [3] (now a loose, not exact, lower bound).
	err = tr.rewriteLeaf([]byte{3}, func(recs []record, leafIsRoot bool) ([]record, int, error) {
		if leafIsRoot {
			t.Fatalf("leaf1 must not be the root in this fixture")
		}
		if len(recs) != 2 || !bytes.Equal(recs[0].key, []byte{3}) {
			t.Fatalf("unexpected leaf1 records: %+v", recs)
		}
		return recs[1:], -1, nil
	})
	if err != nil {
		t.Fatalf("delete [3]: %v", err)
	}

	// Re-insert [3] with a new value. It must land at index 0 of leaf1 (below
	// the leaf's current minimum, [4]) while still routing there via the root
	// separator [3], which is still <= the new key.
	err = tr.rewriteLeaf([]byte{3}, func(recs []record, leafIsRoot bool) ([]record, int, error) {
		if leafIsRoot {
			t.Fatalf("leaf1 must not be the root in this fixture")
		}
		if len(recs) != 1 || !bytes.Equal(recs[0].key, []byte{4}) {
			t.Fatalf("unexpected leaf1 records before insert: %+v", recs)
		}
		return append([]record{{key: []byte{3}, val: []byte{0x11}}}, recs...), 1, nil
	})
	if err != nil {
		t.Fatalf("insert [3]: %v", err)
	}

	// The whole tree, across both leaves and the boundary between them, must
	// still read back in order with every value correct.
	c, err := tr.seek([]byte{0})
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	var gotKeys, gotVals []byte
	for k := c.key(); k != nil; k = c.key() {
		gotKeys = append(gotKeys, k[0])
		gotVals = append(gotVals, c.val()[0])
		if !c.next() {
			break
		}
	}
	if err := c.err(); err != nil {
		t.Fatalf("err() = %v, want nil", err)
	}
	wantKeys := []byte{1, 2, 3, 4}
	wantVals := []byte{0xAA, 0xBB, 0x11, 0xDD}
	if !bytes.Equal(gotKeys, wantKeys) {
		t.Fatalf("keys = %v, want %v", gotKeys, wantKeys)
	}
	if !bytes.Equal(gotVals, wantVals) {
		t.Fatalf("vals = %v, want %v", gotVals, wantVals)
	}
}
