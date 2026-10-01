package ntfs

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
)

func timestampAttribute() []byte {
	return residentTestAttribute(attributeTypeStandardInformation, bytes.Repeat([]byte{0x12}, 72), "")
}

func TestTimestompFieldsAndPreservation(t *testing.T) {
	fields := []filesystem.TimeField{filesystem.TimeCreated, filesystem.TimeModified, filesystem.TimeChanged, filesystem.TimeAccessed}
	for index, field := range fields {
		t.Run(string(field), func(t *testing.T) {
			e, img := adsFixture(t, timestampAttribute(),
				residentTestAttribute(attributeTypeFileName, indexFileNameValue("file.txt", false), ""),
				residentTestAttribute(attributeTypeData, []byte("default"), ""),
				residentTestAttribute(attributeTypeData, bytes.Repeat([]byte{0x67}, 350), "resident"),
				namedNonresident([]byte{0x11, 2, 40, 0}, 2, 900, 900))
			copy(img.data[40*512:], bytes.Repeat([]byte{0xab}, 1024))
			beforeImage := append([]byte(nil), img.data...)
			before, err := e.fs.readMFTRecord(7)
			if err != nil {
				t.Fatal(err)
			}
			want := time.Date(2026, 9, 21, 12, 34, 56, 123456700, time.FixedZone("offset", 19800))
			if err := e.SetTimestamp(field, want); err != nil {
				t.Fatal(err)
			}
			got, err := e.Timestamp(field)
			if err != nil || !got.Equal(want) || got.Location() != time.UTC {
				t.Fatalf("Timestamp = %v, %v", got, err)
			}
			after, err := e.fs.readMFTRecord(7)
			if err != nil {
				t.Fatal(err)
			}
			// Independent FILETIME expectation, including the timezone conversion.
			off := 56 + 24 + index*8
			wantTicks := uint64(want.Unix()+11644473600)*10000000 + 1234567
			if binary.LittleEndian.Uint64(after[off:]) != wantTicks {
				t.Fatal("wrong on-disk timestamp")
			}
			copy(before[off:off+8], after[off:off+8])
			// Only update-sequence bookkeeping may change in addition to the field.
			copy(before[48:54], after[48:54])
			if !bytes.Equal(before, after) {
				t.Fatal("unrelated record bytes changed")
			}
			copy(beforeImage[22*512:24*512], img.data[22*512:24*512])
			if !bytes.Equal(beforeImage, img.data) {
				t.Fatal("bytes outside record changed")
			}
		})
	}
}

func TestTimestompInvalidTimes(t *testing.T) {
	e, img := adsFixture(t, timestampAttribute())
	for _, value := range []time.Time{time.Time{}, decodeFiletime(0).Add(-100 * time.Nanosecond), decodeFiletime(^uint64(0)).Add(100 * time.Nanosecond), time.Unix(0, 1)} {
		unchangedFailure(t, img, "timestamp", func() error { return e.SetTimestamp(filesystem.TimeCreated, value) })
	}
	unchangedFailure(t, img, "unsupported", func() error { return e.SetTimestamp("unknown", time.Unix(0, 0)) })
	for _, ticks := range []uint64{0, 116444736000000000, ^uint64(0)} {
		if err := e.SetTimestamp(filesystem.TimeCreated, decodeFiletime(ticks)); err != nil {
			t.Fatal(err)
		}
		got, err := e.Timestamp(filesystem.TimeCreated)
		if err != nil || !got.Equal(decodeFiletime(ticks)) {
			t.Fatalf("boundary = %v, %v", got, err)
		}
	}
}

func TestTimestompTimestampAcrossFixup(t *testing.T) {
	e, img := adsFixture(t, timestampAttribute())
	// Place creation time at 504, across the first sector's protected trailer.
	b := make([]byte, 1024)
	copy(b, syntheticMFTRecord())
	binary.LittleEndian.PutUint16(b[20:], 480)
	a := timestampAttribute()
	copy(b[480:], a)
	binary.LittleEndian.PutUint32(b[480+len(a):], attributeTypeEnd)
	binary.LittleEndian.PutUint32(b[24:], uint32(480+len(a)+4))
	applyTestFixups(b, 512)
	copy(img.data[22*512:], b)
	want := time.Unix(123456789, 987654300)
	if err := e.SetTimestamp(filesystem.TimeCreated, want); err != nil {
		t.Fatal(err)
	}
	got, err := e.Timestamp(filesystem.TimeCreated)
	if err != nil || !got.Equal(want) {
		t.Fatalf("timestamp across fixup = %v, %v", got, err)
	}
}

