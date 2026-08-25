// Package apfs implements a read-only APFS parser: container superblock,
// checkpoint selection, object map, volume superblock, and the volume's
// filesystem B-tree, exposed through filesystem.FileSystem / filesystem.Entry.
//
// Named streams, timestomp, slack space, and live mode are not implemented
// here; see docs/work-breakdown.md items 18b/19c/20d/21e.
//
// # Limitations
//
// New returns a clear error, rather than a partial or silently wrong
// result, for every layout this parser doesn't handle:
//
//   - Encrypted volumes (apfs_fs_flags' APFS_FS_UNENCRYPTED bit clear):
//     content and, on an encrypted volume, filenames themselves are
//     unreadable without key material this parser doesn't have.
//   - A tree-form checkpoint descriptor area (nx_xp_desc_blocks' high bit
//     set).
//   - No recognizable container: neither an NXSB superblock at byte 0 nor
//     a GPT Apple_APFS partition.
//   - Any object (container/checkpoint/omap/volume superblock or B-tree
//     node) whose Fletcher-64 checksum doesn't match.
//
// Snapshots, hashed (BTNODE_HASHED) B-tree nodes, and the space
// manager/reaper are not read at all — this parser only walks the current
// volume's live filesystem tree.
package apfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/sahithyandev/nemo/internal/binutil"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

func init() {
	filesystem.Register(filesystem.Detector{
		Type:  filesystem.TypeAPFS,
		Sniff: sniff,
		New:   New,
		// Techniques: appended to as 18b/19c/20d land.
	})
}

const (
	nxMagic          = 0x4253584E // "NXSB" little-endian u32
	apsbMagic        = 0x42535041 // "APSB" little-endian u32
	objPhysSize      = 32
	nxSuperblockSize = 1400 // generous; only fixed-offset fields below are read
)

// apfsFSUnencrypted is a bit in apfs_superblock_t's apfs_fs_flags (offset
// 264, u64): when set, the volume carries no encryption. When clear, the
// volume is encrypted — this parser has no key material and can't decrypt
// file content or (on an encrypted volume) filenames, so it refuses to
// mount rather than silently returning wrong/empty results.
const apfsFSUnencrypted = 0x1

// sniff is handed at most the first 4096 bytes from offset 0 (per
// filesystem.Open) and must tolerate a shorter or empty slice.
func sniff(b []byte) bool {
	if hasNXMagic(b) {
		return true
	}
	_, _, ok := findAPFSPartition(b)
	return ok
}

func hasNXMagic(b []byte) bool {
	if len(b) < 36 {
		return false
	}
	return binary.LittleEndian.Uint32(b[32:36]) == nxMagic
}

// apfsPartitionGUID is the Apple_APFS partition type GUID
// (7C3457EF-0000-11AA-AA11-00306543ECAC) in on-disk mixed-endian form.
var apfsPartitionGUID = [16]byte{
	0xEF, 0x57, 0x34, 0x7C, 0x00, 0x00, 0xAA, 0x11,
	0xAA, 0x11, 0x00, 0x30, 0x65, 0x43, 0xEC, 0xAC,
}

// gptHeader holds the fields of a GPT header needed to locate the
// partition-entry array.
type gptHeader struct {
	partLBA    uint64
	numEntries uint32
	entrySize  uint32
}

// parseGPTHeader reads a GPT header out of b, which must contain at least
// offset 512..604 (b may be a short sniff prefix or a full image read).
func parseGPTHeader(b []byte) (gptHeader, bool) {
	const hdrOff = 512
	if len(b) < hdrOff+92 || string(b[hdrOff:hdrOff+8]) != "EFI PART" {
		return gptHeader{}, false
	}
	h := gptHeader{
		partLBA:    binary.LittleEndian.Uint64(b[hdrOff+72 : hdrOff+80]),
		numEntries: binary.LittleEndian.Uint32(b[hdrOff+80 : hdrOff+84]),
		entrySize:  binary.LittleEndian.Uint32(b[hdrOff+84 : hdrOff+88]),
	}
	if h.entrySize < 128 || h.numEntries == 0 || h.numEntries > 1<<20 {
		return gptHeader{}, false
	}
	return h, true
}

