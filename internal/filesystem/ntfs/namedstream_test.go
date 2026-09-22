package ntfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
)

type adsImage struct {
	*testImage
	writes       int
	shortWrite   bool
	shortRead    bool
	corruptWrite bool
}

func (i *adsImage) WriteAt(p []byte, off int64) (int, error) {
	i.writes++
	if i.shortWrite {
		return 0, nil
	}
	if off < 0 || off > int64(len(i.data)) || int64(len(p)) > int64(len(i.data))-off {
		return 0, io.ErrShortWrite
	}
	n := copy(i.data[off:], p)
	if i.corruptWrite && n > 0 {
		i.data[off] ^= 1
	}
	return n, nil
}

func (i *adsImage) ReadAt(p []byte, off int64) (int, error) {
	if i.shortRead {
		return 0, io.ErrUnexpectedEOF
	}
	return i.testImage.ReadAt(p, off)
}

func adsRecord(attrs ...[]byte) []byte {
	for i, a := range attrs {
		binary.LittleEndian.PutUint16(a[14:], uint16(i))
	}
	b := syntheticMFTRecord(attrs...)
	binary.LittleEndian.PutUint16(b[40:], uint16(len(attrs)))
	binary.LittleEndian.PutUint32(b[44:], 7)
	applyTestFixups(b, 512)
	return b
}

func adsFixture(t *testing.T, attrs ...[]byte) (*Entry, *adsImage) {
	t.Helper()
	img := &adsImage{testImage: syntheticNTFSImage()}
	copy(img.data[22*512:], adsRecord(attrs...))
	fsys, err := New(img)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := fsys.Open("/Dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	return entry.(*Entry), img
}

func namedNonresident(runs []byte, clusters, size, initialized uint64) []byte {
	a := nonResidentTestAttribute(attributeTypeData, runs, clusters, size)
	// Insert an eight-byte name area between header and mapping pairs.
	b := append([]byte(nil), a[:64]...)
	b = append(b, 's', 0, 0, 0, 0, 0, 0, 0)
	b = append(b, a[64:]...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)))
	b[9] = 1
	binary.LittleEndian.PutUint16(b[10:], 64)
	binary.LittleEndian.PutUint16(b[32:], 72)
	binary.LittleEndian.PutUint64(b[56:], initialized)
	return b
}

func requireStream(t *testing.T, e *Entry, name string, want []byte) {
	t.Helper()
	got, err := e.ReadStream(name)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("ReadStream(%q) = %q, %v; want %q", name, got, err, want)
	}
}

func unchangedFailure(t *testing.T, img *adsImage, want string, operation func() error) {
	t.Helper()
	before, writes := append([]byte(nil), img.data...), img.writes
	err := operation()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if !bytes.Equal(before, img.data) || writes != img.writes {
		t.Fatal("rejection changed image or attempted a write")
	}
}

func TestNamedStreamsEnumeration(t *testing.T) {
	for _, names := range [][]string{nil, {"secret"}, {"secret", "backup", "秘密😀", "backup"}} {
		t.Run(fmt.Sprint(names), func(t *testing.T) {
			attrs := [][]byte{residentTestAttribute(attributeTypeData, []byte("default"), ""), residentTestAttribute(0x40, nil, "unrelated")}
			for _, name := range names {
				attrs = append(attrs, residentTestAttribute(attributeTypeData, []byte(name), name))
			}
			e, _ := adsFixture(t, attrs...)
			got, err := e.NamedStreams()
			if err != nil {
				t.Fatal(err)
			}
			want := names
			if len(names) > 1 {
				want = []string{"backup", "secret", "秘密😀"}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("names = %v, want %v", got, want)
			}
		})
	}
}

