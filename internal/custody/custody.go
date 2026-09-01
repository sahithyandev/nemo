package custody

import (
	"crypto/sha256"
	"encoding/hex"
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
