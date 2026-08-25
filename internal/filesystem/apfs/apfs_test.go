package apfs

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

var fixtures = []string{"apfs-bare", "apfs-gpt"}

func TestContainerSuperblock(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			img := loadImage(t, name)
			fs, err := New(img)
			if err != nil {
				t.Fatalf("New(%s): %v", name, err)
			}
			apfsFS, ok := fs.(*FS)
			if !ok {
				t.Fatalf("New(%s) returned %T, want *FS", name, fs)
			}
			if apfsFS.blockSize != 4096 {
				t.Fatalf("blockSize = %d, want 4096", apfsFS.blockSize)
			}
		})
	}
}

func TestVolumeName(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fs, err := New(loadImage(t, name))
			if err != nil {
				t.Fatalf("New(%s): %v", name, err)
			}
			apfsFS := fs.(*FS)
			if apfsFS.volume.name != "NEMO" {
				t.Fatalf("volume name = %q, want %q", apfsFS.volume.name, "NEMO")
			}
		})
	}
}

func TestRootChildren(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fs, err := New(loadImage(t, name))
			if err != nil {
				t.Fatalf("New(%s): %v", name, err)
			}
			children, err := fs.Root().Children()
			if err != nil {
				t.Fatalf("Root().Children(): %v", err)
			}

			want := map[string]bool{"hello.txt": false, "xattr.txt": false, "slack.bin": false}
			for _, c := range children {
				n := entryName(c.Path())
				if _, ok := want[n]; ok {
					want[n] = true
					if c.IsDir() {
						t.Errorf("entry %q reported as a directory", n)
					}
				}
			}
			for n, found := range want {
				if !found {
					t.Errorf("expected child %q not found among %d children", n, len(children))
				}
			}
		})
	}
}

func TestOpenPath(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fs, err := New(loadImage(t, name))
			if err != nil {
				t.Fatalf("New(%s): %v", name, err)
			}

			root, err := fs.Open("/")
			if err != nil {
				t.Fatalf("Open(/): %v", err)
			}
			if !root.IsDir() {
				t.Fatalf("Open(/): not a directory")
			}

			hello, err := fs.Open("/hello.txt")
			if err != nil {
				t.Fatalf("Open(/hello.txt): %v", err)
			}
			if hello.IsDir() {
				t.Fatalf("Open(/hello.txt): reported as a directory")
			}

			if _, err := fs.Open("/nope.txt"); err == nil {
				t.Fatalf("Open(/nope.txt): expected error, got nil")
			}
		})
	}
}

func TestDetectorRegistered(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fs, err := filesystem.Open(loadImage(t, name))
			if err != nil {
				t.Fatalf("filesystem.Open(%s): %v", name, err)
			}
			if fs.Type() != filesystem.TypeAPFS {
				t.Fatalf("Type() = %q, want %q", fs.Type(), filesystem.TypeAPFS)
			}
		})
	}
}

// TestChecksumRejects flips one byte inside the checkpoint descriptor area
// on a private, writable fixture copy and confirms New refuses to parse
// past the corrupted checksum rather than silently misreading it.
func TestChecksumRejects(t *testing.T) {
	img := loadImage(t, "apfs-bare")

	// Corrupt a byte well inside block 0's obj_phys_t-checksummed body
	// (past the 32-byte header, before nx_block_size at offset 36) so the
	// container's own checksum no longer matches.
	f, err := os.OpenFile(img.Path(), os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open fixture for corruption: %v", err)
	}
	defer f.Close()
	orig := make([]byte, 1)
	if _, err := f.ReadAt(orig, 100); err != nil {
		t.Fatalf("read byte to corrupt: %v", err)
	}
	corrupt := []byte{orig[0] ^ 0xFF}
	if _, err := f.WriteAt(corrupt, 100); err != nil {
		t.Fatalf("corrupt byte: %v", err)
	}

	if _, err := New(img); err == nil {
		t.Fatalf("New: expected checksum error, got nil")
	}
}

