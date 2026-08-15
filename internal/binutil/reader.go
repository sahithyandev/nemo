package binutil

import "encoding/binary"

// Reader walks a byte slice sequentially. Every method is bounds-checked;
// the first error is sticky, so a parser can chain reads and check Err()
// once instead of checking after every call.
type Reader struct {
	b     []byte
	off   int
	order binary.ByteOrder
	err   error
}

// New creates a Reader over b using order for multi-byte reads.
func New(b []byte, order binary.ByteOrder) *Reader {
	return &Reader{b: b, order: order}
}

// Off returns the current read offset.
func (r *Reader) Off() int {
	return r.off
}

// Remaining returns the number of unread bytes.
func (r *Reader) Remaining() int {
	return len(r.b) - r.off
}

// Err returns the first error encountered, or nil if none.
func (r *Reader) Err() error {
	return r.err
}

// Seek moves the read offset to off. It fails if off is out of range.
func (r *Reader) Seek(off int) {
	if r.err != nil {
		return
	}
	if off < 0 || off > len(r.b) {
		r.err = ErrRange
		return
	}
	r.off = off
}

// Skip advances the read offset by n bytes. It fails if that would move
// past the end of the slice.
func (r *Reader) Skip(n int) {
	if r.err != nil {
		return
	}
	if err := bounds(r.b, r.off, n); err != nil {
		r.err = err
		return
	}
	r.off += n
}

// Uint reads a size-byte unsigned integer and advances the offset by size.
func (r *Reader) Uint(size int) uint64 {
	if r.err != nil {
		return 0
	}
	v, err := Uint(r.b, r.off, size, r.order)
	if err != nil {
		r.err = err
		return 0
	}
	r.off += size
	return v
}

// Int reads a size-byte signed integer and advances the offset by size.
func (r *Reader) Int(size int) int64 {
	if r.err != nil {
		return 0
	}
	v, err := Int(r.b, r.off, size, r.order)
	if err != nil {
		r.err = err
		return 0
	}
	r.off += size
	return v
}
