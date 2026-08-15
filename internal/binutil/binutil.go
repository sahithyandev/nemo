package binutil

import "errors"

// ErrRange is returned when off (and the n bytes starting there) fall
// outside the given slice.
var ErrRange = errors.New("binutil: offset out of range")

// ErrSize is returned when a requested width is not supported.
var ErrSize = errors.New("binutil: invalid size")

// bounds checks that b[off:off+n] is a valid, in-range slice.
func bounds(b []byte, off, n int) error {
	if off < 0 || n < 0 || off+n > len(b) {
		return ErrRange
	}
	return nil
}
