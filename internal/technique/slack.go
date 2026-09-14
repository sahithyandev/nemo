package technique

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

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
		if !validSlackRegion(img, region) || region.Length < int64(len(frame)) {
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

// DetectSlackSpace returns validated Nemo frames without modifying the image.
func DetectSlackSpace(entry filesystem.Entry, img image.Image) ([]Finding, error) {
	capable, regions, err := slackRegions(entry, img)
	if err != nil {
		return nil, err
	}
	_ = capable
	findings := make([]Finding, 0, len(regions))
	for _, region := range regions {
		payload, frameLength, found, err := readSlackFrame(img, region)
		if err != nil {
			return nil, fmt.Errorf("detect slack frame at %d: %w", region.Offset, err)
		}
		if found {
			findings = append(findings, Finding{
				Technique: SlackSpace,
				Location:  fmt.Sprintf("%d-%d", region.Offset, region.Offset+frameLength),
				Size:      int64(len(payload)),
			})
		}
	}
	return findings, nil
}

// ClearSlackSpace zeroes only validated Nemo frames. Bytes outside each frame
// remain unchanged, including the rest of the containing slack region.
func ClearSlackSpace(entry filesystem.Entry, img image.Image) (Result, error) {
	_, regions, err := slackRegions(entry, img)
	if err != nil {
		return Result{}, err
	}
	var details []string
	var payloadBytes int64
	for _, region := range regions {
		payload, frameLength, found, err := readSlackFrame(img, region)
		if err != nil {
			return Result{}, fmt.Errorf("clear slack frame at %d: %w", region.Offset, err)
		}
		if !found {
			continue
		}
		cleared := make([]byte, frameLength)
		n, err := img.WriteAt(cleared, region.Offset)
		if err != nil {
			return Result{}, fmt.Errorf("clear slack frame: %w", err)
		}
		if int64(n) != frameLength {
			return Result{}, fmt.Errorf("clear slack frame: short write (%d of %d bytes)", n, frameLength)
		}
		details = append(details, fmt.Sprintf("%d-%d", region.Offset, region.Offset+frameLength))
		payloadBytes += int64(len(payload))
	}
	if len(details) == 0 {
		return Result{}, errors.New("no valid Nemo slack frame found")
	}
	return Result{Technique: SlackSpace, Target: entry.Path(), Detail: strings.Join(details, ","), Bytes: payloadBytes}, nil
}

func slackRegions(entry filesystem.Entry, img image.Image) (filesystem.SlackSpaceCapable, []filesystem.SlackRegion, error) {
	capable, ok := entry.(filesystem.SlackSpaceCapable)
	if !ok {
		return nil, nil, unsupported(SlackSpace)
	}
	if img == nil {
		return nil, nil, errors.New("slack-space requires image-backed storage")
	}
	regions, err := capable.SlackRegions()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect slack regions: %w", err)
	}
	return capable, regions, nil
}

func readSlackFrame(img image.Image, region filesystem.SlackRegion) ([]byte, int64, bool, error) {
	if !validSlackRegion(img, region) || region.Length < slackFrameHeaderSize {
		return nil, 0, false, nil
	}
	header := make([]byte, slackFrameHeaderSize)
	if err := readImageExact(img, header, region.Offset); err != nil {
		return nil, 0, false, err
	}
	if !bytes.Equal(header[:8], slackFrameMagic[:]) {
		return nil, 0, false, nil
	}
	if header[8] != slackFrameVersion {
		return nil, 0, false, fmt.Errorf("unsupported frame version %d", header[8])
	}
	if header[9] != 0 || header[10] != 0 || header[11] != 0 {
		return nil, 0, false, errors.New("unsupported non-zero frame flags")
	}
	payloadLength := int64(binary.LittleEndian.Uint32(header[12:16]))
	frameLength := int64(slackFrameHeaderSize) + payloadLength
	if frameLength > region.Length {
		return nil, 0, false, fmt.Errorf("frame length %d exceeds %d-byte slack region", frameLength, region.Length)
	}
	payload := make([]byte, int(payloadLength))
	if err := readImageExact(img, payload, region.Offset+slackFrameHeaderSize); err != nil {
		return nil, 0, false, err
	}
	sum := sha256.Sum256(payload)
	if !bytes.Equal(header[16:48], sum[:]) {
		return nil, 0, false, errors.New("slack frame payload hash mismatch")
	}
	return payload, frameLength, true, nil
}

func validSlackRegion(img image.Image, region filesystem.SlackRegion) bool {
	return region.Offset >= 0 && region.Length >= 0 && region.Offset <= img.Size() && region.Length <= img.Size()-region.Offset
}

func readImageExact(img image.Image, data []byte, offset int64) error {
	n, err := img.ReadAt(data, offset)
	if n == len(data) {
		return nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return err
}
