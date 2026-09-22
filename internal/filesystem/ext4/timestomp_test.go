package ext4

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
)

func TestTimestompTechniquePreservesImageAndRestoresOriginal(t *testing.T) {
	for _, test := range []struct {
		field      filesystem.TimeField
		low, extra int
	}{
		{filesystem.TimeCreated, 144, 148},
		{filesystem.TimeModified, 16, 136},
		{filesystem.TimeAccessed, 8, 140},
		{filesystem.TimeChanged, 12, 132},
	} {
		t.Run(string(test.field), func(t *testing.T) {
			field := test.field
			img := syntheticTimestampImage(24)
			entry := timestampTestEntry(t, img)
			original := time.Unix(123456789, 123456789).UTC()
			if err := entry.SetTimestamp(field, original); err != nil {
				t.Fatal(err)
			}
			before := append([]byte(nil), img.data...)
			tech, err := technique.Get(technique.Timestomp)
			if err != nil {
				t.Fatal(err)
			}
			want := mustRFC3339(t, "2310-04-04T16:10:40.999999999Z")
			if _, err := tech.Hide(entry, technique.Request{Field: field, Timestamp: want}); err != nil {
				t.Fatal(err)
			}
			// Reopen to verify persisted bytes rather than entry-local state.
			entry = timestampTestEntry(t, img)
			if got, err := entry.Timestamp(field); err != nil || !got.Equal(want) {
				t.Fatalf("timestamp = %v, %v; want %v", got, err, want)
			}
			raw, off, err := entry.fs.readRawInode(entry.inode)
			if err != nil {
				t.Fatal(err)
			}
			// Independent on-disk expectations, as in the NTFS preservation test:
			// 2310-04-04 begins epoch 3 at signed low seconds -2^31.
			if binary.LittleEndian.Uint32(raw[test.low:]) != 0x80000000 ||
				binary.LittleEndian.Uint32(raw[test.extra:]) != uint32(999999999)<<2|3 {
				t.Fatal("wrong on-disk timestamp")
			}
			for i, b := range before {
				rel := i - int(off)
				allowed := rel >= test.low && rel < test.low+4 || rel >= test.extra && rel < test.extra+4 || rel >= 124 && rel < 126 || rel >= 130 && rel < 132
				if !allowed && img.data[i] != b {
					t.Fatalf("unrelated byte changed at %d", i)
				}
			}
			assertValidInodeChecksum(t, entry.fs.sb, entry.inode, raw)
			if _, err := tech.Clear(entry, technique.Request{Field: field, Timestamp: original}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, img.data) {
				t.Fatal("clear did not restore original image")
			}
		})
	}
}

func TestSetTimestampRejectsInvalidValuesWithoutWriting(t *testing.T) {
	for _, test := range []struct {
		name  string
		extra uint16
		value time.Time
	}{
		{"zero", 24, time.Time{}},
		{"before minimum", 24, time.Unix(-1<<31-1, 0)},
		{"after maximum", 24, time.Unix((3<<32)+(1<<31), 0)},
		{"legacy overflow", 0, time.Unix(1<<31, 0)},
		{"legacy nanoseconds", 0, time.Unix(1, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, field := range []filesystem.TimeField{filesystem.TimeAccessed, filesystem.TimeModified, filesystem.TimeChanged, filesystem.TimeCreated} {
				// A 20-byte extra area provides creation seconds without crtime_extra.
				extra := test.extra
				if field == filesystem.TimeCreated && extra == 0 {
					extra = 20
				}
				img := syntheticTimestampImage(extra)
				recorder := custody.Wrap(img)
				entry := timestampTestEntry(t, recorder)
				before := append([]byte(nil), img.data...)
				if err := entry.SetTimestamp(field, test.value); err == nil {
					t.Fatalf("SetTimestamp(%s, %v) succeeded", field, test.value)
				}
				if !bytes.Equal(before, img.data) || len(recorder.EventsSnapshot()) != 0 {
					t.Fatalf("invalid %s timestamp caused a write", field)
				}
			}
		})
	}
}