// findAPFSPartitionEntry scans the partition-entry array in entries (which
// must start at LBA h.partLBA, i.e. entries == b[base:]) for the first
// Apple_APFS partition, returning its first/last LBA.
func findAPFSPartitionEntry(h gptHeader, entries []byte) (firstLBA, lastLBA uint64, ok bool) {
	for i := uint32(0); i < h.numEntries; i++ {
		off := int64(i) * int64(h.entrySize)
		if off+128 > int64(len(entries)) {
			break
		}
		entry := entries[off : off+128]
		if [16]byte(entry[0:16]) != apfsPartitionGUID {
			continue
		}
		first := binary.LittleEndian.Uint64(entry[32:40])
		last := binary.LittleEndian.Uint64(entry[40:48])
		return first, last, true
	}
	return 0, 0, false
}

// findAPFSPartition looks for a GPT header at offset 512 ("EFI PART") and
// the first Apple_APFS partition entry within the sniffed prefix b.
func findAPFSPartition(b []byte) (firstLBA, lastLBA uint64, ok bool) {
	h, headerOK := parseGPTHeader(b)
	if !headerOK {
		return 0, 0, false
	}
	base := int64(h.partLBA) * 512
	if base < 0 || base > int64(len(b)) {
		return 0, 0, false
	}
	return findAPFSPartitionEntry(h, b[base:])
}

// findAPFSPartitionInImage is findAPFSPartition against the full image
// rather than a possibly-truncated sniff prefix, for use in New.
func findAPFSPartitionInImage(img image.Image) (firstLBA, lastLBA uint64, ok bool, err error) {
	head, err := readFull(img, 0, 604)
	if err != nil {
		return 0, 0, false, fmt.Errorf("apfs: read GPT header: %w", err)
	}
	h, headerOK := parseGPTHeader(head)
	if !headerOK {
		return 0, 0, false, nil
	}
	base := int64(h.partLBA) * 512
	entriesLen := int64(h.numEntries) * int64(h.entrySize)
	if base < 0 || entriesLen < 0 || base+entriesLen > img.Size() {
		return 0, 0, false, nil
	}
	entries, err := readFull(img, base, entriesLen)
	if err != nil {
		return 0, 0, false, fmt.Errorf("apfs: read GPT partition entries: %w", err)
	}
	first, last, ok := findAPFSPartitionEntry(h, entries)
	return first, last, ok, nil
}

// readFull reads exactly n bytes at off from img. image.Image's ReadAt
// contract (mirroring os.File.ReadAt, which RawImage delegates to) fills p
// or errors, but this loops defensively for any other implementation that
// may return a short read without error.
func readFull(img image.Image, off, n int64) ([]byte, error) {
	buf := make([]byte, n)
	var got int64
	for got < n {
		m, err := img.ReadAt(buf[got:], off+got)
		got += int64(m)
		if err != nil {
			if got == n {
				break
			}
			return nil, err
		}
		if m == 0 {
			return nil, fmt.Errorf("apfs: short read at %d (got %d of %d bytes)", off, got, n)
		}
	}
	return buf, nil
}

// section is an offset/size view over an image.Image, used to present a
// GPT-embedded APFS container as if it started at byte 0.
type section struct {
	img        image.Image
	base, size int64
}

var _ image.Image = (*section)(nil)

func (s *section) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off > s.size {
		return 0, fmt.Errorf("apfs: read out of range at %d", off)
	}
	if off+int64(len(p)) > s.size {
		p = p[:s.size-off]
	}
	return s.img.ReadAt(p, s.base+off)
}

func (s *section) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > s.size {
		return 0, fmt.Errorf("apfs: write out of range at %d", off)
	}
	return s.img.WriteAt(p, s.base+off)
}

func (s *section) Size() int64  { return s.size }
func (s *section) Path() string { return s.img.Path() }