func TestReadNamedStreams(t *testing.T) {
	e, _ := adsFixture(t, residentTestAttribute(attributeTypeData, []byte("payload"), "秘密😀"), residentTestAttribute(attributeTypeData, nil, "empty"))
	requireStream(t, e, "秘密😀", []byte("payload"))
	requireStream(t, e, "empty", nil)
	for _, name := range []string{"", "missing"} {
		if _, err := e.ReadStream(name); err == nil {
			t.Fatalf("read %q succeeded", name)
		}
	}
	for _, fragmented := range []bool{false, true} {
		for _, initialized := range []uint64{0, 550, 900} {
			runs := []byte{0x11, 2, 40, 0}
			if fragmented {
				runs = []byte{0x11, 1, 40, 0x11, 1, 4, 0}
			}
			e, img := adsFixture(t, namedNonresident(runs, 2, 900, initialized))
			copy(img.data[40*512:], bytes.Repeat([]byte{'a'}, 1024))
			copy(img.data[44*512:], bytes.Repeat([]byte{'a'}, 512))
			want := make([]byte, 900)
			copy(want, bytes.Repeat([]byte{'a'}, int(initialized)))
			requireStream(t, e, "s", want)
		}
	}
	t.Run("sparse read", func(t *testing.T) {
		e, img := adsFixture(t, namedNonresident([]byte{0x11, 1, 40, 0x01, 1, 0}, 2, 900, 900))
		copy(img.data[40*512:], bytes.Repeat([]byte{'a'}, 512))
		want := make([]byte, 900)
		copy(want, bytes.Repeat([]byte{'a'}, 512))
		requireStream(t, e, "s", want)
	})
}

func TestResidentNamedStreamLifecycle(t *testing.T) {
	defaultData := residentTestAttribute(attributeTypeData, []byte("default"), "")
	fileName := residentTestAttribute(attributeTypeFileName, indexFileNameValue("file.txt", false), "")
	standard := residentTestAttribute(attributeTypeStandardInformation, make([]byte, 48), "")
	other := residentTestAttribute(attributeTypeData, []byte("keep"), "other")
	e, img := adsFixture(t, standard, fileName, defaultData, other)
	for _, data := range [][]byte{nil, []byte("abc"), []byte("xyz"), bytes.Repeat([]byte{0xab}, 520), []byte("x")} {
		if err := e.WriteStream("秘密😀", data); err != nil {
			t.Fatal(err)
		}
		requireStream(t, e, "秘密😀", data)
		requireStream(t, e, "other", []byte("keep"))
		b, err := e.fs.readMFTRecord(7)
		if err != nil {
			t.Fatal(err)
		}
		r, err := parseMFTRecord(b)
		if err != nil {
			t.Fatal(err)
		}
		for i, original := range [][]byte{standard, fileName, defaultData, other} {
			off := attributeOffset(r, i)
			if !bytes.Equal(b[off:off+len(original)], original) {
				t.Fatalf("attribute %d changed", i)
			}
		}
		if r.header.usedSize%8 != 0 || binary.LittleEndian.Uint32(b[r.header.usedSize-8:]) != attributeTypeEnd {
			t.Fatal("invalid end marker/used size")
		}
	}
	if img.writes != 5 {
		t.Fatalf("writes = %d", img.writes)
	}
	if err := e.DeleteStream("秘密😀"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReadStream("秘密😀"); err == nil {
		t.Fatal("deleted stream readable")
	}
	deleted, err := e.fs.readMFTRecord(7)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseMFTRecord(deleted)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.attributes) != 4 || parsed.header.usedSize%8 != 0 || binary.LittleEndian.Uint32(deleted[parsed.header.usedSize-8:]) != attributeTypeEnd {
		t.Fatal("invalid record after deletion")
	}
	for i, original := range [][]byte{standard, fileName, defaultData, other} {
		off := attributeOffset(parsed, i)
		if !bytes.Equal(deleted[off:off+len(original)], original) {
			t.Fatalf("delete changed attribute %d", i)
		}
	}
	requireStream(t, e, "other", []byte("keep"))
	unchangedFailure(t, img, "not found", func() error { return e.DeleteStream("秘密😀") })
	if err := e.WriteStream("秘密😀", nil); err != nil {
		t.Fatal(err)
	}
	requireStream(t, e, "秘密😀", nil)
}

