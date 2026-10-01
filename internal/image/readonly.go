package image

import "errors"

// ErrReadOnly is returned by a ReadOnly image's WriteAt. Match it with
// errors.Is, not by string.
var ErrReadOnly = errors.New("image opened read-only")

// ReadOnly wraps img so any write attempt fails instead of mutating bytes.
// detect wraps its image in this as a second guard behind OpenReadOnly.
func ReadOnly(img Image) Image {
	return readOnly{img}
}

type readOnly struct{ Image }

func (readOnly) WriteAt([]byte, int64) (int, error) {
	return 0, ErrReadOnly
}