func TestSetTimestampRejectsMissingOrInvalidInode(t *testing.T) {
	for _, test := range []struct {
		name       string
		invalidate func(*Entry, *testImage)
	}{
		{"zero reference", func(e *Entry, _ *testImage) { e.inode = 0 }},
		{"reference past inode count", func(e *Entry, _ *testImage) { e.inode = e.fs.sb.inodesCount + 1 }},
		{"missing inode group", func(e *Entry, _ *testImage) { e.fs.groupCount = 0 }},
		{"invalid inode table", func(e *Entry, img *testImage) {
			put32(img.data, int(e.fs.gdtOffset)+8, uint32(e.fs.sb.blocksCount))
		}},
		{"missing inode bytes", func(_ *Entry, img *testImage) {
			img.data = img.data[:timestampTestInodeOffset()]
		}},
		{"truncated inode", func(_ *Entry, img *testImage) {
			img.data = img.data[:timestampTestInodeOffset()+255]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			img := syntheticTimestampImage(24)
			recorder := custody.Wrap(img)
			entry := timestampTestEntry(t, recorder)
			test.invalidate(entry, img)
			before := append([]byte(nil), img.data...)
			for _, field := range []filesystem.TimeField{filesystem.TimeAccessed, filesystem.TimeModified, filesystem.TimeChanged, filesystem.TimeCreated} {
				if err := entry.SetTimestamp(field, time.Unix(123456789, 0)); err == nil {
					t.Fatalf("SetTimestamp(%s) accepted invalid inode", field)
				}
			}
			if !bytes.Equal(before, img.data) || len(recorder.EventsSnapshot()) != 0 {
				t.Fatal("invalid inode caused a write")
			}
		})
	}
}

func TestTimestampRejectsInvalidLayouts(t *testing.T) {
	for _, size := range []int{0, 127, 129} {
		if _, err := timestampLayout(make([]byte, size), filesystem.TimeModified); err == nil {
			t.Fatalf("accepted inode size %d", size)
		}
	}
	for _, extra := range []uint16{1, 2, 3, 5, 132} {
		img := syntheticTimestampImage(extra)
		before := append([]byte(nil), img.data...)
		entry := timestampTestEntry(t, img)
		if err := entry.SetTimestamp(filesystem.TimeModified, time.Unix(1, 0)); err == nil {
			t.Fatalf("accepted extra-isize %d", extra)
		}
		if !bytes.Equal(before, img.data) {
			t.Fatal("invalid layout mutated image")
		}
	}
}

type timestampWriteFailure struct {
	*testImage
	err error
}

func (i timestampWriteFailure) WriteAt([]byte, int64) (int, error) { return 0, i.err }

func TestTimestampWriteErrors(t *testing.T) {
	for _, writeErr := range []error{io.ErrClosedPipe, nil} {
		img := timestampWriteFailure{syntheticTimestampImage(24), writeErr}
		entry := timestampTestEntry(t, img)
		err := entry.SetTimestamp(filesystem.TimeModified, time.Unix(1, 0))
		if err == nil {
			t.Fatal("expected write error")
		}
		if writeErr != nil && !errors.Is(err, writeErr) {
			t.Fatalf("lost underlying error: %v", err)
		}
		if writeErr == nil && !strings.Contains(err.Error(), "short image write") {
			t.Fatalf("unexpected short-write error: %v", err)
		}
	}
}

func TestTimestampRejectedWriteDoesNotRecordCustody(t *testing.T) {
	for _, field := range []filesystem.TimeField{filesystem.TimeAccessed, filesystem.TimeModified, filesystem.TimeChanged, filesystem.TimeCreated} {
		t.Run(string(field), func(t *testing.T) {
			img := timestampWriteFailure{syntheticTimestampImage(24), io.ErrClosedPipe}
			recorder := custody.Wrap(img)
			entry := timestampTestEntry(t, recorder)
			before := append([]byte(nil), img.data...)
			if err := entry.SetTimestamp(field, time.Unix(123456789, 0)); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("write error = %v; want underlying failure", err)
			}
			if !bytes.Equal(before, img.data) {
				t.Fatal("rejected write changed image")
			}
			if len(recorder.EventsSnapshot()) != 0 {
				t.Fatal("rejected write recorded a custody event")
			}
		})
	}
}

