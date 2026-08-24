package filesystem

import (
	"errors"
	"fmt"
	"io"
	"strings"
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
//
// It panics on an invalid Detector (empty Type, nil Sniff, or nil New) or on
// a duplicate Type — registration happens at package init, so these are
// startup-time programmer errors, not runtime conditions to recover from.
func Register(detector Detector) {
	if detector.Type == "" {
		panic("filesystem: Register: empty Type")
	}
	if detector.Sniff == nil {
		panic(fmt.Sprintf("filesystem: Register: detector %q has nil Sniff", detector.Type))
	}
	if detector.New == nil {
		panic(fmt.Sprintf("filesystem: Register: detector %q has nil New", detector.Type))
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	for _, existing := range detectors {
		if existing.Type == detector.Type {
			panic(fmt.Sprintf("filesystem: detector already registered for type %q", detector.Type))
		}
	}

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

	all := Detectors()
	var matches []Detector
	for _, detector := range all {
		if detector.Sniff(signature) {
			matches = append(matches, detector)
		}
	}

	switch len(matches) {
	case 0:
		if len(all) == 0 {
			return nil, errors.New("filesystem: unrecognized image format (no filesystem detectors registered)")
		}
		return nil, fmt.Errorf("filesystem: unrecognized image format (no match among %d registered filesystems: %s)",
			len(all), typeList(all))
	case 1:
		detector := matches[0]
		fs, err := detector.New(img)
		if err != nil {
			return nil, fmt.Errorf("filesystem: open %s: %w", detector.Type, err)
		}
		return fs, nil
	default:
		return nil, fmt.Errorf("filesystem: ambiguous image format: matches %s", typeList(matches))
	}
}

func typeList(detectors []Detector) string {
	names := make([]string, len(detectors))
	for i, d := range detectors {
		names[i] = string(d.Type)
	}
	return strings.Join(names, ", ")
}
