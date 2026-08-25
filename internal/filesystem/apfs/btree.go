package apfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/sahithyandev/nemo/internal/binutil"
)

// btree_node_phys_t flag bits (btn_flags).
const (
	btnodeRoot        = 0x0001
	btnodeLeaf        = 0x0002
	btnodeFixedKVSize = 0x0004
)

// btreeInfoSize is sizeof(btree_info_t), trailing every root node.
const btreeInfoSize = 40

// node is a decoded btree_node_phys_t: parallel slices of key/value byte
// slices (subslices of the node's block buffer — copy before the buffer is
// discarded).
type node struct {
	level uint16
	leaf  bool
	keys  [][]byte
	vals  [][]byte
}

// decodeNode parses a btree_node_phys_t out of buf (one full block,
// checksum already verified by the caller via readObject).
// fixedKeySize/fixedValSize are used only when the node's own
// btnodeFixedKVSize flag is set (e.g. the object map tree); pass 0,0 for a
// tree that is always variable-kv (e.g. the filesystem tree).
func decodeNode(buf []byte, blockSize uint32, fixedKeySize, fixedValSize int) (*node, error) {
	if len(buf) < 56 {
		return nil, errors.New("apfs: btree node shorter than header")
	}
	flags := binary.LittleEndian.Uint16(buf[32:34])
	level := binary.LittleEndian.Uint16(buf[34:36])
	nkeys := binary.LittleEndian.Uint32(buf[36:40])
	tsOff := binary.LittleEndian.Uint16(buf[40:42])
	tsLen := binary.LittleEndian.Uint16(buf[42:44])

	isRoot := flags&btnodeRoot != 0
	isLeaf := flags&btnodeLeaf != 0
	isFixedKV := flags&btnodeFixedKVSize != 0

	tocStart := 56 + int(tsOff)
	tocEnd := tocStart + int(tsLen)
	if tocStart < 56 || tocEnd > len(buf) {
		return nil, errors.New("apfs: btree node TOC out of range")
	}
	keyBase := tocEnd

	valEnd := int(blockSize)
	if isRoot {
		valEnd -= btreeInfoSize
	}
	if valEnd < 0 || valEnd > len(buf) {
		return nil, errors.New("apfs: btree node value area out of range")
	}

	n := &node{level: level, leaf: isLeaf}

	if isFixedKV {
		const entrySize = 4 // kvoff_t: {k uint16, v uint16}
		for i := 0; i < int(nkeys); i++ {
			entOff := tocStart + i*entrySize
			if entOff+entrySize > tocEnd {
				return nil, errors.New("apfs: btree TOC entry out of range")
			}
			k := binary.LittleEndian.Uint16(buf[entOff : entOff+2])
			v := binary.LittleEndian.Uint16(buf[entOff+2 : entOff+4])

			keyAddr := keyBase + int(k)
			keyLen := fixedKeySize
			if keyAddr < 0 || keyAddr+keyLen > len(buf) {
				return nil, errors.New("apfs: btree fixed key out of range")
			}

			valLen := fixedValSize
			if !isLeaf {
				valLen = 8 // non-leaf value is always an 8-byte oid_t child pointer
			}
			valAddr := valEnd - int(v)
			if valAddr < 0 || valAddr+valLen > len(buf) {
				return nil, errors.New("apfs: btree fixed value out of range")
			}

			n.keys = append(n.keys, buf[keyAddr:keyAddr+keyLen])
			n.vals = append(n.vals, buf[valAddr:valAddr+valLen])
		}
		return n, nil
	}

	const entrySize = 8 // kvloc_t: {k nloc_t, v nloc_t}, nloc_t = {off, len} uint16
	for i := 0; i < int(nkeys); i++ {
		entOff := tocStart + i*entrySize
		if entOff+entrySize > tocEnd {
			return nil, errors.New("apfs: btree TOC entry out of range")
		}
		kOff := binary.LittleEndian.Uint16(buf[entOff : entOff+2])
		kLen := binary.LittleEndian.Uint16(buf[entOff+2 : entOff+4])
		vOff := binary.LittleEndian.Uint16(buf[entOff+4 : entOff+6])
		vLen := int(binary.LittleEndian.Uint16(buf[entOff+6 : entOff+8]))
		if !isLeaf {
			// Non-leaf value is always an 8-byte oid_t child pointer,
			// regardless of what the TOC claims — vals[i] later gets read
			// with binary.LittleEndian.Uint64, so trusting an attacker- or
			// corruption-controlled vLen < 8 here would panic downstream
			// instead of failing as a bounds error.
			vLen = 8
		}

		keyAddr := keyBase + int(kOff)
		if keyAddr < 0 || keyAddr+int(kLen) > len(buf) {
			return nil, errors.New("apfs: btree variable key out of range")
		}
		valAddr := valEnd - int(vOff)
		if valAddr < 0 || valAddr+vLen > len(buf) {
			return nil, errors.New("apfs: btree variable value out of range")
		}

		n.keys = append(n.keys, buf[keyAddr:keyAddr+int(kLen)])
		n.vals = append(n.vals, buf[valAddr:valAddr+vLen])
	}
	return n, nil
}

