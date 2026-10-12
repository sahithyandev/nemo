package technique

import (
	"crypto/rand"
	"encoding/binary"
	"hash/crc32"
)

const (
	legacyFrameMagic      = "NEMO"
	legacyFrameHeaderSize = 12
	frameMagic            = "NEM2"
	frameHeaderSize       = 28 // common header[12] + per-hide nonce[16]
)

// encodeFrame gives each hide a fresh identity, including repeated payloads.
func encodeFrame(payload []byte) ([]byte, error) {
	out := make([]byte, frameHeaderSize+len(payload))
	copy(out, frameMagic)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(payload)))
	binary.LittleEndian.PutUint32(out[8:12], crc32.ChecksumIEEE(payload))
	if _, err := rand.Read(out[12:frameHeaderSize]); err != nil {
		return nil, err
	}
	copy(out[frameHeaderSize:], payload)
	return out, nil
}

func frameHeaderLength(magic string) int {
	switch magic {
	case frameMagic:
		return frameHeaderSize
	case legacyFrameMagic:
		return legacyFrameHeaderSize
	default:
		return 0
	}
}

// decodeFrame retains compatibility with historical NEMO frames.
func decodeFrame(buf []byte) (payload []byte, ok bool) {
	if len(buf) < legacyFrameHeaderSize {
		return nil, false
	}
	header := frameHeaderLength(string(buf[:4]))
	if header == 0 || len(buf) < header {
		return nil, false
	}
	length := uint64(binary.LittleEndian.Uint32(buf[4:8]))
	if length > uint64(len(buf)-header) {
		return nil, false
	}
	payload = buf[header : header+int(length)]
	if crc32.ChecksumIEEE(payload) != binary.LittleEndian.Uint32(buf[8:12]) {
		return nil, false
	}
	return payload, true
}
