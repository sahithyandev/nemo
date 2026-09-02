// Package technique implements filesystem-independent hiding techniques.
package technique

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

const (
	NamedStream = "named-stream"
	SlackSpace  = "slack-space"
	Timestomp   = "timestomp"
)

// ErrUnsupported is returned when a technique is run against an Entry whose
// filesystem does not implement the required capability. It is a stable
// sentinel: match it with errors.Is, not by string.
var ErrUnsupported = errors.New("unsupported on this filesystem")

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
	// Bytes is the payload size the operation dealt with, not the number
	// of bytes touched on disk (a slack write also lays down a 12-byte
	// frame header).
	Bytes int64
	// Restored is set by slack-space Clear: true when the caller supplied
	// the original residual bytes and they were written back, false when
	// the frame was zero-filled instead.
	Restored bool
}

// Backup is the record a caller must persist to make a mutating operation
// reversible. slack-space Hide/Clear emit one before overwriting bytes;
// timestomp would emit one too once filesystem.TimestompCapable can read a
// timestamp back (it cannot today). The on-disk manifest format is the
// command layer's decision; this package only hands the record to
// Request.Backup.
type Backup struct {
	Technique string
	Target    string
	Location  string    // stream name or slack offset range
	Original  []byte    // bytes overwritten in place; nil for timestomp
	Timestamp time.Time // timestamp overwritten; zero unless known
}

// Request carries the technique-specific inputs for a Hide, Detect or Clear
// operation. Only the fields a given operation needs are read.
type Request struct {
	Data       []byte // hide payload
	StreamName string
	Field      filesystem.TimeField
	Timestamp  time.Time
	Image      image.Image
	// Restore is the original residual bytes a slack-space Clear writes
	// back over the frame (from a manifest Backup.Original). Nil means
	// "zero-fill the frame instead". A non-nil slice whose length does not
	// match the frame is rejected rather than silently zero-filled.
	Restore []byte
	// Backup, if set, is called with the pre-write state before any
	// destructive write. Returning an error aborts the operation. A nil
	// Backup means the caller has opted out of reversibility.
	Backup func(Backup) error
}

// Technique executes one kind of hiding operation. Each method asserts only
// the capability interface it needs off the Entry and returns ErrUnsupported
// otherwise.
type Technique interface {
	Name() string
	Hide(filesystem.Entry, Request) (Result, error)
	Detect(filesystem.Entry, Request) ([]Finding, error)
	Clear(filesystem.Entry, Request) (Result, error)
}

// Get selects a supported technique by its command-line name. It accepts
// exactly named-stream, slack-space and timestomp.
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

func unsupported(name string) error {
	return fmt.Errorf("technique %q: %w", name, ErrUnsupported)
}

// -------------------------------------------------------------------------
// named-stream
// -------------------------------------------------------------------------

type namedStreamTechnique struct{}

func (namedStreamTechnique) Name() string { return NamedStream }

func (namedStreamTechnique) Hide(entry filesystem.Entry, request Request) (Result, error) {
	capable, ok := entry.(filesystem.NamedStreamCapable)
	if !ok {
		return Result{}, unsupported(NamedStream)
	}
	if request.StreamName == "" {
		return Result{}, errors.New("named-stream hide requires a stream name")
	}
	if err := capable.WriteStream(request.StreamName, request.Data); err != nil {
		return Result{}, fmt.Errorf("write named stream: %w", err)
	}
	return Result{Technique: NamedStream, Target: entry.Path(), Detail: request.StreamName, Bytes: int64(len(request.Data))}, nil
}

func (namedStreamTechnique) Detect(entry filesystem.Entry, _ Request) ([]Finding, error) {
	capable, ok := entry.(filesystem.NamedStreamCapable)
	if !ok {
		return nil, unsupported(NamedStream)
	}
	names, err := entry.NamedStreams()
	if err != nil {
		return nil, fmt.Errorf("list named streams: %w", err)
	}
	var findings []Finding
	for _, name := range names {
		data, err := capable.ReadStream(name)
		if err != nil {
			return nil, fmt.Errorf("read named stream %q: %w", name, err)
		}
		findings = append(findings, Finding{Technique: NamedStream, Location: name, Size: int64(len(data))})
	}
	return findings, nil
}

