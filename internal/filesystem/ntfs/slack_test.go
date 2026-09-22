package ntfs

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
)

func slackAttribute(initialized uint64, fragmented bool) []byte {
	runs := []byte{0x11, 2, 40, 0}
	if fragmented {
		runs = []byte{0x11, 1, 40, 0x11, 1, 4, 0}
	}
	a := nonResidentTestAttribute(attributeTypeData, runs, 2, 900)
	binary.LittleEndian.PutUint64(a[56:], initialized)
	return a
}

func TestSlackCalculation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		initialized uint64
		fragmented  bool
		want        []filesystem.SlackRegion
	}{
		{"contiguous", 900, false, []filesystem.SlackRegion{{Offset: 40*512 + 900, Length: 124}}},
		{"fragmented", 900, true, []filesystem.SlackRegion{{Offset: 44*512 + 388, Length: 124}}},
		{"initialized boundary", 512, true, []filesystem.SlackRegion{{Offset: 44 * 512, Length: 512}}},
		{"multiple tails", 400, true, []filesystem.SlackRegion{{Offset: 40*512 + 400, Length: 112}, {Offset: 44 * 512, Length: 512}}},
		{"uninitialized", 0, true, []filesystem.SlackRegion{{Offset: 40 * 512, Length: 512}, {Offset: 44 * 512, Length: 512}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := adsFixture(t, slackAttribute(tc.initialized, tc.fragmented))
			got, err := e.SlackRegions()
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("regions = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	for _, a := range [][]byte{residentTestAttribute(attributeTypeData, []byte("hello"), ""), nonResidentTestAttribute(attributeTypeData, []byte{0x11, 2, 40, 0}, 2, 1024)} {
		e, _ := adsFixture(t, a)
		if got, err := e.SlackRegions(); err != nil || len(got) != 0 {
			t.Fatalf("no slack = %v, %v", got, err)
		}
	}
	e, _ := adsFixture(t)
	if got, err := e.SlackRegions(); err != nil || len(got) != 0 {
		t.Fatalf("missing DATA = %v, %v", got, err)
	}
	e.isDir = true
	if got, err := e.SlackRegions(); err != nil || len(got) != 0 {
		t.Fatalf("directory = %v, %v", got, err)
	}
}

func TestSlackOperationsAndCustody(t *testing.T) {
	for _, fragmented := range []bool{false, true} {
		for _, restore := range []bool{false, true} {
			t.Run(fmt.Sprintf("fragmented=%v/restore=%v", fragmented, restore), func(t *testing.T) {
				e, img := adsFixture(t, slackAttribute(400, fragmented))
				for i := 40 * 512; i < 46*512; i++ {
					img.data[i] = byte(i % 251)
				}
				wrapped := custody.Wrap(img)
				e.fs.img = wrapped
				before := append([]byte(nil), img.data...)
				tech, _ := technique.Get(technique.SlackSpace)
				// For fragmented files this exceeds the first tail, exercising selection
				// of a later physical run without writing the intervening clusters.
				payload := bytes.Repeat([]byte{0x71}, 150)
				var backup technique.Backup
				result, err := tech.Hide(e, technique.Request{Image: wrapped, Data: payload, Backup: func(b technique.Backup) error { backup = b; return nil }})
				if err != nil {
					t.Fatal(err)
				}
				offset := int64(40*512 + 400)
				if fragmented {
					offset = 44 * 512
				}
				frameLen := 12 + len(payload)
				if result.Bytes != int64(len(payload)) || !bytes.Equal(img.data[int(offset)+12:int(offset)+frameLen], payload) {
					t.Fatal("payload mismatch")
				}
				findings, err := tech.Detect(e, technique.Request{Image: wrapped})
				if err != nil || len(findings) != 1 || findings[0].Size != int64(len(payload)) {
					t.Fatalf("detect = %v, %v", findings, err)
				}
				for i, b := range before {
					if int64(i) < offset || int64(i) >= offset+int64(frameLen) {
						if img.data[i] != b {
							t.Fatalf("non-slack byte %d changed", i)
						}
					}
				}
				events := wrapped.EventsSnapshot()
				sum := sha256.Sum256(img.data[int(offset) : int(offset)+frameLen])
				if len(events) != 1 || events[0].Offset != offset || events[0].SHA256 != fmt.Sprintf("%x", sum) || events[0].Timestamp.IsZero() {
					t.Fatalf("custody = %v", events)
				}
				req := technique.Request{Image: wrapped}
				if restore {
					req.Restore = backup.Original
				}
				if _, err := tech.Clear(e, req); err != nil {
					t.Fatal(err)
				}
				if restore {
					if !bytes.Equal(before, img.data) {
						t.Fatal("restore mismatch")
					}
				} else {
					expected := append([]byte(nil), before...)
					clear(expected[int(offset) : int(offset)+frameLen])
					if !bytes.Equal(expected, img.data) {
						t.Fatal("clear altered bytes outside frame")
					}
				}
				findings, err = tech.Detect(e, technique.Request{Image: wrapped})
				if err != nil || len(findings) != 0 {
					t.Fatalf("detect after clear = %v, %v", findings, err)
				}
				if events = wrapped.EventsSnapshot(); len(events) != 2 || events[1].Offset != offset {
					t.Fatalf("clear custody = %v", events)
				}
				unchangedFailure(t, img, "insufficient slack", func() error {
					_, err := tech.Hide(e, technique.Request{Image: wrapped, Data: make([]byte, 2000)})
					return err
				})
				if len(wrapped.EventsSnapshot()) != 2 {
					t.Fatal("rejected write generated event")
				}
			})
		}
	}
}

func TestSlackRejectsBeforeMutation(t *testing.T) {
	cases := []struct {
		name string
		edit func([]byte) []byte
	}{
		{"sparse flag", func(a []byte) []byte { binary.LittleEndian.PutUint16(a[12:], 0x8000); return a }},
		{"compressed", func(a []byte) []byte { binary.LittleEndian.PutUint16(a[12:], 1); return a }},
		{"encrypted", func(a []byte) []byte { binary.LittleEndian.PutUint16(a[12:], 0x4000); return a }},
		{"compression unit", func(a []byte) []byte { binary.LittleEndian.PutUint16(a[34:], 4); return a }},
		{"sparse run", func(a []byte) []byte { return nonResidentTestAttribute(attributeTypeData, []byte{0x01, 2, 0}, 2, 900) }},
		{"overlapping runs", func(a []byte) []byte {
			return nonResidentTestAttribute(attributeTypeData, []byte{0x11, 1, 40, 0x11, 1, 0, 0}, 2, 900)
		}},
		{"later out of bounds", func(a []byte) []byte {
			return nonResidentTestAttribute(attributeTypeData, []byte{0x11, 1, 40, 0x11, 1, 30, 0}, 2, 400)
		}},
		{"invalid runlist", func(a []byte) []byte { a[64] = 0x19; return a }},
		{"allocation mismatch", func(a []byte) []byte { binary.LittleEndian.PutUint64(a[40:], 1536); return a }},
		{"initialized exceeds data", func(a []byte) []byte { binary.LittleEndian.PutUint64(a[56:], 1000); return a }},
		{"nonzero VCN", func(a []byte) []byte {
			binary.LittleEndian.PutUint64(a[16:], 1)
			binary.LittleEndian.PutUint64(a[24:], 2)
			return a
		}},
		{"MFT overlap", func(a []byte) []byte { a[66] = 22; return a }},
		{"boot overlap", func(a []byte) []byte { a[66] = 0; return a }},
		{"mirror overlap", func(a []byte) []byte { a[66] = 4; return a }},
		{"ATTRIBUTE_LIST", func(a []byte) []byte { return residentTestAttribute(attributeTypeAttributeList, nil, "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, img := adsFixture(t, tc.edit(slackAttribute(400, false)))
			wrapped := custody.Wrap(img)
			e.fs.img = wrapped
			before := append([]byte(nil), img.data...)
			tech, _ := technique.Get(technique.SlackSpace)
			if _, err := e.SlackRegions(); err == nil {
				t.Fatal("accepted invalid regions")
			}
			if _, err := tech.Hide(e, technique.Request{Image: wrapped, Data: []byte("test")}); err == nil {
				t.Fatal("accepted invalid write")
			}
			if _, err := tech.Clear(e, technique.Request{Image: wrapped}); err == nil {
				t.Fatal("accepted invalid clear")
			}
			if img.writes != 0 || !bytes.Equal(before, img.data) || len(wrapped.EventsSnapshot()) != 0 {
				t.Fatal("validation mutated image")
			}
		})
	}
}

func TestSlackRejectsOtherAttributeOverlap(t *testing.T) {
	e, img := adsFixture(t, slackAttribute(400, false), namedNonresident([]byte{0x11, 1, 41, 0}, 1, 100, 100))
	unchangedFailure(t, img, "overlaps", func() error { _, err := e.SlackRegions(); return err })
}