type timestampPartialWrite struct{ *testImage }

func (i timestampPartialWrite) WriteAt(p []byte, off int64) (int, error) {
	// A storage failure can occur after some bytes have already been written.
	n := copy(i.data[off:], p[:len(p)/2])
	return n, io.ErrShortWrite
}

func TestTimestampPartialWriteReturnsFailure(t *testing.T) {
	img := timestampPartialWrite{syntheticTimestampImage(24)}
	entry := timestampTestEntry(t, img)
	if err := entry.SetTimestamp(filesystem.TimeModified, time.Unix(123456789, 0)); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("partial write error = %v; want io.ErrShortWrite", err)
	}
}

func TestExt4TimestampRFC3339EpochBoundaries(t *testing.T) {
	tests := []struct {
		text  string
		epoch uint32
	}{
		{"1901-12-13T20:45:52Z", 0},
		{"1969-12-31T23:59:59.999999999Z", 0},
		{"1970-01-01T00:00:00Z", 0},
		{"2038-01-19T03:14:07.000000001Z", 0},
		{"2038-01-19T03:14:08Z", 1},
		{"2174-02-25T09:42:23Z", 1},
		{"2174-02-25T09:42:24Z", 2},
		{"2310-04-04T16:10:39Z", 2},
		{"2310-04-04T16:10:40Z", 3},
		{"2446-05-10T22:38:55.999999999Z", 3},
	}

	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			want, err := time.Parse(time.RFC3339Nano, test.text)
			if err != nil {
				t.Fatal(err)
			}
			low, extra, err := encodeExt4Timestamp(want, true)
			if err != nil {
				t.Fatalf("encodeExt4Timestamp: %v", err)
			}
			if got := extra & ext4EpochMask; got != test.epoch {
				t.Fatalf("epoch bits = %d, want %d", got, test.epoch)
			}
			got, err := decodeExt4Timestamp(low, extra, true)
			if err != nil {
				t.Fatalf("decodeExt4Timestamp: %v", err)
			}
			if got.Format(time.RFC3339Nano) != test.text {
				t.Fatalf("round trip = %q, want %q", got.Format(time.RFC3339Nano), test.text)
			}
		})
	}
}

func TestExt4TimestampRejectsUnrepresentableValues(t *testing.T) {
	tests := []struct {
		name     string
		value    time.Time
		extended bool
	}{
		{"legacy before minimum", time.Unix(-1<<31-1, 0), false},
		{"legacy after maximum", time.Unix(1<<31, 0), false},
		{"legacy nanoseconds", time.Unix(0, 1), false},
		{"extended before minimum", time.Unix(-1<<31-1, 0), true},
		{"extended after maximum", time.Unix((3<<32)+(1<<31), 0), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := encodeExt4Timestamp(test.value, test.extended); err == nil {
				t.Fatal("encodeExt4Timestamp: expected error")
			}
		})
	}

	if _, err := decodeExt4Timestamp(0, uint32(1_000_000_000)<<ext4EpochBits, true); err == nil {
		t.Fatal("decodeExt4Timestamp: expected invalid nanoseconds error")
	}
}

