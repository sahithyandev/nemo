package ext4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"sort"
	"strings"
)

const xattrMagic = 0xea020000

type xattrRegion struct {
	attrs     map[string][]byte
	block     uint64
	refcount  uint32
	bodyStart int
}

var xattrPrefixes = []struct {
	prefix string
	index  byte
}{
	{"system.posix_acl_access", 2}, {"system.posix_acl_default", 3},
	{"system.richacl", 8}, {"user.", 1}, {"trusted.", 4}, {"security.", 6}, {"system.", 7},
}

func splitXattrName(name string) (byte, string, error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return 0, "", errors.New("xattr name is empty or contains NUL")
	}
	for _, ns := range xattrPrefixes {
		if strings.HasPrefix(name, ns.prefix) {
			tail := strings.TrimPrefix(name, ns.prefix)
			if (ns.index == 2 || ns.index == 3 || ns.index == 8) && tail != "" {
				continue
			}
			if tail == "" && (ns.index == 1 || ns.index == 4 || ns.index == 6 || ns.index == 7) {
				return 0, "", fmt.Errorf("namespace %q requires a non-empty attribute name", ns.prefix)
			}
			if len(tail) > 255 {
				return 0, "", fmt.Errorf("xattr name component is %d bytes; maximum is 255", len(tail))
			}
			return ns.index, tail, nil
		}
	}
	return 0, "", fmt.Errorf("unsupported xattr namespace in %q (supported: user., trusted., security., system.)", name)
}

func joinXattrName(index byte, tail string) (string, error) {
	switch index {
	case 1:
		return "user." + tail, nil
	case 2:
		if tail == "" {
			return "system.posix_acl_access", nil
		}
	case 3:
		if tail == "" {
			return "system.posix_acl_default", nil
		}
	case 4:
		return "trusted." + tail, nil
	case 6:
		return "security." + tail, nil
	case 7:
		return "system." + tail, nil
	case 8:
		if tail == "" {
			return "system.richacl", nil
		}
	}
	return "", fmt.Errorf("unsupported xattr name index %d", index)
}

func (f *FS) readXattrRegions(number uint32) ([]byte, int64, []xattrRegion, error) {
	raw, off, err := f.readRawInode(number)
	if err != nil {
		return nil, 0, nil, err
	}
	regions := make([]xattrRegion, 0, 2)
	extra := 0
	if len(raw) > 128 {
		extra = int(binary.LittleEndian.Uint16(raw[128:]))
	}
	if extra != 0 && (extra < 4 || extra%4 != 0) {
		return nil, 0, nil, fmt.Errorf("invalid inode extra-isize %d: must be zero or a 4-byte-aligned value of at least 4", extra)
	}
	start := 128 + extra
	if start > len(raw) {
		return nil, 0, nil, fmt.Errorf("invalid inode extra-isize %d: xattr body starts beyond %d-byte inode", extra, len(raw))
	}
	if start+4 <= len(raw) && binary.LittleEndian.Uint32(raw[start:]) == xattrMagic {
		a, err := parseXattrs(raw[start:], 4)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("invalid inode-body xattr layout: %w", err)
		}
		regions = append(regions, xattrRegion{attrs: a, bodyStart: start})
	}
	block := uint64(binary.LittleEndian.Uint32(raw[104:]))
	if len(raw) >= 120 {
		block |= uint64(binary.LittleEndian.Uint16(raw[118:])) << 32
	}
	if block != 0 {
		b, err := f.readBlock(block)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("read external xattr block %d: %w", block, err)
		}
		if binary.LittleEndian.Uint32(b) != xattrMagic {
			return nil, 0, nil, fmt.Errorf("external xattr block %d has invalid magic", block)
		}
		refcount := binary.LittleEndian.Uint32(b[4:])
		if refcount == 0 {
			return nil, 0, nil, fmt.Errorf("external xattr block %d has zero refcount", block)
		}
		if binary.LittleEndian.Uint32(b[8:]) != 1 {
			return nil, 0, nil, fmt.Errorf("external xattr block %d has unsupported block count %d", block, binary.LittleEndian.Uint32(b[8:]))
		}
		a, err := parseXattrs(b, 32)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("invalid external xattr block %d layout: %w", block, err)
		}
		regions = append(regions, xattrRegion{attrs: a, block: block, refcount: refcount})
	}
	return raw, off, regions, nil
}

