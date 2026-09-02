// Package ext4 implements traversal and native xattr mutation of extent-backed ext4 images.
package ext4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

const (
	superblockOffset = int64(1024)
	superblockSize   = 1024
	ext4Magic        = 0xef53

	featureIncompatFiletype     = 0x0002
	featureIncompatExtents      = 0x0040
	featureIncompat64Bit        = 0x0080
	featureIncompatFlexBG       = 0x0200
	featureIncompatCsumSeed     = 0x2000
	featureROCompatBigalloc     = 0x0200
	featureROCompatMetadataCsum = 0x0400

	inodeFlagExtents    = 0x00080000
	inodeFlagIndex      = 0x00001000
	inodeFlagInlineData = 0x10000000
	inodeFlagEncrypted  = 0x00000800

	modeTypeMask = 0xf000
	modeDir      = 0x4000
)

var supportedIncompat = uint32(featureIncompatFiletype | featureIncompatExtents | featureIncompat64Bit | featureIncompatFlexBG | featureIncompatCsumSeed)

func init() {
	filesystem.Register(filesystem.Detector{
		Type:  filesystem.TypeEXT4,
		Sniff: Sniff,
		New:   New,
	})
}

// Sniff reports whether signature contains an ext4 superblock magic value.
func Sniff(signature []byte) bool {
	const magicOffset = 1024 + 0x38
	return len(signature) >= magicOffset+2 && binary.LittleEndian.Uint16(signature[magicOffset:]) == ext4Magic
}

type superblock struct {
	inodesCount     uint32
	blocksCount     uint64
	firstDataBlock  uint32
	blockSize       uint32
	blocksPerGroup  uint32
	inodesPerGroup  uint32
	inodeSize       uint16
	descSize        uint16
	featureIncompat uint32
	featureROCompat uint32
	uuid            [16]byte
	checksumSeed    uint32
}

// FS is a view of an ext4 filesystem image.
type FS struct {
	img        image.Image
	sb         superblock
	groupCount uint64
	gdtOffset  int64
}

var _ filesystem.FileSystem = (*FS)(nil)

// New validates an ext4 superblock and prepares inode lookup.
func New(img image.Image) (filesystem.FileSystem, error) {
	if img == nil {
		return nil, errors.New("ext4: nil image")
	}
	b := make([]byte, superblockSize)
	if err := readExact(img, b, superblockOffset); err != nil {
		return nil, fmt.Errorf("ext4: read superblock: %w", err)
	}
	if binary.LittleEndian.Uint16(b[0x38:]) != ext4Magic {
		return nil, errors.New("ext4: invalid superblock magic")
	}

	logBlockSize := binary.LittleEndian.Uint32(b[0x18:])
	if logBlockSize > 6 {
		return nil, fmt.Errorf("ext4: unsupported block-size shift %d", logBlockSize)
	}
	blockSize := uint32(1024) << logBlockSize
	incompat := binary.LittleEndian.Uint32(b[0x60:])
	if unsupported := incompat &^ supportedIncompat; unsupported != 0 {
		return nil, fmt.Errorf("ext4: unsupported incompatible feature flags 0x%x", unsupported)
	}
	if incompat&featureIncompatExtents == 0 {
		return nil, errors.New("ext4: filesystems without extents are unsupported")
	}
	roCompat := binary.LittleEndian.Uint32(b[0x64:])
	if roCompat&featureROCompatBigalloc != 0 {
		return nil, errors.New("ext4: bigalloc filesystems are unsupported")
	}

	blocksCount := uint64(binary.LittleEndian.Uint32(b[0x04:]))
	if incompat&featureIncompat64Bit != 0 {
		blocksCount |= uint64(binary.LittleEndian.Uint32(b[0x150:])) << 32
	}
	inodeSize := binary.LittleEndian.Uint16(b[0x58:])
	if inodeSize == 0 {
		inodeSize = 128
	}
	descSize := uint16(32)
	if incompat&featureIncompat64Bit != 0 {
		descSize = binary.LittleEndian.Uint16(b[0xfe:])
		if descSize < 64 || descSize%8 != 0 || uint32(descSize) > blockSize {
			return nil, fmt.Errorf("ext4: invalid 64-bit group descriptor size %d", descSize)
		}
	}

	sb := superblock{
		inodesCount:     binary.LittleEndian.Uint32(b[0x00:]),
		blocksCount:     blocksCount,
		firstDataBlock:  binary.LittleEndian.Uint32(b[0x14:]),
		blockSize:       blockSize,
		blocksPerGroup:  binary.LittleEndian.Uint32(b[0x20:]),
		inodesPerGroup:  binary.LittleEndian.Uint32(b[0x28:]),
		inodeSize:       inodeSize,
		descSize:        descSize,
		featureIncompat: incompat,
		featureROCompat: roCompat,
	}
	copy(sb.uuid[:], b[0x68:0x78])
	if incompat&featureIncompatCsumSeed != 0 {
		sb.checksumSeed = binary.LittleEndian.Uint32(b[0x270:])
	} else {
		sb.checksumSeed = ext4CRC32C(^uint32(0), sb.uuid[:])
	}
	if err := validateGeometry(sb, img.Size()); err != nil {
		return nil, err
	}

	groups := (blocksCount - uint64(sb.firstDataBlock) + uint64(sb.blocksPerGroup) - 1) / uint64(sb.blocksPerGroup)
	gdtBlock := uint64(sb.firstDataBlock) + 1
	gdtOffset, ok := byteOffset(gdtBlock, uint64(blockSize), img.Size())
	if !ok || uint64(descSize) > uint64(img.Size()-gdtOffset) || groups > uint64(img.Size()-gdtOffset)/uint64(descSize) {
		return nil, errors.New("ext4: group descriptor table exceeds image bounds")
	}

	f := &FS{img: img, sb: sb, groupCount: groups, gdtOffset: gdtOffset}
	root, err := f.readInode(2)
	if err != nil {
		return nil, fmt.Errorf("ext4: read root inode: %w", err)
	}
	if root.mode&modeTypeMask != modeDir {
		return nil, errors.New("ext4: root inode is not a directory")
	}
	return f, nil
}

