package ext4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

const (
	goodOldInodeSize = 128

	inodeAtimeOffset       = 8
	inodeMtimeOffset       = 16
	inodeExtraIsizeOffset  = 128
	inodeMtimeExtraOffset  = 136
	inodeAtimeExtraOffset  = 140
	inodeCrtimeOffset      = 144
	inodeCrtimeExtraOffset = 148

	ext4EpochBits = 2
	ext4EpochMask = (1 << ext4EpochBits) - 1
)

var _ filesystem.TimestompCapable = (*Entry)(nil)

type inodeTimestampLayout struct {
	lowOffset   int
	extraOffset int
	hasExtra    bool
}

// SetTimestamp updates one timestamp in the entry's inode. The complete inode
// is written in one operation so image decorators (including custody.Wrap)
// observe the mutation, and metadata checksums are refreshed when enabled.
func (e *Entry) SetTimestamp(field filesystem.TimeField, value time.Time) error {
	if e == nil || e.fs == nil {
		return newInvalidEntryError()
	}
	raw, off, err := e.fs.readRawInode(e.inode)
	if err != nil {
		return wrapTimestampError("read", field, err)
	}
	layout, err := timestampLayout(raw, field)
	if err != nil {
		return err
	}
	low, extra, err := encodeExt4Timestamp(value, layout.hasExtra)
	if err != nil {
		return timestampRepresentationError(field, err)
	}

	binary.LittleEndian.PutUint32(raw[layout.lowOffset:layout.lowOffset+4], low)
	if layout.hasExtra {
		binary.LittleEndian.PutUint32(raw[layout.extraOffset:layout.extraOffset+4], extra)
	}
	if e.fs.sb.featureROCompat&featureROCompatMetadataCsum != 0 {
		updateInodeChecksum(e.fs.sb, e.inode, raw)
	}
	if err := writeExact(e.fs.img, raw, off); err != nil {
		return fmt.Errorf("ext4: write inode %d timestamp %q: %w", e.inode, field, err)
	}
	return nil
}

// Timestamp reads one timestamp from the entry's inode. Returned values are
// normalized to UTC.
func (e *Entry) Timestamp(field filesystem.TimeField) (time.Time, error) {
	if e == nil || e.fs == nil {
		return time.Time{}, newInvalidEntryError()
	}
	raw, _, err := e.fs.readRawInode(e.inode)
	if err != nil {
		return time.Time{}, wrapTimestampError("read", field, err)
	}
	layout, err := timestampLayout(raw, field)
	if err != nil {
		return time.Time{}, err
	}
	low := binary.LittleEndian.Uint32(raw[layout.lowOffset : layout.lowOffset+4])
	var extra uint32
	if layout.hasExtra {
		extra = binary.LittleEndian.Uint32(raw[layout.extraOffset : layout.extraOffset+4])
	}
	return decodeExt4Timestamp(low, extra, layout.hasExtra)
}

// SupportsTimestamp reports whether this inode contains the requested field.
// Access and modification time are part of every ext4 inode; creation time is
// present only in sufficiently large inodes with a declared extra area.
func (e *Entry) SupportsTimestamp(field filesystem.TimeField) (bool, error) {
	if e == nil || e.fs == nil {
		return false, newInvalidEntryError()
	}
	raw, _, err := e.fs.readRawInode(e.inode)
	if err != nil {
		return false, wrapTimestampError("inspect", field, err)
	}
	_, err = timestampLayout(raw, field)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, errTimestampFieldUnavailable) {
		return false, nil
	}
	return false, err
}

var errTimestampFieldUnavailable = errors.New("timestamp field is unavailable")

