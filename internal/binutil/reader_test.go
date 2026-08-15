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
