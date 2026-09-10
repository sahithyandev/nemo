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
