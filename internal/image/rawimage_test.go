package image

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRawImageReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.img")

	t.Logf("temporary image path: %s", path)

	original := []byte{
		0x10, 0x20, 0x30, 0x40,
		0x50, 0x60, 0x70, 0x80,
	}

	err := os.WriteFile(path, original, 0600)
	if err != nil {
		t.Fatal(err)
	}

	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()

	// Check Size().
	if img.Size() != int64(len(original)) {
		t.Fatalf(
			"expected size %d, got %d",
			len(original),
			img.Size(),
		)
	}

	// Check Path().
	if img.Path() != path {
		t.Fatalf(
			"expected path %q, got %q",
			path,
			img.Path(),
		)
	}

	// Read two bytes starting at offset 2.
	buf := make([]byte, 2)

	_, err = img.ReadAt(buf, 2)
	if err != nil {
		t.Fatal(err)
	}

	expectedRead := []byte{0x30, 0x40}

	if !bytes.Equal(buf, expectedRead) {
		t.Fatalf(
			"expected %v, got %v",
			expectedRead,
			buf,
		)
	}

	// Write two bytes starting at offset 4.
	_, err = img.WriteAt([]byte{0xAA, 0xBB}, 4)
	if err != nil {
		t.Fatal(err)
	}

	// Read the same location again.
	_, err = img.ReadAt(buf, 4)
	if err != nil {
		t.Fatal(err)
	}

	expectedWrite := []byte{0xAA, 0xBB}

	if !bytes.Equal(buf, expectedWrite) {
		t.Fatalf(
			"expected %v, got %v",
			expectedWrite,
			buf,
		)
	}
}
