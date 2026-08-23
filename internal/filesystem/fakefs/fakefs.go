// Package fakefs is a deterministic in-memory implementation of the
// internal/filesystem contracts, for use in command and technique tests.
package fakefs

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

// Entry is an in-memory filesystem.Entry implementing every optional
// capability interface. Fields are exported so tests can configure or
// assert against them directly.
type Entry struct {
	Streams map[string][]byte
	Slack   []filesystem.SlackRegion
	Times   map[filesystem.TimeField]time.Time

	path     string
	dir      bool
	children []*Entry
}

// --------------------
// Entry implementation
// --------------------

func (e *Entry) Path() string {
	return e.path
}

func (e *Entry) IsDir() bool {
	return e.dir
}

func (e *Entry) Children() ([]filesystem.Entry, error) {
	sorted := make([]*Entry, len(e.children))
	copy(sorted, e.children)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })

	out := make([]filesystem.Entry, len(sorted))
	for i, c := range sorted {
		out[i] = c
	}

	return out, nil
}

func (e *Entry) NamedStreams() ([]string, error) {
	names := make([]string, 0, len(e.Streams))
	for name := range e.Streams {
		names = append(names, name)
	}
	sort.Strings(names)

	return names, nil
}

// ---------------------------------
// NamedStreamCapable implementation
// ---------------------------------

func (e *Entry) WriteStream(name string, data []byte) error {
	if e.Streams == nil {
		e.Streams = make(map[string][]byte)
	}

	e.Streams[name] = data
	return nil
}

func (e *Entry) ReadStream(name string) ([]byte, error) {
	data, exists := e.Streams[name]
	if !exists {
		return nil, fs.ErrNotExist
	}

	return data, nil
}

func (e *Entry) DeleteStream(name string) error {
	if _, exists := e.Streams[name]; !exists {
		return fs.ErrNotExist
	}

	delete(e.Streams, name)
	return nil
}

// ---------------------------------
// SlackSpaceCapable implementation
// ---------------------------------

func (e *Entry) SlackRegions() ([]filesystem.SlackRegion, error) {
	return e.Slack, nil
}

// ---------------------------------
// TimestompCapable implementation
// ---------------------------------

func (e *Entry) SetTimestamp(field filesystem.TimeField, t time.Time) error {
	if e.Times == nil {
		e.Times = make(map[filesystem.TimeField]time.Time)
	}

	e.Times[field] = t
	return nil
}

// --------------------
// FS
// --------------------

// FS is an in-memory filesystem.FileSystem over a fixed set of paths.
type FS struct {
	FSType filesystem.Type
	Img    *Image

	entries map[string]*Entry
}

// New builds a tree from the given paths, creating intermediate directories
// implicitly. A path ending in "/" is created as an explicit directory.
// FSType defaults to filesystem.TypeNTFS. Img defaults to a 1KiB in-memory
// image, for entries whose SlackRegions point into it.
func New(paths ...string) *FS {
	f := &FS{
		FSType:  filesystem.TypeNTFS,
		Img:     NewImage(1024),
		entries: map[string]*Entry{"/": {path: "/", dir: true}},
	}

	for _, p := range paths {
		isDir := strings.HasSuffix(p, "/")
		f.ensure(normalize(p), isDir)
	}

	return f
}

// normalize makes p absolute (relative to "/") and cleans it, so every path
// in the tree hangs off the same "/" root regardless of how it was given.
func normalize(p string) string {
	return path.Clean(path.Join("/", p))
}

// ensure creates the entry at p (and its ancestors, as directories) if not
// already present, and returns it.
func (f *FS) ensure(p string, dir bool) *Entry {
	if e, ok := f.entries[p]; ok {
		if dir {
			e.dir = true
		}
		return e
	}

	e := &Entry{path: p, dir: dir}
	f.entries[p] = e

	parentPath := path.Dir(p)
	if parentPath != p {
		parent := f.ensure(parentPath, true)
		parent.children = append(parent.children, e)
	}

	return e
}

// Entry returns the concrete *Entry at path, for test setup and assertions.
// It returns nil if the path is not part of the tree.
func (f *FS) Entry(p string) *Entry {
	return f.entries[normalize(p)]
}

func (f *FS) Type() filesystem.Type {
	return f.FSType
}

func (f *FS) Root() filesystem.Entry {
	return f.entries["/"]
}

func (f *FS) Open(p string) (filesystem.Entry, error) {
	e, exists := f.entries[normalize(p)]
	if !exists {
		return nil, fs.ErrNotExist
	}

	return e, nil
}

// --------------------
// Compile-time checks
// --------------------

var _ filesystem.Entry = (*Entry)(nil)
var _ filesystem.NamedStreamCapable = (*Entry)(nil)
var _ filesystem.SlackSpaceCapable = (*Entry)(nil)
var _ filesystem.TimestompCapable = (*Entry)(nil)
var _ filesystem.FileSystem = (*FS)(nil)
