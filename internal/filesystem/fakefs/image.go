package fakefs

import (
	"errors"
	"io"

	"github.com/sahithyandev/nemo/internal/image"
)

// Image is a []byte-backed image.Image, so writes into a fake entry's slack
// regions are observable in Data.
type Image struct {
	Data []byte
	Name string
}

// NewImage allocates a zeroed image of the given size.
func NewImage(size int) *Image {
	return &Image{Data: make([]byte, size), Name: "fake.img"}
}

func (i *Image) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off > int64(len(i.Data)) {
		return 0, errors.New("fakefs: offset out of range")
	}

	n := copy(p, i.Data[off:])
	if n < len(p) {
		return n, io.EOF
	}

	return n, nil
}

func (i *Image) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(i.Data)) {
		return 0, errors.New("fakefs: write out of range")
	}

	return copy(i.Data[off:], p), nil
}

func (i *Image) Size() int64 {
	return int64(len(i.Data))
}

func (i *Image) Path() string {
	return i.Name
}

var _ image.Image = (*Image)(nil)