// TestReadVolumeSuperblockRejectsEncrypted builds a synthetic, checksum-valid
// apfs_superblock_t with the APFS_FS_UNENCRYPTED bit clear and confirms it's
// rejected with a clear error rather than parsed (and, downstream, rather
// than silently returning wrong results — an encrypted volume's on-disk
// filenames are also encrypted, so a naive parse would just come back with
// empty-looking directories instead of failing loudly).
func TestReadVolumeSuperblockRejectsEncrypted(t *testing.T) {
	const blockSize = 4096
	buf := make([]byte, blockSize)
	binary.LittleEndian.PutUint32(buf[32:36], apsbMagic)
	binary.LittleEndian.PutUint64(buf[264:272], 0) // apfs_fs_flags: UNENCRYPTED bit clear
	copy(buf[704:], "ENCVOL")
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))

	img := fakefs.NewImage(blockSize)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write synthetic volume superblock: %v", err)
	}

	_, err := readVolumeSuperblock(img, 0, blockSize)
	if err == nil {
		t.Fatalf("readVolumeSuperblock: expected error for encrypted volume, got nil")
	}
	if !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("readVolumeSuperblock error = %q, want it to mention encryption", err)
	}
}

// TestReadVolumeSuperblockAcceptsUnencrypted is the control case: the same
// synthetic superblock with the UNENCRYPTED bit set must parse successfully.
func TestReadVolumeSuperblockAcceptsUnencrypted(t *testing.T) {
	const blockSize = 4096
	buf := make([]byte, blockSize)
	binary.LittleEndian.PutUint32(buf[32:36], apsbMagic)
	binary.LittleEndian.PutUint64(buf[264:272], apfsFSUnencrypted)
	copy(buf[704:], "PLAINVOL")
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))

	img := fakefs.NewImage(blockSize)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write synthetic volume superblock: %v", err)
	}

	vol, err := readVolumeSuperblock(img, 0, blockSize)
	if err != nil {
		t.Fatalf("readVolumeSuperblock: %v", err)
	}
	if vol.name != "PLAINVOL" {
		t.Fatalf("volume name = %q, want %q", vol.name, "PLAINVOL")
	}
}

func TestParseGPTHeaderRejectsOversizedEntriesTable(t *testing.T) {
	tests := []struct {
		name       string
		numEntries uint32
		entrySize  uint32
		wantOK     bool
	}{
		{"standard layout", 128, 128, true},                  // 16 KiB total, real-world GPT
		{"huge entrySize, one entry", 1, 0xFFFFFFFF, false},  // entrySize alone can blow the cap
		{"many entries, standard size", 1 << 20, 128, false}, // 128 MiB total
		{"at the cap", 1024, 1024, true},                     // exactly 1 MiB
		{"just over the cap", 1024, 1025, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, 604)
			copy(buf[512:520], "EFI PART")
			binary.LittleEndian.PutUint64(buf[512+72:512+80], 2) // part_entry_lba
			binary.LittleEndian.PutUint32(buf[512+80:512+84], tc.numEntries)
			binary.LittleEndian.PutUint32(buf[512+84:512+88], tc.entrySize)

			_, ok := parseGPTHeader(buf)
			if ok != tc.wantOK {
				t.Fatalf("parseGPTHeader(numEntries=%d, entrySize=%d) ok = %v, want %v", tc.numEntries, tc.entrySize, ok, tc.wantOK)
			}
		})
	}
}

// TestFindAPFSPartitionInImageRejectsHugeEntriesTable is an end-to-end
// regression test: a crafted GPT header claiming an 8 MiB-per-entry
// partition table, on an image large enough (16 MiB) that the old
// "base+entriesLen <= img.Size()" bounds check alone would NOT have caught
// it. Only gptMaxEntriesTableSize does, and it must reject before
// findAPFSPartitionInImage's readFull ever allocates that much.
func TestFindAPFSPartitionInImageRejectsHugeEntriesTable(t *testing.T) {
	const bogusEntrySize = 8 << 20 // 8 MiB; real GPT entries are 128 bytes
	img := fakefs.NewImage(2 * bogusEntrySize)

	buf := make([]byte, 604)
	copy(buf[512:520], "EFI PART")
	binary.LittleEndian.PutUint64(buf[512+72:512+80], 2) // part_entry_lba
	binary.LittleEndian.PutUint32(buf[512+80:512+84], 1) // numEntries
	binary.LittleEndian.PutUint32(buf[512+84:512+88], bogusEntrySize)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write synthetic GPT header: %v", err)
	}

	_, _, ok, err := findAPFSPartitionInImage(img)
	if err != nil {
		t.Fatalf("findAPFSPartitionInImage: unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("findAPFSPartitionInImage: expected ok=false for an oversized entries table")
	}
}

