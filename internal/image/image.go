package image

// Image = A random-access filesystem image or backing storage.
type Image interface {
	ReadAt(p []byte, off int64) (int, error)
	WriteAt(p []byte, off int64) (int, error)
	Size() int64
	Path() string
}
