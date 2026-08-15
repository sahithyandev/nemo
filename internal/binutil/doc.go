// Package binutil provides bounds-safe helpers for reading integers,
// bitfields, and fixed-width strings out of raw byte slices.
//
// The stdlib encoding/binary package already handles endian conversion, but
// it panics when the slice is too short. Since nemo parses disk images that
// may be truncated or corrupted, every helper here returns an error instead
// of panicking on out-of-range input.
package binutil
