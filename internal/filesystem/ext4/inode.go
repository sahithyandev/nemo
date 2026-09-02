package ext4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const extentMagic = 0xf30a

type inode struct {
	mode   uint16
	size   uint64
	flags  uint32
	blocks [60]byte
}

type extentBlock struct {
	logical  uint32
	physical uint64
}

func (f *FS) readInode(number uint32) (inode, error) {
	b, _, err := f.readRawInode(number)
	if err != nil {
		return inode{}, err
	}
	var blockData [60]byte
	copy(blockData[:], b[40:100])
	return inode{
		mode:   binary.LittleEndian.Uint16(b[0:]),
		size:   uint64(binary.LittleEndian.Uint32(b[4:])) | uint64(binary.LittleEndian.Uint32(b[108:]))<<32,
		flags:  binary.LittleEndian.Uint32(b[32:]),
		blocks: blockData,
	}, nil
}

func (f *FS) readRawInode(number uint32) ([]byte, int64, error) {
	if number == 0 || number > f.sb.inodesCount {
		return nil, 0, fmt.Errorf("inode %d is outside filesystem bounds", number)
	}
	group := uint64(number-1) / uint64(f.sb.inodesPerGroup)
	index := uint64(number-1) % uint64(f.sb.inodesPerGroup)
	if group >= f.groupCount {
		return nil, 0, fmt.Errorf("inode %d references missing group %d", number, group)
	}

	descOff := f.gdtOffset + int64(group)*int64(f.sb.descSize)
	desc := make([]byte, f.sb.descSize)
	if err := readExact(f.img, desc, descOff); err != nil {
		return nil, 0, fmt.Errorf("read group %d descriptor: %w", group, err)
	}
	inodeTable := uint64(binary.LittleEndian.Uint32(desc[8:]))
	if f.sb.featureIncompat&featureIncompat64Bit != 0 {
		inodeTable |= uint64(binary.LittleEndian.Uint32(desc[40:])) << 32
	}
	tableOff, ok := byteOffset(inodeTable, uint64(f.sb.blockSize), f.img.Size())
	if !ok || inodeTable >= f.sb.blocksCount {
		return nil, 0, fmt.Errorf("group %d inode table block %d is outside filesystem bounds", group, inodeTable)
	}
	inodeOffset := uint64(tableOff) + index*uint64(f.sb.inodeSize)
	if inodeOffset > uint64(f.img.Size()) || uint64(f.sb.inodeSize) > uint64(f.img.Size())-inodeOffset {
		return nil, 0, fmt.Errorf("inode %d exceeds image bounds", number)
	}

	b := make([]byte, f.sb.inodeSize)
	if err := readExact(f.img, b, int64(inodeOffset)); err != nil {
		return nil, 0, err
	}
	return b, int64(inodeOffset), nil
}

func (f *FS) extentBlocks(in inode) ([]extentBlock, error) {
	if in.flags&inodeFlagInlineData != 0 {
		return nil, errors.New("inline inode data is unsupported")
	}
	if in.flags&inodeFlagEncrypted != 0 {
		return nil, errors.New("encrypted inode data is unsupported")
	}
	if in.flags&inodeFlagExtents == 0 {
		return nil, errors.New("non-extent inode data is unsupported")
	}
	blocks, err := f.walkExtentNode(in.blocks[:], -1, make(map[uint64]struct{}))
	if err != nil {
		return nil, err
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].logical < blocks[j].logical })
	for i := 1; i < len(blocks); i++ {
		if blocks[i].logical == blocks[i-1].logical {
			return nil, fmt.Errorf("overlapping extent at logical block %d", blocks[i].logical)
		}
	}
	return blocks, nil
}

func (f *FS) walkExtentNode(node []byte, expectedDepth int, visited map[uint64]struct{}) ([]extentBlock, error) {
	if len(node) < 12 || binary.LittleEndian.Uint16(node[0:]) != extentMagic {
		return nil, errors.New("invalid extent header")
	}
	entries := int(binary.LittleEndian.Uint16(node[2:]))
	maximum := int(binary.LittleEndian.Uint16(node[4:]))
	depth := int(binary.LittleEndian.Uint16(node[6:]))
	if expectedDepth >= 0 && depth != expectedDepth {
		return nil, fmt.Errorf("extent depth %d does not match parent depth %d", depth, expectedDepth)
	}
	if depth > 5 {
		return nil, fmt.Errorf("unsupported extent-tree depth %d", depth)
	}
	capacity := (len(node) - 12) / 12
	if entries > maximum || maximum > capacity || entries > capacity {
		return nil, errors.New("extent entries exceed node bounds")
	}

	var blocks []extentBlock
	for i := 0; i < entries; i++ {
		record := node[12+i*12 : 24+i*12]
		logical := binary.LittleEndian.Uint32(record[0:])
		if depth == 0 {
			lengthRaw := binary.LittleEndian.Uint16(record[4:])
			length := uint32(lengthRaw)
			if lengthRaw > 0x8000 {
				length -= 0x8000
			}
			if length == 0 || lengthRaw > 0x8000 {
				return nil, fmt.Errorf("unsupported empty or unwritten extent at logical block %d", logical)
			}
			physical := uint64(binary.LittleEndian.Uint16(record[6:]))<<32 | uint64(binary.LittleEndian.Uint32(record[8:]))
			if physical >= f.sb.blocksCount || uint64(length) > f.sb.blocksCount-physical || uint64(logical)+uint64(length) > uint64(^uint32(0))+1 {
				return nil, fmt.Errorf("extent at logical block %d exceeds filesystem bounds", logical)
			}
			for j := uint32(0); j < length; j++ {
				blocks = append(blocks, extentBlock{logical: logical + j, physical: physical + uint64(j)})
			}
			continue
		}

		leaf := uint64(binary.LittleEndian.Uint16(record[8:]))<<32 | uint64(binary.LittleEndian.Uint32(record[4:]))
		if _, ok := visited[leaf]; ok {
			return nil, fmt.Errorf("extent tree references block %d more than once", leaf)
		}
		visited[leaf] = struct{}{}
		child, err := f.readBlock(leaf)
		if err != nil {
			return nil, fmt.Errorf("read extent node block %d: %w", leaf, err)
		}
		childBlocks, err := f.walkExtentNode(child, depth-1, visited)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, childBlocks...)
	}
	return blocks, nil
}