// fletcher64 computes the Fletcher-64 checksum APFS uses for every
// obj_phys_t, over data[8:] (the checksum field itself is excluded) as
// little-endian u32 words, modulo 2^32-1.
func fletcher64(data []byte) uint64 {
	body := data[8:]
	n := len(body) / 4
	var sum1, sum2 uint64
	const mod = 0xFFFFFFFF
	for i := 0; i < n; i++ {
		w := uint64(binary.LittleEndian.Uint32(body[i*4 : i*4+4]))
		sum1 = (sum1 + w) % mod
		sum2 = (sum2 + sum1) % mod
	}
	c1 := mod - (sum1+sum2)%mod
	c2 := mod - (sum1+c1)%mod
	return c1 | (c2 << 32)
}

// imageReader is the subset of image.Image the read-only parser needs; kept
// narrow so btree.go's tree type doesn't have to depend on internal/image.
// Any image.Image satisfies it.
type imageReader interface {
	ReadAt(p []byte, off int64) (int, error)
	Size() int64
}

// readObject reads one blockSize-byte block at physical address paddr and
// verifies its Fletcher-64 checksum, returning an error on any mismatch or
// out-of-range address.
func readObject(img imageReader, paddr int64, blockSize uint32) ([]byte, error) {
	if paddr < 0 || blockSize == 0 {
		return nil, fmt.Errorf("apfs: invalid object address %d", paddr)
	}
	off := paddr * int64(blockSize)
	if off < 0 || off+int64(blockSize) > img.Size() {
		return nil, fmt.Errorf("apfs: object address %d out of range", paddr)
	}
	buf := make([]byte, blockSize)
	if _, err := img.ReadAt(buf, off); err != nil {
		return nil, fmt.Errorf("apfs: read object at %d: %w", paddr, err)
	}
	if len(buf) < objPhysSize {
		return nil, fmt.Errorf("apfs: object at %d shorter than obj_phys_t", paddr)
	}
	want := binary.LittleEndian.Uint64(buf[0:8])
	got := fletcher64(buf)
	if got != want {
		return nil, fmt.Errorf("apfs: checksum mismatch at object %d (want %#x, got %#x)", paddr, want, got)
	}
	return buf, nil
}

// objPhys is the decoded obj_phys_t header common to every APFS object.
type objPhys struct {
	oid     uint64
	xid     uint64
	typ     uint32
	subtype uint32
}

func decodeObjPhys(buf []byte) objPhys {
	return objPhys{
		oid:     binary.LittleEndian.Uint64(buf[8:16]),
		xid:     binary.LittleEndian.Uint64(buf[16:24]),
		typ:     binary.LittleEndian.Uint32(buf[24:28]),
		subtype: binary.LittleEndian.Uint32(buf[28:32]),
	}
}

// containerSB holds the fields of nx_superblock_t this parser needs.
type containerSB struct {
	blockSize    uint32
	blockCount   uint64
	xpDescBlocks uint32
	xpDescBase   int64
	omapOid      uint64
	maxFS        uint32
	fsOid        []uint64 // len == maxFS
	xid          uint64   // transaction id of the chosen checkpoint
}

