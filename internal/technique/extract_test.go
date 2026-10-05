package technique

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

func TestNamedStreamExtractReturnsStreamData(t *testing.T) {
	fake := fakefs.New("/target")
	if err := fake.Entry("/target").WriteStream("secret", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	entry, _ := fake.Open("/target")
	tech, _ := Get(NamedStream)

	got, err := tech.Extract(entry, Request{StreamName: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("unexpected payload %q", got)
	}
}

func TestNamedStreamExtractRequiresStreamName(t *testing.T) {
	fake := fakefs.New("/target")
	entry, _ := fake.Open("/target")
	tech, _ := Get(NamedStream)

	if _, err := tech.Extract(entry, Request{}); err == nil {
		t.Fatal("expected an error when no stream name is given")
	}
}

func TestSlackSpaceExtractRoundTripsHiddenPayload(t *testing.T) {
	fake := fakefs.New("/target")
	fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fake.Img.Data))}}
	entry, _ := fake.Open("/target")
	tech, _ := Get(SlackSpace)

	want := []byte("hidden!")
	if _, err := tech.Hide(entry, Request{Data: want, Image: fake.Img}); err != nil {
		t.Fatal(err)
	}

	got, err := tech.Extract(entry, Request{Image: fake.Img})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected payload %q, want %q", got, want)
	}
}

func TestSlackSpaceExtractNoFrameErrors(t *testing.T) {
	fake := fakefs.New("/target")
	fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fake.Img.Data))}}
	entry, _ := fake.Open("/target")
	tech, _ := Get(SlackSpace)

	got, err := tech.Extract(entry, Request{Image: fake.Img})
	if err == nil {
		t.Fatalf("expected an error for an unframed region, got payload %q", got)
	}
	if got != nil {
		t.Fatalf("expected nil payload on error, got %q", got)
	}
}

// bareEntry implements only filesystem.Entry, no capability interfaces.
type bareEntry struct{}

func (bareEntry) Path() string                          { return "/bare" }
func (bareEntry) IsDir() bool                           { return false }
func (bareEntry) Children() ([]filesystem.Entry, error) { return nil, nil }
func (bareEntry) NamedStreams() ([]string, error)       { return nil, nil }

func TestExtractUnsupportedEntryReturnsSentinel(t *testing.T) {
	tech, _ := Get(NamedStream)
	if _, err := tech.Extract(bareEntry{}, Request{StreamName: "secret"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestTimestompExtractHasNoPayload(t *testing.T) {
	fake := fakefs.New("/target")
	entry, _ := fake.Open("/target")
	tech, _ := Get(Timestomp)

	if _, err := tech.Extract(entry, Request{}); err == nil {
		t.Fatal("expected timestomp extract to error")
	}
}
