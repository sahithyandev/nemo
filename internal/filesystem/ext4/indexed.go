package ext4

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

// indexedChildren follows the HTree rather than interpreting index bytes as
// directory records. Hash lookup is unnecessary when enumerating all leaves.
func (e *Entry) indexedChildren(in inode, blocks []extentBlock) ([]filesystem.Entry, error) {
	bs := uint64(e.fs.sb.blockSize)
	if in.size == 0 || in.size%bs != 0 || uint64(len(blocks)) != in.size/bs {
		return nil, fmt.Errorf("ext4: indexed directory %q has incomplete block mapping", e.path)
	}
	for i, b := range blocks {
		if uint64(b.logical) != uint64(i) {
			return nil, fmt.Errorf("ext4: indexed directory %q has a hole", e.path)
		}
	}
	root, err := e.fs.readBlock(blocks[0].physical)
	if err != nil {
		return nil, err
	}
	// dx_root overlays the space following the two obligatory dot records.
	if binary.LittleEndian.Uint32(root) != e.inode || binary.LittleEndian.Uint16(root[4:]) != 12 || root[6] != 1 || string(root[8:9]) != "." || binary.LittleEndian.Uint32(root[12:]) == 0 || directoryRecordLength(binary.LittleEndian.Uint16(root[16:]), e.fs.sb.blockSize) != int(bs-12) || root[18] != 2 || string(root[20:22]) != ".." {
		return nil, fmt.Errorf("ext4: invalid HTree root directory records in %q", e.path)
	}
	if binary.LittleEndian.Uint32(root[24:]) != 0 || root[28] > 5 || root[29] != 8 || root[30] > 2 || root[31] != 0 {
		return nil, fmt.Errorf("ext4: invalid or unsupported HTree root info in %q", e.path)
	}
	visited := map[uint32]bool{0: true}
	var out []filesystem.Entry
	var walk func([]byte, int, byte) error
	walk = func(buf []byte, off int, depth byte) error {
		limit := int(binary.LittleEndian.Uint16(buf[off:]))
		count := int(binary.LittleEndian.Uint16(buf[off+2:]))
		tail := 0
		if e.fs.sb.featureROCompat&featureROCompatMetadataCsum != 0 {
			tail = 8
		}
		if limit != (len(buf)-off-tail)/8 || count == 0 || count > limit {
			return fmt.Errorf("ext4: invalid HTree count/limit in %q", e.path)
		}
		var previous uint32
		for i := 0; i < count; i++ {
			pos := off + i*8
			if i > 0 {
				hash := binary.LittleEndian.Uint32(buf[pos:])
				if i > 1 && hash < previous {
					return fmt.Errorf("ext4: unordered HTree hashes in %q", e.path)
				}
				previous = hash
			}
			logical := binary.LittleEndian.Uint32(buf[pos+4:])
			if logical == 0 || uint64(logical) >= uint64(len(blocks)) || visited[logical] {
				return fmt.Errorf("ext4: invalid or repeated HTree block %d in %q", logical, e.path)
			}
			visited[logical] = true
			child, err := e.fs.readBlock(blocks[logical].physical)
			if err != nil {
				return err
			}
			if depth > 0 {
				if binary.LittleEndian.Uint32(child) != 0 || directoryRecordLength(binary.LittleEndian.Uint16(child[4:]), e.fs.sb.blockSize) != int(bs) || child[6] != 0 || child[7] != 0 {
					return fmt.Errorf("ext4: invalid HTree index node in %q", e.path)
				}
				if err := walk(child, 8, depth-1); err != nil {
					return err
				}
			} else {
				entries, err := e.fs.parseDirectory(child, e.path)
				if err != nil {
					return err
				}
				out = append(out, entries...)
			}
		}
		return nil
	}
	if err := walk(root, 32, root[30]); err != nil {
		return nil, err
	}
	if len(visited) != len(blocks) {
		return nil, fmt.Errorf("ext4: unreferenced HTree directory blocks in %q", e.path)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path() < out[j].Path() })
	for i := 1; i < len(out); i++ {
		if out[i].Path() == out[i-1].Path() {
			return nil, fmt.Errorf("ext4: duplicate directory entry %q", out[i].Path())
		}
	}
	return out, nil
}