func TestResidentNamedStreamCapacity(t *testing.T) {
	e, img := adsFixture(t)
	// 56-byte FILE header + 32-byte named resident header + 928 + 8 end.
	if err := e.WriteStream("s", make([]byte, 928)); err != nil {
		t.Fatal(err)
	}
	requireStream(t, e, "s", make([]byte, 928))
	unchangedFailure(t, img, "insufficient MFT record room", func() error { return e.WriteStream("s", make([]byte, 929)) })
	unchangedFailure(t, img, "new nonresident ADS", func() error { return e.WriteStream("new", make([]byte, 4096)) })
	unchangedFailure(t, img, "insufficient MFT record room", func() error { return e.WriteStream("new", nil) })
	for _, name := range []string{"", strings.Repeat("x", 256), "bad:name", string([]byte{0xff})} {
		unchangedFailure(t, img, "stream name", func() error { return e.WriteStream(name, nil) })
	}
	unchangedFailure(t, img, "empty stream name", func() error { return e.DeleteStream("") })
}

func TestNonresidentNamedStreamReplacement(t *testing.T) {
	for _, runs := range [][]byte{{0x11, 2, 40, 0}, {0x11, 1, 40, 0x11, 1, 4, 0}} {
		e, img := adsFixture(t, namedNonresident(runs, 2, 900, 800), residentTestAttribute(attributeTypeData, []byte("default"), ""))
		for _, size := range []int{900, 1024, 700, 1, 0, 1024} {
			data := bytes.Repeat([]byte{byte(size)}, size)
			if err := e.WriteStream("s", data); err != nil {
				t.Fatal(err)
			}
			requireStream(t, e, "s", data)
			b, err := e.fs.readMFTRecord(7)
			if err != nil {
				t.Fatal(err)
			}
			r, err := parseMFTRecord(b)
			if err != nil {
				t.Fatal(err)
			}
			a := r.attributes[0]
			if a.allocatedSize != 1024 || a.dataSize != uint64(size) || a.initializedSize != uint64(size) {
				t.Fatalf("sizes = %+v", a)
			}
			physical := make([]byte, 1024)
			if err := e.fs.readRunsAt(a.runs, 1024, 0, physical); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(physical[size:], make([]byte, 1024-size)) {
				t.Fatal("stale tail exposed")
			}
		}
		unchangedFailure(t, img, "exceeds existing allocation", func() error { return e.WriteStream("s", make([]byte, 1025)) })
		unchangedFailure(t, img, "cluster release", func() error { return e.DeleteStream("s") })
	}
}

func TestNamedStreamUnsafeAttributeRejections(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		edit       func([]byte)
	}{
		{"compressed", "compressed", func(a []byte) { binary.LittleEndian.PutUint16(a[12:], 1) }},
		{"encrypted", "encrypted", func(a []byte) { binary.LittleEndian.PutUint16(a[12:], 0x4000) }},
		{"compression unit", "compressed", func(a []byte) { binary.LittleEndian.PutUint16(a[34:], 4) }},
		{"bad run", "data run", func(a []byte) { a[72] = 0x91 }},
		{"out of bounds", "image bounds", func(a []byte) { a[74] = 100 }},
		{"name overlap", "overlaps", func(a []byte) { binary.LittleEndian.PutUint16(a[10:], 24) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := namedNonresident([]byte{0x11, 2, 40, 0}, 2, 900, 900)
			tc.edit(a)
			e, img := adsFixture(t, a)
			if _, err := e.ReadStream("s"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("read error = %v", err)
			}
			unchangedFailure(t, img, tc.want, func() error { return e.WriteStream("s", nil) })
		})
	}
	for _, flags := range []uint16{1, 0x4000} {
		a := residentTestAttribute(attributeTypeData, nil, "s")
		binary.LittleEndian.PutUint16(a[12:], flags)
		e, img := adsFixture(t, a)
		if _, err := e.ReadStream("s"); err == nil {
			t.Fatal("flagged resident read succeeded")
		}
		unchangedFailure(t, img, "unsupported", func() error { return e.WriteStream("s", nil) })
		unchangedFailure(t, img, "unsupported", func() error { return e.DeleteStream("s") })
	}
	for _, sparseFlag := range []bool{false, true} {
		runs := []byte{0x01, 2, 0}
		if sparseFlag {
			runs = []byte{0x11, 2, 40, 0}
		}
		a := namedNonresident(runs, 2, 900, 900)
		if sparseFlag {
			binary.LittleEndian.PutUint16(a[12:], 0x8000)
		}
		e, img := adsFixture(t, a)
		unchangedFailure(t, img, "sparse", func() error { return e.WriteStream("s", nil) })
	}
	for _, attrs := range [][][]byte{
		{residentTestAttribute(attributeTypeAttributeList, nil, "")},
		{residentTestAttribute(attributeTypeData, nil, "s"), residentTestAttribute(attributeTypeData, nil, "s")},
	} {
		e, img := adsFixture(t, attrs...)
		unchangedFailure(t, img, "ATTRIBUTE_LIST", func() error { return e.WriteStream("s", nil) })
	}
}

