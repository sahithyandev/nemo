package technique

import (
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

func TestNamedStreamHideAgainstFakeFilesystem(t *testing.T) {
	fake := fakefs.New("/target")
	entry, err := fake.Open("/target")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Get(NamedStream)
	if err != nil {
		t.Fatal(err)
	}

	result, err := selected.Hide(entry, HideRequest{StreamName: "secret", Data: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Technique != NamedStream || result.Target != "/target" || result.Detail != "secret" || result.Bytes != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
	written, err := fake.Entry("/target").ReadStream("secret")
	if err != nil || string(written) != "payload" {
		t.Fatalf("unexpected stream: %q, %v", written, err)
	}
}

func TestNamedStreamHideRejectsEntryWithoutCapability(t *testing.T) {
	selected, err := Get(NamedStream)
	if err != nil {
		t.Fatal(err)
	}
	_, err = selected.Hide(basicEntry{}, HideRequest{StreamName: "secret", Data: []byte("payload")})
	if err == nil || !strings.Contains(err.Error(), "unsupported on this filesystem") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSlackSpaceHideWritesFirstLargeEnoughRegion(t *testing.T) {
	fake := fakefs.New("/target")
	fake.Entry("/target").Slack = []filesystem.SlackRegion{
		{Offset: 10, Length: 2},
		{Offset: 20, Length: 64},
	}
	entry, err := fake.Open("/target")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Get(SlackSpace)
	if err != nil {
		t.Fatal(err)
	}

	result, err := selected.Hide(entry, HideRequest{Data: []byte("payload"), Image: fake.Img})
	if err != nil {
		t.Fatal(err)
	}
	if result.Detail != "20-75" || result.Bytes != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := string(fake.Img.Data[20+slackFrameHeaderSize : 20+slackFrameHeaderSize+7]); got != "payload" {
		t.Fatalf("unexpected slack payload %q", got)
	}
	if got := fake.Img.Data[20:28]; string(got) != string(slackFrameMagic[:]) {
		t.Fatalf("unexpected slack magic %q", got)
	}
}

func TestSlackSpaceHideRejectsPayloadWhenFrameExceedsRegion(t *testing.T) {
	fake := fakefs.New("/target")
	fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 20, Length: slackFrameHeaderSize + 6}}
	entry, err := fake.Open("/target")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Get(SlackSpace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = selected.Hide(entry, HideRequest{Data: []byte("payload"), Image: fake.Img})
	if err == nil || !strings.Contains(err.Error(), "payload plus") {
		t.Fatalf("Hide error = %v; want framed capacity error", err)
	}
}

func TestSlackSpaceFrameHideDetectClearRoundTrip(t *testing.T) {
	fake := fakefs.New("/target")
	region := filesystem.SlackRegion{Offset: 20, Length: 80}
	fake.Entry("/target").Slack = []filesystem.SlackRegion{region}
	for i := region.Offset; i < region.Offset+region.Length; i++ {
		fake.Img.Data[i] = 0xa5
	}
	entry, err := fake.Open("/target")
	if err != nil {
		t.Fatal(err)
	}
	recorder := custody.Wrap(fake.Img)
	selected, _ := Get(SlackSpace)
	result, err := selected.Hide(entry, HideRequest{Data: []byte("payload"), Image: recorder})
	if err != nil {
		t.Fatal(err)
	}
	frameLength := int64(slackFrameHeaderSize + len("payload"))
	untouched := append([]byte(nil), fake.Img.Data[region.Offset+frameLength:region.Offset+region.Length]...)
	beforeDetect := append([]byte(nil), fake.Img.Data...)

	findings, err := DetectSlackSpace(entry, recorder)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Size != 7 || findings[0].Location != result.Detail {
		t.Fatalf("findings = %+v", findings)
	}
	if !bytes.Equal(fake.Img.Data, beforeDetect) || len(recorder.EventsSnapshot()) != 1 {
		t.Fatal("slack detection modified the image")
	}

	cleared, err := ClearSlackSpace(entry, recorder)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Bytes != 7 || cleared.Detail != result.Detail {
		t.Fatalf("clear result = %+v", cleared)
	}
	if !bytes.Equal(fake.Img.Data[region.Offset:region.Offset+frameLength], make([]byte, frameLength)) {
		t.Fatal("validated slack frame was not cleared")
	}
	if !bytes.Equal(fake.Img.Data[region.Offset+frameLength:region.Offset+region.Length], untouched) {
		t.Fatal("clear changed bytes outside the Nemo frame")
	}
	if len(recorder.EventsSnapshot()) != 2 {
		t.Fatalf("custody events = %d; want hide and clear", len(recorder.EventsSnapshot()))
	}
}

func TestSlackSpaceDetectRejectsCorruptFrameWithoutWriting(t *testing.T) {
	fake := fakefs.New("/target")
	fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 20, Length: 80}}
	entry, _ := fake.Open("/target")
	selected, _ := Get(SlackSpace)
	if _, err := selected.Hide(entry, HideRequest{Data: []byte("payload"), Image: fake.Img}); err != nil {
		t.Fatal(err)
	}
	fake.Img.Data[20+slackFrameHeaderSize] ^= 0xff
	before := append([]byte(nil), fake.Img.Data...)
	if _, err := DetectSlackSpace(entry, fake.Img); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("DetectSlackSpace error = %v", err)
	}
	if !bytes.Equal(fake.Img.Data, before) {
		t.Fatal("corrupt-frame detection modified the image")
	}
}

func TestTimestompHideSetsSelectedField(t *testing.T) {
	fake := fakefs.New("/target")
	entry, err := fake.Open("/target")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Get(Timestomp)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)

	result, err := selected.Hide(entry, HideRequest{Field: filesystem.TimeModified, Timestamp: want})
	if err != nil {
		t.Fatal(err)
	}
	if result.Detail != "modified=2026-08-23T12:00:00Z" || result.Bytes != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := fake.Entry("/target").Times[filesystem.TimeModified]; !got.Equal(want) {
		t.Fatalf("unexpected modified time %v", got)
	}
}

func TestTimestompHidePreservesFractionalTimestampInResult(t *testing.T) {
	fake := fakefs.New("/target")
	entry, err := fake.Open("/target")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Get(Timestomp)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.September, 14, 12, 0, 0, 123456789, time.FixedZone("offset", 5*60*60+30*60))

	result, err := selected.Hide(entry, HideRequest{Field: filesystem.TimeAccessed, Timestamp: want})
	if err != nil {
		t.Fatal(err)
	}
	if result.Detail != "accessed=2026-09-14T12:00:00.123456789+05:30" {
		t.Fatalf("result detail = %q", result.Detail)
	}
}

type basicEntry struct{}

func (basicEntry) Path() string { return "/target" }
func (basicEntry) IsDir() bool  { return false }
func (basicEntry) Children() ([]filesystem.Entry, error) {
	return nil, errors.New("not a directory")
}
func (basicEntry) NamedStreams() ([]string, error) { return nil, fs.ErrNotExist }
