// Package ntfs implements read-only traversal of NTFS filesystem images.
package ntfs

import (
	"errors"

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
	img image.Image
}

var _ filesystem.FileSystem = (*FS)(nil)

func (f *FS) Type() filesystem.Type {
	return filesystem.TypeNTFS
}

func (f *FS) Root() filesystem.Entry {
	return nil
}

func (f *FS) Open(path string) (filesystem.Entry, error) {
	return nil, errors.New("ntfs: open not implemented yet")
}

// New creates an NTFS filesystem instance.
func New(img image.Image) (filesystem.FileSystem, error) {
	if img == nil {
		return nil, errors.New("ntfs: nil image")
	}

	return &FS{
		img: img,
	}, nil
}
