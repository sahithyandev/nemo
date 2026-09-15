package apfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

// j_inode_val_t stores its four timestamps as fixed-offset uint64 fields
// (nanoseconds since the Unix epoch), ahead of the variable-length xfields
// that follow the 92-byte fixed part. Unlike ext4's inode, there is no
// separate high/low or extra-area split to worry about: one field is one
// 8-byte write.
const (
	inodeCreateTimeOffset = 16
	inodeModTimeOffset    = 24
	inodeChangeTimeOffset = 32
	inodeAccessTimeOffset = 40

	// inodeValFixedSize is sizeof(j_inode_val_t) up to (not including)
	// xfields.
	inodeValFixedSize = 92
)

var _ filesystem.TimestompCapable = (*Entry)(nil)

func inodeTimeOffset(field filesystem.TimeField) (int, error) {
	switch field {
	case filesystem.TimeCreated:
		return inodeCreateTimeOffset, nil
	case filesystem.TimeModified:
		return inodeModTimeOffset, nil
	case filesystem.TimeChanged:
		return inodeChangeTimeOffset, nil
	case filesystem.TimeAccessed:
		return inodeAccessTimeOffset, nil
	default:
		return 0, fmt.Errorf("apfs: unsupported timestamp field %q", field)
	}
}

// isInodeRecord reports whether key is the INODE record for oid.
func isInodeRecord(key []byte, oid uint64) bool {
	kOid, typ, err := decodeJKey(key)
	return err == nil && kOid == oid && typ == objTypeInode
}

// inodeRecordValue returns the INODE record's value bytes for oid. A missing
// record is reported as fs.ErrNotExist.
func (f *FS) inodeRecordValue(oid uint64) ([]byte, error) {
	c, err := f.fsTree.seek(encodeJKey(oid, objTypeInode))
	if err != nil {
		return nil, err
	}
	k := c.key()
	if k == nil || !isInodeRecord(k, oid) {
		return nil, fs.ErrNotExist
	}
	val := c.val()
	if err := c.err(); err != nil {
		return nil, err
	}
	if len(val) < inodeValFixedSize {
		return nil, fmt.Errorf("apfs: inode record for object %d is shorter than j_inode_val_t", oid)
	}
	return append([]byte(nil), val...), nil
}

// timestamp reads one timestamp field from oid's INODE record.
func (f *FS) timestamp(oid uint64, field filesystem.TimeField) (time.Time, error) {
	off, err := inodeTimeOffset(field)
	if err != nil {
		return time.Time{}, err
	}
	val, err := f.inodeRecordValue(oid)
	if err != nil {
		return time.Time{}, err
	}
	ns := binary.LittleEndian.Uint64(val[off : off+8])
	return time.Unix(0, int64(ns)).UTC(), nil
}

// setTimestamp writes one timestamp field into oid's INODE record, in place.
// The record's value is fixed-length, so this is a same-size replace: the
// leaf never changes size, matching the in-place rewrite btree_write.go
// already supports for xattrs.
func (f *FS) setTimestamp(oid uint64, field filesystem.TimeField, t time.Time) error {
	off, err := inodeTimeOffset(field)
	if err != nil {
		return err
	}
	if t.Before(time.Unix(0, 0)) || t.After(time.Unix(0, math.MaxInt64)) {
		return fmt.Errorf("apfs: timestamp %s is out of range for j_inode_val_t (must be between 1970 and 2262)", t)
	}
	ns := t.UnixNano()

	return f.fsTree.rewriteLeaf(encodeJKey(oid, objTypeInode), func(recs []record, _ bool) ([]record, int, error) {
		for i := range recs {
			if !isInodeRecord(recs[i].key, oid) {
				continue
			}
			if len(recs[i].val) < inodeValFixedSize {
				return nil, 0, fmt.Errorf("apfs: inode record for object %d is shorter than j_inode_val_t", oid)
			}
			val := append([]byte(nil), recs[i].val...)
			binary.LittleEndian.PutUint64(val[off:off+8], uint64(ns))
			recs[i].val = val
			return recs, 0, nil
		}
		return nil, 0, fs.ErrNotExist
	})
}

// SetTimestamp implements filesystem.TimestompCapable.
func (e *Entry) SetTimestamp(field filesystem.TimeField, t time.Time) error {
	if e == nil || e.fs == nil {
		return errors.New("apfs: invalid nil entry")
	}
	if err := e.fs.setTimestamp(e.oid, field, t); err != nil {
		return fmt.Errorf("apfs: set timestamp %q on %q: %w", field, e.path, err)
	}
	return nil
}

// Timestamp reads one timestamp field from e's INODE record.
func (e *Entry) Timestamp(field filesystem.TimeField) (time.Time, error) {
	if e == nil || e.fs == nil {
		return time.Time{}, errors.New("apfs: invalid nil entry")
	}
	t, err := e.fs.timestamp(e.oid, field)
	if err != nil {
		return time.Time{}, fmt.Errorf("apfs: read timestamp %q on %q: %w", field, e.path, err)
	}
	return t, nil
}
