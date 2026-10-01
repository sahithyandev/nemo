package ntfs

import (
	"encoding/binary"
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
	want := syntheticMFTRecord()
	applyTestFixups(want, 512)
	copy(img.data[8192+1024:], want)

	record, err := f.readMFTRecord(1)
	if err != nil {
		t.Fatalf("readMFTRecord() error = %v", err)
	}
	if len(record) != 1024 {
		t.Fatalf("record length = %d, want 1024", len(record))
	}
}

func applyTestFixups(record []byte, sectorSize int) {
	count := len(record)/sectorSize + 1
	binary.LittleEndian.PutUint16(record[4:6], 48)
	binary.LittleEndian.PutUint16(record[6:8], uint16(count))
	record[48], record[49] = 0xaa, 0xbb
	for i := 0; i < count-1; i++ {
		trailer := (i+1)*sectorSize - 2
		copy(record[50+i*2:52+i*2], record[trailer:trailer+2])
		record[trailer], record[trailer+1] = 0xaa, 0xbb
	}
}

func TestApplyFixups(t *testing.T) {
	record := syntheticMFTRecord()
	record[510], record[511], record[1022], record[1023] = 1, 2, 3, 4
	applyTestFixups(record, 512)
	if err := applyFixups(record, 512, fileRecordMagic); err != nil {
		t.Fatal(err)
	}
	if record[510] != 1 || record[511] != 2 || record[1022] != 3 || record[1023] != 4 {
		t.Fatal("trailers not restored")
	}
}

func TestApplyFixupsRejectsMalformed(t *testing.T) {
	edits := []func([]byte){
		func(b []byte) { b[510] = 0 },
		func(b []byte) { binary.LittleEndian.PutUint16(b[6:8], 9) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[4:6], 1023) },
	}
	for _, edit := range edits {
		record := syntheticMFTRecord()
		applyTestFixups(record, 512)
		edit(record)
		if err := applyFixups(record, 512, fileRecordMagic); err == nil {
			t.Fatal("applyFixups error = nil")
		}
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
