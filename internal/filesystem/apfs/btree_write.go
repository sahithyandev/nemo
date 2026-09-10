package apfs

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// In-place B-tree leaf rewriting.
//
// APFS is copy-on-write: a faithful update would allocate new blocks, rewrite
// the root-to-leaf path, update the object map and write a new checkpoint.
// nemo does none of that. It re-encodes one leaf compactly at its existing
// physical address and recomputes the Fletcher-64 checksum, leaving oid, xid,
// the object map, the space manager and the checkpoint untouched. The volume
// stays checksum-valid and mountable; it is just not a real APFS transaction.
//
// Because a non-leaf node's key is only a lower bound on its child subtree
// (descent uses lastLE), inserting or deleting anywhere except the first
// record of a non-root leaf keeps every lookup correct without touching the
// parent. Changing a non-root leaf's minimum key is refused rather than
// silently corrupting the parent's separator.

// btree_info_t field offsets within the 40-byte trailer of a root node.
const (
	btInfoLongestKey = 16 // uint32
	btInfoLongestVal = 20 // uint32
	btInfoKeyCount   = 24 // uint64
)

// btoffInvalid is BTOFF_INVALID, the nloc_t offset marking an empty free list.
const btoffInvalid = 0xFFFF

// record is one key/value pair, owning copies of both byte slices.
type record struct {
	key []byte
	val []byte
}

func recordsFromNode(n *node) []record {
	recs := make([]record, len(n.keys))
	for i := range n.keys {
		recs[i] = record{
			key: append([]byte(nil), n.keys[i]...),
			val: append([]byte(nil), n.vals[i]...),
		}
	}
	return recs
}

func align8(n int) int { return (n + 7) &^ 7 }

func isRootNode(raw []byte) bool {
	return binary.LittleEndian.Uint16(raw[32:34])&btnodeRoot != 0
}

// encodeLeaf rewrites raw (one full block) in place as a variable-kv leaf
// btree_node_phys_t holding recs in the given order. It preserves obj_phys,
// btn_flags, btn_level and btn_table_space, repacks keys forward and values
// backward (8-byte aligned, matching Apple's layout), recomputes btn_nkeys and
// btn_free_space, and for a root leaf updates the trailing btree_info_t. It
// does not recompute the Fletcher-64 checksum; writeNodeBlock does that.
func encodeLeaf(raw []byte, blockSize uint32, recs []record) error {
	if uint32(len(raw)) != blockSize || len(raw) < 56 {
		return errors.New("apfs: encodeLeaf: bad block buffer")
	}
	flags := binary.LittleEndian.Uint16(raw[32:34])
	if flags&btnodeLeaf == 0 {
		return errors.New("apfs: encodeLeaf: not a leaf node")
	}
	if flags&btnodeFixedKVSize != 0 {
		return errors.New("apfs: encodeLeaf: fixed-kv leaves are not rewritten")
	}
	isRoot := flags&btnodeRoot != 0

	tsOff := int(binary.LittleEndian.Uint16(raw[40:42]))
	tsLen := int(binary.LittleEndian.Uint16(raw[42:44]))
	tocStart := 56 + tsOff
	tocEnd := tocStart + tsLen
	if tocStart < 56 || tocEnd > len(raw) {
		return errors.New("apfs: encodeLeaf: table space out of range")
	}
	if len(recs)*8 > tsLen {
		return fmt.Errorf("apfs: node full: table space holds %d entries, need %d", tsLen/8, len(recs))
	}
	keyBase := tocEnd

	valEnd := int(blockSize)
	if isRoot {
		valEnd -= btreeInfoSize
	}

	type loc struct{ kOff, kLen, vOff, vLen int }
	locs := make([]loc, len(recs))
	keyPos, valPos := keyBase, valEnd
	longestKey, longestVal := 0, 0
	for i, r := range recs {
		if len(r.key) > longestKey {
			longestKey = len(r.key)
		}
		if len(r.val) > longestVal {
			longestVal = len(r.val)
		}
		kAt := keyPos
		kEnd := align8(kAt + len(r.key))
		vAt := (valPos - len(r.val)) &^ 7
		if vAt < kEnd {
			return fmt.Errorf("apfs: node full: cannot fit %d records in a %d-byte block", len(recs), blockSize)
		}
		locs[i] = loc{kOff: kAt - keyBase, kLen: len(r.key), vOff: valEnd - vAt, vLen: len(r.val)}
		keyPos, valPos = kEnd, vAt
	}

	// Clear the whole dynamic area, then lay records back down. The root's
	// trailing btree_info_t (valEnd..blockSize) is preserved.
	for i := tocStart; i < valEnd; i++ {
		raw[i] = 0
	}
	for i, r := range recs {
		l := locs[i]
		ent := tocStart + i*8
		binary.LittleEndian.PutUint16(raw[ent:], uint16(l.kOff))
		binary.LittleEndian.PutUint16(raw[ent+2:], uint16(l.kLen))
		binary.LittleEndian.PutUint16(raw[ent+4:], uint16(l.vOff))
		binary.LittleEndian.PutUint16(raw[ent+6:], uint16(l.vLen))
		copy(raw[keyBase+l.kOff:], r.key)
		copy(raw[valEnd-l.vOff:], r.val)
	}

	binary.LittleEndian.PutUint32(raw[36:40], uint32(len(recs)))
	binary.LittleEndian.PutUint16(raw[44:46], uint16(keyPos-keyBase)) // btn_free_space.off
	binary.LittleEndian.PutUint16(raw[46:48], uint16(valPos-keyPos))  // btn_free_space.len
	binary.LittleEndian.PutUint16(raw[48:50], btoffInvalid)           // btn_key_free_list.off
	binary.LittleEndian.PutUint16(raw[50:52], 0)
	binary.LittleEndian.PutUint16(raw[52:54], btoffInvalid) // btn_val_free_list.off
	binary.LittleEndian.PutUint16(raw[54:56], 0)

	if isRoot {
		updateBtreeInfo(raw, len(recs), longestKey, longestVal)
	}
	return nil
}

