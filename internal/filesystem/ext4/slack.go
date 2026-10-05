package ext4

import (
	"errors"
	"fmt"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

const modeRegular = 0x8000

var _ filesystem.SlackSpaceCapable = (*Entry)(nil)

// SlackRegions returns the unused tail of an allocated regular-file block.
// Only a fully mapped extent layout is accepted: holes, unwritten extents,
// inline data, encryption, and allocation beyond EOF are rejected rather than
// risking writes into ambiguous storage.
func (e *Entry) SlackRegions() ([]filesystem.SlackRegion, error) {
	if e == nil || e.fs == nil {
		return nil, errors.New("ext4: invalid nil entry")
	}
	in, err := e.fs.readInode(e.inode)
	if err != nil {
		return nil, fmt.Errorf("ext4: read inode %d for slack space: %w", e.inode, err)
	}
	if in.mode&modeTypeMask != modeRegular {
		return nil, fmt.Errorf("ext4: slack space requires a regular file: %q: %w", e.path, filesystem.ErrUnsupported)
	}

	blocks, err := e.fs.extentBlocks(in)
	if err != nil {
		return nil, fmt.Errorf("ext4: inspect slack extents for %q: %w", e.path, err)
	}
	blockSize := uint64(e.fs.sb.blockSize)
	requiredBlocks := in.size / blockSize
	if in.size%blockSize != 0 {
		requiredBlocks++
	}
	if uint64(len(blocks)) != requiredBlocks {
		return nil, fmt.Errorf("ext4: sparse or overallocated file %q maps %d blocks for %d-byte size; expected %d: %w", e.path, len(blocks), in.size, requiredBlocks, filesystem.ErrUnsupported)
	}
	for i, block := range blocks {
		if block.logical != uint32(i) {
			return nil, fmt.Errorf("ext4: sparse file %q has a hole at logical block %d: %w", e.path, i, filesystem.ErrUnsupported)
		}
	}

	usedInLast := in.size % blockSize
	if in.size == 0 || usedInLast == 0 {
		return nil, nil
	}
	last := blocks[len(blocks)-1]
	blockOffset, ok := byteOffset(last.physical, blockSize, e.fs.img.Size())
	if !ok || usedInLast > uint64(e.fs.img.Size()-blockOffset) {
		return nil, fmt.Errorf("ext4: final data block for %q exceeds image bounds", e.path)
	}
	offset := blockOffset + int64(usedInLast)
	length := int64(blockSize - usedInLast)
	if length > e.fs.img.Size()-offset {
		return nil, fmt.Errorf("ext4: slack region for %q exceeds image bounds", e.path)
	}
	return []filesystem.SlackRegion{{Offset: offset, Length: length}}, nil
}
