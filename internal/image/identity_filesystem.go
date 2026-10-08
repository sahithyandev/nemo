package image

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
)

// filesystemIdentity binds a backing object to the ext4 filesystem generation.
// Other image formats retain object identity; each new hide also has a nonce.
func (r *RawImage) filesystemIdentity() (string, error) {
	magic := make([]byte, 2)
	n, err := r.file.ReadAt(magic, 1024+56)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read filesystem identity: %w", err)
	}
	if n != 2 || binary.LittleEndian.Uint16(magic) != 0xef53 {
		return "", nil
	}
	sb := make([]byte, 1024)
	if _, err := r.file.ReadAt(sb, 1024); err != nil {
		return "", fmt.Errorf("read recognized ext4 identity metadata: %w", err)
	}
	if binary.LittleEndian.Uint32(sb[24:28]) > 6 || binary.LittleEndian.Uint32(sb[0:4]) == 0 || binary.LittleEndian.Uint32(sb[4:8]) == 0 {
		return "", fmt.Errorf("invalid recognized ext4 identity metadata")
	}
	return ":ext4:" + hex.EncodeToString(sb[104:120]), nil
}
