package image

import (
	"os"
)

// RawImage = An os.File,backend implementation of image.
type RawImage struct {
	file *os.File
	path string
	size int64
}

// Make sure RawImage implements Image.
var _ Image = (*RawImage)(nil)

// Open = Open an existing file as a read+write RawImage.
func Open(path string) (*RawImage, error) {
	return open(path, os.O_RDWR)
}

// OpenReadOnly opens an existing file for reading only. detect uses this so it
// never requests write access and can run against read-only forensic media.
func OpenReadOnly(path string) (*RawImage, error) {
	return open(path, os.O_RDONLY)
}

func open(path string, flag int) (*RawImage, error) {
	file, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	return &RawImage{
		file: file,
		path: path,
		size: info.Size(),
	}, nil

}

// Reads bytes starting from the given offset.
func (r *RawImage) ReadAt(p []byte, off int64) (int, error) {
	return r.file.ReadAt(p, off)
}

// Writes bytes starting from the given offset.
func (r *RawImage) WriteAt(p []byte, off int64) (int, error) {
	return r.file.WriteAt(p, off)
}

// Returns the size of the image in bytes.
func (r *RawImage) Size() int64 {
	return r.size
}

// Returns the path used to open the image.
func (r *RawImage) Path() string {
	return r.path
}

func (r *RawImage) Close() error {
	return r.file.Close()
}
