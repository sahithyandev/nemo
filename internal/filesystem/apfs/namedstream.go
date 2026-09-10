package apfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"

	"github.com/sahithyandev/nemo/internal/image"
)

// APFS named streams are extended attributes. Every xattr is a record in the
// volume's filesystem B-tree, keyed by (file object id, XATTR type, name). The
// value is either embedded (the bytes stored inline, up to
// xattrMaxEmbeddedSize) or a stream (the record holds a j_xattr_dstream_t
// pointing at a data stream with its own file extents). The resource fork is
// just the xattr named com.apple.ResourceFork.
//
// Writes go through the in-place leaf rewriter in btree_write.go. That means:
//   - an embedded value can be replaced with any value up to
//     xattrMaxEmbeddedSize bytes;
//   - a stream-backed value can be overwritten with anything that fits inside
//     its already-allocated extents;
//   - growing past those limits, which would need block allocation, fails with
//     a clear error rather than a partial write;
//   - deleting a stream-backed xattr drops the record but does not free its
//     extents (no space manager).

const (
	xattrDataStream      = 0x0001 // XATTR_DATA_STREAM
	xattrDataEmbedded    = 0x0002 // XATTR_DATA_EMBEDDED
	xattrFileSystemOwned = 0x0004 // XATTR_FILE_SYSTEM_OWNED

	// xattrMaxEmbeddedSize is XATTR_MAX_EMBEDDED_SIZE.
	xattrMaxEmbeddedSize = 3804

	// xattrDStreamSize is sizeof(j_xattr_dstream_t): an 8-byte object id plus
	// a 40-byte j_dstream_t.
	xattrDStreamSize = 48

	extentLengthMask = (1 << 56) - 1
)

// xattrDStream is a decoded j_xattr_dstream_t.
type xattrDStream struct {
	objID             uint64
	size              uint64
	allocedSize       uint64
	defaultCryptoID   uint64
	totalBytesWritten uint64
	totalBytesRead    uint64
}

// xattrRecord is one decoded xattr: exactly one of inline / stream is set.
type xattrRecord struct {
	name   string
	flags  uint16
	inline []byte
	stream *xattrDStream
}

// decodeXattrName reads the name out of a j_xattr_key_t (j_key_t + u16
// name_len + NUL-terminated name; name_len includes the NUL).
func decodeXattrName(k []byte) (string, bool) {
	if len(k) < 10 {
		return "", false
	}
	nameLen := int(binary.LittleEndian.Uint16(k[8:10]))
	if nameLen < 1 || 10+nameLen > len(k) {
		return "", false
	}
	name := k[10 : 10+nameLen]
	if name[len(name)-1] == 0 {
		name = name[:len(name)-1]
	}
	if len(name) == 0 {
		return "", false
	}
	return string(name), true
}

// encodeXattrKey builds a j_xattr_key_t for (oid, name).
func encodeXattrKey(oid uint64, name string) []byte {
	nb := append([]byte(name), 0)
	k := make([]byte, 10+len(nb))
	copy(k[0:8], encodeJKey(oid, objTypeXattr))
	binary.LittleEndian.PutUint16(k[8:10], uint16(len(nb)))
	copy(k[10:], nb)
	return k
}

// decodeXattrVal decodes a j_xattr_val_t.
func decodeXattrVal(v []byte) (xattrRecord, error) {
	if len(v) < 4 {
		return xattrRecord{}, errors.New("apfs: xattr value shorter than j_xattr_val_t")
	}
	flags := binary.LittleEndian.Uint16(v[0:2])
	xlen := int(binary.LittleEndian.Uint16(v[2:4]))
	if 4+xlen > len(v) {
		return xattrRecord{}, errors.New("apfs: xattr value data runs past the record")
	}
	xdata := v[4 : 4+xlen]
	rec := xattrRecord{flags: flags}
	switch {
	case flags&xattrDataEmbedded != 0:
		rec.inline = append([]byte(nil), xdata...)
	case flags&xattrDataStream != 0:
		if len(xdata) < xattrDStreamSize {
			return xattrRecord{}, errors.New("apfs: xattr stream data shorter than j_xattr_dstream_t")
		}
		rec.stream = &xattrDStream{
			objID:             binary.LittleEndian.Uint64(xdata[0:8]),
			size:              binary.LittleEndian.Uint64(xdata[8:16]),
			allocedSize:       binary.LittleEndian.Uint64(xdata[16:24]),
			defaultCryptoID:   binary.LittleEndian.Uint64(xdata[24:32]),
			totalBytesWritten: binary.LittleEndian.Uint64(xdata[32:40]),
			totalBytesRead:    binary.LittleEndian.Uint64(xdata[40:48]),
		}
	default:
		return xattrRecord{}, fmt.Errorf("apfs: xattr flags 0x%x are neither embedded nor stream", flags)
	}
	return rec, nil
}

