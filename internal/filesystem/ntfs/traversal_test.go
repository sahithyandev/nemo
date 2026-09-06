package ntfs

import (
	"encoding/binary"
	"path"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

func indexFileNameValue(name string, directory bool) []byte {
	encoded := utf16.Encode([]rune(name))
	b := make([]byte, 66+len(encoded)*2)
	binary.LittleEndian.PutUint64(b[0:8], 5)
	if directory {
		binary.LittleEndian.PutUint32(b[56:60], fileAttributeDirectory)
	}
	b[64] = byte(len(encoded))
	b[65] = 1
	for i, c := range encoded {
		binary.LittleEndian.PutUint16(b[66+i*2:], c)
	}
	return b
}

func indexRootAttribute(entries ...directoryItem) []byte {
	value := make([]byte, 32)
	binary.LittleEndian.PutUint32(value[0:4], attributeTypeFileName)
	binary.LittleEndian.PutUint32(value[4:8], 1)
	value[8] = 1
	binary.LittleEndian.PutUint32(value[16:20], 16)
	for _, item := range entries {
		key := indexFileNameValue(item.name, item.fileFlags&fileAttributeDirectory != 0)
		length := (16 + len(key) + 7) &^ 7
		e := make([]byte, length)
		binary.LittleEndian.PutUint64(e[0:8], item.reference)
		binary.LittleEndian.PutUint16(e[8:10], uint16(length))
		binary.LittleEndian.PutUint16(e[10:12], uint16(len(key)))
		copy(e[16:], key)
		value = append(value, e...)
	}
	last := make([]byte, 16)
	binary.LittleEndian.PutUint16(last[8:10], 16)
	binary.LittleEndian.PutUint16(last[12:14], indexEntryLast)
	value = append(value, last...)
	binary.LittleEndian.PutUint32(value[20:24], uint32(len(value)-16))
	binary.LittleEndian.PutUint32(value[24:28], uint32(len(value)-16))
	return residentTestAttribute(attributeTypeIndexRoot, value, "$I30")
}

func syntheticNTFSImage() *testImage {
	data := make([]byte, 64*512)
	boot := validBootSector()
	binary.LittleEndian.PutUint16(boot[0x0b:0x0d], 512)
	boot[0x0d] = 1
	binary.LittleEndian.PutUint64(boot[0x30:0x38], 2)
	boot[0x40] = 0xf6
	boot[0x44] = 0xf6
	copy(data, boot)
	// $MFT occupies clusters 2-3 and then resumes at cluster 10.
	mftData := nonResidentTestAttribute(attributeTypeData, []byte{0x11, 2, 2, 0x11, 20, 8, 0}, 22, 22*512)
	records := map[uint64][]byte{0: syntheticMFTRecord(mftData), 5: syntheticMFTRecord(indexRootAttribute(directoryItem{reference: 6, name: "Dir", fileFlags: fileAttributeDirectory})), 6: syntheticMFTRecord(indexRootAttribute(directoryItem{reference: 7, name: "file.txt"})), 7: syntheticMFTRecord(residentTestAttribute(attributeTypeData, []byte("hello"), ""))}
	for number, record := range records {
		applyTestFixups(record, 512)
		logical := number * 1024
		vcn := logical / 512
		physical := uint64(2) + vcn
		if vcn >= 2 {
			physical = uint64(10) + (vcn - 2)
		}
		copy(data[physical*512:], record)
	}
	return &testImage{data: data}
}

func TestTraversalAndOpen(t *testing.T) {
	fsys, err := New(syntheticNTFSImage())
	if err != nil {
		t.Fatal(err)
	}
	f := fsys.(*FS)
	if f.Type() != filesystem.TypeNTFS {
		t.Fatalf("Type=%q", f.Type())
	}
	if f.Root().Path() != "/" || !f.Root().IsDir() {
		t.Fatalf("root=%#v", f.Root())
	}
	children, err := f.Root().Children()
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Path() != "/Dir" || !children[0].IsDir() {
		t.Fatalf("children=%#v", children)
	}
	entry, err := f.Open("//Dir/./file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Path() != "/Dir/file.txt" || entry.IsDir() {
		t.Fatalf("entry=%#v", entry)
	}
	if got, err := entry.Children(); err != nil || got != nil {
		t.Fatalf("file children=%v,%v", got, err)
	}
	if _, err = f.Open("/missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing error=%v", err)
	}
	if _, err = f.Open(path.Join("Dir", "file.txt", "child")); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("walk error=%v", err)
	}
}

func TestDirectoryNamePreference(t *testing.T) {
	in := []directoryItem{{reference: 8, name: "FILE~1.TXT", namespace: 2}, {reference: 8, name: "File name.txt", namespace: 1}}
	out := preferDirectoryNames(in)
	if len(out) != 1 || out[0].name != "File name.txt" {
		t.Fatalf("out=%#v", out)
	}
}

func TestFilesystemOpenIntegration(t *testing.T) {
	fsys, err := filesystem.Open(syntheticNTFSImage())
	if err != nil {
		t.Fatal(err)
	}
	if fsys.Type() != filesystem.TypeNTFS {
		t.Fatalf("Type=%q", fsys.Type())
	}
}

func TestIndexAllocationHonorsBitmap(t *testing.T) {
	img := &testImage{data: make([]byte, 16*512)}
	f := &FS{img: img, bytesPerSector: 512, sectorsPerCluster: 1, indexRecordSize: 1024}
	root := make([]byte, 56)
	binary.LittleEndian.PutUint32(root[0:4], attributeTypeFileName)
	binary.LittleEndian.PutUint32(root[4:8], 1)
	root[8] = 1
	binary.LittleEndian.PutUint32(root[16:20], 16)
	binary.LittleEndian.PutUint32(root[20:24], 40)
	binary.LittleEndian.PutUint32(root[24:28], 40)
	binary.LittleEndian.PutUint16(root[32+8:32+10], 24)
	binary.LittleEndian.PutUint16(root[32+12:32+14], indexEntryLast|indexEntryHasSubnode)

	indx := make([]byte, 1024)
	copy(indx[:4], "INDX")
	binary.LittleEndian.PutUint32(indx[24:28], 40)
	key := indexFileNameValue("allocated.txt", false)
	entryLen := (16 + len(key) + 7) &^ 7
	binary.LittleEndian.PutUint32(indx[28:32], uint32(40+entryLen+16))
	binary.LittleEndian.PutUint32(indx[32:36], uint32(40+entryLen+16))
	off := 64
	binary.LittleEndian.PutUint64(indx[off:off+8], 9)
	binary.LittleEndian.PutUint16(indx[off+8:off+10], uint16(entryLen))
	binary.LittleEndian.PutUint16(indx[off+10:off+12], uint16(len(key)))
	copy(indx[off+16:], key)
	off += entryLen
	binary.LittleEndian.PutUint16(indx[off+8:off+10], 16)
	binary.LittleEndian.PutUint16(indx[off+12:off+14], indexEntryLast)
	applyTestFixups(indx, 512)
	copy(img.data[4*512:], indx)

	record := mftRecord{indexRoot: root, indexAllocation: &attributeHeader{nonResident: true, dataSize: 1024, runs: []dataRun{{VCN: 0, LCN: 4, Clusters: 2}}}, bitmap: &attributeHeader{value: []byte{1}}}
	items, err := f.directoryEntries(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].name != "allocated.txt" {
		t.Fatalf("items = %#v", items)
	}
	record.bitmap.value[0] = 0
	items, err = f.directoryEntries(record)
	if err != nil || len(items) != 0 {
		t.Fatalf("inactive items = %#v, %v", items, err)
	}
}
