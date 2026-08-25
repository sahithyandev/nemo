package apfs

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

// buildSyntheticTwoLeafTree writes a real, checksum-valid 3-node B-tree into
// a fake image: a root (non-leaf, level 1) with two children, each a leaf
// holding two single-byte-keyed records.
//
//	root  (paddr 0): keys [1, 3] -> children paddr 1, paddr 2
//	leaf0 (paddr 1): [1]=0xAA, [2]=0xBB
//	leaf1 (paddr 2): [3]=0xCC, [4]=0xDD
//
// This is the smallest tree that actually exercises cross-node traversal —
// every fixture-backed test in this package fits in a single leaf, so
// lastLE, descendLeftmost, and the cross-leaf branches of seek/next are
// otherwise never reached.
func buildSyntheticTwoLeafTree(t *testing.T) (img *fakefs.Image, blockSize uint32) {
	t.Helper()
	const bs = 256
	img = fakefs.NewImage(3 * bs)

	writeLeaf := func(paddr int64, k0, v0, k1, v1 byte) {
		buf := make([]byte, bs)
		binary.LittleEndian.PutUint16(buf[32:34], btnodeLeaf) // leaf, variable-kv, non-root
		binary.LittleEndian.PutUint32(buf[36:40], 2)          // nkeys
		binary.LittleEndian.PutUint16(buf[40:42], 0)          // table_space.off
		binary.LittleEndian.PutUint16(buf[42:44], 16)         // table_space.len: 2 kvloc_t

		// TOC (kvloc_t x2). keyBase = 56+0+16 = 72. valEnd = bs (non-root).
		binary.LittleEndian.PutUint16(buf[56:58], 0) // entry0 k.off
		binary.LittleEndian.PutUint16(buf[58:60], 1) // entry0 k.len
		binary.LittleEndian.PutUint16(buf[60:62], 1) // entry0 v.off (addr bs-1)
		binary.LittleEndian.PutUint16(buf[62:64], 1) // entry0 v.len
		binary.LittleEndian.PutUint16(buf[64:66], 1) // entry1 k.off
		binary.LittleEndian.PutUint16(buf[66:68], 1) // entry1 k.len
		binary.LittleEndian.PutUint16(buf[68:70], 2) // entry1 v.off (addr bs-2)
		binary.LittleEndian.PutUint16(buf[70:72], 1) // entry1 v.len

		buf[72] = k0
		buf[73] = k1
		buf[bs-1] = v0
		buf[bs-2] = v1

		binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))
		if _, err := img.WriteAt(buf, paddr*bs); err != nil {
			t.Fatalf("write leaf at paddr %d: %v", paddr, err)
		}
	}
	writeLeaf(1, 1, 0xAA, 2, 0xBB)
	writeLeaf(2, 3, 0xCC, 4, 0xDD)

	root := make([]byte, bs)
	binary.LittleEndian.PutUint16(root[32:34], btnodeRoot) // non-leaf, root, variable-kv
	binary.LittleEndian.PutUint16(root[34:36], 1)          // level
	binary.LittleEndian.PutUint32(root[36:40], 2)          // nkeys
	binary.LittleEndian.PutUint16(root[40:42], 0)          // table_space.off
	binary.LittleEndian.PutUint16(root[42:44], 16)         // table_space.len

	// keyBase = 72. valEnd = bs-40 (root adjustment) = 216.
	binary.LittleEndian.PutUint16(root[56:58], 0)  // entry0 k.off (key [1])
	binary.LittleEndian.PutUint16(root[58:60], 1)  // entry0 k.len
	binary.LittleEndian.PutUint16(root[60:62], 8)  // entry0 v.off (addr 216-8=208)
	binary.LittleEndian.PutUint16(root[62:64], 8)  // entry0 v.len (forced to 8 regardless)
	binary.LittleEndian.PutUint16(root[64:66], 1)  // entry1 k.off (key [3])
	binary.LittleEndian.PutUint16(root[66:68], 1)  // entry1 k.len
	binary.LittleEndian.PutUint16(root[68:70], 16) // entry1 v.off (addr 216-16=200)
	binary.LittleEndian.PutUint16(root[70:72], 8)  // entry1 v.len

	root[72] = 1                                    // key0
	root[73] = 3                                    // key1
	binary.LittleEndian.PutUint64(root[208:216], 1) // child for key [1]: leaf0, paddr 1
	binary.LittleEndian.PutUint64(root[200:208], 2) // child for key [3]: leaf1, paddr 2

	binary.LittleEndian.PutUint64(root[0:8], fletcher64(root))
	if _, err := img.WriteAt(root, 0); err != nil {
		t.Fatalf("write root: %v", err)
	}

	return img, bs
}

