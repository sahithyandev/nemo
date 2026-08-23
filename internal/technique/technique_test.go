package technique

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

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
		{Offset: 20, Length: 8},
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
	if result.Detail != "20-27" || result.Bytes != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := string(fake.Img.Data[20:27]); got != "payload" {
		t.Fatalf("unexpected slack payload %q", got)
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

type basicEntry struct{}

func (basicEntry) Path() string { return "/target" }
func (basicEntry) IsDir() bool  { return false }
func (basicEntry) Children() ([]filesystem.Entry, error) {
	return nil, errors.New("not a directory")
}
func (basicEntry) NamedStreams() ([]string, error) { return nil, fs.ErrNotExist }
