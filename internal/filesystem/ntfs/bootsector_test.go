package ntfs

import (
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

type testImage struct {
	data []byte
}

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

func (i *testImage) WriteAt(p []byte, off int64) (int, error) {
	return 0, io.ErrClosedPipe
}

func (i *testImage) Size() int64  { return int64(len(i.data)) }
func (i *testImage) Path() string { return "test.ntfs" }

func validBootSector() []byte {
	b := make([]byte, bootSectorSize)
	copy(b[3:11], ntfsMagic)
	binary.LittleEndian.PutUint16(b[0x0b:0x0d], 512)
	b[0x0d] = 8
	binary.LittleEndian.PutUint64(b[0x30:0x38], 786432)
	binary.LittleEndian.PutUint64(b[0x38:0x40], 2)
	b[0x40] = 0xf6 // -10 as a signed byte: file records are 2^10 bytes.
	return b
}

func TestNewParsesBootSector(t *testing.T) {
	fs, err := New(&testImage{data: validBootSector()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	f := fs.(*FS)
	if f.bytesPerSector != 512 || f.sectorsPerCluster != 8 {
		t.Fatalf("geometry = %d bytes/sector, %d sectors/cluster", f.bytesPerSector, f.sectorsPerCluster)
	}
	if f.mftCluster != 786432 || f.mftMirrorCluster != 2 {
		t.Fatalf("MFT locations = %d, %d", f.mftCluster, f.mftMirrorCluster)
	}
	if f.fileRecordSize != 1024 {
		t.Fatalf("file record size = %d, want 1024", f.fileRecordSize)
	}
}

func TestNewDecodesClusterMultipleFileRecordSize(t *testing.T) {
	b := validBootSector()
	b[0x40] = 2

	fs, err := New(&testImage{data: b})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := fs.(*FS).fileRecordSize; got != 8192 {
		t.Fatalf("file record size = %d, want 8192", got)
	}
}

func TestNewRejectsInvalidBootSector(t *testing.T) {
	tests := []struct {
		name string
		edit func([]byte)
		want string
	}{
		{"magic", func(b []byte) { copy(b[3:11], "NOTNTFS ") }, "magic"},
		{"bytes per sector", func(b []byte) { binary.LittleEndian.PutUint16(b[0x0b:0x0d], 1000) }, "bytes per sector"},
		{"sectors per cluster", func(b []byte) { b[0x0d] = 3 }, "sectors per cluster"},
		{"file record size", func(b []byte) { b[0x40] = 0 }, "file record size"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := validBootSector()
			tt.edit(b)
			_, err := New(&testImage{data: b})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestNewRejectsTruncatedBootSector(t *testing.T) {
	_, err := New(&testImage{data: validBootSector()[:bootSectorSize-1]})
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("New() error = %v, want too-small error", err)
	}
}