// tree is a read-only handle on one APFS B-tree.
type tree struct {
	img                    imageReader
	blockSize              uint32
	rootPaddr              int64
	resolve                func(oid uint64) (int64, error)
	cmp                    func(a, b []byte) int
	fixedKeySize, fixedVal int
}

func openTree(img imageReader, blockSize uint32, rootPaddr int64, resolve func(uint64) (int64, error), cmp func(a, b []byte) int, fixedKeySize, fixedValSize int) (*tree, error) {
	if rootPaddr < 0 {
		return nil, fmt.Errorf("apfs: invalid btree root address %d", rootPaddr)
	}
	return &tree{
		img: img, blockSize: blockSize, rootPaddr: rootPaddr,
		resolve: resolve, cmp: cmp,
		fixedKeySize: fixedKeySize, fixedVal: fixedValSize,
	}, nil
}

func (t *tree) readNode(paddr int64) (*node, error) {
	buf, err := readObject(t.img, paddr, t.blockSize)
	if err != nil {
		return nil, err
	}
	return decodeNode(buf, t.blockSize, t.fixedKeySize, t.fixedVal)
}

// firstGE returns the index of the first key >= target (len(keys) if none).
func firstGE(keys [][]byte, target []byte, cmp func(a, b []byte) int) int {
	return sort.Search(len(keys), func(i int) bool { return cmp(keys[i], target) >= 0 })
}

// lastLE returns the index of the last key <= target, clamped to 0. Used to
// choose which child subtree to descend into: a non-leaf node's key i is
// the smallest key present in child subtree i.
func lastLE(keys [][]byte, target []byte, cmp func(a, b []byte) int) int {
	idx := sort.Search(len(keys), func(i int) bool { return cmp(keys[i], target) > 0 })
	if idx == 0 {
		return 0
	}
	return idx - 1
}

type frame struct {
	node *node
	idx  int
}

// cursor walks records in ascending key order starting from a seek point.
// APFS btree nodes carry no sibling pointers, so cursor keeps the full
// root-to-leaf descent path to re-descend into the next subtree on leaf
// exhaustion.
type cursor struct {
	t     *tree
	stack []frame
	errv  error
}

// seek positions a new cursor at the first record >= key.
func (t *tree) seek(key []byte) (*cursor, error) {
	c := &cursor{t: t}
	paddr := t.rootPaddr
	for {
		n, err := t.readNode(paddr)
		if err != nil {
			return nil, err
		}
		var idx int
		if n.leaf {
			idx = firstGE(n.keys, key, t.cmp)
		} else {
			idx = lastLE(n.keys, key, t.cmp)
		}
		c.stack = append(c.stack, frame{node: n, idx: idx})
		if n.leaf {
			if idx < len(n.keys) {
				return c, nil
			}
			// Every key in this leaf is < target: the correct record, if
			// any, is the first one in the next leaf. next() from an
			// out-of-bounds leaf position pops and ascends to find it,
			// which is exactly what's needed here too.
			c.next()
			if err := c.errv; err != nil {
				return nil, err
			}
			return c, nil
		}
		if idx >= len(n.vals) {
			return nil, errors.New("apfs: empty non-leaf btree node")
		}
		childOid := binary.LittleEndian.Uint64(n.vals[idx])
		childPaddr, err := t.resolve(childOid)
		if err != nil {
			return nil, err
		}
		paddr = childPaddr
	}
}

// descendLeftmost pushes frames from paddr down to (and including) a leaf,
// always following child index 0.
func (c *cursor) descendLeftmost(paddr int64) error {
	for {
		n, err := c.t.readNode(paddr)
		if err != nil {
			return err
		}
		c.stack = append(c.stack, frame{node: n, idx: 0})
		if n.leaf {
			return nil
		}
		if len(n.vals) == 0 {
			return errors.New("apfs: empty non-leaf btree node")
		}
		childOid := binary.LittleEndian.Uint64(n.vals[0])
		paddr, err = c.t.resolve(childOid)
		if err != nil {
			return err
		}
	}
}

// next advances the cursor to the next record in ascending key order.
// Returns false at end of tree or on error (check err()).
func (c *cursor) next() bool {
	if c.errv != nil {
		return false
	}
	for len(c.stack) > 0 {
		top := &c.stack[len(c.stack)-1]
		if top.node.leaf {
			top.idx++
			if top.idx < len(top.node.keys) {
				return true
			}
			c.stack = c.stack[:len(c.stack)-1]
			continue
		}
		top.idx++
		if top.idx >= len(top.node.keys) {
			c.stack = c.stack[:len(c.stack)-1]
			continue
		}
		childOid := binary.LittleEndian.Uint64(top.node.vals[top.idx])
		childPaddr, err := c.t.resolve(childOid)
		if err != nil {
			c.errv = err
			return false
		}
		if err := c.descendLeftmost(childPaddr); err != nil {
			c.errv = err
			return false
		}
		if leaf := c.stack[len(c.stack)-1]; len(leaf.node.keys) > 0 {
			return true
		}
		// empty leaf: loop again, which will pop it and keep ascending.
	}
	return false
}

