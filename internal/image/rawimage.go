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

// Open = Open an existing file as a RawImage.
func Open(path string) (*RawImage, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	// O_RDWR = read + write.
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