// readContainerSuperblock reads block 0 to learn the checkpoint descriptor
// area's location, then scans that area for the checkpoint superblock with
// the highest verified transaction id.
func readContainerSuperblock(img image.Image) (*containerSB, error) {
	block0, err := readFull(img, 0, minInt64(4096, img.Size()))
	if err != nil {
		return nil, fmt.Errorf("apfs: read block 0: %w", err)
	}
	if !hasNXMagic(block0) {
		return nil, errors.New("apfs: block 0 is not an APFS container superblock")
	}
	blockSize := binary.LittleEndian.Uint32(block0[36:40])
	if blockSize == 0 {
		return nil, errors.New("apfs: nx_block_size is zero")
	}

	// Re-read block 0 at the real block size and verify its checksum (the
	// first readFull above deliberately skipped verification, since we
	// didn't yet know the real block size to read it at).
	block0, err = readObject(img, 0, blockSize)
	if err != nil {
		return nil, fmt.Errorf("apfs: read block 0: %w", err)
	}
	if !hasNXMagic(block0) {
		return nil, errors.New("apfs: block 0 is not an APFS container superblock")
	}

	xpDescBlocks := binary.LittleEndian.Uint32(block0[104:108])
	xpDescBase := int64(binary.LittleEndian.Uint64(block0[112:120]))
	if xpDescBlocks&0x80000000 != 0 {
		return nil, errors.New("apfs: unsupported: tree-form checkpoint descriptor area")
	}
	if xpDescBlocks == 0 {
		return nil, errors.New("apfs: nx_xp_desc_blocks is zero")
	}

	var best []byte
	var bestXid uint64
	for i := uint32(0); i < xpDescBlocks; i++ {
		buf, err := readObject(img, xpDescBase+int64(i), blockSize)
		if err != nil {
			continue // checkpoint-map blocks or stale entries fail checksum/type checks
		}
		if !hasNXMagic(buf) {
			continue // a checkpoint-map block, not a superblock
		}
		xid := decodeObjPhys(buf).xid
		if best == nil || xid > bestXid {
			best, bestXid = buf, xid
		}
	}
	if best == nil {
		return nil, errors.New("apfs: no valid checkpoint superblock found")
	}

	sb := &containerSB{
		blockSize:    blockSize,
		blockCount:   binary.LittleEndian.Uint64(best[40:48]),
		xpDescBlocks: xpDescBlocks,
		xpDescBase:   xpDescBase,
		omapOid:      binary.LittleEndian.Uint64(best[160:168]),
		maxFS:        binary.LittleEndian.Uint32(best[180:184]),
		xid:          bestXid,
	}
	if sb.maxFS == 0 || sb.maxFS > 100 {
		return nil, fmt.Errorf("apfs: implausible nx_max_file_systems %d", sb.maxFS)
	}
	sb.fsOid = make([]uint64, sb.maxFS)
	for i := uint32(0); i < sb.maxFS; i++ {
		off := 184 + int(i)*8
		sb.fsOid[i] = binary.LittleEndian.Uint64(best[off : off+8])
	}
	return sb, nil
}

// omapEntry is one resolved (oid, paddr) mapping.
type omap struct {
	img       image.Image
	blockSize uint32
	treeRoot  int64 // physical
}

// readOmap reads the omap_phys_t at the given physical oid and returns an
// omap ready for resolve().
func readOmap(img image.Image, oid uint64, blockSize uint32) (*omap, error) {
	buf, err := readObject(img, int64(oid), blockSize)
	if err != nil {
		return nil, fmt.Errorf("apfs: read object map: %w", err)
	}
	treeOid := binary.LittleEndian.Uint64(buf[48:56])
	return &omap{img: img, blockSize: blockSize, treeRoot: int64(treeOid)}, nil
}

// resolve looks up the physical address of the entry with the given oid and
// the largest xid <= atXid.
func (o *omap) resolve(oid uint64, atXid uint64) (int64, error) {
	// omap_key_t/omap_val_t are fixed-size: 16-byte keys, 16-byte leaf
	// values (non-leaf values are always the 8-byte child pointer,
	// regardless of fixedValSize; see decodeNode).
	t, err := openTree(o.img, o.blockSize, o.treeRoot, omapResolveIdentity, omapKeyCompare, 16, 16)
	if err != nil {
		return 0, err
	}
	// Seek to (oid, 0): the smallest possible key for this oid, i.e. its
	// oldest entry. Entries for one oid are sorted ascending by xid, so a
	// forward scan from there visits every version and can pick the
	// largest one <= atXid. Seeking directly to (oid, atXid) would be
	// wrong here — a forward-only cursor would skip every entry with a
	// smaller xid, which is exactly the common case (no entry has xid
	// equal to atXid).
	key := make([]byte, 16)
	binary.LittleEndian.PutUint64(key[0:8], oid)
	binary.LittleEndian.PutUint64(key[8:16], 0)

	c, err := t.seek(key)
	if err != nil {
		return 0, err
	}
	var bestPaddr int64
	var bestXid uint64
	found := false
	for {
		k := c.key()
		if k == nil {
			break
		}
		kOid := binary.LittleEndian.Uint64(k[0:8])
		kXid := binary.LittleEndian.Uint64(k[8:16])
		if kOid != oid {
			break
		}
		if kXid <= atXid && (!found || kXid > bestXid) {
			v := c.val()
			bestPaddr = int64(binary.LittleEndian.Uint64(v[8:16]))
			bestXid = kXid
			found = true
		}
		if !c.next() {
			break
		}
	}
	if err := c.err(); err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("apfs: object %d not found in object map (at xid %d)", oid, atXid)
	}
	return bestPaddr, nil
}