func encodeEmbeddedXattrVal(data []byte) []byte {
	v := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint16(v[0:2], xattrDataEmbedded)
	binary.LittleEndian.PutUint16(v[2:4], uint16(len(data)))
	copy(v[4:], data)
	return v
}

func encodeStreamXattrVal(ds xattrDStream) []byte {
	v := make([]byte, 4+xattrDStreamSize)
	binary.LittleEndian.PutUint16(v[0:2], xattrDataStream)
	binary.LittleEndian.PutUint16(v[2:4], xattrDStreamSize)
	binary.LittleEndian.PutUint64(v[4:12], ds.objID)
	binary.LittleEndian.PutUint64(v[12:20], ds.size)
	binary.LittleEndian.PutUint64(v[20:28], ds.allocedSize)
	binary.LittleEndian.PutUint64(v[28:36], ds.defaultCryptoID)
	binary.LittleEndian.PutUint64(v[36:44], ds.totalBytesWritten)
	binary.LittleEndian.PutUint64(v[44:52], ds.totalBytesRead)
	return v
}

// xattrRecords returns the xattr records on the file with object id oid, in
// on-disk (byte) order.
func (f *FS) xattrRecords(oid uint64) ([]xattrRecord, error) {
	c, err := f.fsTree.seek(encodeJKey(oid, objTypeXattr))
	if err != nil {
		return nil, err
	}
	var out []xattrRecord
	for k := c.key(); k != nil; k = c.key() {
		kOid, typ, kerr := decodeJKey(k)
		if kerr != nil || kOid != oid || typ != objTypeXattr {
			break
		}
		name, ok := decodeXattrName(k)
		if !ok {
			return nil, fmt.Errorf("apfs: undecodable xattr key on object %d", oid)
		}
		rec, derr := decodeXattrVal(c.val())
		if derr != nil {
			return nil, fmt.Errorf("apfs: xattr %q on object %d: %w", name, oid, derr)
		}
		rec.name = name
		out = append(out, rec)
		if !c.next() {
			break
		}
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (f *FS) xattrNames(oid uint64) ([]string, error) {
	recs, err := f.xattrRecords(oid)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(recs))
	for i, r := range recs {
		names[i] = r.name
	}
	return names, nil
}

func (f *FS) readXattr(oid uint64, name string) ([]byte, error) {
	recs, err := f.xattrRecords(oid)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if r.name != name {
			continue
		}
		if r.stream != nil {
			return f.readExtents(r.stream.objID, r.stream.size)
		}
		return append([]byte(nil), r.inline...), nil
	}
	return nil, fs.ErrNotExist
}

// fileExtent is one decoded j_file_extent record.
type fileExtent struct {
	logical uint64
	length  uint64
	phys    uint64
}

// extentsOf walks the FILE_EXTENT records for objID, verifying they start at
// logical offset 0 and are contiguous.
func (f *FS) extentsOf(objID uint64) ([]fileExtent, error) {
	c, err := f.fsTree.seek(encodeJKey(objID, objTypeFileExtent))
	if err != nil {
		return nil, err
	}
	var out []fileExtent
	var next uint64
	for k := c.key(); k != nil; k = c.key() {
		kOid, typ, kerr := decodeJKey(k)
		if kerr != nil || kOid != objID || typ != objTypeFileExtent {
			break
		}
		if len(k) < 16 {
			return nil, errors.New("apfs: file-extent key shorter than j_file_extent_key_t")
		}
		v := c.val()
		if len(v) < 24 {
			return nil, errors.New("apfs: file-extent value shorter than j_file_extent_val_t")
		}
		ext := fileExtent{
			logical: binary.LittleEndian.Uint64(k[8:16]),
			length:  binary.LittleEndian.Uint64(v[0:8]) & extentLengthMask,
			phys:    binary.LittleEndian.Uint64(v[8:16]),
		}
		if ext.logical != next {
			return nil, fmt.Errorf("apfs: object %d has a sparse or out-of-order extent (want logical %d, got %d)", objID, next, ext.logical)
		}
		if ext.phys == 0 {
			return nil, fmt.Errorf("apfs: object %d has an unsupported hole extent", objID)
		}
		// A crafted image can put an arbitrary phys in an extent record;
		// int64(phys)*int64(blockSize) must not wrap negative and hand
		// ReadAt/WriteAt a negative offset.
		if ext.phys > uint64(math.MaxInt64)/uint64(f.blockSize) {
			return nil, fmt.Errorf("apfs: object %d has an out-of-range physical extent address %d", objID, ext.phys)
		}
		out = append(out, ext)
		next += ext.length
		if !c.next() {
			break
		}
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("apfs: object %d has no file extents", objID)
	}
	return out, nil
}