func TestValidateBlockSize(t *testing.T) {
	tests := []struct {
		name      string
		blockSize uint32
		wantErr   bool
	}{
		{"zero", 0, true},
		{"too small", 2048, true},
		{"minimum", 4096, false},
		{"default-ish", 8192, false},
		{"maximum", 65536, false},
		{"too large", 65536 * 2, true},
		{"in range but not power of two", 5000, true},
		{"implausibly large", 1 << 30, true},
		{"max uint32, not power of two", 0xFFFFFFFF, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBlockSize(tc.blockSize)
			if tc.wantErr && err == nil {
				t.Fatalf("validateBlockSize(%d): expected error, got nil", tc.blockSize)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateBlockSize(%d): unexpected error: %v", tc.blockSize, err)
			}
		})
	}
}

// TestReadContainerSuperblockRejectsImplausibleBlockSize is a regression
// test for a crafted nx_block_size that's within the image's own size but
// far outside APFS's actual valid range. The image is deliberately made
// large enough (2 MiB) that readObject's own "off+blockSize <= img.Size()"
// bounds check would NOT have caught a 1 MiB declared block size — only
// validateBlockSize does, which is the point of this test: without it, a
// crafted image sized to match its own bogus block size would sail through
// and drive a correspondingly huge allocation on every subsequent object
// read.
func TestReadContainerSuperblockRejectsImplausibleBlockSize(t *testing.T) {
	const bogusBlockSize = 1 << 20 // 1 MiB; APFS's real max is 65536
	img := fakefs.NewImage(2 * bogusBlockSize)
	buf := make([]byte, 4096)
	binary.LittleEndian.PutUint32(buf[32:36], nxMagic)
	binary.LittleEndian.PutUint32(buf[36:40], bogusBlockSize)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write synthetic block 0: %v", err)
	}

	if _, err := readContainerSuperblock(img); err == nil {
		t.Fatalf("readContainerSuperblock: expected error for implausible block size, got nil")
	}
}

// buildValidBlock0 builds a checksum-valid nx_superblock_t with sane
// defaults (blockSize 4096, maxFS 1, all-zero fsOid), self-referencing as
// its own single checkpoint-descriptor entry (xpDescBase=0, xpDescBlocks=1)
// so readContainerSuperblock's checkpoint scan re-reads this same block and
// succeeds without needing a separate checkpoint area. Callers tweak one
// field and recompute the checksum to test a specific rejection.
func buildValidBlock0(blockSize uint32) []byte {
	buf := make([]byte, blockSize)
	binary.LittleEndian.PutUint32(buf[32:36], nxMagic)
	binary.LittleEndian.PutUint32(buf[36:40], blockSize)
	binary.LittleEndian.PutUint32(buf[104:108], 1) // nx_xp_desc_blocks
	binary.LittleEndian.PutUint64(buf[112:120], 0) // nx_xp_desc_base (self)
	binary.LittleEndian.PutUint32(buf[180:184], 1) // nx_max_file_systems
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))
	return buf
}

func TestReadContainerSuperblockAcceptsSelfReferencingCheckpoint(t *testing.T) {
	const bs = 4096
	img := fakefs.NewImage(bs)
	if _, err := img.WriteAt(buildValidBlock0(bs), 0); err != nil {
		t.Fatalf("write block 0: %v", err)
	}
	sb, err := readContainerSuperblock(img)
	if err != nil {
		t.Fatalf("readContainerSuperblock: %v", err)
	}
	if sb.maxFS != 1 {
		t.Fatalf("maxFS = %d, want 1", sb.maxFS)
	}
}

func TestReadContainerSuperblockRejectsTreeFormCheckpoint(t *testing.T) {
	const bs = 4096
	buf := buildValidBlock0(bs)
	binary.LittleEndian.PutUint32(buf[104:108], 1|0x80000000) // high bit set
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))  // recompute after edit

	img := fakefs.NewImage(bs)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write block 0: %v", err)
	}
	_, err := readContainerSuperblock(img)
	if err == nil {
		t.Fatalf("readContainerSuperblock: expected error for tree-form checkpoint descriptor, got nil")
	}
	if !strings.Contains(err.Error(), "tree-form") {
		t.Fatalf("error = %q, want it to mention tree-form", err)
	}
}

func TestReadContainerSuperblockRejectsZeroXpDescBlocks(t *testing.T) {
	const bs = 4096
	buf := buildValidBlock0(bs)
	binary.LittleEndian.PutUint32(buf[104:108], 0)
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))

	img := fakefs.NewImage(bs)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write block 0: %v", err)
	}
	if _, err := readContainerSuperblock(img); err == nil {
		t.Fatalf("readContainerSuperblock: expected error for zero nx_xp_desc_blocks, got nil")
	}
}