func byteCmp(a, b []byte) int { return bytes.Compare(a, b) }

func TestTreeSeekDescendsNonLeaf(t *testing.T) {
	img, bs := buildSyntheticTwoLeafTree(t)
	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}

	tests := []struct {
		name    string
		seek    byte
		wantKey byte
		wantVal byte
	}{
		{"before all keys descends leftmost", 0, 1, 0xAA},
		{"exact match in leaf0", 1, 1, 0xAA},
		{"exact match at second child boundary", 3, 3, 0xCC},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := tr.seek([]byte{tc.seek})
			if err != nil {
				t.Fatalf("seek(%d): %v", tc.seek, err)
			}
			k, v := c.key(), c.val()
			if len(k) != 1 || k[0] != tc.wantKey {
				t.Fatalf("key = %v, want [%d]", k, tc.wantKey)
			}
			if len(v) != 1 || v[0] != tc.wantVal {
				t.Fatalf("val = %v, want [%d]", v, tc.wantVal)
			}
			if err := c.err(); err != nil {
				t.Fatalf("err() = %v, want nil", err)
			}
		})
	}
}

// TestTreeSeekCrossesLeafBoundary is a direct regression test for the bug
// fixed in seek(): a target that sorts after every key in the leaf reached
// by descent (but before the next leaf's keys) must advance the cursor into
// that next leaf, not report "not found".
func TestTreeSeekCrossesLeafBoundary(t *testing.T) {
	img, bs := buildSyntheticTwoLeafTree(t)
	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}

	// [2,1] sorts after leaf0's last key [2] (since [2] is a prefix of
	// [2,1], the shorter one is lesser) but the root still descends into
	// leaf0 for it (its only other child starts at [3] > [2,1]). The
	// correct next record is leaf1's first: [3]=0xCC.
	c, err := tr.seek([]byte{2, 1})
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	k, v := c.key(), c.val()
	if len(k) != 1 || k[0] != 3 {
		t.Fatalf("key = %v, want [3]", k)
	}
	if len(v) != 1 || v[0] != 0xCC {
		t.Fatalf("val = %v, want [0xCC]", v)
	}
	if err := c.err(); err != nil {
		t.Fatalf("err() = %v, want nil", err)
	}
}

// TestTreeFullTraversal walks the entire tree via seek+next and confirms
// every record is visited in order exactly once, including the cross-leaf
// step from leaf0 to leaf1 in the middle of the walk.
func TestTreeFullTraversal(t *testing.T) {
	img, bs := buildSyntheticTwoLeafTree(t)
	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}

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
	wantVals := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	if !bytes.Equal(gotKeys, wantKeys) {
		t.Fatalf("keys = %v, want %v", gotKeys, wantKeys)
	}
	if !bytes.Equal(gotVals, wantVals) {
		t.Fatalf("vals = %v, want %v", gotVals, wantVals)
	}

	// One more next() past the end must report exhaustion cleanly, not an
	// error.
	if c.next() {
		t.Fatalf("next() past end = true, want false")
	}
	if err := c.err(); err != nil {
		t.Fatalf("err() after exhaustion = %v, want nil", err)
	}
	if k := c.key(); k != nil {
		t.Fatalf("key() after exhaustion = %v, want nil", k)
	}
}

func TestTreeSeekPastAllKeysFindsNothing(t *testing.T) {
	img, bs := buildSyntheticTwoLeafTree(t)
	tr, err := openTree(img, bs, 0, omapResolveIdentity, byteCmp, 0, 0)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}

	c, err := tr.seek([]byte{5})
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	if k := c.key(); k != nil {
		t.Fatalf("key() = %v, want nil (target past every record)", k)
	}
	if err := c.err(); err != nil {
		t.Fatalf("err() = %v, want nil", err)
	}
}
