package apfs

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

// buildNode lays out a minimal btree_node_phys_t. header sets flags/level/
// nkeys/table_space at their real offsets; the caller fills the TOC and
// key/value bytes into the returned buffer directly.
func buildNode(blockSize int, flags, level uint16, nkeys uint32, tocLen uint16) []byte {
	buf := make([]byte, blockSize)
	binary.LittleEndian.PutUint16(buf[32:34], flags)
	binary.LittleEndian.PutUint16(buf[34:36], level)
	binary.LittleEndian.PutUint32(buf[36:40], nkeys)
	binary.LittleEndian.PutUint16(buf[40:42], 0)      // btn_table_space.off
	binary.LittleEndian.PutUint16(buf[42:44], tocLen) // btn_table_space.len
	return buf
}

// TestBtreeNodeDecodeFixedKVLeaf covers the fixed-kv TOC layout (kvoff_t),
// as used by the object map tree: a leaf node with two 4-byte keys and
// 4-byte values, addressed forward from the TOC (keys) and backward from
// the end of the block (values).
func TestBtreeNodeDecodeFixedKVLeaf(t *testing.T) {
	const blockSize = 128
	buf := buildNode(blockSize, btnodeLeaf|btnodeFixedKVSize, 0, 2, 8)

	// TOC: kvoff_t{k,v} x2. keyBase = 56+0+8 = 64. valEnd = 128 (non-root).
	binary.LittleEndian.PutUint16(buf[56:58], 0)  // key0 at keyBase+0 = 64
	binary.LittleEndian.PutUint16(buf[58:60], 8)  // val0 at valEnd-8 = 120
	binary.LittleEndian.PutUint16(buf[60:62], 4)  // key1 at keyBase+4 = 68
	binary.LittleEndian.PutUint16(buf[62:64], 12) // val1 at valEnd-12 = 116

	copy(buf[64:68], []byte{0, 0, 0, 1})    // key0
	copy(buf[68:72], []byte{0, 0, 0, 2})    // key1
	copy(buf[116:120], []byte{0, 0, 0, 20}) // val1
	copy(buf[120:124], []byte{0, 0, 0, 10}) // val0

	n, err := decodeNode(buf, blockSize, 4, 4)
	if err != nil {
		t.Fatalf("decodeNode: %v", err)
	}
	if !n.leaf {
		t.Fatalf("leaf = false, want true")
	}
	if len(n.keys) != 2 || len(n.vals) != 2 {
		t.Fatalf("got %d keys, %d vals; want 2, 2", len(n.keys), len(n.vals))
	}
	if !bytes.Equal(n.keys[0], []byte{0, 0, 0, 1}) {
		t.Errorf("keys[0] = %v, want [0 0 0 1]", n.keys[0])
	}
	if !bytes.Equal(n.keys[1], []byte{0, 0, 0, 2}) {
		t.Errorf("keys[1] = %v, want [0 0 0 2]", n.keys[1])
	}
	if !bytes.Equal(n.vals[0], []byte{0, 0, 0, 10}) {
		t.Errorf("vals[0] = %v, want [0 0 0 10]", n.vals[0])
	}
	if !bytes.Equal(n.vals[1], []byte{0, 0, 0, 20}) {
		t.Errorf("vals[1] = %v, want [0 0 0 20]", n.vals[1])
	}
}

// TestBtreeNodeDecodeVariableKVRoot covers the variable-kv TOC layout
// (kvloc_t) and the root-node value-area adjustment: a root, non-leaf node
// whose value area ends btreeInfoSize bytes before the block end to make
// room for the trailing btree_info_t.
func TestBtreeNodeDecodeVariableKVRoot(t *testing.T) {
	const blockSize = 128
	buf := buildNode(blockSize, btnodeRoot, 1, 1, 8)

	// TOC: one kvloc_t{k{off,len}, v{off,len}}. keyBase = 56+0+8 = 64.
	// valEnd = 128-40 = 88 (root adjustment).
	binary.LittleEndian.PutUint16(buf[56:58], 0) // k.off: key at keyBase+0 = 64
	binary.LittleEndian.PutUint16(buf[58:60], 8) // k.len: 8 bytes
	binary.LittleEndian.PutUint16(buf[60:62], 8) // v.off: val at valEnd-8 = 80
	binary.LittleEndian.PutUint16(buf[62:64], 8) // v.len: 8 bytes (oid_t)

	copy(buf[64:72], []byte{9, 9, 9, 9, 9, 9, 9, 9}) // key
	binary.LittleEndian.PutUint64(buf[80:88], 42)    // child oid

	n, err := decodeNode(buf, blockSize, 0, 0)
	if err != nil {
		t.Fatalf("decodeNode: %v", err)
	}
	if n.leaf {
		t.Fatalf("leaf = true, want false")
	}
	if n.level != 1 {
		t.Fatalf("level = %d, want 1", n.level)
	}
	if len(n.keys) != 1 || len(n.vals) != 1 {
		t.Fatalf("got %d keys, %d vals; want 1, 1", len(n.keys), len(n.vals))
	}
	if !bytes.Equal(n.keys[0], []byte{9, 9, 9, 9, 9, 9, 9, 9}) {
		t.Errorf("keys[0] = %v, want all-9", n.keys[0])
	}
	gotOid := binary.LittleEndian.Uint64(n.vals[0])
	if gotOid != 42 {
		t.Errorf("child oid = %d, want 42", gotOid)
	}
}