func parseXattrs(buf []byte, entriesStart int) (map[string][]byte, error) {
	out := make(map[string][]byte)
	type valueRange struct{ start, end int }
	var values []valueRange
	valueBase := 0
	if entriesStart == 4 {
		valueBase = 4
	} // ibody offsets are relative to the first entry.
	for p := entriesStart; ; {
		if p+4 > len(buf) {
			return nil, errors.New("unterminated entry list")
		}
		if binary.LittleEndian.Uint32(buf[p:]) == 0 {
			entriesEnd := p + 4
			sort.Slice(values, func(i, j int) bool { return values[i].start < values[j].start })
			for i, value := range values {
				if value.start < entriesEnd {
					return nil, fmt.Errorf("xattr value at offset %d overlaps entry list ending at %d", value.start, entriesEnd)
				}
				if i > 0 && value.start < values[i-1].end {
					return nil, fmt.Errorf("xattr values overlap at offset %d", value.start)
				}
			}
			return out, nil
		}
		if p+16 > len(buf) {
			return nil, errors.New("truncated entry")
		}
		nl := int(buf[p])
		entryLen := (16 + nl + 3) &^ 3
		if p+entryLen > len(buf) {
			return nil, errors.New("entry name exceeds region")
		}
		if binary.LittleEndian.Uint32(buf[p+4:]) != 0 {
			return nil, errors.New("ea-inode values are unsupported")
		}
		vo := valueBase + int(binary.LittleEndian.Uint16(buf[p+2:]))
		vs := int(binary.LittleEndian.Uint32(buf[p+8:]))
		if vo%4 != 0 {
			return nil, fmt.Errorf("xattr value offset %d is not 4-byte aligned", vo)
		}
		paddedSize := alignedXattrSize(vs)
		if vo < 0 || vs < 0 || paddedSize < vs || vo > len(buf) || paddedSize > len(buf)-vo {
			return nil, errors.New("entry value exceeds region")
		}
		tail := string(buf[p+16 : p+16+nl])
		if strings.IndexByte(tail, 0) >= 0 {
			return nil, errors.New("xattr name contains NUL")
		}
		name, err := joinXattrName(buf[p+1], tail)
		if err != nil {
			return nil, err
		}
		if _, exists := out[name]; exists {
			return nil, fmt.Errorf("duplicate xattr %q", name)
		}
		out[name] = append([]byte(nil), buf[vo:vo+vs]...)
		if vs != 0 {
			values = append(values, valueRange{vo, vo + paddedSize})
		}
		p += entryLen
	}
}