func TestEntryReportsTimestampSupportFromInodeLayout(t *testing.T) {
	tests := []struct {
		name         string
		extraIsize   uint16
		wantCreated  bool
		wantMtimeExt bool
		wantAtimeExt bool
	}{
		{"legacy", 0, false, false, false},
		{"modification extra", 12, false, true, false},
		{"access extra", 16, false, true, true},
		{"creation seconds", 20, true, true, true},
		{"creation extra", 24, true, true, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := timestampTestEntry(t, syntheticTimestampImage(test.extraIsize))
			for _, field := range []filesystem.TimeField{filesystem.TimeModified, filesystem.TimeAccessed, filesystem.TimeChanged} {
				got, err := entry.SupportsTimestamp(field)
				if err != nil || !got {
					t.Fatalf("SupportsTimestamp(%q) = %v, %v; want true", field, got, err)
				}
			}
			got, err := entry.SupportsTimestamp(filesystem.TimeCreated)
			if err != nil || got != test.wantCreated {
				t.Fatalf("SupportsTimestamp(created) = %v, %v; want %v", got, err, test.wantCreated)
			}

			raw, _, err := entry.fs.readRawInode(entry.inode)
			if err != nil {
				t.Fatal(err)
			}
			mtimeLayout, err := timestampLayout(raw, filesystem.TimeModified)
			if err != nil || mtimeLayout.hasExtra != test.wantMtimeExt {
				t.Fatalf("mtime extra = %v, %v; want %v", mtimeLayout.hasExtra, err, test.wantMtimeExt)
			}
			atimeLayout, err := timestampLayout(raw, filesystem.TimeAccessed)
			if err != nil || atimeLayout.hasExtra != test.wantAtimeExt {
				t.Fatalf("atime extra = %v, %v; want %v", atimeLayout.hasExtra, err, test.wantAtimeExt)
			}
		})
	}
}

func TestEntryTimestampRoundTripRestoresOriginalInode(t *testing.T) {
	img := syntheticTimestampImage(24)
	inodeOff := timestampTestInodeOffset()
	raw := img.data[inodeOff : inodeOff+256]
	originals := map[filesystem.TimeField]time.Time{
		filesystem.TimeCreated:  mustRFC3339(t, "2038-01-19T03:14:08.123456789Z"),
		filesystem.TimeModified: mustRFC3339(t, "1969-12-31T23:59:59.987654321Z"),
		filesystem.TimeAccessed: mustRFC3339(t, "2310-04-04T16:10:40.000000001Z"),
		filesystem.TimeChanged:  mustRFC3339(t, "2174-02-25T09:42:23.555000111Z"),
	}
	for field, value := range originals {
		writeTimestampFixture(t, raw, field, value)
	}
	updateInodeChecksum(timestampTestSuperblock(img), 3, raw)
	wantRaw := append([]byte(nil), raw...)

	entry := timestampTestEntry(t, img)
	for field, want := range originals {
		got, err := entry.Timestamp(field)
		if err != nil || !got.Equal(want) {
			t.Fatalf("Timestamp(%q) = %v, %v; want %v", field, got, err, want)
		}
	}

	changed := mustRFC3339(t, "2174-02-25T09:42:24.222333444Z")
	for field := range originals {
		if err := entry.SetTimestamp(field, changed); err != nil {
			t.Fatalf("SetTimestamp(%q, changed): %v", field, err)
		}
	}
	for field, original := range originals {
		if err := entry.SetTimestamp(field, original); err != nil {
			t.Fatalf("SetTimestamp(%q, original): %v", field, err)
		}
	}
	if got := img.data[inodeOff : inodeOff+256]; !bytes.Equal(got, wantRaw) {
		t.Fatal("restoring timestamps did not restore the original checksum-valid inode")
	}
}

func TestSetTimestampUpdatesChecksumAndUsesCustodyWrappedImage(t *testing.T) {
	for _, field := range []filesystem.TimeField{filesystem.TimeAccessed, filesystem.TimeModified, filesystem.TimeChanged, filesystem.TimeCreated} {
		t.Run(string(field), func(t *testing.T) {
			img := syntheticTimestampImage(24)
			recorder := custody.Wrap(img)
			entry := timestampTestEntry(t, recorder)
			want := mustRFC3339(t, "2446-05-10T22:38:55.999999999Z")

			before := time.Now()
			if err := entry.SetTimestamp(field, want); err != nil {
				t.Fatalf("SetTimestamp: %v", err)
			}
			after := time.Now()
			got, err := entry.Timestamp(field)
			if err != nil || !got.Equal(want) {
				t.Fatalf("Timestamp = %v, %v; want %v", got, err, want)
			}
			events := recorder.EventsSnapshot()
			if len(events) != 1 {
				t.Fatalf("custody events = %d, want 1", len(events))
			}
			if events[0].Offset != int64(timestampTestInodeOffset()) {
				t.Fatalf("custody offset = %d, want %d", events[0].Offset, timestampTestInodeOffset())
			}

			raw, _, err := entry.fs.readRawInode(entry.inode)
			if err != nil {
				t.Fatal(err)
			}
			assertValidInodeChecksum(t, entry.fs.sb, entry.inode, raw)
			sum := sha256.Sum256(raw)
			if events[0].SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatal("custody hash does not cover the complete updated inode")
			}
			if events[0].Timestamp.Before(before) || events[0].Timestamp.After(after) || events[0].Timestamp.Location() != time.UTC {
				t.Fatalf("invalid custody event time: %v", events[0].Timestamp)
			}
		})
	}
}

