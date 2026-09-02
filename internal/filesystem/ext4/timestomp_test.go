package ext4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
)

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
			for _, field := range []filesystem.TimeField{filesystem.TimeModified, filesystem.TimeAccessed} {
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
	img := syntheticTimestampImage(24)
	recorder := custody.Wrap(img)
	entry := timestampTestEntry(t, recorder)
	want := mustRFC3339(t, "2446-05-10T22:38:55.999999999Z")

	if err := entry.SetTimestamp(filesystem.TimeModified, want); err != nil {
		t.Fatalf("SetTimestamp: %v", err)
	}
	got, err := entry.Timestamp(filesystem.TimeModified)
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
