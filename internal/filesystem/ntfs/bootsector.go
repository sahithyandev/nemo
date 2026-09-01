package ntfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/sahithyandev/nemo/internal/image"
)

const bootSectorSize = 512

type bootSector struct {
	bytesPerSector    uint16
	sectorsPerCluster uint8
	mftCluster        uint64
	mftMirrorCluster  uint64
	fileRecordSize    uint32
}

func readBootSector(img image.Image) (bootSector, error) {
	if img.Size() < bootSectorSize {
		return bootSector{}, errors.New("ntfs: image is too small for a boot sector")
	}

	b := make([]byte, bootSectorSize)
	n, err := img.ReadAt(b, 0)
	if n != len(b) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return bootSector{}, fmt.Errorf("ntfs: read boot sector: %w", err)
	}
	if string(b[3:11]) != ntfsMagic {
		return bootSector{}, errors.New("ntfs: invalid boot sector magic")
	}

	bytesPerSector := binary.LittleEndian.Uint16(b[0x0b:0x0d])
	if bytesPerSector < 256 || bytesPerSector > 4096 || !isPowerOfTwo(uint64(bytesPerSector)) {
		return bootSector{}, fmt.Errorf("ntfs: invalid bytes per sector %d", bytesPerSector)
	}

	sectorsPerCluster := b[0x0d]
	if sectorsPerCluster == 0 || !isPowerOfTwo(uint64(sectorsPerCluster)) {
		return bootSector{}, fmt.Errorf("ntfs: invalid sectors per cluster %d", sectorsPerCluster)
	}

	clusterSize := uint64(bytesPerSector) * uint64(sectorsPerCluster)
	fileRecordSize, err := decodeFileRecordSize(int8(b[0x40]), clusterSize)
	if err != nil {
		return bootSector{}, err
	}

	return bootSector{
		bytesPerSector:    bytesPerSector,
		sectorsPerCluster: sectorsPerCluster,
		mftCluster:        binary.LittleEndian.Uint64(b[0x30:0x38]),
		mftMirrorCluster:  binary.LittleEndian.Uint64(b[0x38:0x40]),
		fileRecordSize:    fileRecordSize,
	}, nil
}

func decodeFileRecordSize(encoded int8, clusterSize uint64) (uint32, error) {
	if encoded == 0 {
		return 0, errors.New("ntfs: invalid zero file record size")
	}

	var size uint64
	if encoded > 0 {
		size = uint64(encoded) * clusterSize
	} else {
		exponent := -int(encoded)
		if exponent > 31 {
			return 0, fmt.Errorf("ntfs: invalid file record size exponent %d", exponent)
		}
		size = uint64(1) << exponent
	}
	if size == 0 || size > uint64(^uint32(0)) {
		return 0, fmt.Errorf("ntfs: file record size %d is unsupported", size)
	}
	return uint32(size), nil
}

func isPowerOfTwo(value uint64) bool {
	return value != 0 && value&(value-1) == 0
}
