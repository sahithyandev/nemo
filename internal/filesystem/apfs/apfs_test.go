package apfs

import (
	"os"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
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