func TestSetTimestampUnavailableFieldDoesNotMutateImage(t *testing.T) {
	img := syntheticTimestampImage(0)
	before := append([]byte(nil), img.data...)
	entry := timestampTestEntry(t, img)
	if err := entry.SetTimestamp(filesystem.TimeCreated, time.Unix(0, 0)); !errors.Is(err, errTimestampFieldUnavailable) {
		t.Fatalf("SetTimestamp(created) error = %v", err)
	}
	if !bytes.Equal(img.data, before) {
		t.Fatal("unsupported creation timestamp mutated the image")
	}
}

func syntheticTimestampImage(extraIsize uint16) *testImage {
	img := syntheticImage()
	sb := img.data[superblockOffset : superblockOffset+superblockSize]
	put32(sb, 0x64, featureROCompatMetadataCsum)
	for i := range 16 {
		sb[0x68+i] = byte(i + 1)
	}
	inodeOff := timestampTestInodeOffset()
	put16(img.data, inodeOff+inodeExtraIsizeOffset, extraIsize)
	return img
}

func timestampTestEntry(t *testing.T, img interface {
	ReadAt([]byte, int64) (int, error)
	WriteAt([]byte, int64) (int, error)
	Size() int64
	Path() string
}) *Entry {
	t.Helper()
	fsi, err := New(img)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entry, err := fsi.Open("/hello.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return entry.(*Entry)
}

func timestampTestInodeOffset() int {
	return 5*testBlockSize + 2*256
}

func timestampTestSuperblock(img *testImage) superblock {
	sbBytes := img.data[superblockOffset : superblockOffset+superblockSize]
	var uuid [16]byte
	copy(uuid[:], sbBytes[0x68:0x78])
	return superblock{
		featureROCompat: featureROCompatMetadataCsum,
		checksumSeed:    ext4CRC32C(^uint32(0), uuid[:]),
	}
}

func writeTimestampFixture(t *testing.T, raw []byte, field filesystem.TimeField, value time.Time) {
	t.Helper()
	layout, err := timestampLayout(raw, field)
	if err != nil {
		t.Fatal(err)
	}
	low, extra, err := encodeExt4Timestamp(value, layout.hasExtra)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(raw[layout.lowOffset:layout.lowOffset+4], low)
	if layout.hasExtra {
		binary.LittleEndian.PutUint32(raw[layout.extraOffset:layout.extraOffset+4], extra)
	}
}

func mustRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func assertValidInodeChecksum(t *testing.T, sb superblock, number uint32, raw []byte) {
	t.Helper()
	wantLow := binary.LittleEndian.Uint16(raw[124:126])
	wantHigh := binary.LittleEndian.Uint16(raw[130:132])
	copyRaw := append([]byte(nil), raw...)
	updateInodeChecksum(sb, number, copyRaw)
	if got := binary.LittleEndian.Uint16(copyRaw[124:126]); got != wantLow {
		t.Fatalf("inode checksum low = %#x, recomputed %#x", wantLow, got)
	}
	if got := binary.LittleEndian.Uint16(copyRaw[130:132]); got != wantHigh {
		t.Fatalf("inode checksum high = %#x, recomputed %#x", wantHigh, got)
	}
}
