package technique

import (
	"encoding/binary"
	"hash/crc32"

	"github.com/sahithyandev/nemo/internal/binutil"
)

// Slack regions normally hold whatever residual bytes the filesystem left
// behind, so a raw payload is indistinguishable from noise. Nemo wraps every
// slack payload in a small framed header: detect only reports a finding when
// the magic and CRC both validate, and clear knows exactly which bytes it put
// there.
const (
	frameMagic      = "NEMO"
	frameHeaderSize = 12 // magic[4] + length[4] + crc32[4]
)

// encodeFrame wraps payload in a slack frame.
func encodeFrame(payload []byte) []byte {
	out := make([]byte, frameHeaderSize+len(payload))
	copy(out, frameMagic)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(payload)))
	binary.LittleEndian.PutUint32(out[8:12], crc32.ChecksumIEEE(payload))
	copy(out[frameHeaderSize:], payload)
	return out
}

// decodeFrame extracts the payload from a slack frame. ok is false when buf
// does not begin with a valid, CRC-checked Nemo frame (i.e. it is ordinary
// residual data).
func decodeFrame(buf []byte) (payload []byte, ok bool) {
	r := binutil.New(buf, binary.LittleEndian)
	if r.String(4) != frameMagic {
		return nil, false
	}
	length := int(r.Uint(4))
	want := uint32(r.Uint(4))
	payload = r.Bytes(length)
	if r.Err() != nil {
		return nil, false
	}
	if crc32.ChecksumIEEE(payload) != want {
		return nil, false
	}
	return payload, true
}