func (c *cursor) key() []byte {
	if len(c.stack) == 0 {
		return nil
	}
	top := c.stack[len(c.stack)-1]
	if top.idx >= len(top.node.keys) {
		return nil
	}
	return top.node.keys[top.idx]
}

func (c *cursor) val() []byte {
	if len(c.stack) == 0 {
		return nil
	}
	top := c.stack[len(c.stack)-1]
	if top.idx >= len(top.node.vals) {
		return nil
	}
	return top.node.vals[top.idx]
}

func (c *cursor) err() error { return c.errv }

// --- FS-tree records ---
//
// j_key_t is a single little-endian u64: the low 60 bits are the object id,
// the high 4 bits are the record type. Records sort by object id first,
// then by type — NOT by a raw u64 comparison, since the type occupies the
// high nibble.

const (
	objTypeInode      = 3
	objTypeXattr      = 4
	objTypeDstreamID  = 6
	objTypeFileExtent = 8
	objTypeDirRec     = 9
)

// rootDirOid is the fixed object id of the volume's root directory.
const rootDirOid = 2

func decodeJKey(k []byte) (oid uint64, typ uint8, err error) {
	if len(k) < 8 {
		return 0, 0, errors.New("apfs: fs-tree key shorter than j_key_t")
	}
	raw := binary.LittleEndian.Uint64(k[0:8])
	oidBits, err := binutil.Bits(raw, 0, 60)
	if err != nil {
		return 0, 0, err
	}
	typBits, err := binutil.Bits(raw, 60, 4)
	if err != nil {
		return 0, 0, err
	}
	return oidBits, uint8(typBits), nil
}

// encodeJKey builds a search key for the first record of the given
// (oid, type) — the sub-key (name hash, extent offset, ...) is left as all
// zero bits, which sorts as the smallest possible sub-key for that pair.
func encodeJKey(oid uint64, typ uint8) []byte {
	raw := uint64(typ)<<60 | (oid & (1<<60 - 1))
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, raw)
	return buf
}

// fsKeyCompare orders fs-tree records by (object id, record type) only.
// The per-type sub-key (drec name hash, xattr name, extent offset) is
// deliberately not modeled: search descends to the first record of a given
// (oid, type) and callers scan forward linearly from there.
func fsKeyCompare(a, b []byte) int {
	aOid, aTyp, aErr := decodeJKey(a)
	bOid, bTyp, bErr := decodeJKey(b)
	if aErr != nil || bErr != nil {
		// Malformed keys sort last so a scan simply stops at them.
		switch {
		case aErr != nil && bErr != nil:
			return 0
		case aErr != nil:
			return 1
		default:
			return -1
		}
	}
	if aOid != bOid {
		if aOid < bOid {
			return -1
		}
		return 1
	}
	if aTyp != bTyp {
		if aTyp < bTyp {
			return -1
		}
		return 1
	}
	return 0
}

// decodeDrecKey extracts the entry name from a DIR_REC record key. APFS
// volumes support two on-disk key layouts distinguished by a volume
// incompatible-features flag: a "hashed" layout (j_drec_hashed_key_t, a u32
// length+hash) used by case-insensitive/normalization-sensitive volumes,
// and a plain layout (j_drec_key_t, a bare u16 length). Rather than parse
// that flag (whose exact bit position isn't load-bearing for anything else
// this parser needs), this tries the hashed layout first and falls back to
// the plain layout if the result doesn't look like a valid, printable name.
func decodeDrecKey(k []byte) (string, bool) {
	if len(k) >= 12 {
		lh := binary.LittleEndian.Uint32(k[8:12])
		nameLen, err := binutil.Bits(uint64(lh), 0, 10) // includes trailing NUL
		if err == nil {
			if name, ok := extractDrecName(k[12:], int(nameLen)); ok {
				return name, true
			}
		}
	}
	if len(k) >= 10 {
		nameLen := binary.LittleEndian.Uint16(k[8:10])
		if name, ok := extractDrecName(k[10:], int(nameLen)); ok {
			return name, true
		}
	}
	return "", false
}

// extractDrecName trims a possible trailing NUL from raw[:nameLen] and
// accepts the result only if it looks like a plausible printable name.
func extractDrecName(raw []byte, nameLen int) (string, bool) {
	if nameLen < 1 || nameLen > len(raw) {
		return "", false
	}
	name := raw[:nameLen]
	if name[len(name)-1] == 0 {
		name = name[:len(name)-1]
	}
	if !isPrintableName(name) {
		return "", false
	}
	return string(name), true
}

func isPrintableName(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// decodeDrecVal extracts the fields of a j_drec_val_t this parser needs.
func decodeDrecVal(v []byte) (fileID uint64, isDir bool, err error) {
	if len(v) < 18 {
		return 0, false, errors.New("apfs: drec value shorter than j_drec_val_t")
	}
	fileID = binary.LittleEndian.Uint64(v[0:8])
	flags := binary.LittleEndian.Uint16(v[16:18])
	const dtDir = 4
	return fileID, flags&0xf == dtDir, nil
}
