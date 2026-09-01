package ntfs

import (
	"io"
	"strings"
	"testing"
)

func testMFTFileSystem(imageSize int) (*FS, *testImage) {
	img := &testImage{data: make([]byte, imageSize)}
	return &FS{
		img:               img,
		bytesPerSector:    512,
		sectorsPerCluster: 8,
		mftCluster:        2,
		fileRecordSize:    1024,
	}, img
}

func TestMFTOffset(t *testing.T) {
	f, _ := testMFTFileSystem(16 * 1024)
	offset, err := f.mftOffset()
	if err != nil {
		t.Fatalf("mftOffset() error = %v", err)
	}
	if offset != 8192 {
		t.Fatalf("mftOffset() = %d, want 8192", offset)
	}
}

func TestReadMFTRecord(t *testing.T) {
	f, img := testMFTFileSystem(16 * 1024)
	copy(img.data[8192+1024:], fileRecordMagic)
	img.data[8192+1024+20] = 0x7a

	record, err := f.readMFTRecord(1)
	if err != nil {
		t.Fatalf("readMFTRecord() error = %v", err)
	}
	if len(record) != 1024 {
		t.Fatalf("record length = %d, want 1024", len(record))
	}
	if record[20] != 0x7a {
		t.Fatalf("record payload byte = %#x, want 0x7a", record[20])
	}
}

func TestReadMFTRecordRejectsInvalidSignature(t *testing.T) {
	f, _ := testMFTFileSystem(16 * 1024)
	_, err := f.readMFTRecord(0)
	if err == nil || !strings.Contains(err.Error(), "FILE signature") {
		t.Fatalf("readMFTRecord() error = %v, want FILE signature error", err)
	}
}

func TestReadMFTRecordRejectsOutOfBounds(t *testing.T) {
	tests := []struct {
		name   string
		change func(*FS)
		record uint64
	}{
		{"MFT cluster", func(f *FS) { f.mftCluster = 4 }, 0},
		{"record", func(f *FS) {}, 8},
		{"record size", func(f *FS) { f.fileRecordSize = 3 }, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := testMFTFileSystem(16 * 1024)
			tt.change(f)
			_, err := f.readMFTRecord(tt.record)
			if err == nil {
				t.Fatal("readMFTRecord() error = nil, want boundary error")
			}
		})
	}
}

type shortMFTImage struct {
	*testImage
	reportedSize int64
}

func (i *shortMFTImage) Size() int64 { return i.reportedSize }

func TestReadMFTRecordRejectsShortRead(t *testing.T) {
	img := &shortMFTImage{
		testImage:    &testImage{data: make([]byte, 8192+100)},
		reportedSize: 16 * 1024,
	}
	f := &FS{
		img:               img,
		bytesPerSector:    512,
		sectorsPerCluster: 8,
		mftCluster:        2,
		fileRecordSize:    1024,
	}

	_, err := f.readMFTRecord(0)
	if err == nil || !strings.Contains(err.Error(), "read MFT record") || !strings.Contains(err.Error(), io.EOF.Error()) {
		t.Fatalf("readMFTRecord() error = %v, want wrapped short-read error", err)
	}
}
