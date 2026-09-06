// Package ntfs implements read-only traversal of NTFS filesystem images.
package ntfs

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

const ntfsMagic = "NTFS    "

func init() {
	filesystem.Register(filesystem.Detector{
		Type:  filesystem.TypeNTFS,
		Sniff: Sniff,
		New:   New,
	})
}

// Sniff checks whether the image contains an NTFS boot sector signature.
func Sniff(signature []byte) bool {
	if len(signature) < 11 {
		return false
	}

	return string(signature[3:11]) == ntfsMagic
}

// FS represents an NTFS filesystem image.
type FS struct {
	img               image.Image
	bytesPerSector    uint16
	sectorsPerCluster uint8
	mftCluster        uint64
	mftMirrorCluster  uint64
	fileRecordSize    uint32
	indexRecordSize   uint32
	mftRuns           []dataRun
	mftDataSize       uint64
	mftMu             sync.Mutex
}

var _ filesystem.FileSystem = (*FS)(nil)

func (f *FS) Type() filesystem.Type {
	return filesystem.TypeNTFS
}

func (f *FS) Root() filesystem.Entry {
	return &Entry{fs: f, recordNumber: 5, path: "/", isDir: true}
}

func (f *FS) Open(name string) (filesystem.Entry, error) {
	clean := path.Clean("/" + strings.ReplaceAll(name, "\\", "/"))
	if clean == "/" {
		return f.Root(), nil
	}
	cur := f.Root().(*Entry)
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for i, part := range parts {
		if !cur.isDir {
			return nil, fmt.Errorf("ntfs: %q is not a directory", cur.path)
		}
		children, err := cur.Children()
		if err != nil {
			return nil, err
		}
		var next *Entry
		for _, child := range children {
			candidate := child.(*Entry)
			if strings.EqualFold(path.Base(candidate.path), part) {
				next = candidate
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("ntfs: %q not found", "/"+strings.Join(parts[:i+1], "/"))
		}
		cur = next
	}
	return cur, nil
}

// Entry is an MFT record reached through an NTFS directory index.
type Entry struct {
	fs           *FS
	recordNumber uint64
	path         string
	isDir        bool
}

var _ filesystem.Entry = (*Entry)(nil)

func (e *Entry) Path() string                    { return e.path }
func (e *Entry) IsDir() bool                     { return e.isDir }
func (e *Entry) NamedStreams() ([]string, error) { return nil, nil }
func (e *Entry) Children() ([]filesystem.Entry, error) {
	if !e.isDir {
		return nil, nil
	}
	record, err := e.fs.loadMFTRecord(e.recordNumber)
	if err != nil {
		return nil, fmt.Errorf("ntfs: read directory %q: %w", e.path, err)
	}
	items, err := e.fs.directoryEntries(record)
	if err != nil {
		return nil, fmt.Errorf("ntfs: read directory %q: %w", e.path, err)
	}
	out := make([]filesystem.Entry, 0, len(items))
	seen := make(map[uint64]bool)
	for _, item := range items {
		n := item.reference & 0x0000ffffffffffff
		if n == e.recordNumber || seen[n] || item.name == "." || item.name == ".." {
			continue
		}
		seen[n] = true
		p := e.path + item.name
		if e.path != "/" {
			p = e.path + "/" + item.name
		}
		out = append(out, &Entry{fs: e.fs, recordNumber: n, path: p, isDir: item.fileFlags&fileAttributeDirectory != 0})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path()) < strings.ToLower(out[j].Path()) })
	return out, nil
}

// New creates an NTFS filesystem instance.
func New(img image.Image) (filesystem.FileSystem, error) {
	if img == nil {
		return nil, errors.New("ntfs: nil image")
	}

	boot, err := readBootSector(img)
	if err != nil {
		return nil, err
	}

	return &FS{
		img:               img,
		bytesPerSector:    boot.bytesPerSector,
		sectorsPerCluster: boot.sectorsPerCluster,
		mftCluster:        boot.mftCluster,
		mftMirrorCluster:  boot.mftMirrorCluster,
		fileRecordSize:    boot.fileRecordSize,
		indexRecordSize:   boot.indexRecordSize,
	}, nil
}