func (namedStreamTechnique) Clear(entry filesystem.Entry, request Request) (Result, error) {
	capable, ok := entry.(filesystem.NamedStreamCapable)
	if !ok {
		return Result{}, unsupported(NamedStream)
	}
	if request.StreamName == "" {
		return Result{}, errors.New("named-stream clear requires a stream name")
	}
	if err := capable.DeleteStream(request.StreamName); err != nil {
		return Result{}, fmt.Errorf("delete named stream: %w", err)
	}
	return Result{Technique: NamedStream, Target: entry.Path(), Detail: request.StreamName}, nil
}

// -------------------------------------------------------------------------
// slack-space
// -------------------------------------------------------------------------

type slackSpaceTechnique struct{}

func (slackSpaceTechnique) Name() string { return SlackSpace }

func (slackSpaceTechnique) Hide(entry filesystem.Entry, request Request) (Result, error) {
	capable, ok := entry.(filesystem.SlackSpaceCapable)
	if !ok {
		return Result{}, unsupported(SlackSpace)
	}
	if request.Image == nil {
		return Result{}, errors.New("slack-space requires image-backed storage")
	}
	if int64(len(request.Data)) > math.MaxUint32 {
		return Result{}, fmt.Errorf("payload of %d bytes exceeds the slack frame's 4 GiB limit", len(request.Data))
	}
	regions, err := capable.SlackRegions()
	if err != nil {
		return Result{}, fmt.Errorf("inspect slack regions: %w", err)
	}
	frame := encodeFrame(request.Data)
	for _, region := range regions {
		if region.Length < int64(len(frame)) {
			continue
		}
		original, err := readRegion(request.Image, int64(len(frame)), region.Offset)
		if err != nil {
			return Result{}, err
		}
		if len(original) < len(frame) {
			return Result{}, fmt.Errorf("slack region at %d runs past the image end", region.Offset)
		}
		detail := fmt.Sprintf("%d-%d", region.Offset, region.Offset+int64(len(frame)))
		if request.Backup != nil {
			if err := request.Backup(Backup{
				Technique: SlackSpace,
				Target:    entry.Path(),
				Location:  detail,
				Original:  original,
			}); err != nil {
				return Result{}, fmt.Errorf("record backup: %w", err)
			}
		}
		if err := writeAll(request.Image, frame, region.Offset); err != nil {
			return Result{}, err
		}
		return Result{
			Technique: SlackSpace,
			Target:    entry.Path(),
			Detail:    detail,
			Bytes:     int64(len(request.Data)),
		}, nil
	}
	return Result{}, fmt.Errorf("insufficient slack space for %d-byte payload", len(request.Data))
}

func (slackSpaceTechnique) Detect(entry filesystem.Entry, request Request) ([]Finding, error) {
	capable, ok := entry.(filesystem.SlackSpaceCapable)
	if !ok {
		return nil, unsupported(SlackSpace)
	}
	if request.Image == nil {
		return nil, errors.New("slack-space requires image-backed storage")
	}
	regions, err := capable.SlackRegions()
	if err != nil {
		return nil, fmt.Errorf("inspect slack regions: %w", err)
	}
	var findings []Finding
	for _, region := range regions {
		_, payload, ok, err := readFrame(request.Image, region)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		end := region.Offset + int64(frameHeaderSize) + int64(len(payload))
		findings = append(findings, Finding{
			Technique: SlackSpace,
			Location:  fmt.Sprintf("%d-%d", region.Offset, end),
			Size:      int64(len(payload)),
		})
	}
	return findings, nil
}

func (slackSpaceTechnique) Clear(entry filesystem.Entry, request Request) (Result, error) {
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
	// Hide only ever writes one frame, so clearing the first framed region
	// and returning is enough. Detect reports every framed region it finds;
	// if that ever exceeds one, this needs to loop.
	for _, region := range regions {
		frame, payload, ok, err := readFrame(request.Image, region)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			continue
		}
		frameLen := frameHeaderSize + len(payload)
		detail := fmt.Sprintf("%d-%d", region.Offset, region.Offset+int64(frameLen))

		// Restore the original residual bytes if the caller kept them
		// (via a manifest); otherwise zero the frame out. A supplied slice
		// of the wrong length is a mistake, not a reason to zero-fill.
		restore := request.Restore
		restored := restore != nil
		if restored && len(restore) != frameLen {
			return Result{}, fmt.Errorf("restore data is %d bytes, need %d for the frame at %s", len(restore), frameLen, detail)
		}
		if !restored {
			restore = make([]byte, frameLen)
		}
		if request.Backup != nil {
			if err := request.Backup(Backup{
				Technique: SlackSpace,
				Target:    entry.Path(),
				Location:  detail,
				Original:  append([]byte(nil), frame...),
			}); err != nil {
				return Result{}, fmt.Errorf("record backup: %w", err)
			}
		}
		if err := writeAll(request.Image, restore, region.Offset); err != nil {
			return Result{}, err
		}
		return Result{Technique: SlackSpace, Target: entry.Path(), Detail: detail, Bytes: int64(len(payload)), Restored: restored}, nil
	}
	return Result{}, errors.New("no framed slack payload found to clear")
}

