package fakefs

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

// --------------------
// Tree / Open tests
// --------------------

func TestOpenNested(t *testing.T) {
	f := New("/dir/test.txt")

	entry, err := f.Open("/dir/test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if entry.Path() != "/dir/test.txt" {
		t.Fatalf("expected /dir/test.txt, got %s", entry.Path())
	}

	if entry.IsDir() {
		t.Fatal("expected file, got directory")
	}
}

func TestOpenMissingEntry(t *testing.T) {
	f := New()

	entry, err := f.Open("/missing.txt")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", err)
	}

	if entry != nil {
		t.Fatal("expected nil entry for missing path")
	}
}

func TestRelativePathsAreRootedUnderSlash(t *testing.T) {
	f := New("dir/test.txt")

	entry, err := f.Open("/dir/test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if entry.Path() != "/dir/test.txt" {
		t.Fatalf("expected /dir/test.txt, got %s", entry.Path())
	}

	root, err := f.Open("/")
	if err != nil {
		t.Fatal(err)
	}

	children, err := root.Children()
	if err != nil {
		t.Fatal(err)
	}

	if len(children) != 1 || children[0].Path() != "/dir" {
		t.Fatalf("expected root to have child /dir, got %v", children)
	}
}

func TestRootIsDir(t *testing.T) {
	f := New()

	if f.Root().Path() != "/" {
		t.Fatalf("expected root path /, got %s", f.Root().Path())
	}

	if !f.Root().IsDir() {
		t.Fatal("expected root to be a directory")
	}
}

func TestChildrenAreDirectAndSorted(t *testing.T) {
	f := New("/dir/b.txt", "/dir/a.txt", "/dir/sub/c.txt")

	dir, err := f.Open("/dir")
	if err != nil {
		t.Fatal(err)
	}

	children, err := dir.Children()
	if err != nil {
		t.Fatal(err)
	}

	if len(children) != 3 {
		t.Fatalf("expected 3 direct children, got %d", len(children))
	}

	if children[0].Path() != "/dir/a.txt" || children[1].Path() != "/dir/b.txt" || children[2].Path() != "/dir/sub" {
		t.Fatalf("unexpected child order: %v", []string{children[0].Path(), children[1].Path(), children[2].Path()})
	}
}

func TestFSType(t *testing.T) {
	f := New()
	f.FSType = filesystem.TypeAPFS

	if f.Type() != filesystem.TypeAPFS {
		t.Fatalf("expected %s, got %s", filesystem.TypeAPFS, f.Type())
	}
}

// --------------------
// Named stream tests
// --------------------

func TestNamedStreamRoundTrip(t *testing.T) {
	f := New("/test.txt")
	entry := f.Entry("/test.txt")

	data := []byte("hidden data")

	if err := entry.WriteStream("secret", data); err != nil {
		t.Fatal(err)
	}

	result, err := entry.ReadStream("secret")
	if err != nil {
		t.Fatal(err)
	}

	if string(result) != "hidden data" {
		t.Fatalf("expected hidden data, got %s", string(result))
	}

	names, err := entry.NamedStreams()
	if err != nil {
		t.Fatal(err)
	}

	if len(names) != 1 || names[0] != "secret" {
		t.Fatalf("expected [secret], got %v", names)
	}

	if err := entry.DeleteStream("secret"); err != nil {
		t.Fatal(err)
	}

	names, err = entry.NamedStreams()
	if err != nil {
		t.Fatal(err)
	}

	if len(names) != 0 {
		t.Fatalf("expected 0 named streams after deletion, got %d", len(names))
	}
}

func TestReadMissingStream(t *testing.T) {
	entry := &Entry{}

	_, err := entry.ReadStream("nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestDeleteMissingStream(t *testing.T) {
	entry := &Entry{}

	if err := entry.DeleteStream("nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", err)
	}
}

// --------------------
// Slack-space tests
// --------------------

func TestSlackRegionsConfigurable(t *testing.T) {
	entry := &Entry{
		Slack: []filesystem.SlackRegion{{Offset: 100, Length: 20}},
	}

	regions, err := entry.SlackRegions()
	if err != nil {
		t.Fatal(err)
	}

	if len(regions) != 1 || regions[0].Offset != 100 || regions[0].Length != 20 {
		t.Fatalf("unexpected regions: %v", regions)
	}
}

func TestSlackWriteRoundTripsThroughImage(t *testing.T) {
	f := New("/test.txt")
	entry := f.Entry("/test.txt")
	entry.Slack = []filesystem.SlackRegion{{Offset: 100, Length: 4}}

	regions, err := entry.SlackRegions()
	if err != nil {
		t.Fatal(err)
	}

	region := regions[0]
	data := []byte("hide")

	if _, err := f.Img.WriteAt(data, region.Offset); err != nil {
		t.Fatal(err)
	}

	got := f.Img.Data[region.Offset : region.Offset+region.Length]
	if string(got) != "hide" {
		t.Fatalf("expected hide, got %s", string(got))
	}
}

// --------------------
// Timestomp tests
// --------------------

func TestSetTimestamp(t *testing.T) {
	entry := &Entry{}

	expected := time.Date(2026, time.August, 22, 10, 30, 0, 0, time.UTC)

	if err := entry.SetTimestamp(filesystem.TimeModified, expected); err != nil {
		t.Fatal(err)
	}

	actual, exists := entry.Times[filesystem.TimeModified]
	if !exists {
		t.Fatal("modified timestamp was not stored")
	}

	if !actual.Equal(expected) {
		t.Fatalf("expected timestamp %v, got %v", expected, actual)
	}
}
