package binutil

import (
	"encoding/binary"
	"errors"
)

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

// Uint reads a size-byte unsigned integer at b[off:off+size] using order.
// size must be 1..8; NTFS run lists use odd widths like 3 and 6.
func Uint(b []byte, off, size int, order binary.ByteOrder) (uint64, error) {
	if size < 1 || size > 8 {
		return 0, ErrSize
	}
	if err := bounds(b, off, size); err != nil {
		return 0, err
	}

	switch size {
	case 2:
		return uint64(order.Uint16(b[off:])), nil
	case 4:
		return uint64(order.Uint32(b[off:])), nil
	case 8:
		return order.Uint64(b[off:]), nil
	}

	var v uint64
	if order == binary.LittleEndian {
		for i := size - 1; i >= 0; i-- {
			v = v<<8 | uint64(b[off+i])
		}
	} else {
		for i := 0; i < size; i++ {
			v = v<<8 | uint64(b[off+i])
		}
	}
	return v, nil
}