// -------------------------------------------------------------------------
// timestomp
// -------------------------------------------------------------------------

type timestompTechnique struct{}

func (timestompTechnique) Name() string { return Timestomp }

func (timestompTechnique) Hide(entry filesystem.Entry, request Request) (Result, error) {
	capable, ok := entry.(filesystem.TimestompCapable)
	if !ok {
		return Result{}, unsupported(Timestomp)
	}
	if !request.Field.Valid() {
		return Result{}, fmt.Errorf("timestomp hide: invalid time field %q (want created, modified, or accessed)", request.Field)
	}
	if request.Timestamp.IsZero() {
		return Result{}, errors.New("timestomp hide requires a non-zero timestamp")
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

// Detect always reports nothing for timestomp: filesystem.TimestompCapable
// exposes only SetTimestamp, so there is no way to read a field back and
// judge whether it was altered. Restoring a stomped timestamp likewise needs
// the caller to supply the original value.
func (timestompTechnique) Detect(entry filesystem.Entry, _ Request) ([]Finding, error) {
	if _, ok := entry.(filesystem.TimestompCapable); !ok {
		return nil, unsupported(Timestomp)
	}
	return nil, nil
}

func (timestompTechnique) Clear(entry filesystem.Entry, request Request) (Result, error) {
	capable, ok := entry.(filesystem.TimestompCapable)
	if !ok {
		return Result{}, unsupported(Timestomp)
	}
	if !request.Field.Valid() {
		return Result{}, fmt.Errorf("timestomp clear: invalid time field %q (want created, modified, or accessed)", request.Field)
	}
	if request.Timestamp.IsZero() {
		return Result{}, errors.New("timestomp clear requires the original timestamp")
	}
	if err := capable.SetTimestamp(request.Field, request.Timestamp); err != nil {
		return Result{}, fmt.Errorf("restore timestamp: %w", err)
	}
	return Result{
		Technique: Timestomp,
		Target:    entry.Path(),
		Detail:    fmt.Sprintf("%s=%s", request.Field, request.Timestamp.Format(time.RFC3339)),
	}, nil
}

// readRegion reads up to n bytes at off, tolerating a short read at the end
// of the image (io.EOF). It returns the bytes actually read.
func readRegion(img image.Image, n int64, off int64) ([]byte, error) {
	if n < 0 || n > math.MaxInt {
		return nil, fmt.Errorf("read slack space: region length %d out of range", n)
	}
	buf := make([]byte, int(n))
	read, err := img.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read slack space: %w", err)
	}
	return buf[:read], nil
}

// readFrame reads a slack frame at the start of region, fetching only the
// 12-byte header first and then exactly frameHeaderSize+payloadLen bytes when
// the magic matches. This avoids pulling a whole large region into memory just
// to check for a frame. frame is the raw on-disk frame bytes; ok is false for
// an unframed or malformed region.
func readFrame(img image.Image, region filesystem.SlackRegion) (frame, payload []byte, ok bool, err error) {
	if region.Length < frameHeaderSize {
		return nil, nil, false, nil
	}
	head, err := readRegion(img, frameHeaderSize, region.Offset)
	if err != nil {
		return nil, nil, false, err
	}
	if len(head) < frameHeaderSize || string(head[:4]) != frameMagic {
		return nil, nil, false, nil
	}
	length := int64(binary.LittleEndian.Uint32(head[4:8]))
	if frameHeaderSize+length > region.Length {
		return nil, nil, false, nil
	}
	frame, err = readRegion(img, frameHeaderSize+length, region.Offset)
	if err != nil {
		return nil, nil, false, err
	}
	payload, ok = decodeFrame(frame)
	if !ok {
		return nil, nil, false, nil
	}
	return frame, payload, true, nil
}

func writeAll(img image.Image, p []byte, off int64) error {
	n, err := img.WriteAt(p, off)
	if err != nil {
		return fmt.Errorf("write slack space: %w", err)
	}
	if n != len(p) {
		return fmt.Errorf("write slack space: short write (%d of %d bytes)", n, len(p))
	}
	return nil
}