func TestReadContainerSuperblockRejectsImplausibleMaxFS(t *testing.T) {
	const bs = 4096
	buf := buildValidBlock0(bs)
	binary.LittleEndian.PutUint32(buf[180:184], 101) // > 100
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))

	img := fakefs.NewImage(bs)
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write block 0: %v", err)
	}
	if _, err := readContainerSuperblock(img); err == nil {
		t.Fatalf("readContainerSuperblock: expected error for implausible nx_max_file_systems, got nil")
	}
}

// TestNewRejectsNoContainer covers New's top-level failure mode: an image
// with neither an NXSB superblock at byte 0 nor a GPT header at all.
func TestNewRejectsNoContainer(t *testing.T) {
	img := fakefs.NewImage(8192) // all zero: no "NXSB", no "EFI PART"
	if _, err := New(img); err == nil {
		t.Fatalf("New: expected error for an image with no recognizable container, got nil")
	}
}

func TestReadVolumeSuperblockRejectsBadMagic(t *testing.T) {
	const bs = 4096
	img := fakefs.NewImage(bs)
	buf := make([]byte, bs) // zeroed: no APSB magic
	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))
	if _, err := img.WriteAt(buf, 0); err != nil {
		t.Fatalf("write volume superblock: %v", err)
	}
	if _, err := readVolumeSuperblock(img, 0, bs); err == nil {
		t.Fatalf("readVolumeSuperblock: expected error for missing APSB magic, got nil")
	}
}

// TestOmapResolve builds a minimal single-node (root+leaf) fixed-kv omap
// tree with one entry and confirms both the success path and the
// oid-not-found error path.
func TestOmapResolve(t *testing.T) {
	const bs = 256
	buf := make([]byte, bs)
	binary.LittleEndian.PutUint16(buf[32:34], btnodeRoot|btnodeLeaf|btnodeFixedKVSize)
	binary.LittleEndian.PutUint32(buf[36:40], 1) // nkeys
	binary.LittleEndian.PutUint16(buf[40:42], 0) // table_space.off
	binary.LittleEndian.PutUint16(buf[42:44], 4) // table_space.len: one kvoff_t

	// keyBase = 56+0+4 = 60. valEnd = bs-40 = 216.
	binary.LittleEndian.PutUint16(buf[56:58], 0)  // k.off: key at 60
	binary.LittleEndian.PutUint16(buf[58:60], 16) // v.off: val at 216-16=200

	binary.LittleEndian.PutUint64(buf[60:68], 5) // omap_key_t.ok_oid
	binary.LittleEndian.PutUint64(buf[68:76], 1) // omap_key_t.ok_xid

	binary.LittleEndian.PutUint32(buf[200:204], 0)  // omap_val_t.ov_flags
	binary.LittleEndian.PutUint32(buf[204:208], 0)  // omap_val_t.ov_size
	binary.LittleEndian.PutUint64(buf[208:216], 42) // omap_val_t.ov_paddr

	binary.LittleEndian.PutUint64(buf[0:8], fletcher64(buf))

	img := fakefs.NewImage(2 * bs)
	if _, err := img.WriteAt(buf, bs); err != nil { // paddr 1
		t.Fatalf("write omap node: %v", err)
	}

	o := &omap{img: img, blockSize: bs, treeRoot: 1}

	paddr, err := o.resolve(5, 1)
	if err != nil {
		t.Fatalf("resolve(5, 1): %v", err)
	}
	if paddr != 42 {
		t.Fatalf("resolve(5, 1) = %d, want 42", paddr)
	}

	if _, err := o.resolve(999, 1); err == nil {
		t.Fatalf("resolve(999, 1): expected not-found error, got nil")
	}
}