// TestBtreeNodeDecodeNonLeafRejectsShortValue is a regression test: a
// malformed non-leaf node whose TOC claims a value shorter than 8 bytes
// must be rejected with a bounds error at decode time, not accepted and
// then panic later when a caller reads the child oid with
// binary.LittleEndian.Uint64 on a too-short slice.
func TestBtreeNodeDecodeNonLeafRejectsShortValue(t *testing.T) {
	const blockSize = 128
	buf := buildNode(blockSize, 0, 1, 1, 8) // non-leaf, non-root, variable-kv

	// TOC: kvloc_t claiming k.len=8, v.len=2 (malformed for a non-leaf
	// value). v.off=2 places the value at valAddr=126, which fits a 2-byte
	// read (126+2=128) but not the real, forced 8-byte oid_t read
	// (126+8=134 > 128) — the case that used to panic.
	binary.LittleEndian.PutUint16(buf[56:58], 0) // k.off
	binary.LittleEndian.PutUint16(buf[58:60], 8) // k.len
	binary.LittleEndian.PutUint16(buf[60:62], 2) // v.off
	binary.LittleEndian.PutUint16(buf[62:64], 2) // v.len (malformed)

	copy(buf[64:72], []byte{1, 2, 3, 4, 5, 6, 7, 8}) // key

	if _, err := decodeNode(buf, blockSize, 0, 0); err == nil {
		t.Fatalf("decodeNode: expected out-of-range error for short non-leaf value, got nil")
	}
}

func TestJKeyRoundTrip(t *testing.T) {
	tests := []struct {
		oid uint64
		typ uint8
	}{
		{2, objTypeDirRec},
		{1<<60 - 1, objTypeXattr},
		{0, objTypeInode},
	}
	for _, tc := range tests {
		k := encodeJKey(tc.oid, tc.typ)
		oid, typ, err := decodeJKey(k)
		if err != nil {
			t.Fatalf("decodeJKey: %v", err)
		}
		if oid != tc.oid || typ != tc.typ {
			t.Errorf("round-trip (%d,%d) -> (%d,%d)", tc.oid, tc.typ, oid, typ)
		}
	}
}

func TestOpenTreeRejectsNegativeRootPaddr(t *testing.T) {
	img := fakefs.NewImage(64)
	if _, err := openTree(img, 64, -1, omapResolveIdentity, byteCmp, 0, 0); err == nil {
		t.Fatalf("openTree(rootPaddr=-1): expected error, got nil")
	}
}

func TestCursorKeyValOnEmptyStack(t *testing.T) {
	var c cursor
	if k := c.key(); k != nil {
		t.Fatalf("key() on empty cursor = %v, want nil", k)
	}
	if v := c.val(); v != nil {
		t.Fatalf("val() on empty cursor = %v, want nil", v)
	}
}

func TestFsKeyCompareOrdersByOidThenType(t *testing.T) {
	// Type occupies the high nibble, so a raw u64 comparison would sort by
	// type first; fsKeyCompare must not do that.
	low := encodeJKey(1, objTypeDirRec) // high type nibble
	high := encodeJKey(2, objTypeInode) // low type nibble, but larger oid
	if fsKeyCompare(low, high) >= 0 {
		t.Fatalf("fsKeyCompare(oid=1,type=DIR_REC, oid=2,type=INODE) >= 0, want < 0 (sorts by oid first)")
	}
}

func TestDecodeDrecKeyHashed(t *testing.T) {
	// j_drec_hashed_key_t: j_key_t(8) + name_len_and_hash u32 + name.
	name := "hello.txt"
	nameBytes := append([]byte(name), 0) // NUL-terminated on disk
	k := make([]byte, 12+len(nameBytes))
	copy(k[0:8], encodeJKey(2, objTypeDirRec))
	lenAndHash := uint32(len(nameBytes)) // low 10 bits = length incl. NUL; hash bits left 0
	binary.LittleEndian.PutUint32(k[8:12], lenAndHash)
	copy(k[12:], nameBytes)

	got, ok := decodeDrecKey(k)
	if !ok {
		t.Fatalf("decodeDrecKey: not ok")
	}
	if got != name {
		t.Errorf("name = %q, want %q", got, name)
	}
}

// TestDecodeDrecKeyNonHashedFallback uses a key shorter than 12 bytes, so
// decodeDrecKey can't even attempt the hashed j_drec_hashed_key_t layout
// and must fall back to the plain j_drec_key_t (bare u16 length) layout.
func TestDecodeDrecKeyNonHashedFallback(t *testing.T) {
	k := make([]byte, 11) // 8 (j_key_t) + 2 (u16 len) + 1 (name, no NUL)
	copy(k[0:8], encodeJKey(2, objTypeDirRec))
	binary.LittleEndian.PutUint16(k[8:10], 1)
	k[10] = 'A'

	got, ok := decodeDrecKey(k)
	if !ok {
		t.Fatalf("decodeDrecKey: not ok")
	}
	if got != "A" {
		t.Errorf("name = %q, want %q", got, "A")
	}
}

func TestIsPrintableNameRejectsEmpty(t *testing.T) {
	if isPrintableName(nil) {
		t.Fatalf("isPrintableName(nil) = true, want false")
	}
}

// TestCursorDescendLeftmostRejectsEmptyNonLeaf covers the defensive check
// in descendLeftmost for a malformed non-leaf node with zero keys (so it
// has no child to follow index 0 into).
func TestCursorDescendLeftmostRejectsEmptyNonLeaf(t *testing.T) {
	const bs = 128
	buf := make([]byte, bs)
	binary.LittleEndian.PutUint16(buf[32:34], btnodeRoot) // non-leaf, root, 0 keys
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))

	img := fakefs.NewImage(bs)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write empty non-leaf node: %v", err)
	}

	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}
	c := &cursor{t: tr}
	if err := c.descendLeftmost(0); err == nil {
		t.Fatalf("descendLeftmost: expected error for empty non-leaf node, got nil")
	}
}