func encodeXattrs(size, header int, attrs map[string][]byte) ([]byte, error) {
	b := make([]byte, size)
	binary.LittleEndian.PutUint32(b, xattrMagic)
	if header == 32 {
		binary.LittleEndian.PutUint32(b[4:], 1)
		binary.LittleEndian.PutUint32(b[8:], 1)
	}
	type encodedName struct {
		full, tail string
		index      byte
	}
	names := make([]encodedName, 0, len(attrs))
	for n := range attrs {
		idx, tail, err := splitXattrName(n)
		if err != nil {
			return nil, err
		}
		names = append(names, encodedName{n, tail, idx})
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i].index != names[j].index {
			return names[i].index < names[j].index
		}
		if len(names[i].tail) != len(names[j].tail) {
			return len(names[i].tail) < len(names[j].tail)
		}
		return names[i].tail < names[j].tail
	})
	p, valueEnd := header, size
	valueBase := 0
	if header == 4 {
		valueBase = 4
	}
	for _, encoded := range names {
		full, idx, tail := encoded.full, encoded.index, encoded.tail
		entryLen := (16 + len(tail) + 3) &^ 3
		valueEnd = (valueEnd - len(attrs[full])) &^ 3
		if valueEnd < p+entryLen+4 {
			return nil, fmt.Errorf("xattr region capacity %d bytes exceeded while storing %q", size, full)
		}
		b[p] = byte(len(tail))
		b[p+1] = idx
		binary.LittleEndian.PutUint16(b[p+2:], uint16(valueEnd-valueBase))
		binary.LittleEndian.PutUint32(b[p+8:], uint32(len(attrs[full])))
		copy(b[p+16:], tail)
		copy(b[valueEnd:], attrs[full])
		if header == 32 {
			binary.LittleEndian.PutUint32(b[p+12:], xattrEntryHash(tail, b[valueEnd:valueEnd+alignedXattrSize(len(attrs[full]))]))
		}
		p += entryLen
	}
	if header == 32 {
		var blockHash uint32
		for p := header; binary.LittleEndian.Uint32(b[p:]) != 0; {
			entryHash := binary.LittleEndian.Uint32(b[p+12:])
			blockHash = (blockHash << 16) ^ (blockHash >> 16) ^ entryHash
			p += (16 + int(b[p]) + 3) &^ 3
		}
		binary.LittleEndian.PutUint32(b[12:], blockHash)
	}
	return b, nil
}

func alignedXattrSize(size int) int { return (size + 3) &^ 3 }

func xattrEntryHash(name string, paddedValue []byte) uint32 {
	var hash uint32
	for i := range len(name) {
		hash = (hash << 5) ^ (hash >> 27) ^ uint32(name[i])
	}
	for off := 0; off < len(paddedValue); off += 4 {
		hash = (hash << 16) ^ (hash >> 16) ^ binary.LittleEndian.Uint32(paddedValue[off:])
	}
	return hash
}

func (f *FS) readXattrs(number uint32) (map[string][]byte, error) {
	_, _, rs, err := f.readXattrRegions(number)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, r := range rs {
		for n, v := range r.attrs {
			if _, ok := out[n]; ok {
				return nil, fmt.Errorf("xattr %q appears in multiple regions", n)
			}
			out[n] = v
		}
	}
	return out, nil
}

func (f *FS) readXattr(number uint32, name string) ([]byte, error) {
	if _, _, err := splitXattrName(name); err != nil {
		return nil, err
	}
	a, err := f.readXattrs(number)
	if err != nil {
		return nil, err
	}
	v, ok := a[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), v...), nil
}

func (f *FS) writeXattr(number uint32, name string, data []byte) error {
	if _, _, err := splitXattrName(name); err != nil {
		return err
	}
	raw, off, rs, err := f.readXattrRegions(number)
	if err != nil {
		return err
	}
	for i := range rs {
		if _, ok := rs[i].attrs[name]; ok {
			rs[i].attrs[name] = append([]byte(nil), data...)
			return f.storeXattrRegion(number, raw, off, rs[i])
		}
	}
	// Keep new attributes with existing inode-body attributes when they fit.
	for i := range rs {
		if rs[i].block == 0 {
			attrs := cloneXattrs(rs[i].attrs)
			attrs[name] = append([]byte(nil), data...)
			if _, e := encodeXattrs(len(raw)-rs[i].bodyStart, 4, attrs); e == nil {
				rs[i].attrs = attrs
				return f.storeXattrRegion(number, raw, off, rs[i])
			}
		}
	}
	// Prefer inode body, including an unused body with valid extra-isize.
	extra := 0
	if len(raw) > 128 {
		extra = int(binary.LittleEndian.Uint16(raw[128:]))
	}
	start := 128 + extra
	candidate := xattrRegion{attrs: map[string][]byte{name: append([]byte(nil), data...)}, bodyStart: start}
	if start+4 <= len(raw) {
		if _, e := encodeXattrs(len(raw)-start, 4, candidate.attrs); e == nil {
			return f.storeXattrRegion(number, raw, off, candidate)
		}
	}
	for i := range rs {
		if rs[i].block != 0 {
			attrs := cloneXattrs(rs[i].attrs)
			attrs[name] = append([]byte(nil), data...)
			rs[i].attrs = attrs
			return f.storeXattrRegion(number, raw, off, rs[i])
		}
	}
	return fmt.Errorf("xattr requires an external block: inode has %d bytes available and no external xattr block is allocated", max(0, len(raw)-start))
}