func TestTimestompRejectsRecords(t *testing.T) {
	cases := []struct {
		name   string
		attrs  [][]byte
		mutate func([]byte)
	}{
		{name: "missing", attrs: [][]byte{residentTestAttribute(attributeTypeData, nil, "")}},
		{name: "truncated", attrs: [][]byte{residentTestAttribute(attributeTypeStandardInformation, make([]byte, 32), "")}},
		{name: "named", attrs: [][]byte{residentTestAttribute(attributeTypeStandardInformation, make([]byte, 48), "bad")}},
		{name: "duplicate", attrs: [][]byte{timestampAttribute(), timestampAttribute()}},
		{name: "attribute list", attrs: [][]byte{timestampAttribute(), residentTestAttribute(attributeTypeAttributeList, nil, "")}},
		{name: "nonresident", attrs: [][]byte{nonResidentTestAttribute(attributeTypeStandardInformation, []byte{0x11, 1, 40, 0}, 1, 48)}},
		{name: "inactive", mutate: func(b []byte) { binary.LittleEndian.PutUint16(b[22:], 0) }},
		{name: "extension", mutate: func(b []byte) { binary.LittleEndian.PutUint64(b[32:], 5) }},
		{name: "bad fixup", mutate: func(b []byte) { b[510] ^= 1 }},
		{name: "bad length", mutate: func(b []byte) { binary.LittleEndian.PutUint32(b[60:], 0) }},
		{name: "bad USA", mutate: func(b []byte) { binary.LittleEndian.PutUint16(b[20:], 48) }},
		{name: "duplicate IDs", attrs: [][]byte{timestampAttribute(), residentTestAttribute(attributeTypeData, nil, "")}, mutate: func(b []byte) { binary.LittleEndian.PutUint16(b[56+96+14:], 0) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs := tc.attrs
			if attrs == nil {
				attrs = [][]byte{timestampAttribute()}
			}
			e, img := adsFixture(t, attrs...)
			if tc.mutate != nil {
				tc.mutate(img.data[22*512 : 24*512])
			}
			wrapped := custody.Wrap(img)
			e.fs.img = wrapped
			unchangedFailure(t, img, "ntfs:", func() error { return e.SetTimestamp(filesystem.TimeCreated, time.Unix(0, 0)) })
			if len(wrapped.EventsSnapshot()) != 0 {
				t.Fatal("validation failure emitted write event")
			}
		})
	}
}

func TestTimestompFragmentedMFTCustody(t *testing.T) {
	e, img := adsFixture(t, timestampAttribute())
	mft := nonResidentTestAttribute(attributeTypeData, []byte{0x11, 2, 2, 0x11, 13, 8, 0x11, 7, 20, 0}, 22, 22*512)
	copy(img.data[2*512:], adsRecord(mft))
	copy(img.data[30*512:31*512], img.data[23*512:24*512])
	e.fs.mftRuns = nil // Reload the fragmented mapping from record zero.
	wrapped := custody.Wrap(img)
	e.fs.img = wrapped
	before := append([]byte(nil), img.data...)
	if err := e.SetTimestamp(filesystem.TimeAccessed, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	got, err := e.Timestamp(filesystem.TimeAccessed)
	if err != nil || !got.Equal(time.Unix(0, 0)) {
		t.Fatalf("Timestamp = %v, %v", got, err)
	}
	events := wrapped.EventsSnapshot()
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	for i, offset := range []int{22 * 512, 30 * 512} {
		if events[i].Offset != int64(offset) || events[i].SHA256 != fmt.Sprintf("%x", sha256.Sum256(img.data[offset:offset+512])) {
			t.Fatalf("event = %+v", events[i])
		}
		copy(before[offset:offset+512], img.data[offset:offset+512])
	}
	if !bytes.Equal(before, img.data) {
		t.Fatal("unmapped bytes changed")
	}
}

type timestampFailImage struct{ *adsImage }

func (i timestampFailImage) WriteAt([]byte, int64) (int, error) { return 0, io.ErrClosedPipe }

func TestTimestompFailedWrites(t *testing.T) {
	for _, mode := range []string{"error", "short", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			e, img := adsFixture(t, timestampAttribute())
			switch mode {
			case "error":
				e.fs.img = timestampFailImage{img}
			case "short":
				img.shortWrite = true
			case "corrupt":
				img.corruptWrite = true
			}
			wrapped := custody.Wrap(e.fs.img)
			e.fs.img = wrapped
			before := append([]byte(nil), img.data...)
			err := e.SetTimestamp(filesystem.TimeModified, time.Unix(0, 0))
			if err == nil {
				t.Fatal("failed write succeeded")
			}
			if mode == "error" && (!errors.Is(err, io.ErrClosedPipe) || len(wrapped.EventsSnapshot()) != 0) {
				t.Fatalf("error/events = %v, %+v", err, wrapped.EventsSnapshot())
			}
			if mode == "short" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
			if mode != "corrupt" && !bytes.Equal(before, img.data) {
				t.Fatal("failed write changed image")
			}
		})
	}
}