func TestOmapKeyCompare(t *testing.T) {
	key := func(oid, xid uint64) []byte {
		b := make([]byte, 16)
		binary.LittleEndian.PutUint64(b[0:8], oid)
		binary.LittleEndian.PutUint64(b[8:16], xid)
		return b
	}
	tests := []struct {
		name string
		a, b []byte
		want int
	}{
		{"a oid < b oid", key(1, 5), key(2, 1), -1},
		{"a oid > b oid", key(2, 1), key(1, 5), 1},
		{"same oid, a xid < b xid", key(1, 1), key(1, 2), -1},
		{"same oid, a xid > b xid", key(1, 2), key(1, 1), 1},
		{"equal", key(1, 1), key(1, 1), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := omapKeyCompare(tc.a, tc.b); got != tc.want {
				t.Fatalf("omapKeyCompare = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSectionReadWritePathBounds(t *testing.T) {
	img := fakefs.NewImage(64)
	s := &section{img: img, base: 16, size: 32} // window [16,48) into img

	if s.Path() != img.Path() {
		t.Fatalf("Path() = %q, want %q", s.Path(), img.Path())
	}

	// WriteAt within bounds, then ReadAt (clamped) confirms it landed at
	// the section-relative offset, not the underlying image's raw offset.
	if _, err := s.WriteAt([]byte{1, 2, 3, 4}, 0); err != nil {
		t.Fatalf("WriteAt in bounds: %v", err)
	}
	got := make([]byte, 4)
	if _, err := s.ReadAt(got, 0); err != nil {
		t.Fatalf("ReadAt in bounds: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("ReadAt = %v, want [1 2 3 4]", got)
	}

	// A read requesting more than remains in the window must be clamped,
	// not passed straight through to the underlying image.
	big := make([]byte, 100)
	n, err := s.ReadAt(big, 30)
	if err != nil {
		t.Fatalf("ReadAt clamped: %v", err)
	}
	if n != 2 { // size(32) - off(30)
		t.Fatalf("ReadAt clamped n = %d, want 2", n)
	}

	if _, err := s.ReadAt(got, -1); err == nil {
		t.Fatalf("ReadAt(off=-1): expected error, got nil")
	}
	if _, err := s.ReadAt(got, 100); err == nil {
		t.Fatalf("ReadAt(off past size): expected error, got nil")
	}
	if _, err := s.WriteAt(got, 100); err == nil {
		t.Fatalf("WriteAt(off past size): expected error, got nil")
	}
}

func TestHasNXMagicShortBuffer(t *testing.T) {
	if hasNXMagic(nil) {
		t.Fatalf("hasNXMagic(nil) = true, want false")
	}
	if hasNXMagic(make([]byte, 10)) {
		t.Fatalf("hasNXMagic(10 bytes) = true, want false")
	}
}

func TestEntryNameNoSlash(t *testing.T) {
	if got := entryName("hello.txt"); got != "hello.txt" {
		t.Fatalf("entryName(no slash) = %q, want %q", got, "hello.txt")
	}
}

func TestJoinPathNoTrailingSlash(t *testing.T) {
	if got := joinPath("/sub", "file.txt"); got != "/sub/file.txt" {
		t.Fatalf("joinPath = %q, want %q", got, "/sub/file.txt")
	}
}

func TestMinInt64(t *testing.T) {
	if got := minInt64(3, 7); got != 3 {
		t.Fatalf("minInt64(3,7) = %d, want 3", got)
	}
	if got := minInt64(7, 3); got != 3 {
		t.Fatalf("minInt64(7,3) = %d, want 3", got)
	}
}

func TestEntryNamedStreamsReturnsNil(t *testing.T) {
	e := &Entry{path: "/f", isDir: false}
	names, err := e.NamedStreams()
	if names != nil || err != nil {
		t.Fatalf("NamedStreams() = (%v, %v), want (nil, nil)", names, err)
	}
}

func TestFindAPFSPartitionEntrySkipsNonMatchingGUID(t *testing.T) {
	h := gptHeader{numEntries: 2, entrySize: 128}
	entries := make([]byte, 2*128)
	// entry 0: some other partition type (all-zero GUID, doesn't match).
	// entry 1: the Apple_APFS GUID, with first/last LBA 10/20.
	copy(entries[128:144], apfsPartitionGUID[:])
	binary.LittleEndian.PutUint64(entries[128+32:128+40], 10)
	binary.LittleEndian.PutUint64(entries[128+40:128+48], 20)

	first, last, ok := findAPFSPartitionEntry(h, entries)
	if !ok {
		t.Fatalf("findAPFSPartitionEntry: expected a match, got none")
	}
	if first != 10 || last != 20 {
		t.Fatalf("first,last = %d,%d, want 10,20", first, last)
	}
}

func TestReadObjectRejectsInvalidParams(t *testing.T) {
	img := fakefs.NewImage(4096)
	if _, err := readObject(img, -1, 4096); err == nil {
		t.Fatalf("readObject(paddr=-1): expected error, got nil")
	}
	if _, err := readObject(img, 0, 0); err == nil {
		t.Fatalf("readObject(blockSize=0): expected error, got nil")
	}
}

func TestFindAPFSPartitionEntryNoMatch(t *testing.T) {
	h := gptHeader{numEntries: 1, entrySize: 128}
	entries := make([]byte, 128) // zeroed: no Apple_APFS GUID anywhere
	if _, _, ok := findAPFSPartitionEntry(h, entries); ok {
		t.Fatalf("findAPFSPartitionEntry: expected no match, got one")
	}
}
