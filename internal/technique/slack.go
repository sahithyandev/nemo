package technique

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

const (
	slackFrameVersion    = 1
	slackFrameHeaderSize = 48
)

var slackFrameMagic = [8]byte{'N', 'E', 'M', 'O', 'S', 'L', 'K', '1'}

func encodeSlackFrame(payload []byte) ([]byte, error) {
	if uint64(len(payload)) > uint64(^uint32(0)) {
		return nil, errors.New("slack-space payload exceeds frame length limit")
	}
	frame := make([]byte, slackFrameHeaderSize+len(payload))
	copy(frame[:8], slackFrameMagic[:])
	frame[8] = slackFrameVersion
	binary.LittleEndian.PutUint32(frame[12:16], uint32(len(payload)))
	sum := sha256.Sum256(payload)
	copy(frame[16:48], sum[:])
	copy(frame[slackFrameHeaderSize:], payload)
	return frame, nil
}

func writeSlackFrame(entry filesystem.Entry, img image.Image, payload []byte) (Result, error) {
	capable, ok := entry.(filesystem.SlackSpaceCapable)
	if !ok {
		return Result{}, unsupported(SlackSpace)
	}
	if img == nil {
		return Result{}, errors.New("slack-space requires image-backed storage")
	}
	frame, err := encodeSlackFrame(payload)
	if err != nil {
		return Result{}, err
	}
	regions, err := capable.SlackRegions()
	if err != nil {
		return Result{}, fmt.Errorf("inspect slack regions: %w", err)
	}
	for _, region := range regions {
		if region.Offset < 0 || region.Length < int64(len(frame)) || region.Offset > img.Size()-region.Length {
			continue
		}
		n, err := img.WriteAt(frame, region.Offset)
		if err != nil {
			return Result{}, fmt.Errorf("write slack frame: %w", err)
		}
		if n != len(frame) {
			return Result{}, fmt.Errorf("write slack frame: short write (%d of %d bytes)", n, len(frame))
		}
		return Result{
			Technique: SlackSpace,
			Target:    entry.Path(),
			Detail:    fmt.Sprintf("%d-%d", region.Offset, region.Offset+int64(n)),
			Bytes:     int64(len(payload)),
		}, nil
	}
	return Result{}, fmt.Errorf("insufficient slack space for %d-byte payload plus %d-byte frame header", len(payload), slackFrameHeaderSize)
}
