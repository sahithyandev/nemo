package ntfs

import (
	"errors"
	"fmt"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

var _ filesystem.SlackSpaceCapable = (*Entry)(nil)

// SlackRegions maps the uninitialized tail of unnamed DATA into existing
// allocated clusters. The technique layer reads and writes these regions through
// the custody-wrapped image; neither allocation nor file metadata is changed.
// Validate the entire allocation before returning any writable physical region.
func (e *Entry) SlackRegions() ([]filesystem.SlackRegion, error) {
	if e == nil || e.fs == nil {
		return nil, errors.New("ntfs: invalid nil entry")
	}
	e.fs.mftMu.Lock()
	defer e.fs.mftMu.Unlock()
	if e.isDir {
		return nil, nil
	}
	b, r, err := e.streamRecord()
	if err != nil {
		return nil, err
	}
	if r.header.flags&2 != 0 {
		return nil, nil
	}
	// File-level flags can advertise unsupported storage even if DATA flags
	// in a malformed record fail to do so.
	const unsupportedFlags = 0x200 | 0x800 | 0x4000 // sparse, compressed, encrypted
	if r.standardInformation != nil && r.standardInformation.fileFlags&unsupportedFlags != 0 {
		return nil, fmt.Errorf("ntfs: sparse, compressed or encrypted file: %w", filesystem.ErrUnsupported)
	}
	index, err := findStream(r, "")
	if err != nil || index < 0 {
		return nil, err
	}
	a := r.attributes[index]
	if err := streamFlags(b, r, index); err != nil {
		return nil, err
	}
	if a.flags&0x8000 != 0 {
		return nil, fmt.Errorf("ntfs: sparse file: %w", filesystem.ErrUnsupported)
	}
	if !a.nonResident {
		return nil, nil
	}
	capacity, err := e.fs.streamRuns(a.runs, true)
	if err != nil {
		return nil, err
	}
	if a.lowestVCN != 0 || a.allocatedSize != capacity {
		return nil, errors.New("ntfs: invalid nonresident allocation/extent (ATTRIBUTE_LIST unsupported)")
	}
	if err := e.validateStreamAllocation(r, index); err != nil {
		return nil, err
	}
	length := capacity - a.initializedSize
	if length > uint64(^uint(0)>>1) {
		return nil, errors.New("ntfs: slack allocation is too large")
	}
	plan, err := e.fs.streamWritePlan(a.runs, a.initializedSize, int(length))
	if err != nil {
		return nil, err
	}
	var regions []filesystem.SlackRegion
	for _, part := range plan {
		regions = append(regions, filesystem.SlackRegion{Offset: part.offset, Length: int64(part.length)})
	}
	return regions, nil
}