func validateGeometry(sb superblock, imageSize int64) error {
	if imageSize < superblockOffset+superblockSize {
		return errors.New("ext4: image is too small for a superblock")
	}
	if sb.inodesCount < 2 || sb.blocksCount == 0 || sb.blocksPerGroup == 0 || sb.inodesPerGroup == 0 {
		return errors.New("ext4: invalid zero filesystem geometry")
	}
	if sb.blocksCount <= uint64(sb.firstDataBlock) {
		return errors.New("ext4: invalid block count")
	}
	if (sb.blockSize == 1024 && sb.firstDataBlock != 1) || (sb.blockSize > 1024 && sb.firstDataBlock != 0) {
		return errors.New("ext4: first data block does not match block size")
	}
	if sb.blocksPerGroup > sb.blockSize*8 {
		return errors.New("ext4: blocks per group exceeds bitmap capacity")
	}
	if sb.inodeSize < 128 || uint32(sb.inodeSize) > sb.blockSize || sb.inodeSize%4 != 0 {
		return fmt.Errorf("ext4: invalid inode size %d", sb.inodeSize)
	}
	if sb.blocksCount > uint64(imageSize)/uint64(sb.blockSize) {
		return errors.New("ext4: filesystem geometry exceeds image bounds")
	}
	blockGroups := (sb.blocksCount - uint64(sb.firstDataBlock) + uint64(sb.blocksPerGroup) - 1) / uint64(sb.blocksPerGroup)
	inodeGroups := (uint64(sb.inodesCount) + uint64(sb.inodesPerGroup) - 1) / uint64(sb.inodesPerGroup)
	if inodeGroups > blockGroups {
		return errors.New("ext4: inode groups exceed block groups")
	}
	return nil
}

func (f *FS) Type() filesystem.Type { return filesystem.TypeEXT4 }

func (f *FS) Root() filesystem.Entry {
	return &Entry{fs: f, inode: 2, path: "/", isDir: true}
}

