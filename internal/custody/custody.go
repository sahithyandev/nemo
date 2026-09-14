package custody

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	"github.com/sahithyandev/nemo/internal/image"
)

type WriteEvent struct {
	Offset    int64
	SHA256    string
	Timestamp time.Time
}

type Recorder interface {
	image.Image
	EventsSnapshot() []WriteEvent
	// Close releases the wrapped image, closing it if it is closeable.
	// Callers should close through the recorder rather than reaching past it
	// to the image they wrapped, so a future addition here (flushing custody
	// state on close, say) is not silently bypassed by code written before
	// it existed.
	Close() error
}

type wrappedImage struct {
	underlying image.Image
	events     []WriteEvent
}

// Wrap adds custody handling around an Image.
func Wrap(img image.Image) Recorder {
	return &wrappedImage{
		underlying: img,
	}
}

// Close closes the underlying image if it implements io.Closer, and is a
// no-op otherwise (image.Image itself carries no Close method).
func (w *wrappedImage) Close() error {
	if c, ok := w.underlying.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (w *wrappedImage) ReadAt(p []byte, off int64) (int, error) {
	return w.underlying.ReadAt(p, off)
}

func (w *wrappedImage) WriteAt(p []byte, off int64) (int, error) {
	n, err := w.underlying.WriteAt(p, off)
	if err != nil {
		return n, err
	}

	sum := sha256.Sum256(p[:n])

	w.events = append(w.events, WriteEvent{
		Offset:    off,
		SHA256:    hex.EncodeToString(sum[:]),
		Timestamp: time.Now().UTC(),
	})

	return n, nil
}

func (w *wrappedImage) Size() int64 {
	return w.underlying.Size()
}

func (w *wrappedImage) EventsSnapshot() []WriteEvent {
	events := make([]WriteEvent, len(w.events))
	copy(events, w.events)
	return events
}

func (w *wrappedImage) Path() string {
	return w.underlying.Path()
}
