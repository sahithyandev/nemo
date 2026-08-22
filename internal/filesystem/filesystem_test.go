package filesystem

import (
	"errors"
	"testing"
	"time"
)

// fakeEntry is a test implementation of Entry and
// all optional filesystem capabilities.
type fakeEntry struct {
	path       string
	isDir      bool
	streams    map[string][]byte
	timestamps map[TimeField]time.Time
}

// --------------------
// Entry implementation
// --------------------

func (f *fakeEntry) Path() string {
	return f.path
}

func (f *fakeEntry) IsDir() bool {
	return f.isDir
}

func (f *fakeEntry) Children() ([]Entry, error) {
	return nil, nil
}

func (f *fakeEntry) NamedStreams() ([]string, error) {
	names := make([]string, 0, len(f.streams))

	for name := range f.streams {
		names = append(names, name)
	}

	return names, nil
}

// ---------------------------------
// NamedStreamCapable implementation
// ---------------------------------

func (f *fakeEntry) WriteStream(name string, data []byte) error {
	if f.streams == nil {
		f.streams = make(map[string][]byte)
	}

	f.streams[name] = data
	return nil
}

func (f *fakeEntry) ReadStream(name string) ([]byte, error) {
	data, exists := f.streams[name]
	if !exists {
		return nil, errors.New("stream not found")
	}

	return data, nil
}

func (f *fakeEntry) DeleteStream(name string) error {
	if _, exists := f.streams[name]; !exists {
		return errors.New("stream not found")
	}

	delete(f.streams, name)
	return nil
}

// ---------------------------------
// SlackSpaceCapable implementation
// ---------------------------------

func (f *fakeEntry) SlackRegions() ([]SlackRegion, error) {
	return []SlackRegion{
		{
			Offset: 100,
			Length: 20,
		},
	}, nil
}

// ---------------------------------
// TimestompCapable implementation
// ---------------------------------

func (f *fakeEntry) SetTimestamp(field TimeField, timestamp time.Time) error {
	if f.timestamps == nil {
		f.timestamps = make(map[TimeField]time.Time)
	}

	f.timestamps[field] = timestamp
	return nil
}

// -------------------------
// fake FileSystem
// -------------------------

type fakeFileSystem struct {
	fsType Type
	root   Entry
	files  map[string]Entry
}

func (f *fakeFileSystem) Type() Type {
	return f.fsType
}

func (f *fakeFileSystem) Root() Entry {
	return f.root
}

func (f *fakeFileSystem) Open(path string) (Entry, error) {
	entry, exists := f.files[path]
	if !exists {
		return nil, errors.New("entry not found")
	}

	return entry, nil
}

// --------------------
// Compile-time checks
// --------------------

var _ Entry = (*fakeEntry)(nil)
var _ NamedStreamCapable = (*fakeEntry)(nil)
var _ SlackSpaceCapable = (*fakeEntry)(nil)
var _ TimestompCapable = (*fakeEntry)(nil)
var _ FileSystem = (*fakeFileSystem)(nil)

// --------------------
// Entry tests
// --------------------

func TestFakeEntryImplementsEntry(t *testing.T) {
	entry := &fakeEntry{
		path:  "/test.txt",
		isDir: false,
	}

	if entry.Path() != "/test.txt" {
		t.Fatalf(
			"expected path /test.txt, got %s",
			entry.Path(),
		)
	}

	if entry.IsDir() {
		t.Fatal("expected entry to be a file")
	}
}

// --------------------
// Named stream tests
// --------------------

func TestNamedStreamCapability(t *testing.T) {
	entry := &fakeEntry{
		streams: make(map[string][]byte),
	}

	data := []byte("hidden data")

	err := entry.WriteStream("secret", data)
	if err != nil {
		t.Fatal(err)
	}

	result, err := entry.ReadStream("secret")
	if err != nil {
		t.Fatal(err)
	}

	if string(result) != "hidden data" {
		t.Fatalf(
			"expected hidden data, got %s",
			string(result),
		)
	}

	names, err := entry.NamedStreams()
	if err != nil {
		t.Fatal(err)
	}

	if len(names) != 1 {
		t.Fatalf(
			"expected 1 named stream, got %d",
			len(names),
		)
	}

	err = entry.DeleteStream("secret")
	if err != nil {
		t.Fatal(err)
	}

	names, err = entry.NamedStreams()
	if err != nil {
		t.Fatal(err)
	}

	if len(names) != 0 {
		t.Fatalf(
			"expected 0 named streams after deletion, got %d",
			len(names),
		)
	}
}

// --------------------
// Slack-space tests
// --------------------

func TestSlackSpaceCapability(t *testing.T) {
	entry := &fakeEntry{}

	regions, err := entry.SlackRegions()
	if err != nil {
		t.Fatal(err)
	}

	if len(regions) != 1 {
		t.Fatalf(
			"expected 1 slack region, got %d",
			len(regions),
		)
	}

	if regions[0].Offset != 100 {
		t.Fatalf(
			"expected offset 100, got %d",
			regions[0].Offset,
		)
	}

	if regions[0].Length != 20 {
		t.Fatalf(
			"expected length 20, got %d",
			regions[0].Length,
		)
	}
}

// --------------------
// Timestomp tests
// --------------------

func TestTimestompCapability(t *testing.T) {
	entry := &fakeEntry{
		timestamps: make(map[TimeField]time.Time),
	}

	expected := time.Date(
		2026,
		time.August,
		22,
		10,
		30,
		0,
		0,
		time.UTC,
	)

	err := entry.SetTimestamp(TimeModified, expected)
	if err != nil {
		t.Fatal(err)
	}

	actual, exists := entry.timestamps[TimeModified]
	if !exists {
		t.Fatal("modified timestamp was not stored")
	}

	if !actual.Equal(expected) {
		t.Fatalf(
			"expected timestamp %v, got %v",
			expected,
			actual,
		)
	}
}

// --------------------
// FileSystem tests
// --------------------

func TestFakeFileSystem(t *testing.T) {
	root := &fakeEntry{
		path:  "/",
		isDir: true,
	}

	file := &fakeEntry{
		path:  "/test.txt",
		isDir: false,
	}

	fs := &fakeFileSystem{
		fsType: TypeNTFS,
		root:   root,
		files: map[string]Entry{
			"/test.txt": file,
		},
	}

	if fs.Type() != TypeNTFS {
		t.Fatalf(
			"expected filesystem type %s, got %s",
			TypeNTFS,
			fs.Type(),
		)
	}

	if fs.Root() == nil {
		t.Fatal("expected root entry, got nil")
	}

	if fs.Root().Path() != "/" {
		t.Fatalf(
			"expected root path /, got %s",
			fs.Root().Path(),
		)
	}

	entry, err := fs.Open("/test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatal("expected entry, got nil")
	}

	if entry.Path() != "/test.txt" {
		t.Fatalf(
			"expected /test.txt, got %s",
			entry.Path(),
		)
	}
}

func TestFakeFileSystemOpenMissingEntry(t *testing.T) {
	fs := &fakeFileSystem{
		fsType: TypeEXT4,
		files:  make(map[string]Entry),
	}

	entry, err := fs.Open("/missing.txt")

	if err == nil {
		t.Fatal("expected error for missing entry")
	}

	if entry != nil {
		t.Fatal("expected nil entry for missing path")
	}
}
