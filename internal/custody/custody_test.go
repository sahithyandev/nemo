package custody

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

func TestWrappedImageCapturesWrite(t *testing.T) {
	img := fakefs.NewImage(32)

	wrapped := Wrap(img)

	data := []byte("ABC")
	before := time.Now().UTC()

	n, err := wrapped.WriteAt(data, 5)
	if err != nil {
		t.Fatal(err)
	}

	after := time.Now().UTC()

	// Underlying image must still receive the write.
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}

	if got := string(img.Data[5:8]); got != "ABC" {
		t.Fatalf("expected underlying image to contain ABC, got %q", got)
	}

	// Exactly one custody event must be captured.
	events := wrapped.EventsSnapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 write event, got %d", len(events))
	}

	event := events[0]

	if event.Offset != 5 {
		t.Fatalf("expected offset 5, got %d", event.Offset)
	}

	sum := sha256.Sum256(data)
	expectedHash := hex.EncodeToString(sum[:])

	if event.SHA256 != expectedHash {
		t.Fatalf("expected hash %q, got %q", expectedHash, event.SHA256)
	}

	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Fatalf("timestamp %v is outside expected range", event.Timestamp)
	}

	if event.Timestamp.Location() != time.UTC {
		t.Fatalf("expected UTC timestamp, got %v", event.Timestamp.Location())
	}
}

// TestRecorderCloseNoopsOnNonCloseableImage confirms Close is safe to call on
// a Recorder wrapping an image (like fakefs.Image) that has no Close method
// of its own.
func TestRecorderCloseNoopsOnNonCloseableImage(t *testing.T) {
	wrapped := Wrap(fakefs.NewImage(32))
	if err := wrapped.Close(); err != nil {
		t.Fatalf("Close() on a non-closeable underlying image = %v, want nil", err)
	}
}

type closeTrackingImage struct {
	*fakefs.Image
	closeErr error
	closed   bool
}

func (c *closeTrackingImage) Close() error {
	c.closed = true
	return c.closeErr
}

// TestRecorderCloseDelegates confirms Close reaches the underlying image
// when it is closeable, forwarding its error.
func TestRecorderCloseDelegates(t *testing.T) {
	want := errors.New("boom")
	underlying := &closeTrackingImage{Image: fakefs.NewImage(32), closeErr: want}
	wrapped := Wrap(underlying)

	if err := wrapped.Close(); err != want {
		t.Fatalf("Close() = %v, want %v", err, want)
	}
	if !underlying.closed {
		t.Fatalf("underlying image was not closed")
	}
}
