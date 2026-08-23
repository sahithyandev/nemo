package filesystem

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/sahithyandev/nemo/internal/image"
)

// Detector describes how to identify and construct one filesystem type.
type Detector struct {
	Type       Type
	Sniff      func([]byte) bool
	New        func(image.Image) (FileSystem, error)
	Techniques []string
}

var (
	registryMu sync.RWMutex
	detectors  []Detector
)

// Register adds a filesystem detector to the process-wide registry.
func Register(detector Detector) {
	registryMu.Lock()
	defer registryMu.Unlock()
	detectors = append(detectors, detector)
}

// Detectors returns a snapshot of the registered detectors.
func Detectors() []Detector {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return append([]Detector(nil), detectors...)
}

// Open detects and constructs the filesystem contained in img.
func Open(img image.Image) (FileSystem, error) {
	if img == nil {
		return nil, errors.New("filesystem: nil image")
	}

	signatureSize := img.Size()
	if signatureSize > 4096 {
		signatureSize = 4096
	}
	if signatureSize < 0 {
		return nil, errors.New("filesystem: invalid image size")
	}

	signature := make([]byte, signatureSize)
	if len(signature) > 0 {
		n, err := img.ReadAt(signature, 0)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("filesystem: read signature: %w", err)
		}
		signature = signature[:n]
	}

	for _, detector := range Detectors() {
		if detector.Sniff != nil && detector.Sniff(signature) {
			if detector.New == nil {
				return nil, fmt.Errorf("filesystem: detector %q has no constructor", detector.Type)
			}
			fs, err := detector.New(img)
			if err != nil {
				return nil, fmt.Errorf("filesystem: open %s: %w", detector.Type, err)
			}
			return fs, nil
		}
	}

	return nil, errors.New("filesystem: unrecognized image format")
}