// updateBtreeInfo grows the longest-key/longest-val hints and sets the
// tree-wide key count in a root node's trailing btree_info_t.
func updateBtreeInfo(raw []byte, keyCount, longestKey, longestVal int) {
	info := raw[len(raw)-btreeInfoSize:]
	if uint32(longestKey) > binary.LittleEndian.Uint32(info[btInfoLongestKey:]) {
		binary.LittleEndian.PutUint32(info[btInfoLongestKey:], uint32(longestKey))
	}
	if uint32(longestVal) > binary.LittleEndian.Uint32(info[btInfoLongestVal:]) {
		binary.LittleEndian.PutUint32(info[btInfoLongestVal:], uint32(longestVal))
	}
	binary.LittleEndian.PutUint64(info[btInfoKeyCount:], uint64(keyCount))
}

// writeNodeBlock recomputes the Fletcher-64 checksum over raw and writes it
// back to its physical address.
func (t *tree) writeNodeBlock(paddr int64, raw []byte) error {
	if uint32(len(raw)) != t.blockSize {
		return errors.New("apfs: writeNodeBlock: buffer is not one block")
	}
	binary.LittleEndian.PutUint64(raw[0:8], fletcher64(raw))
	off := paddr * int64(t.blockSize)
	if off < 0 || off+int64(len(raw)) > t.img.Size() {
		return fmt.Errorf("apfs: writeNodeBlock: address %d out of range", paddr)
	}
	n, err := t.img.WriteAt(raw, off)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return fmt.Errorf("apfs: short node write: %d of %d bytes", n, len(raw))
	}
	return nil
}

// bumpRootKeyCount adjusts the tree-wide key count in the root's btree_info_t
// after an insert or delete in a non-root leaf, and grows the longest-key/val
// hints. It is a no-op when the root is itself a leaf (encodeLeaf already
// handled it).
func (t *tree) bumpRootKeyCount(delta, longestKey, longestVal int) error {
	n, raw, err := t.readNodeRaw(t.rootPaddr)
	if err != nil {
		return err
	}
	if n.leaf {
		return nil
	}
	info := raw[len(raw)-btreeInfoSize:]
	count := int64(binary.LittleEndian.Uint64(info[btInfoKeyCount:])) + int64(delta)
	if count < 0 {
		count = 0
	}
	binary.LittleEndian.PutUint64(info[btInfoKeyCount:], uint64(count))
	if uint32(longestKey) > binary.LittleEndian.Uint32(info[btInfoLongestKey:]) {
		binary.LittleEndian.PutUint32(info[btInfoLongestKey:], uint32(longestKey))
	}
	if uint32(longestVal) > binary.LittleEndian.Uint32(info[btInfoLongestVal:]) {
		binary.LittleEndian.PutUint32(info[btInfoLongestVal:], uint32(longestVal))
	}
	return t.writeNodeBlock(t.rootPaddr, raw)
}

// rewriteLeaf descends to the leaf where key belongs, hands its records to
// mutate, and writes the result back in place. mutate returns the new record
// list and the change in record count (+1 insert, -1 delete, 0 replace).
func (t *tree) rewriteLeaf(key []byte, mutate func(recs []record, leafIsRoot bool) ([]record, int, error)) error {
	n, paddr, raw, err := t.descendToLeaf(key)
	if err != nil {
		return err
	}
	leafIsRoot := isRootNode(raw)
	newRecs, delta, err := mutate(recordsFromNode(n), leafIsRoot)
	if err != nil {
		return err
	}
	longestKey, longestVal := 0, 0
	for _, r := range newRecs {
		if len(r.key) > longestKey {
			longestKey = len(r.key)
		}
		if len(r.val) > longestVal {
			longestVal = len(r.val)
		}
	}
	if err := encodeLeaf(raw, t.blockSize, newRecs); err != nil {
		return err
	}
	if err := t.writeNodeBlock(paddr, raw); err != nil {
		return err
	}
	if delta != 0 && paddr != t.rootPaddr {
		return t.bumpRootKeyCount(delta, longestKey, longestVal)
	}
	return nil
}