func cloneXattrs(src map[string][]byte) map[string][]byte {
	dst := make(map[string][]byte, len(src)+1)
	for name, value := range src {
		dst[name] = append([]byte(nil), value...)
	}
	return dst
}

func (f *FS) deleteXattr(number uint32, name string) error {
	if _, _, err := splitXattrName(name); err != nil {
		return err
	}
	raw, off, rs, err := f.readXattrRegions(number)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if _, ok := r.attrs[name]; ok {
			if r.block != 0 && len(r.attrs) == 1 {
				return fmt.Errorf("external xattr block %d would become empty; safe deletion requires block deallocation", r.block)
			}
			delete(r.attrs, name)
			return f.storeXattrRegion(number, raw, off, r)
		}
	}
	return fs.ErrNotExist
}

func (f *FS) storeXattrRegion(number uint32, raw []byte, inodeOff int64, r xattrRegion) error {
	if r.block != 0 {
		if r.refcount > 1 {
			return fmt.Errorf("external xattr block %d is shared (refcount %d); safe mutation requires copy-on-write allocation", r.block, r.refcount)
		}
		b, err := encodeXattrs(int(f.sb.blockSize), 32, r.attrs)
		if err != nil {
			return err
		}
		if f.sb.featureROCompat&featureROCompatMetadataCsum != 0 {
			binary.LittleEndian.PutUint32(b[16:], 0)
			seed := ext4CRC32C(f.sb.checksumSeed, u64le(r.block))
			binary.LittleEndian.PutUint32(b[16:], ext4CRC32C(seed, b))
		}
		return writeExact(f.img, b, int64(r.block)*int64(f.sb.blockSize))
	}
	enc, err := encodeXattrs(len(raw)-r.bodyStart, 4, r.attrs)
	if err != nil {
		return err
	}
	copy(raw[r.bodyStart:], enc)
	if f.sb.featureROCompat&featureROCompatMetadataCsum != 0 {
		updateInodeChecksum(f.sb, number, raw)
	}
	return writeExact(f.img, raw, inodeOff)
}

func writeExact(img interface {
	WriteAt([]byte, int64) (int, error)
}, b []byte, off int64) error {
	n, err := img.WriteAt(b, off)
	if err != nil {
		return err
	}
	if n != len(b) {
		return fmt.Errorf("short image write: wrote %d of %d bytes", n, len(b))
	}
	return nil
}
func u64le(v uint64) []byte { b := make([]byte, 8); binary.LittleEndian.PutUint64(b, v); return b }
func ext4CRC32C(seed uint32, b []byte) uint32 {
	return ^crc32.Update(^seed, crc32.MakeTable(crc32.Castagnoli), b)
}
func updateInodeChecksum(sb superblock, n uint32, b []byte) {
	if len(b) < 128 {
		return
	}
	binary.LittleEndian.PutUint16(b[124:], 0)
	hasHigh := len(b) >= 132 && binary.LittleEndian.Uint16(b[128:]) >= 4
	if hasHigh {
		binary.LittleEndian.PutUint16(b[130:], 0)
	}
	x := make([]byte, 4)
	binary.LittleEndian.PutUint32(x, n)
	c := ext4CRC32C(sb.checksumSeed, x)
	binary.LittleEndian.PutUint32(x, binary.LittleEndian.Uint32(b[100:]))
	c = ext4CRC32C(c, x)
	c = ext4CRC32C(c, b)
	binary.LittleEndian.PutUint16(b[124:], uint16(c))
	if hasHigh {
		binary.LittleEndian.PutUint16(b[130:], uint16(c>>16))
	}
}
