package apfs

import (
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
		{"standard layout", 128, 128, true},                 // 16 KiB total, real-world GPT
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
