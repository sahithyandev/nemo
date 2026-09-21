package ntfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

var _ filesystem.TimestompCapable = (*Entry)(nil)

const (
	filetimeEpochSeconds   int64  = 11644473600
	filetimeTicksPerSecond uint64 = 10000000
)

// FILETIME counts 100 ns intervals since 1601-01-01. Reject values that
// would overflow or lose precision instead of silently rounding them.
func encodeFiletime(t time.Time) (uint64, error) {
	if t.Before(decodeFiletime(0)) || t.After(decodeFiletime(^uint64(0))) {
		return 0, errors.New("ntfs: timestamp outside FILETIME range")
	}
	if t.Nanosecond()%100 != 0 {
		return 0, errors.New("ntfs: timestamp requires 100 ns precision")
	}
	return uint64(t.Unix()+filetimeEpochSeconds)*filetimeTicksPerSecond + uint64(t.Nanosecond()/100), nil
}

func decodeFiletime(ticks uint64) time.Time {
	return time.Unix(int64(ticks/filetimeTicksPerSecond)-filetimeEpochSeconds, int64(ticks%filetimeTicksPerSecond)*100).UTC()
}

// timestampRecord requires mftMu. Use the same record validation as stream
// edits, including rejection of ATTRIBUTE_LIST and extension records.
func (e *Entry) timestampRecord(field filesystem.TimeField) ([]byte, int, error) {
	var relative int
	switch field {
	case filesystem.TimeCreated:
		relative = 0
	case filesystem.TimeModified:
		relative = 8
	case filesystem.TimeChanged:
		relative = 16
	case filesystem.TimeAccessed:
		relative = 24
	default:
		return nil, 0, fmt.Errorf("ntfs: unsupported timestamp field %q", field)
	}
	b, r, err := e.streamRecord()
	if err != nil {
		return nil, 0, err
	}
	for i, a := range r.attributes {
		if a.typeCode == attributeTypeStandardInformation {
			if a.nonResident || a.name != "" || a.flags != 0 {
				return nil, 0, errors.New("ntfs: invalid STANDARD_INFORMATION attribute")
			}
			return b, attributeOffset(r, i) + int(a.valueOffset) + relative, nil
		}
	}
	return nil, 0, errors.New("ntfs: missing STANDARD_INFORMATION attribute")
}

// SetTimestamp changes only the selected STANDARD_INFORMATION timestamp.
// The shared FILE writer validates all mappings before writing, regenerates
// fixups and verifies the result. Writes pass through the image's custody wrapper.
func (e *Entry) SetTimestamp(field filesystem.TimeField, value time.Time) error {
	if e == nil || e.fs == nil {
		return errors.New("ntfs: invalid nil entry")
	}
	ticks, err := encodeFiletime(value)
	if err != nil {
		return err
	}
	e.fs.mftMu.Lock()
	defer e.fs.mftMu.Unlock()
	b, off, err := e.timestampRecord(field)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(b[off:off+8], ticks)
	if err := e.writeStreamRecord(b); err != nil {
		return fmt.Errorf("ntfs: write timestamp %q: %w", field, err)
	}
	return nil
}

// Timestamp reads a STANDARD_INFORMATION timestamp normalized to UTC.
func (e *Entry) Timestamp(field filesystem.TimeField) (time.Time, error) {
	if e == nil || e.fs == nil {
		return time.Time{}, errors.New("ntfs: invalid nil entry")
	}
	e.fs.mftMu.Lock()
	defer e.fs.mftMu.Unlock()
	b, off, err := e.timestampRecord(field)
	if err != nil {
		return time.Time{}, err
	}
	return decodeFiletime(binary.LittleEndian.Uint64(b[off : off+8])), nil
}