func (f *FS) Open(name string) (filesystem.Entry, error) {
	clean := path.Clean("/" + name)
	if clean == "/" {
		return f.Root(), nil
	}
	cur := f.Root().(*Entry)
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for i, part := range parts {
		children, err := cur.Children()
		if err != nil {
			return nil, err
		}
		var next *Entry
		for _, child := range children {
			candidate := child.(*Entry)
			if path.Base(candidate.path) == part {
				next = candidate
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("ext4: %q not found", "/"+strings.Join(parts[:i+1], "/"))
		}
		if i < len(parts)-1 && !next.isDir {
			return nil, fmt.Errorf("ext4: %q is not a directory", next.path)
		}
		cur = next
	}
	return cur, nil
}

// Entry is an inode reached by walking ext4 directory records.
type Entry struct {
	fs    *FS
	inode uint32
	path  string
	isDir bool
}

var _ filesystem.Entry = (*Entry)(nil)
var _ filesystem.NamedStreamCapable = (*Entry)(nil)

func (e *Entry) Path() string { return e.path }
func (e *Entry) IsDir() bool  { return e.isDir }

// Children returns the entries in a linear, extent-backed directory.
func (e *Entry) Children() ([]filesystem.Entry, error) {
	if !e.isDir {
		return nil, nil
	}
	in, err := e.fs.readInode(e.inode)
	if err != nil {
		return nil, fmt.Errorf("ext4: read directory inode %d: %w", e.inode, err)
	}
	if in.flags&inodeFlagIndex != 0 {
		return nil, fmt.Errorf("ext4: indexed directory %q is unsupported", e.path)
	}
	blocks, err := e.fs.extentBlocks(in)
	if err != nil {
		return nil, fmt.Errorf("ext4: read extents for %q: %w", e.path, err)
	}

	var out []filesystem.Entry
	remaining := in.size
	for i, block := range blocks {
		if remaining == 0 {
			break
		}
		if block.logical != uint32(i) {
			return nil, fmt.Errorf("ext4: directory %q has a hole at logical block %d", e.path, i)
		}
		buf, err := e.fs.readBlock(block.physical)
		if err != nil {
			return nil, fmt.Errorf("ext4: read directory block for %q: %w", e.path, err)
		}
		limit := uint64(len(buf))
		if remaining < limit {
			limit = remaining
		}
		entries, err := e.fs.parseDirectory(buf[:limit], e.path)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
		remaining -= limit
	}
	if remaining != 0 {
		return nil, fmt.Errorf("ext4: directory %q data is shorter than inode size", e.path)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path() < out[j].Path() })
	return out, nil
}

func (e *Entry) NamedStreams() ([]string, error) {
	attrs, err := e.fs.readXattrs(e.inode)
	if err != nil {
		return nil, fmt.Errorf("ext4: list xattrs for %q: %w", e.path, err)
	}
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (e *Entry) ReadStream(name string) ([]byte, error) {
	return e.fs.readXattr(e.inode, name)
}

func (e *Entry) WriteStream(name string, data []byte) error {
	if err := e.fs.writeXattr(e.inode, name, data); err != nil {
		return fmt.Errorf("ext4: write xattr %q on %q: %w", name, e.path, err)
	}
	return nil
}

func (e *Entry) DeleteStream(name string) error {
	if err := e.fs.deleteXattr(e.inode, name); err != nil {
		return fmt.Errorf("ext4: delete xattr %q on %q: %w", name, e.path, err)
	}
	return nil
}

func (f *FS) parseDirectory(block []byte, parent string) ([]filesystem.Entry, error) {
	var entries []filesystem.Entry
	for off := 0; off < len(block); {
		if len(block)-off < 8 {
			return nil, fmt.Errorf("ext4: truncated directory record in %q at offset %d", parent, off)
		}
		inodeNumber := binary.LittleEndian.Uint32(block[off:])
		recordLength := int(binary.LittleEndian.Uint16(block[off+4:]))
		nameLength := int(block[off+6])
		if recordLength < 8 || recordLength%4 != 0 || recordLength > len(block)-off || nameLength > recordLength-8 {
			return nil, fmt.Errorf("ext4: invalid directory record in %q at offset %d", parent, off)
		}
		if inodeNumber != 0 && nameLength != 0 {
			name := string(block[off+8 : off+8+nameLength])
			if name != "." && name != ".." {
				if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\x00') {
					return nil, fmt.Errorf("ext4: invalid directory name in %q", parent)
				}
				in, err := f.readInode(inodeNumber)
				if err != nil {
					return nil, fmt.Errorf("ext4: read inode %d for %q: %w", inodeNumber, name, err)
				}
				entries = append(entries, &Entry{fs: f, inode: inodeNumber, path: joinPath(parent, name), isDir: in.mode&modeTypeMask == modeDir})
			}
		}
		off += recordLength
	}
	return entries, nil
}

func (f *FS) readBlock(number uint64) ([]byte, error) {
	off, ok := byteOffset(number, uint64(f.sb.blockSize), f.img.Size())
	if !ok || number >= f.sb.blocksCount || int64(f.sb.blockSize) > f.img.Size()-off {
		return nil, fmt.Errorf("block %d exceeds filesystem bounds", number)
	}
	b := make([]byte, f.sb.blockSize)
	if err := readExact(f.img, b, off); err != nil {
		return nil, err
	}
	return b, nil
}

func readExact(img image.Image, b []byte, off int64) error {
	n, err := img.ReadAt(b, off)
	if n == len(b) {
		return nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return err
}

func byteOffset(index, size uint64, limit int64) (int64, bool) {
	if limit < 0 || size != 0 && index > uint64(limit)/size {
		return 0, false
	}
	off := index * size
	if off > uint64(limit) {
		return 0, false
	}
	return int64(off), true
}

func joinPath(parent, name string) string {
	if parent == "/" {
		return parent + name
	}
	return parent + "/" + name
}
