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
