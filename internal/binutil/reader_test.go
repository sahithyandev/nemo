package binutil

import (
	"encoding/binary"
	"testing"
)

func TestReaderCore(t *testing.T) {
	b := []byte{0x01, 0x02, 0x03, 0x04}
	r := New(b, binary.LittleEndian)

	if r.Off() != 0 {
		t.Fatalf("Off() = %d, want 0", r.Off())
	}
	if r.Remaining() != 4 {
		t.Fatalf("Remaining() = %d, want 4", r.Remaining())
	}

	r.Skip(2)
	if r.Err() != nil {
		t.Fatalf("unexpected err after Skip: %v", r.Err())
	}
	if r.Off() != 2 {
		t.Fatalf("Off() = %d, want 2", r.Off())
	}
	if r.Remaining() != 2 {
		t.Fatalf("Remaining() = %d, want 2", r.Remaining())
	}

	r.Seek(0)
	if r.Off() != 0 {
		t.Fatalf("Off() after Seek = %d, want 0", r.Off())
	}
}

func TestReaderStickyError(t *testing.T) {
	b := []byte{0x01, 0x02}
	r := New(b, binary.LittleEndian)

	r.Skip(5)
	if r.Err() == nil {
		t.Fatal("expected error after Skip past end")
	}
	firstErr := r.Err()

	r.Skip(1)
	if r.Err() != firstErr {
		t.Fatalf("err changed after subsequent Skip: %v", r.Err())
	}
	if r.Off() != 0 {
		t.Fatalf("Off() moved after error: %d", r.Off())
	}

	r.Seek(0)
	if r.Err() != firstErr {
		t.Fatalf("Seek should be a no-op once errored, err = %v", r.Err())
	}
}

func TestReaderUintInt(t *testing.T) {
	b := []byte{0x01, 0x02, 0xff, 0xff}
	r := New(b, binary.LittleEndian)

	if got := r.Uint(2); got != 0x0201 {
		t.Fatalf("Uint(2) = 0x%x, want 0x0201", got)
	}
	if r.Off() != 2 {
		t.Fatalf("Off() = %d, want 2", r.Off())
	}
	if got := r.Int(2); got != -1 {
		t.Fatalf("Int(2) = %d, want -1", got)
	}
	if r.Off() != 4 {
		t.Fatalf("Off() = %d, want 4", r.Off())
	}
	if r.Err() != nil {
		t.Fatalf("unexpected err: %v", r.Err())
	}
}

func TestReaderUintPastEnd(t *testing.T) {
	b := []byte{0x01}
	r := New(b, binary.LittleEndian)

	if got := r.Uint(4); got != 0 {
		t.Fatalf("Uint(4) = %d, want 0", got)
	}
	if r.Err() == nil {
		t.Fatal("expected error reading past end")
	}
	if got := r.Int(1); got != 0 {
		t.Fatalf("Int(1) after error = %d, want 0 (no-op)", got)
	}
}

func TestReaderSequentialParse(t *testing.T) {
	// A hand-written fixture: 2-byte magic, 4-byte LE length, 4-byte
	// NUL-padded ASCII name, 4-byte LE UTF-16 name, 3 trailing bytes.
	b := []byte{
		'N', 'M', // magic
		0x05, 0x00, 0x00, 0x00, // length = 5
		'a', 'b', 0, 0, // name, NUL padded
		'h', 0, 'i', 0, // UTF-16 LE "hi"
		0xaa, 0xbb, 0xcc, // trailer
	}
	r := New(b, binary.LittleEndian)

	magic := r.Bytes(2)
	length := r.Uint(4)
	name := r.String(4)
	name16 := r.UTF16String(4)
	trailer := r.Bytes(3)

	if r.Err() != nil {
		t.Fatalf("unexpected err: %v", r.Err())
	}
	if string(magic) != "NM" {
		t.Fatalf("magic = %q, want NM", magic)
	}
	if length != 5 {
		t.Fatalf("length = %d, want 5", length)
	}
	if name != "ab" {
		t.Fatalf("name = %q, want ab", name)
	}
	if name16 != "hi" {
		t.Fatalf("name16 = %q, want hi", name16)
	}
	if len(trailer) != 3 || trailer[0] != 0xaa {
		t.Fatalf("trailer = %x, want aabbcc", trailer)
	}
	if r.Off() != len(b) {
		t.Fatalf("Off() = %d, want %d", r.Off(), len(b))
	}
	if r.Remaining() != 0 {
		t.Fatalf("Remaining() = %d, want 0", r.Remaining())
	}
}