func timestampLayout(raw []byte, field filesystem.TimeField) (inodeTimestampLayout, error) {
	extraEnd, err := inodeExtraEnd(raw)
	if err != nil {
		return inodeTimestampLayout{}, fmt.Errorf("ext4: %w", err)
	}
	has := func(offset int) bool { return offset+4 <= extraEnd }

	switch field {
	case filesystem.TimeModified:
		return inodeTimestampLayout{lowOffset: inodeMtimeOffset, extraOffset: inodeMtimeExtraOffset, hasExtra: has(inodeMtimeExtraOffset)}, nil
	case filesystem.TimeAccessed:
		return inodeTimestampLayout{lowOffset: inodeAtimeOffset, extraOffset: inodeAtimeExtraOffset, hasExtra: has(inodeAtimeExtraOffset)}, nil
	case filesystem.TimeCreated:
		if !has(inodeCrtimeOffset) {
			return inodeTimestampLayout{}, fmt.Errorf("ext4: %w: %q requires i_crtime in the inode extra area", errTimestampFieldUnavailable, field)
		}
		return inodeTimestampLayout{lowOffset: inodeCrtimeOffset, extraOffset: inodeCrtimeExtraOffset, hasExtra: has(inodeCrtimeExtraOffset)}, nil
	default:
		return inodeTimestampLayout{}, fmt.Errorf("ext4: unsupported timestamp field %q", field)
	}
}

func inodeExtraEnd(raw []byte) (int, error) {
	if len(raw) < goodOldInodeSize {
		return 0, fmt.Errorf("inode is %d bytes; need at least %d", len(raw), goodOldInodeSize)
	}
	if len(raw) == goodOldInodeSize {
		return goodOldInodeSize, nil
	}
	extra := int(binary.LittleEndian.Uint16(raw[inodeExtraIsizeOffset:]))
	if extra == 0 {
		return goodOldInodeSize, nil
	}
	if extra < 4 || extra%4 != 0 {
		return 0, fmt.Errorf("invalid inode extra-isize %d: must be zero or a 4-byte-aligned value of at least 4", extra)
	}
	if extra > len(raw)-goodOldInodeSize {
		return 0, fmt.Errorf("invalid inode extra-isize %d: exceeds %d-byte inode", extra, len(raw))
	}
	return goodOldInodeSize + extra, nil
}

func encodeExt4Timestamp(t time.Time, hasExtra bool) (uint32, uint32, error) {
	seconds := t.Unix()
	if !hasExtra {
		if seconds < -1<<31 || seconds > 1<<31-1 {
			return 0, 0, fmt.Errorf("seconds %d are outside the signed 32-bit inode range", seconds)
		}
		if t.Nanosecond() != 0 {
			return 0, 0, errors.New("sub-second precision requires the inode's extra timestamp field")
		}
		return uint32(int32(seconds)), 0, nil
	}

	lowSigned := int64(int32(uint32(seconds)))
	epoch := (seconds - lowSigned) >> 32
	if epoch < 0 || epoch > ext4EpochMask {
		return 0, 0, fmt.Errorf("seconds %d are outside the ext4 extended inode range", seconds)
	}
	extra := uint32(t.Nanosecond())<<ext4EpochBits | uint32(epoch)
	return uint32(seconds), extra, nil
}

func decodeExt4Timestamp(low, extra uint32, hasExtra bool) (time.Time, error) {
	seconds := int64(int32(low))
	if !hasExtra {
		return time.Unix(seconds, 0).UTC(), nil
	}
	nanoseconds := extra >> ext4EpochBits
	if nanoseconds >= 1_000_000_000 {
		return time.Time{}, fmt.Errorf("ext4: invalid inode timestamp nanoseconds %d", nanoseconds)
	}
	seconds += int64(extra&ext4EpochMask) << 32
	return time.Unix(seconds, int64(nanoseconds)).UTC(), nil
}

func newInvalidEntryError() error {
	return errors.New("ext4: invalid nil entry")
}

func wrapTimestampError(action string, field filesystem.TimeField, err error) error {
	return fmt.Errorf("ext4: %s inode timestamp %q: %w", action, field, err)
}

func timestampRepresentationError(field filesystem.TimeField, err error) error {
	return fmt.Errorf("ext4: timestamp %q cannot be represented: %w", field, err)
}
