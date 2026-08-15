package binutil

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// ErrRange is returned when off (and the n bytes starting there) fall
// outside the given slice.
var ErrRange = errors.New("binutil: offset out of range")

// ErrSize is returned when a requested width is not supported.
var ErrSize = errors.New("binutil: invalid size")

// bounds checks that b[off:off+n] is a valid, in-range slice.
func bounds(b []byte, off, n int) error {
	if off < 0 || n < 0 || n > len(b)-off {
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

// Int is Uint plus sign extension from the top bit of the size-byte value.
// NTFS data-run LCN deltas are signed and variable-width.
func Int(b []byte, off, size int, order binary.ByteOrder) (int64, error) {
	v, err := Uint(b, off, size, order)
	if err != nil {
		return 0, err
	}
	shift := 64 - 8*size
	return int64(v<<shift) >> shift, nil
}

// Bits extracts width bits from v starting at bit lo (LSB = 0).
func Bits(v uint64, lo, width int) (uint64, error) {
	if lo < 0 || width <= 0 || lo+width > 64 {
		return 0, ErrRange
	}
	mask := uint64(1)<<width - 1
	return (v >> lo) & mask, nil
}

// String reads n bytes at off and trims trailing NULs and spaces.
func String(b []byte, off, n int) (string, error) {
	if err := bounds(b, off, n); err != nil {
		return "", err
	}
	s := b[off : off+n]
	i := len(s)
	for i > 0 && (s[i-1] == 0 || s[i-1] == ' ') {
		i--
	}
	return string(s[:i]), nil
}

// UTF16String decodes n bytes at off as UTF-16 code units in the given order
// and trims trailing NULs. n must be even. Used for NTFS $FILE_NAME and APFS
// names.
func UTF16String(b []byte, off, n int, order binary.ByteOrder) (string, error) {
	if n%2 != 0 {
		return "", ErrSize
	}
	if err := bounds(b, off, n); err != nil {
		return "", err
	}

	units := make([]uint16, n/2)
	for i := range units {
		units[i] = order.Uint16(b[off+2*i:])
	}
	for len(units) > 0 && units[len(units)-1] == 0 {
		units = units[:len(units)-1]
	}
	return string(utf16.Decode(units)), nil
}
