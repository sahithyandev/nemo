package ext4

import (
	"encoding/binary"
	"io"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

const testBlockSize = 1024

type testImage struct{ data []byte }

func (i *testImage) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(i.data)) {
		return 0, io.EOF
	}
	n := copy(p, i.data[off:])
	if n != len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (i *testImage) WriteAt(p []byte, off int64) (int, error) { return copy(i.data[off:], p), nil }
func (i *testImage) Size() int64                              { return int64(len(i.data)) }
func (i *testImage) Path() string                             { return "synthetic-ext4.img" }

func syntheticImage() *testImage {
	b := make([]byte, 32*testBlockSize)
	sb := b[1024:2048]
	put32(sb, 0x00, 16)
	put32(sb, 0x04, 32)
	put32(sb, 0x14, 1)
	put32(sb, 0x18, 0)
	put32(sb, 0x20, 32)
	put32(sb, 0x28, 16)
	put16(sb, 0x38, ext4Magic)
	put16(sb, 0x58, 256)
	put32(sb, 0x60, featureIncompatFiletype|featureIncompatExtents)

	groupDesc := b[2*testBlockSize : 3*testBlockSize]
	put32(groupDesc, 8, 5)

	writeInode(b, 2, modeDir, 10)
	writeInode(b, 3, 0x8000, 0)
	writeInode(b, 4, modeDir, 11)
	writeInode(b, 5, 0x8000, 0)

	root := b[10*testBlockSize : 11*testBlockSize]
	writeDirent(root, 0, 2, 12, ".", 2)
	writeDirent(root, 12, 2, 12, "..", 2)
	writeDirent(root, 24, 3, 20, "hello.txt", 1)
	writeDirent(root, 44, 4, testBlockSize-44, "subdir", 2)

	subdir := b[11*testBlockSize : 12*testBlockSize]
	writeDirent(subdir, 0, 4, 12, ".", 2)
	writeDirent(subdir, 12, 2, 12, "..", 2)
	writeDirent(subdir, 24, 5, testBlockSize-24, "nested.txt", 1)
	return &testImage{data: b}
}

func writeInode(image []byte, number uint32, mode uint16, dataBlock uint32) {
	off := 5*testBlockSize + int(number-1)*256
	in := image[off : off+256]
	put16(in, 0, mode)
	if mode == modeDir {
		put32(in, 4, testBlockSize)
		put32(in, 32, inodeFlagExtents)
		put16(in, 40, extentMagic)
		put16(in, 42, 1)
		put16(in, 44, 4)
		put16(in, 46, 0)
		put32(in, 52, 0)
		put16(in, 56, 1)
		put32(in, 60, dataBlock)
	}
}

func writeDirent(block []byte, off int, inode uint32, recordLength int, name string, fileType byte) {
	put32(block, off, inode)
	put16(block, off+4, uint16(recordLength))
	block[off+6] = byte(len(name))
	block[off+7] = fileType
	copy(block[off+8:], name)
}

func put16(b []byte, off int, value uint16) { binary.LittleEndian.PutUint16(b[off:], value) }
func put32(b []byte, off int, value uint32) { binary.LittleEndian.PutUint32(b[off:], value) }

func TestOpenAndWalkSyntheticImage(t *testing.T) {
	fs, err := New(syntheticImage())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if fs.Type() != filesystem.TypeEXT4 {
		t.Fatalf("Type() = %q, want %q", fs.Type(), filesystem.TypeEXT4)
	}

	children, err := fs.Root().Children()
	if err != nil {
		t.Fatalf("Root().Children(): %v", err)
	}
	if len(children) != 2 || children[0].Path() != "/hello.txt" || children[1].Path() != "/subdir" {
		t.Fatalf("root children = %v", entryPaths(children))
	}
	if children[0].IsDir() || !children[1].IsDir() {
		t.Fatalf("unexpected child types")
	}

	nested, err := fs.Open("/subdir/nested.txt")
	if err != nil {
		t.Fatalf("Open nested file: %v", err)
	}
	if nested.IsDir() || nested.Path() != "/subdir/nested.txt" {
		t.Fatalf("nested entry = %q, dir=%v", nested.Path(), nested.IsDir())
	}
	if _, err := fs.Open("/missing"); err == nil {
		t.Fatal("Open missing path: expected error")
	}
}

func TestDetectorRegistered(t *testing.T) {
	fs, err := filesystem.Open(syntheticImage())
	if err != nil {
		t.Fatalf("filesystem.Open: %v", err)
	}
	if fs.Type() != filesystem.TypeEXT4 {
		t.Fatalf("Type() = %q, want ext4", fs.Type())
	}
}

func Test64BitGroupDescriptor(t *testing.T) {
	img := syntheticImage()
	sb := img.data[1024:2048]
	put32(sb, 0x60, featureIncompatFiletype|featureIncompatExtents|featureIncompat64Bit|featureIncompatCsumSeed)
	put16(sb, 0xfe, 64)

	fs, err := New(img)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := fs.Open("/subdir/nested.txt"); err != nil {
		t.Fatalf("Open nested file: %v", err)
	}
}

func TestRejectsInvalidAndUnsupportedSuperblocks(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte)
		want   string
	}{
		{"bad magic", func(sb []byte) { put16(sb, 0x38, 0) }, "magic"},
		{"zero geometry", func(sb []byte) { put32(sb, 0x20, 0) }, "geometry"},
		{"unsupported incompat", func(sb []byte) { put32(sb, 0x60, 0x1) }, "unsupported incompatible"},
		{"bigalloc", func(sb []byte) { put32(sb, 0x64, featureROCompatBigalloc) }, "bigalloc"},
		{"invalid descriptor size", func(sb []byte) {
			put32(sb, 0x60, featureIncompatFiletype|featureIncompatExtents|featureIncompat64Bit)
			put16(sb, 0xfe, 32)
		}, "descriptor size"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img := syntheticImage()
			tc.mutate(img.data[1024:2048])
			_, err := New(img)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestRejectsMalformedExtentAndDirectory(t *testing.T) {
	t.Run("extent", func(t *testing.T) {
		img := syntheticImage()
		rootInode := 5*testBlockSize + 256
		put16(img.data, rootInode+42, 5)
		fs, err := New(img)
		if err == nil {
			_, err = fs.Root().Children()
		}
		if err == nil || !strings.Contains(err.Error(), "extent entries exceed") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		img := syntheticImage()
		put16(img.data, 10*testBlockSize+4, 6)
		fs, err := New(img)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, err = fs.Root().Children()
		if err == nil || !strings.Contains(err.Error(), "invalid directory record") {
			t.Fatalf("error = %v", err)
		}
	})
}

func entryPaths(entries []filesystem.Entry) []string {
	paths := make([]string, len(entries))
	for i, entry := range entries {
		paths[i] = entry.Path()
	}
	return paths
}
