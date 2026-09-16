package apfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

// A regular file's data stream is described by a j_dstream_t carried as an
// extended field (xfield) on its INODE record, right after the fixed
// inodeValFixedSize bytes. The xfield area is:
//
//	xf_blob_t { num_exts u16, used_data u16 }
//	x_field_t[num_exts] { type u8, flags u8, size u16 }
//	values, back to back, each padded up to the next 8-byte boundary
//
// The FILE_EXTENT records that actually hold the stream's physical blocks
// are keyed by j_inode_val_t's private_id (offset 8), not by the inode's own
// object id: usually the same value, but not guaranteed once clones exist.
const (
	inoExtTypeDstream = 8  // INO_EXT_TYPE_DSTREAM
	dstreamSize       = 40 // sizeof(j_dstream_t)

	// decmpfsXattrName is the xattr APFS uses to mark and describe an
	// HFS-compression-style compressed file: when present, the dstream's
	// bytes are compressed (or the "real" content lives in the xattr/resource
	// fork instead), so slack computed against the dstream's logical size
	// would be wrong.
	decmpfsXattrName = "com.apple.decmpfs"
)

// dstream is the decoded fields of a j_dstream_t this parser needs.
type dstream struct {
	size            uint64
	allocedSize     uint64
	defaultCryptoID uint64
}

// inodeDStream extracts the INO_EXT_TYPE_DSTREAM xfield from a decoded
// j_inode_val_t, if present. ok is false when the inode carries no data
// stream at all (e.g. an empty file or a symlink), which is not an error.
func inodeDStream(val []byte) (dstream, bool, error) {
	if len(val) < inodeValFixedSize {
		return dstream{}, false, errors.New("apfs: inode record shorter than j_inode_val_t")
	}
	xfields := val[inodeValFixedSize:]
	if len(xfields) == 0 {
		return dstream{}, false, nil
	}
	if len(xfields) < 4 {
		return dstream{}, false, errors.New("apfs: inode xfields blob shorter than xf_blob_t")
	}
	// numExts and size below are both widened from a uint16, so they're
	// always non-negative and neither the multiplication nor the additions
	// against them can overflow an int.
	numExts := int(binary.LittleEndian.Uint16(xfields[0:2]))
	descOff := 4
	dataOff := descOff + numExts*4
	if dataOff > len(xfields) {
		return dstream{}, false, fmt.Errorf("apfs: inode xfields blob: %d entries don't fit in %d bytes", numExts, len(xfields))
	}

	valOff := dataOff
	for i := 0; i < numExts; i++ {
		entry := xfields[descOff+i*4 : descOff+i*4+4]
		typ := entry[0]
		size := int(binary.LittleEndian.Uint16(entry[2:4]))
		if valOff+size > len(xfields) {
			return dstream{}, false, fmt.Errorf("apfs: inode xfield %d value runs past the blob", i)
		}
		if typ == inoExtTypeDstream {
			if size < dstreamSize {
				return dstream{}, false, fmt.Errorf("apfs: inode DSTREAM xfield is %d bytes, want at least %d", size, dstreamSize)
			}
			v := xfields[valOff : valOff+dstreamSize]
			return dstream{
				size:            binary.LittleEndian.Uint64(v[0:8]),
				allocedSize:     binary.LittleEndian.Uint64(v[8:16]),
				defaultCryptoID: binary.LittleEndian.Uint64(v[16:24]),
			}, true, nil
		}
		// Each value is padded up to the next 8-byte boundary so the next
		// xfield's value starts aligned.
		valOff += (size + 7) &^ 7
	}
	return dstream{}, false, nil
}

// slackFromExtents computes the slack regions past the logical size within
// exts, as absolute byte offsets into the image that base is relative to
// (0 for a bare container, the GPT partition's start otherwise). imgSize is
// the upper bound an offset+length must not cross: base plus the size of the
// image view the extents' physical addresses are read against.
//
// Slack is computed per extent so a region never straddles the boundary
// between two (possibly non-contiguous) extents; since extent lengths are
// always whole blocks, block boundaries fall out for free.
func slackFromExtents(exts []fileExtent, size uint64, blockSize uint32, base, imgSize int64) ([]filesystem.SlackRegion, error) {
	var regions []filesystem.SlackRegion
	var logical uint64
	for _, ext := range exts {
		end := logical + ext.length
		if end <= size {
			logical = end
			continue
		}
		start := size
		if logical > start {
			start = logical
		}
		skip := start - logical
		offset := base + int64(ext.phys)*int64(blockSize) + int64(skip)
		length := int64(end - start)
		if offset < 0 || length <= 0 {
			logical = end
			continue
		}
		if offset+length > imgSize {
			return nil, fmt.Errorf("apfs: slack region at %d runs past the image end", offset)
		}
		regions = append(regions, filesystem.SlackRegion{Offset: offset, Length: length})
		logical = end
	}
	return regions, nil
}

// slackRegions computes e's slack regions, in absolute image offsets. A nil,
// nil result means "no slack space here" (a directory, an empty file, a
// symlink) rather than an error: hide then fails through the ordinary
// "insufficient slack space" path instead of aborting a whole-image scan. A
// non-nil error wrapping filesystem.ErrUnsupported means the layout itself
// (compression, per-file encryption) makes the dstream's logical size
// meaningless, so a caller must be told clearly rather than silently getting
// zero regions.
func (f *FS) slackRegions(e *Entry) ([]filesystem.SlackRegion, error) {
	if e.isDir {
		return nil, nil
	}
	val, err := f.inodeRecordValue(e.oid)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	ds, ok, err := inodeDStream(val)
	if err != nil {
		return nil, fmt.Errorf("parse dstream xfield for object %d: %w", e.oid, err)
	}
	if !ok || ds.size == 0 {
		return nil, nil
	}
	// default_crypto_id 0 means "no per-file crypto context"; every other
	// value is refused rather than special-cased, since this parser has no
	// way to confirm any particular non-zero id is safe to treat as
	// plaintext without key material to check it against.
	if ds.defaultCryptoID != 0 {
		return nil, fmt.Errorf("%q uses per-file encryption (crypto id %d): %w", e.path, ds.defaultCryptoID, filesystem.ErrUnsupported)
	}

	names, err := f.xattrNames(e.oid)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if name == decmpfsXattrName {
			return nil, fmt.Errorf("%q is HFS-compressed (carries %s): %w", e.path, decmpfsXattrName, filesystem.ErrUnsupported)
		}
	}

	privateID := binary.LittleEndian.Uint64(val[8:16])
	exts, err := f.extentsOf(privateID)
	if err != nil {
		return nil, fmt.Errorf("read extents for %q: %w", e.path, err)
	}
	// f.img's own Size() is the container view's size (partition-relative
	// for a GPT-wrapped container); adding f.base bounds an absolute offset
	// against the same span New already validated the partition against.
	imgSize := f.base + f.img.Size()
	return slackFromExtents(exts, ds.size, f.blockSize, f.base, imgSize)
}

var _ filesystem.SlackSpaceCapable = (*Entry)(nil)

// SlackRegions implements filesystem.SlackSpaceCapable.
func (e *Entry) SlackRegions() ([]filesystem.SlackRegion, error) {
	regions, err := e.fs.slackRegions(e)
	if err != nil {
		return nil, fmt.Errorf("apfs: slack regions for %q: %w", e.path, err)
	}
	return regions, nil
}