func (f *FS) readExtents(objID, size uint64) ([]byte, error) {
	exts, err := f.extentsOf(objID)
	if err != nil {
		return nil, err
	}
	// Cap the allocation at the bytes the extents actually cover, not the
	// size claimed by the (possibly corrupted) j_xattr_dstream_t: a bogus
	// multi-gigabyte size must not make this OOM before the check below.
	var coverage uint64
	for _, ext := range exts {
		coverage += ext.length
	}
	if size > coverage {
		return nil, fmt.Errorf("apfs: object %d extents cover %d bytes, expected %d", objID, coverage, size)
	}
	out := make([]byte, 0, coverage)
	for _, ext := range exts {
		buf, err := readFull(f.img, int64(ext.phys)*int64(f.blockSize), int64(ext.length))
		if err != nil {
			return nil, err
		}
		out = append(out, buf...)
	}
	return out[:size], nil
}

// writeExtents overwrites objID's extents with data, zero-filling the rest of
// the allocated space. data must not exceed alloced. Every allocated block is
// rewritten on every call, even when only the first bytes changed: the tail
// must be zeroed so no residual old data survives, so the cost of a write
// scales with the allocation, not the payload.
func (f *FS) writeExtents(objID, alloced uint64, data []byte) error {
	if uint64(len(data)) > alloced {
		return fmt.Errorf("apfs: value of %d bytes exceeds the stream's %d-byte allocation", len(data), alloced)
	}
	exts, err := f.extentsOf(objID)
	if err != nil {
		return err
	}
	var written uint64
	var covered uint64
	for _, ext := range exts {
		chunk := make([]byte, ext.length)
		if written < uint64(len(data)) {
			written += uint64(copy(chunk, data[written:]))
		}
		if err := writeExact(f.img, chunk, int64(ext.phys)*int64(f.blockSize)); err != nil {
			return err
		}
		covered += ext.length
	}
	if written < uint64(len(data)) {
		return fmt.Errorf("apfs: object %d extents cover %d bytes, cannot store %d", objID, covered, len(data))
	}
	return nil
}

func writeExact(img image.Image, p []byte, off int64) error {
	n, err := img.WriteAt(p, off)
	if err != nil {
		return err
	}
	if n != len(p) {
		return fmt.Errorf("apfs: short write at %d: %d of %d bytes", off, n, len(p))
	}
	return nil
}

func (f *FS) writeXattr(oid uint64, name string, data []byte) error {
	recs, err := f.xattrRecords(oid)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.name != name {
			continue
		}
		if r.flags&xattrFileSystemOwned != 0 {
			return fmt.Errorf("apfs: xattr %q is filesystem-owned and cannot be modified", name)
		}
		if r.stream != nil {
			if uint64(len(data)) > r.stream.allocedSize {
				return fmt.Errorf("apfs: xattr %q is stream-backed with %d bytes allocated; storing %d bytes needs block allocation, which is unsupported", name, r.stream.allocedSize, len(data))
			}
			// Extent data first, then the record's size field. The record
			// rewrite replaces a fixed 52-byte value in a leaf that does not
			// change size, so it only fails on an I/O error or a crash. If it
			// does, the extents hold the new bytes but the record still says
			// the old size: a reader gets a prefix of the new value (possibly
			// zero-padded), never a torn block or an unmountable volume.
			// Making this atomic needs the copy-on-write path nemo does not
			// implement.
			if err := f.writeExtents(r.stream.objID, r.stream.allocedSize, data); err != nil {
				return err
			}
			ds := *r.stream
			ds.size = uint64(len(data))
			ds.totalBytesWritten = uint64(len(data))
			ds.totalBytesRead = uint64(len(data))
			return f.replaceXattr(oid, name, encodeStreamXattrVal(ds))
		}
		if len(data) > xattrMaxEmbeddedSize {
			return errValueNeedsStream(len(data))
		}
		return f.replaceXattr(oid, name, encodeEmbeddedXattrVal(data))
	}
	if len(data) > xattrMaxEmbeddedSize {
		return errValueNeedsStream(len(data))
	}
	return f.insertXattr(oid, name, encodeEmbeddedXattrVal(data))
}

