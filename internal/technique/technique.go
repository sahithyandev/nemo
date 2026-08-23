// Package technique implements filesystem-independent hiding techniques.
package technique

import (
	"errors"
	"fmt"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

const (
	NamedStream = "named-stream"
	SlackSpace  = "slack-space"
	Timestomp   = "timestomp"
)

// Finding describes hidden data detected in an entry.
type Finding struct {
	Technique string
	Location  string
	Size      int64
}

// Result describes a completed mutating technique operation.
type Result struct {
	Technique string
	Target    string
	Detail    string
	Bytes     int64
}

// HideRequest contains the technique-specific inputs for a hide operation.
type HideRequest struct {
	Data       []byte
	StreamName string
	Field      filesystem.TimeField
	Timestamp  time.Time
	Image      image.Image
}

// Technique executes one kind of hiding operation.
type Technique interface {
	Name() string
	Hide(filesystem.Entry, HideRequest) (Result, error)
}

// Get selects a supported technique by its command-line name.
func Get(name string) (Technique, error) {
	switch name {
	case NamedStream:
		return namedStreamTechnique{}, nil
	case SlackSpace:
		return slackSpaceTechnique{}, nil
	case Timestomp:
		return timestompTechnique{}, nil
	default:
		return nil, fmt.Errorf("unknown technique %q (must be named-stream, slack-space, or timestomp)", name)
	}
}

type namedStreamTechnique struct{}

func (namedStreamTechnique) Name() string { return NamedStream }

func (namedStreamTechnique) Hide(entry filesystem.Entry, request HideRequest) (Result, error) {
	capable, ok := entry.(filesystem.NamedStreamCapable)
	if !ok {
		return Result{}, unsupported(NamedStream)
	}
	if err := capable.WriteStream(request.StreamName, request.Data); err != nil {
		return Result{}, fmt.Errorf("write named stream: %w", err)
	}
	return Result{Technique: NamedStream, Target: entry.Path(), Detail: request.StreamName, Bytes: int64(len(request.Data))}, nil
}

type slackSpaceTechnique struct{}

func (slackSpaceTechnique) Name() string { return SlackSpace }

func (slackSpaceTechnique) Hide(entry filesystem.Entry, request HideRequest) (Result, error) {
	capable, ok := entry.(filesystem.SlackSpaceCapable)
	if !ok {
		return Result{}, unsupported(SlackSpace)
	}
	if request.Image == nil {
		return Result{}, errors.New("slack-space requires image-backed storage")
	}
	regions, err := capable.SlackRegions()
	if err != nil {
		return Result{}, fmt.Errorf("inspect slack regions: %w", err)
	}
	for _, region := range regions {
		if region.Length < int64(len(request.Data)) {
			continue
		}
		n, err := request.Image.WriteAt(request.Data, region.Offset)
		if err != nil {
			return Result{}, fmt.Errorf("write slack space: %w", err)
		}
		if n != len(request.Data) {
			return Result{}, fmt.Errorf("write slack space: short write (%d of %d bytes)", n, len(request.Data))
		}
		return Result{
			Technique: SlackSpace,
			Target:    entry.Path(),
			Detail:    fmt.Sprintf("%d-%d", region.Offset, region.Offset+int64(n)),
			Bytes:     int64(n),
		}, nil
	}
	return Result{}, fmt.Errorf("insufficient slack space for %d-byte payload", len(request.Data))
}

type timestompTechnique struct{}

func (timestompTechnique) Name() string { return Timestomp }

func (timestompTechnique) Hide(entry filesystem.Entry, request HideRequest) (Result, error) {
	capable, ok := entry.(filesystem.TimestompCapable)
	if !ok {
		return Result{}, unsupported(Timestomp)
	}
	if err := capable.SetTimestamp(request.Field, request.Timestamp); err != nil {
		return Result{}, fmt.Errorf("set timestamp: %w", err)
	}
	return Result{
		Technique: Timestomp,
		Target:    entry.Path(),
		Detail:    fmt.Sprintf("%s=%s", request.Field, request.Timestamp.Format(time.RFC3339)),
	}, nil
}

func unsupported(name string) error {
	return fmt.Errorf("technique %q unsupported on this filesystem", name)
}