// omapResolveIdentity is used to open the object map's own tree, whose
// nodes are addressed physically (an omap tree cannot itself be indirected
// through another omap).
func omapResolveIdentity(oid uint64) (int64, error) { return int64(oid), nil }

// omapKeyCompare compares omap_key_t records: {ok_oid, ok_xid}, both u64 LE,
// sorted by oid then xid.
func omapKeyCompare(a, b []byte) int {
	aOid := binary.LittleEndian.Uint64(a[0:8])
	bOid := binary.LittleEndian.Uint64(b[0:8])
	if aOid != bOid {
		if aOid < bOid {
			return -1
		}
		return 1
	}
	aXid := binary.LittleEndian.Uint64(a[8:16])
	bXid := binary.LittleEndian.Uint64(b[8:16])
	switch {
	case aXid < bXid:
		return -1
	case aXid > bXid:
		return 1
	default:
		return 0
	}
}

// volumeSB holds the fields of apfs_superblock_t this parser needs.
type volumeSB struct {
	name        string
	omapOid     uint64 // physical
	rootTreeOid uint64 // virtual, resolved through the volume's own omap
}

func readVolumeSuperblock(img image.Image, paddr int64, blockSize uint32) (*volumeSB, error) {
	buf, err := readObject(img, paddr, blockSize)
	if err != nil {
		return nil, fmt.Errorf("apfs: read volume superblock: %w", err)
	}
	if len(buf) < 960 || binary.LittleEndian.Uint32(buf[32:36]) != apsbMagic {
		return nil, errors.New("apfs: not a valid volume superblock (APSB)")
	}
	fsFlags := binary.LittleEndian.Uint64(buf[264:272])
	if fsFlags&apfsFSUnencrypted == 0 {
		return nil, errors.New("apfs: encrypted volumes are not supported")
	}
	name, err := binutil.String(buf, 704, 256)
	if err != nil {
		return nil, fmt.Errorf("apfs: read volume name: %w", err)
	}
	return &volumeSB{
		name:        name,
		omapOid:     binary.LittleEndian.Uint64(buf[128:136]),
		rootTreeOid: binary.LittleEndian.Uint64(buf[136:144]),
	}, nil
}

// FS is a read-only, mounted APFS volume (the first volume in the
// container).
type FS struct {
	img       image.Image
	blockSize uint32
	xid       uint64
	volume    *volumeSB
	volOmap   *omap
	fsTree    *tree
}

var _ filesystem.FileSystem = (*FS)(nil)