func errValueNeedsStream(n int) error {
	return fmt.Errorf("apfs: value of %d bytes exceeds the %d-byte embedded xattr limit; a data stream is required, which needs block allocation (unsupported)", n, xattrMaxEmbeddedSize)
}

// xattrKeyAfter reports whether the record key rkey sorts strictly after the
// xattr (oid, name). fsKeyCompare ignores the name sub-key, so ordering within
// one file's xattr run is resolved here by name bytes.
func xattrKeyAfter(rkey []byte, oid uint64, name string) bool {
	ro, rt, err := decodeJKey(rkey)
	if err != nil {
		return false
	}
	if ro != oid {
		return ro > oid
	}
	if rt != objTypeXattr {
		return rt > objTypeXattr
	}
	rn, ok := decodeXattrName(rkey)
	if !ok {
		return false
	}
	return rn > name
}

func isXattrNamed(rkey []byte, oid uint64, name string) bool {
	ro, rt, err := decodeJKey(rkey)
	if err != nil || ro != oid || rt != objTypeXattr {
		return false
	}
	rn, ok := decodeXattrName(rkey)
	return ok && rn == name
}

func (f *FS) replaceXattr(oid uint64, name string, val []byte) error {
	return f.fsTree.rewriteLeaf(encodeXattrKey(oid, name), func(recs []record, _ bool) ([]record, int, error) {
		for i := range recs {
			if isXattrNamed(recs[i].key, oid, name) {
				recs[i].val = val
				return recs, 0, nil
			}
		}
		return nil, 0, fs.ErrNotExist
	})
}

func (f *FS) insertXattr(oid uint64, name string, val []byte) error {
	key := encodeXattrKey(oid, name)
	return f.fsTree.rewriteLeaf(key, func(recs []record, leafIsRoot bool) ([]record, int, error) {
		idx := len(recs)
		for i := range recs {
			if isXattrNamed(recs[i].key, oid, name) {
				return nil, 0, fmt.Errorf("apfs: xattr %q already exists on object %d", name, oid)
			}
			if xattrKeyAfter(recs[i].key, oid, name) {
				idx = i
				break
			}
		}
		if idx == 0 && !leafIsRoot {
			return nil, 0, errors.New("apfs: inserting before the first record of a non-root leaf is unsupported")
		}
		out := make([]record, 0, len(recs)+1)
		out = append(out, recs[:idx]...)
		out = append(out, record{key: key, val: val})
		out = append(out, recs[idx:]...)
		return out, 1, nil
	})
}

// deleteXattr removes the xattr record. For a stream-backed value the extent
// blocks are intentionally left allocated: freeing them needs the space
// manager, which nemo does not touch.
func (f *FS) deleteXattr(oid uint64, name string) error {
	recs, err := f.xattrRecords(oid)
	if err != nil {
		return err
	}
	found := false
	for _, r := range recs {
		if r.name != name {
			continue
		}
		found = true
		if r.flags&xattrFileSystemOwned != 0 {
			return fmt.Errorf("apfs: xattr %q is filesystem-owned and cannot be deleted", name)
		}
	}
	if !found {
		return fs.ErrNotExist
	}
	return f.fsTree.rewriteLeaf(encodeXattrKey(oid, name), func(recs []record, leafIsRoot bool) ([]record, int, error) {
		for i := range recs {
			if isXattrNamed(recs[i].key, oid, name) {
				if i == 0 && !leafIsRoot {
					return nil, 0, errors.New("apfs: deleting the first record of a non-root leaf is unsupported")
				}
				return append(recs[:i:i], recs[i+1:]...), -1, nil
			}
		}
		return nil, 0, fs.ErrNotExist
	})
}
