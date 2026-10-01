package ntfs

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	fileAttributeDirectory = uint32(0x10000000)
	indexEntryHasSubnode   = uint16(0x0001)
	indexEntryLast         = uint16(0x0002)
)

type directoryItem struct {
	reference uint64
	name      string
	namespace uint8
	fileFlags uint32
}

func (f *FS) directoryEntries(record mftRecord) ([]directoryItem, error) {
	if record.indexRoot == nil {
		return nil, errors.New("ntfs: directory has no INDEX_ROOT")
	}
	root, hasChildren, err := parseIndexRoot(record.indexRoot)
	if err != nil {
		return nil, err
	}
	items := append([]directoryItem(nil), root...)
	if hasChildren || record.indexAllocation != nil {
		if record.indexAllocation == nil || !record.indexAllocation.nonResident {
			return nil, errors.New("ntfs: directory index requires non-resident INDEX_ALLOCATION")
		}
		if record.bitmap == nil {
			return nil, errors.New("ntfs: directory index has no BITMAP")
		}
		bitmap, err := f.attributeBytes(record.bitmap)
		if err != nil {
			return nil, fmt.Errorf("ntfs: read index bitmap: %w", err)
		}
		if f.indexRecordSize == 0 {
			return nil, errors.New("ntfs: invalid zero index record size")
		}
		count := (record.indexAllocation.dataSize + uint64(f.indexRecordSize) - 1) / uint64(f.indexRecordSize)
		clusterSize := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
		if clusterSize == 0 || uint64(f.indexRecordSize)%clusterSize != 0 {
			return nil, errors.New("ntfs: index record size is not cluster aligned")
		}
		for i := uint64(0); i < count; i++ {
			if i/8 >= uint64(len(bitmap)) {
				return nil, errors.New("ntfs: index bitmap is truncated")
			}
			if bitmap[i/8]&(1<<uint(i%8)) == 0 {
				continue
			}
			buf := make([]byte, f.indexRecordSize)
			if err := f.readRunsAt(record.indexAllocation.runs, record.indexAllocation.dataSize, i*uint64(f.indexRecordSize), buf); err != nil {
				return nil, fmt.Errorf("ntfs: read INDX record %d: %w", i, err)
			}
			if err := applyFixups(buf, uint32(f.bytesPerSector), "INDX"); err != nil {
				return nil, fmt.Errorf("ntfs: INDX record %d: %w", i, err)
			}
			expectedVCN := i * uint64(f.indexRecordSize) / clusterSize
			if binary.LittleEndian.Uint64(buf[16:24]) != expectedVCN {
				return nil, fmt.Errorf("ntfs: INDX record %d has unexpected VCN", i)
			}
			part, _, err := parseIndexHeader(buf, 24)
			if err != nil {
				return nil, fmt.Errorf("ntfs: INDX record %d: %w", i, err)
			}
			items = append(items, part...)
		}
	}
	return preferDirectoryNames(items), nil
}

func (f *FS) attributeBytes(a *attributeHeader) ([]byte, error) {
	if !a.nonResident {
		return append([]byte(nil), a.value...), nil
	}
	if a.dataSize > uint64(f.img.Size()) || a.dataSize > uint64(^uint(0)>>1) {
		return nil, errors.New("ntfs: attribute is too large")
	}
	if err := f.validateRuns(a.runs); err != nil {
		return nil, err
	}
	b := make([]byte, int(a.dataSize))
	if err := f.readRunsAt(a.runs, a.dataSize, 0, b); err != nil {
		return nil, err
	}
	return b, nil
}

func parseIndexRoot(value []byte) ([]directoryItem, bool, error) {
	if len(value) < 32 {
		return nil, false, errors.New("ntfs: truncated INDEX_ROOT")
	}
	if binary.LittleEndian.Uint32(value[0:4]) != attributeTypeFileName {
		return nil, false, errors.New("ntfs: unsupported INDEX_ROOT key type")
	}
	return parseIndexHeader(value, 16)
}

func parseIndexHeader(buf []byte, base int) ([]directoryItem, bool, error) {
	if base < 0 || base+16 > len(buf) {
		return nil, false, errors.New("ntfs: truncated index header")
	}
	entriesOffset := int(binary.LittleEndian.Uint32(buf[base : base+4]))
	total := int(binary.LittleEndian.Uint32(buf[base+4 : base+8]))
	allocated := int(binary.LittleEndian.Uint32(buf[base+8 : base+12]))
	if entriesOffset < 16 || total < entriesOffset || allocated < total || base+allocated > len(buf) {
		return nil, false, errors.New("ntfs: invalid index header bounds")
	}
	start, end := base+entriesOffset, base+total
	var out []directoryItem
	hasChildren := false
	for off := start; off < end; {
		if end-off < 16 {
			return nil, false, errors.New("ntfs: truncated index entry")
		}
		length := int(binary.LittleEndian.Uint16(buf[off+8 : off+10]))
		keyLength := int(binary.LittleEndian.Uint16(buf[off+10 : off+12]))
		flags := binary.LittleEndian.Uint16(buf[off+12 : off+14])
		if length < 16 || length%8 != 0 || length > end-off || keyLength > length-16 {
			return nil, false, errors.New("ntfs: invalid index entry bounds")
		}
		if flags&indexEntryHasSubnode != 0 {
			hasChildren = true
			if length < 24 {
				return nil, false, errors.New("ntfs: truncated index subnode")
			}
		}
		if flags&indexEntryLast != 0 {
			return out, hasChildren, nil
		}
		name, err := parseFileName(buf[off+16 : off+16+keyLength])
		if err != nil {
			return nil, false, err
		}
		out = append(out, directoryItem{reference: binary.LittleEndian.Uint64(buf[off : off+8]), name: name.name, namespace: name.namespace, fileFlags: name.fileFlags})
		off += length
	}
	return nil, false, errors.New("ntfs: index has no final entry")
}

func preferDirectoryNames(in []directoryItem) []directoryItem {
	pos := make(map[uint64]int)
	out := make([]directoryItem, 0, len(in))
	priority := func(ns uint8) int {
		switch ns {
		case 1, 3:
			return 2
		case 0:
			return 1
		default:
			return 0
		}
	}
	for _, item := range in {
		key := item.reference & 0x0000ffffffffffff
		if at, ok := pos[key]; ok {
			if priority(item.namespace) > priority(out[at].namespace) {
				out[at] = item
			}
			continue
		}
		pos[key] = len(out)
		out = append(out, item)
	}
	return out
}
