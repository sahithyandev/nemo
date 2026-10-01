//go:build darwin

package image

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// newTestAlignedDevice builds an AlignedRawDevice around a plain regular
// file holding data, bypassing OpenRawReadOnly (which needs a real device
// and its DKIOCGETBLOCKSIZE/DKIOCGETBLOCKCOUNT ioctls) so the alignment and
// copy-back math in ReadAt can be exercised without hardware or root.
func newTestAlignedDevice(t *testing.T, data []byte, blockSize int64) *AlignedRawDevice {
	t.Helper()
	path := filepath.Join(t.TempDir(), "device")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return &AlignedRawDevice{file: file, path: path, size: int64(len(data)), blockSize: blockSize}
}

func TestAlignedRawDeviceReadAt(t *testing.T) {
	const blockSize = 512
	data := make([]byte, blockSize*4)
	for i := range data {
		data[i] = byte(i)
	}
	dev := newTestAlignedDevice(t, data, blockSize)

	cases := []struct {
		name string
		off  int64
		n    int
	}{
		{"aligned offset and length", 0, blockSize},
		{"unaligned offset", 100, 50},
		{"unaligned length crossing a block boundary", blockSize - 10, 20},
		{"spans multiple blocks", blockSize + 5, blockSize*2 + 7},
		{"single byte mid-block", 777, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, tc.n)
			n, err := dev.ReadAt(buf, tc.off)
			if err != nil {
				t.Fatalf("ReadAt(off=%d, n=%d): %v", tc.off, tc.n, err)
			}
			if n != tc.n {
				t.Fatalf("ReadAt(off=%d, n=%d): read %d bytes", tc.off, tc.n, n)
			}
			want := data[tc.off : tc.off+int64(tc.n)]
			if !bytes.Equal(buf, want) {
				t.Fatalf("ReadAt(off=%d, n=%d) = %v, want %v", tc.off, tc.n, buf, want)
			}
		})
	}
}

func TestAlignedRawDeviceReadAtPastEnd(t *testing.T) {
	const blockSize = 512
	data := make([]byte, blockSize*2)
	dev := newTestAlignedDevice(t, data, blockSize)

	// Fully past the end: nothing to read.
	buf := make([]byte, 10)
	n, err := dev.ReadAt(buf, int64(len(data))+blockSize)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt fully past end: n=%d, err=%v, want n=0, io.EOF", n, err)
	}

	// Straddling the end: a short read, not a hard error, matching
	// RawImage.ReadAt's tolerance of a short final read.
	buf = make([]byte, blockSize)
	off := int64(len(data)) - 10
	n, err = dev.ReadAt(buf, off)
	if n != 10 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt straddling end: n=%d, err=%v, want n=10, io.EOF", n, err)
	}
	if !bytes.Equal(buf[:10], data[off:]) {
		t.Fatalf("ReadAt straddling end: got %v, want %v", buf[:10], data[off:])
	}
}

func TestAlignedRawDeviceReadAtNegativeOffset(t *testing.T) {
	dev := newTestAlignedDevice(t, make([]byte, 512), 512)
	if _, err := dev.ReadAt(make([]byte, 1), -1); err == nil {
		t.Fatal("ReadAt(-1): expected an error, got nil")
	}
}

func TestAlignedRawDeviceWriteAtFails(t *testing.T) {
	dev := newTestAlignedDevice(t, make([]byte, 512), 512)
	if _, err := dev.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("WriteAt: expected an error, got nil (AlignedRawDevice must be read-only)")
	}
}

func TestAlignedRawDeviceImplementsImage(t *testing.T) {
	var _ Image = (*AlignedRawDevice)(nil)
}