// New constructs an APFS FileSystem from img. It handles both a bare
// container (nx_superblock_t at byte 0) and a GPT-wrapped one.
func New(img image.Image) (filesystem.FileSystem, error) {
	prefix, err := readFull(img, 0, minInt64(4096, img.Size()))
	if err != nil {
		return nil, fmt.Errorf("apfs: read prefix: %w", err)
	}

	var container image.Image = img
	if !hasNXMagic(prefix) {
		first, last, ok, err := findAPFSPartitionInImage(img)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("apfs: no APFS container found (no NXSB, no GPT Apple_APFS partition)")
		}
		base := int64(first) * 512
		size := int64(last-first+1) * 512
		if base < 0 || size <= 0 || base+size > img.Size() {
			return nil, errors.New("apfs: GPT Apple_APFS partition out of range")
		}
		container = &section{img: img, base: base, size: size}
	}

	sb, err := readContainerSuperblock(container)
	if err != nil {
		return nil, err
	}

	containerOmap, err := readOmap(container, sb.omapOid, sb.blockSize)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, oid := range sb.fsOid {
		if oid == 0 {
			continue
		}
		paddr, err := containerOmap.resolve(oid, sb.xid)
		if err != nil {
			lastErr = err
			continue
		}
		vol, err := readVolumeSuperblock(container, paddr, sb.blockSize)
		if err != nil {
			lastErr = err
			continue
		}
		volOmap, err := readOmap(container, vol.omapOid, sb.blockSize)
		if err != nil {
			lastErr = err
			continue
		}
		rootPaddr, err := volOmap.resolve(vol.rootTreeOid, sb.xid)
		if err != nil {
			lastErr = err
			continue
		}
		resolve := func(oid uint64) (int64, error) {
			return volOmap.resolve(oid, sb.xid)
		}
		// The fs tree is always variable-kv (names vary in length), so no
		// fixed key/value size applies.
		fsTree, err := openTree(container, sb.blockSize, rootPaddr, resolve, fsKeyCompare, 0, 0)
		if err != nil {
			lastErr = err
			continue
		}
		return &FS{
			img:       container,
			blockSize: sb.blockSize,
			xid:       sb.xid,
			volume:    vol,
			volOmap:   volOmap,
			fsTree:    fsTree,
		}, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("apfs: no mountable volume found: %w", lastErr)
	}
	return nil, errors.New("apfs: container has no volumes")
}

func (f *FS) Type() filesystem.Type { return filesystem.TypeAPFS }

func (f *FS) Root() filesystem.Entry {
	return &Entry{fs: f, oid: rootDirOid, path: "/", isDir: true}
}

func (f *FS) Open(path string) (filesystem.Entry, error) {
	clean := strings.Trim(path, "/")
	if clean == "" {
		return f.Root(), nil
	}
	cur := f.Root().(*Entry)
	parts := strings.Split(clean, "/")
	for i, part := range parts {
		children, err := cur.Children()
		if err != nil {
			return nil, err
		}
		var next *Entry
		for _, c := range children {
			e := c.(*Entry)
			if entryName(e.path) == part {
				next = e
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("apfs: %q not found", "/"+strings.Join(parts[:i+1], "/"))
		}
		cur = next
	}
	return cur, nil
}

func entryName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// Entry is a file or directory reached by walking the volume's filesystem
// B-tree.
type Entry struct {
	fs    *FS
	oid   uint64
	path  string
	isDir bool
}

var _ filesystem.Entry = (*Entry)(nil)

func (e *Entry) Path() string { return e.path }
func (e *Entry) IsDir() bool  { return e.isDir }

// Children lists the directory entries of e by scanning DIR_REC records for
// e.oid in the volume's fs tree. Returns nil for a non-directory.
func (e *Entry) Children() ([]filesystem.Entry, error) {
	if !e.isDir {
		return nil, nil
	}
	c, err := e.fs.fsTree.seek(encodeJKey(e.oid, objTypeDirRec))
	if err != nil {
		return nil, fmt.Errorf("apfs: seek children of %q: %w", e.path, err)
	}

	var out []filesystem.Entry
	for k := c.key(); k != nil; k = c.key() {
		oid, typ, kerr := decodeJKey(k)
		if kerr != nil || oid != e.oid || typ != objTypeDirRec {
			break
		}
		if name, ok := decodeDrecKey(k); ok {
			if fileID, isDir, verr := decodeDrecVal(c.val()); verr == nil {
				out = append(out, &Entry{fs: e.fs, oid: fileID, path: joinPath(e.path, name), isDir: isDir})
			}
		}
		if !c.next() {
			break
		}
	}
	if err := c.err(); err != nil {
		return nil, fmt.Errorf("apfs: list children of %q: %w", e.path, err)
	}
	return out, nil
}

// NamedStreams always returns no streams for now; xattr listing lands in
// work-breakdown item 18b.
func (e *Entry) NamedStreams() ([]string, error) { return nil, nil }

func joinPath(dir, name string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + name
	}
	return dir + "/" + name
}