func TestNamedStreamMalformedRecordAndIO(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		edit       func([]byte)
	}{
		{"signature", "signature", func(b []byte) { b[0] = 0 }},
		{"fixup", "sequence mismatch", func(b []byte) { b[510] ^= 1 }},
		{"USA", "sequence array", func(b []byte) { binary.LittleEndian.PutUint16(b[6:], 7) }},
		{"sizes", "record sizes", func(b []byte) { binary.LittleEndian.PutUint32(b[24:], 2000) }},
		{"inactive", "inactive", func(b []byte) { binary.LittleEndian.PutUint16(b[22:], 0) }},
		{"extension", "ATTRIBUTE_LIST", func(b []byte) { binary.LittleEndian.PutUint64(b[32:], 123) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, img := adsFixture(t)
			tc.edit(img.data[22*512 : 24*512])
			unchangedFailure(t, img, tc.want, func() error { return e.WriteStream("s", nil) })
		})
	}
	t.Run("short read", func(t *testing.T) {
		e, img := adsFixture(t)
		img.shortRead = true
		unchangedFailure(t, img, "EOF", func() error { return e.WriteStream("s", nil) })
	})
	t.Run("short write", func(t *testing.T) {
		e, img := adsFixture(t)
		img.shortWrite = true
		if err := e.WriteStream("s", nil); !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("verification", func(t *testing.T) {
		e, img := adsFixture(t)
		img.corruptWrite = true
		if err := e.WriteStream("s", nil); err == nil || !strings.Contains(err.Error(), "verify") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestNamedStreamFragmentedMFTAndCustody(t *testing.T) {
	img := &adsImage{testImage: syntheticNTFSImage()}
	// Record 7 crosses the boundary: first sector LCN 22, second LCN 30.
	mft := nonResidentTestAttribute(attributeTypeData, []byte{0x11, 2, 2, 0x11, 13, 8, 0x11, 7, 20, 0}, 22, 22*512)
	zero := adsRecord(mft)
	copy(img.data[2*512:], zero)
	b := adsRecord(residentTestAttribute(attributeTypeData, []byte("default"), ""))
	copy(img.data[22*512:], b[:512])
	copy(img.data[30*512:], b[512:])
	wrapped := custody.Wrap(img)
	fsys, err := New(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := fsys.Open("/Dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	e := entry.(*Entry)
	before := append([]byte(nil), img.data...)
	payload := bytes.Repeat([]byte{0x7b}, 700)
	if err := e.WriteStream("s", payload); err != nil {
		t.Fatal(err)
	}
	requireStream(t, e, "s", payload)
	for i := range before {
		if i >= 22*512 && i < 23*512 || i >= 30*512 && i < 31*512 {
			continue
		}
		if img.data[i] != before[i] {
			t.Fatalf("unrelated byte %d changed", i)
		}
	}
	events := wrapped.EventsSnapshot()
	if len(events) != 2 || events[0].Offset != 22*512 || events[1].Offset != 30*512 {
		t.Fatalf("events = %+v", events)
	}
	unchangedFailure(t, img, "insufficient", func() error { return e.WriteStream("s", make([]byte, 2000)) })
	if len(wrapped.EventsSnapshot()) != 2 {
		t.Fatal("failed mutation generated custody event")
	}
	if err := e.DeleteStream("s"); err != nil {
		t.Fatal(err)
	}
}

func TestNamedStreamConcurrentMutations(t *testing.T) {
	e, _ := adsFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("s%02d", i)
			if err := e.WriteStream(name, []byte(name)); err != nil {
				t.Error(err)
				return
			}
			if err := e.DeleteStream(name); err != nil {
				t.Error(err)
				return
			}
			if err := e.WriteStream(name, []byte(name)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("s%02d", i)
		requireStream(t, e, name, []byte(name))
	}
}

func TestNTFSAdvertisesTechniques(t *testing.T) {
	for _, d := range filesystem.Detectors() {
		if d.Type == filesystem.TypeNTFS {
			if !reflect.DeepEqual(d.Techniques, []string{"named-stream", "timestomp", "slack-space"}) {
				t.Fatalf("techniques = %v", d.Techniques)
			}
			return
		}
	}
	t.Fatal("missing NTFS detector")
}

func TestNamedStreamPreflightSafety(t *testing.T) {
	for _, nonresident := range []bool{false, true} {
		for _, tc := range []struct {
			name, want string
			edit       func(*Entry, *adsImage)
		}{
			{"geometry", "geometry", func(e *Entry, _ *adsImage) { e.fs.bytesPerSector = 4096 }},
			{"mirror bounds", "mirror exceeds image bounds", func(e *Entry, _ *adsImage) { e.fs.mftMirrorCluster = ^uint64(0) }},
			{"mirror overlap", "MFTMirr", func(e *Entry, _ *adsImage) { e.fs.mftMirrorCluster = 22 }},
			{"MFT coverage", "runlist", func(e *Entry, _ *adsImage) { e.fs.mftRuns[1].VCN++ }},
			{"duplicate IDs", "duplicate attribute", func(_ *Entry, img *adsImage) {
				b := img.data[22*512 : 24*512]
				second := 56 + int(binary.LittleEndian.Uint32(b[60:]))
				binary.LittleEndian.PutUint16(b[second+14:], 0)
			}},
		} {
			t.Run(fmt.Sprintf("%v/%s", nonresident, tc.name), func(t *testing.T) {
				a := residentTestAttribute(attributeTypeData, nil, "s")
				if nonresident {
					a = namedNonresident([]byte{0x11, 2, 40, 0}, 2, 900, 900)
				}
				e, img := adsFixture(t, a, residentTestAttribute(attributeTypeData, []byte("default"), ""))
				tc.edit(e, img)
				unchangedFailure(t, img, tc.want, func() error { return e.WriteStream("s", []byte("new")) })
			})
		}
	}
	t.Run("mirrored record number", func(t *testing.T) {
		e, img := adsFixture(t)
		// Record 3 maps to LCN 14 in this synthetic MFT.
		copy(img.data[14*512:], adsRecord())
		e.recordNumber = 3
		unchangedFailure(t, img, "$MFTMirr synchronization", func() error { return e.WriteStream("s", nil) })
	})
	for _, lcn := range []byte{0, 3, 20} {
		e, img := adsFixture(t, namedNonresident([]byte{0x11, 2, lcn, 0}, 2, 900, 900))
		unchangedFailure(t, img, "overlap", func() error { return e.WriteStream("s", []byte("new")) })
	}
	t.Run("another attribute allocation", func(t *testing.T) {
		e, img := adsFixture(t, namedNonresident([]byte{0x11, 2, 40, 0}, 2, 900, 900), nonResidentTestAttribute(attributeTypeData, []byte{0x11, 2, 41, 0}, 2, 900))
		unchangedFailure(t, img, "overlaps another attribute", func() error { return e.WriteStream("s", nil) })
	})
	t.Run("allocation mismatch", func(t *testing.T) {
		a := namedNonresident([]byte{0x11, 2, 40, 0}, 2, 900, 900)
		binary.LittleEndian.PutUint64(a[40:], 2048)
		e, img := adsFixture(t, a)
		unchangedFailure(t, img, "unsupported nonresident allocation", func() error { return e.WriteStream("s", nil) })
	})
}

func TestNamedStreamIDsAndSequenceRollover(t *testing.T) {
	e, img := adsFixture(t, residentTestAttribute(attributeTypeData, []byte("keep"), "other"))
	b := img.data[22*512 : 24*512]
	// The allocator must skip an occupied next-ID. The sequence must skip
	// reserved 0xffff and zero while still restoring both sector trailers.
	binary.LittleEndian.PutUint16(b[40:], 0)
	binary.LittleEndian.PutUint16(b[48:], 0xfffe)
	binary.LittleEndian.PutUint16(b[510:], 0xfffe)
	binary.LittleEndian.PutUint16(b[1022:], 0xfffe)
	if err := e.WriteStream("s", bytes.Repeat([]byte{9}, 650)); err != nil {
		t.Fatal(err)
	}
	requireStream(t, e, "other", []byte("keep"))
	logical, err := e.fs.readMFTRecord(7)
	if err != nil {
		t.Fatal(err)
	}
	r, err := parseMFTRecord(logical)
	if err != nil {
		t.Fatal(err)
	}
	if r.attributes[0].attributeID != 0 || r.attributes[1].attributeID != 1 || binary.LittleEndian.Uint16(logical[40:]) != 2 {
		t.Fatal("incorrect IDs")
	}
	if binary.LittleEndian.Uint16(b[48:]) != 1 || binary.LittleEndian.Uint16(b[510:]) != 1 || binary.LittleEndian.Uint16(b[1022:]) != 1 {
		t.Fatal("incorrect USA sequence")
	}
}

func TestStreamWritePlanBoundsAndChunking(t *testing.T) {
	img := &adsImage{testImage: &testImage{data: bytes.Repeat([]byte{0x77}, 200*512)}}
	f := &FS{img: img, bytesPerSector: 512, sectorsPerCluster: 1}
	runs := []dataRun{{VCN: 0, LCN: 2, Clusters: 140}, {VCN: 140, LCN: 150, Clusters: 20}}
	plan, err := f.streamWritePlan(runs, 7, 160*512-7)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0x33}, 70*1024)
	if err := f.writeStreamPlan(plan, payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 160*512)
	if err := f.readRunsAt(runs, uint64(len(got)), 0, got); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, len(got))
	copy(want, bytes.Repeat([]byte{0x77}, 7))
	copy(want[7:], payload)
	if !bytes.Equal(got, want) {
		t.Fatal("fragmented chunked write/padding mismatch")
	}
	if !bytes.Equal(img.data[142*512:150*512], bytes.Repeat([]byte{0x77}, 8*512)) {
		t.Fatal("gap modified")
	}
	for _, bad := range [][]dataRun{
		{{VCN: 1, LCN: 2, Clusters: 1}},
		{{VCN: 0, LCN: -1, Clusters: 1}},
		{{VCN: 0, LCN: 2, Clusters: 0}},
		{{VCN: 0, LCN: 2, Clusters: ^uint64(0)}},
		{{VCN: 0, LCN: 2, Clusters: 2}, {VCN: 2, LCN: 3, Clusters: 1}},
		{{VCN: 0, LCN: 200, Clusters: 1}},
	} {
		if _, err := f.streamWritePlan(bad, 0, 1); err == nil {
			t.Fatalf("accepted invalid runs: %+v", bad)
		}
	}
	if _, err := f.streamWritePlan(runs, 0, 160*512+1); err == nil {
		t.Fatal("accepted write beyond allocation")
	}
}
