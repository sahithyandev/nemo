package apfs

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sahithyandev/nemo/internal/image"
)

// loadImage decompresses testdata/<name>.img.gz (relative to the repo root)
// into a private, writable copy in t.TempDir() and opens it as a RawImage.
// name is the fixture's base name, e.g. "apfs-bare".
func loadImage(t *testing.T, name string) *image.RawImage {
	t.Helper()

	src, err := os.Open(filepath.Join("..", "..", "..", "testdata", name+".img.gz"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()

	dstPath := filepath.Join(t.TempDir(), name+".img")
	dst, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("create temp image: %v", err)
	}
	if _, err := io.Copy(dst, gz); err != nil {
		dst.Close()
		t.Fatalf("decompress fixture: %v", err)
	}
	if err := dst.Close(); err != nil {
		t.Fatalf("close temp image: %v", err)
	}

	img, err := image.Open(dstPath)
	if err != nil {
		t.Fatalf("open temp image: %v", err)
	}
	t.Cleanup(func() { img.Close() })

	return img
}
