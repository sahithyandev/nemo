package ntfs

import (
	"errors"
	"fmt"
	"io"
)

const fileRecordMagic = "FILE"

// mftOffset returns the byte offset of the first MFT record.
func (f *FS) mftOffset() (int64, error) {
	if f.img == nil {
		return 0, errors.New("ntfs: nil image")
	}
	imageSize := f.img.Size()
	if imageSize < 0 {
		return 0, errors.New("ntfs: invalid image size")
	}

	clusterSize := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
	if clusterSize == 0 {
		return 0, errors.New("ntfs: invalid zero cluster size")
	}
	if f.mftCluster > uint64(imageSize)/clusterSize {
		return 0, fmt.Errorf("ntfs: MFT cluster %d exceeds image bounds", f.mftCluster)
	}
	offset := f.mftCluster * clusterSize
	if offset >= uint64(imageSize) {
		return 0, fmt.Errorf("ntfs: MFT offset %d exceeds image bounds", offset)
	}
	return int64(offset), nil
}

// readMFTRecord reads one contiguous record relative to the MFT's starting
// cluster. Resolving non-contiguous MFT data runs belongs to the full parser.
func (f *FS) readMFTRecord(number uint64) ([]byte, error) {
	base, err := f.mftOffset()
	if err != nil {
		return nil, err
	}
	if f.fileRecordSize < uint32(len(fileRecordMagic)) {
		return nil, fmt.Errorf("ntfs: invalid file record size %d", f.fileRecordSize)
	}

	recordSize := uint64(f.fileRecordSize)
	available := uint64(f.img.Size() - base)
	if number > available/recordSize {
		return nil, fmt.Errorf("ntfs: MFT record %d offset exceeds image bounds", number)
	}
	relative := number * recordSize
	if relative > available || recordSize > available-relative {
		return nil, fmt.Errorf("ntfs: MFT record %d exceeds image bounds", number)
	}
	offset := base + int64(relative)

	b := make([]byte, f.fileRecordSize)
	n, readErr := f.img.ReadAt(b, offset)
	if n != len(b) {
		if readErr == nil {
			readErr = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("ntfs: read MFT record %d: %w", number, readErr)
	}
	if string(b[:len(fileRecordMagic)]) != fileRecordMagic {
		return nil, fmt.Errorf("ntfs: MFT record %d has invalid FILE signature", number)
	}
	return b, nil
}
