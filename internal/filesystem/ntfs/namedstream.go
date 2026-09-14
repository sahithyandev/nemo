package ntfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

var _ filesystem.NamedStreamCapable = (*Entry)(nil)

func streamName(name string) ([]uint16, error) {
	if name == "" {
		return nil, errors.New("ntfs: empty stream name")
	}
	n := utf16.Encode([]rune(name))
	if !utf8.ValidString(name) || len(n) > 255 {
		return nil, errors.New("ntfs: invalid stream name (maximum 255 UTF-16 code units)")
	}
	for _, c := range n {
		if c == 0 || c == ':' || c == '/' || c == '\\' {
			return nil, errors.New("ntfs: invalid character in stream name")
		}
	}
	return n, nil
}

// streamRecord reloads the record under mftMu; no Entry caches attributes.
func (e *Entry) streamRecord() ([]byte, mftRecord, error) {
	if e.fs.bytesPerSector != 512 {
		return nil, mftRecord{}, errors.New("ntfs: unsupported named-stream fixup geometry")
	}
	if len(e.fs.mftRuns) != 0 {
		if _, err := e.fs.streamRuns(e.fs.mftRuns, true); err != nil {
			return nil, mftRecord{}, err
		}
	}
	b, err := e.fs.loadMFTBytes(e.recordNumber)
	if err != nil {
		return nil, mftRecord{}, err
	}
	r, err := parseMFTRecord(b)
	if err != nil {
		return nil, r, err
	}
	if r.header.flags&1 == 0 || r.header.baseRecordReference != 0 {
		return nil, r, errors.New("ntfs: inactive or extension FILE record unsupported (ATTRIBUTE_LIST)")
	}
	usa, count := int(binary.LittleEndian.Uint16(b[4:])), int(binary.LittleEndian.Uint16(b[6:]))
	if usa < 48 || usa%2 != 0 || usa+count*2 > int(r.header.firstAttributeOffset) || usa+count*2 > 510 {
		return nil, r, errors.New("ntfs: malformed FILE record update sequence array")
	}
	off := int(r.header.firstAttributeOffset)
	for _, a := range r.attributes {
		nameEnd := int(binary.LittleEndian.Uint16(b[off+10:])) + int(b[off+9])*2
		minimum, limit := 24, int(a.valueOffset)
		if a.nonResident {
			minimum, limit = 64, int(a.mappingPairsOffset)
		}
		if b[off+9] != 0 && (int(binary.LittleEndian.Uint16(b[off+10:])) < minimum || nameEnd > limit) {
			return nil, r, errors.New("ntfs: malformed attribute name overlaps header or value/runlist")
		}
		off += int(a.length)
	}
	return b, r, nil
}

func (e *Entry) NamedStreams() ([]string, error) {
	e.fs.mftMu.Lock()
	defer e.fs.mftMu.Unlock()
	_, r, err := e.streamRecord()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var names []string
	for _, a := range r.attributes {
		if a.typeCode == attributeTypeData && a.name != "" && !seen[a.name] {
			seen[a.name] = true
			names = append(names, a.name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func findStream(r mftRecord, name string) (int, error) {
	found := -1
	for i, a := range r.attributes {
		if a.typeCode == attributeTypeData && a.name == name {
			if found != -1 {
				return -1, errors.New("ntfs: multiple stream extents unsupported (ATTRIBUTE_LIST)")
			}
			found = i
		}
	}
	return found, nil
}

func streamFlags(b []byte, r mftRecord, index int) error {
	a := r.attributes[index]
	if a.flags&0x00ff != 0 || a.nonResident && binary.LittleEndian.Uint16(b[attributeOffset(r, index)+34:]) != 0 {
		return errors.New("ntfs: compressed stream unsupported")
	}
	if a.flags&0x4000 != 0 {
		return errors.New("ntfs: encrypted stream unsupported")
	}
	if !a.nonResident && a.flags&0x8000 != 0 {
		return errors.New("ntfs: sparse resident stream unsupported")
	}
	return nil
}

func (e *Entry) ReadStream(name string) ([]byte, error) {
	if _, err := streamName(name); err != nil {
		return nil, err
	}
	e.fs.mftMu.Lock()
	defer e.fs.mftMu.Unlock()
	b, r, err := e.streamRecord()
	if err != nil {
		return nil, err
	}
	i, err := findStream(r, name)
	if err != nil {
		return nil, err
	}
	if i < 0 {
		return nil, fmt.Errorf("ntfs: stream %q not found", name)
	}
	a := r.attributes[i]
	if err := streamFlags(b, r, i); err != nil {
		return nil, err
	}
	if !a.nonResident {
		return append([]byte{}, a.value...), nil
	}
	capacity, err := e.fs.streamRuns(a.runs, false)
	if err != nil {
		return nil, err
	}
	if a.lowestVCN != 0 || a.dataSize > capacity || a.dataSize > uint64(e.fs.img.Size()) || a.dataSize > uint64(^uint(0)>>1) {
		return nil, errors.New("ntfs: invalid or unsupported stream size/extent")
	}
	b = make([]byte, int(a.dataSize))
	if err := e.fs.readRunsAt(a.runs, a.dataSize, 0, b[:int(a.initializedSize)]); err != nil {
		return nil, fmt.Errorf("ntfs: read stream: %w", err)
	}
	return b, nil
}

func (e *Entry) WriteStream(name string, data []byte) error {
	n, err := streamName(name)
	if err != nil {
		return err
	}
	e.fs.mftMu.Lock()
	defer e.fs.mftMu.Unlock()
	b, r, err := e.streamRecord()
	if err != nil {
		return err
	}
	i, err := findStream(r, name)
	if err != nil {
		return err
	}
	if i >= 0 {
		a := r.attributes[i]
		if err := streamFlags(b, r, i); err != nil {
			return err
		}
		if a.nonResident {
			return e.replaceNonresident(b, r, i, data)
		}
	}
	// Bound the allocation before constructing an attribute from caller data.
	valueOffset := (24 + 2*len(n) + 7) &^ 7
	if len(data) > len(b)-valueOffset-8 {
		return errors.New("ntfs: insufficient MFT record room; new nonresident ADS allocation unsupported")
	}
	a := make([]byte, (valueOffset+len(data)+7)&^7)
	binary.LittleEndian.PutUint32(a, attributeTypeData)
	binary.LittleEndian.PutUint32(a[4:], uint32(len(a)))
	a[9] = byte(len(n))
	binary.LittleEndian.PutUint16(a[10:], 24)
	for j, c := range n {
		binary.LittleEndian.PutUint16(a[24+j*2:], c)
	}
	binary.LittleEndian.PutUint32(a[16:], uint32(len(data)))
	binary.LittleEndian.PutUint16(a[20:], uint16(valueOffset))
	copy(a[valueOffset:], data)
	if i >= 0 {
		binary.LittleEndian.PutUint16(a[14:], r.attributes[i].attributeID)
		binary.LittleEndian.PutUint16(a[12:], r.attributes[i].flags)
		old := attributeOffset(r, i)
		a[22] = b[old+22]
	} else {
		used := make(map[uint16]bool)
		for _, old := range r.attributes {
			used[old.attributeID] = true
		}
		id := binary.LittleEndian.Uint16(b[40:42])
		for attempts := 0; used[id]; attempts++ {
			if attempts == 65535 {
				return errors.New("ntfs: no free attribute instance ID")
			}
			id++
		}
		binary.LittleEndian.PutUint16(a[14:], id)
		binary.LittleEndian.PutUint16(b[40:], id+1)
	}
	updated, err := repackStream(b, r, i, a)
	if err != nil {
		return err
	}
	return e.writeStreamRecord(updated)
}

func (e *Entry) DeleteStream(name string) error {
	if _, err := streamName(name); err != nil {
		return err
	}
	e.fs.mftMu.Lock()
	defer e.fs.mftMu.Unlock()
	b, r, err := e.streamRecord()
	if err != nil {
		return err
	}
	i, err := findStream(r, name)
	if err != nil {
		return err
	}
	if i < 0 {
		return fmt.Errorf("ntfs: stream %q not found", name)
	}
	if err := streamFlags(b, r, i); err != nil {
		return err
	}
	if r.attributes[i].nonResident {
		return errors.New("ntfs: deleting nonresident ADS requires cluster release; deallocation unsupported")
	}
	updated, err := repackStream(b, r, i, nil)
	if err != nil {
		return err
	}
	return e.writeStreamRecord(updated)
}

func attributeOffset(r mftRecord, index int) int {
	off := int(r.header.firstAttributeOffset)
	for _, a := range r.attributes[:index] {
		off += int(a.length)
	}
	return off
}

func repackStream(b []byte, r mftRecord, index int, replacement []byte) ([]byte, error) {
	out := append([]byte(nil), b[:r.header.firstAttributeOffset]...)
	off := int(r.header.firstAttributeOffset)
	inserted := index >= 0
	for i, a := range r.attributes {
		// Attributes are ordered by type. Preserve the relative order of all
		// existing attributes, inserting new DATA before a greater type.
		if !inserted && a.typeCode > attributeTypeData {
			out = append(out, replacement...)
			inserted = true
		}
		if i == index {
			out = append(out, replacement...)
		} else {
			out = append(out, b[off:off+int(a.length)]...)
		}
		off += int(a.length)
	}
	if !inserted {
		out = append(out, replacement...)
	}
	out = append(out, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0)
	if len(out) > int(r.header.allocatedSize) {
		return nil, errors.New("ntfs: insufficient MFT record room; new nonresident ADS allocation unsupported")
	}
	binary.LittleEndian.PutUint32(out[24:], uint32(len(out)))
	return append(out, make([]byte, len(b)-len(out))...), nil
}

// streamRuns validates full mapping coverage and arithmetic before any I/O.
func (f *FS) streamRuns(runs []dataRun, writing bool) (uint64, error) {
	cs := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
	if cs == 0 || f.img.Size() < 0 {
		return 0, errors.New("ntfs: invalid run geometry/image size")
	}
	var clusters uint64
	for i, r := range runs {
		if r.VCN != clusters || r.Clusters == 0 || r.Clusters > ^uint64(0)/cs-clusters {
			return 0, errors.New("ntfs: invalid runlist coverage or overflow")
		}
		clusters += r.Clusters
		if r.Sparse {
			if writing {
				return 0, errors.New("ntfs: sparse stream mutation unsupported")
			}
			continue
		}
		if r.LCN < 0 || uint64(r.LCN) > uint64(f.img.Size())/cs || r.Clusters > uint64(f.img.Size())/cs-uint64(r.LCN) {
			return 0, errors.New("ntfs: data run exceeds image bounds")
		}
		if writing {
			for _, prev := range runs[:i] {
				if !prev.Sparse && uint64(r.LCN) < uint64(prev.LCN)+prev.Clusters && uint64(prev.LCN) < uint64(r.LCN)+r.Clusters {
					return 0, errors.New("ntfs: overlapping physical runs")
				}
			}
		}
	}
	return clusters * cs, nil
}

type streamExtent struct {
	offset int64
	length int
}

func (f *FS) streamWritePlan(runs []dataRun, logical uint64, length int) ([]streamExtent, error) {
	capacity, err := f.streamRuns(runs, true)
	if err != nil {
		return nil, err
	}
	if length < 0 || logical > capacity || uint64(length) > capacity-logical {
		return nil, errors.New("ntfs: write exceeds existing run allocation")
	}
	cs := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
	var plan []streamExtent
	remaining := uint64(length)
	for _, r := range runs {
		start, end := r.VCN*cs, (r.VCN+r.Clusters)*cs
		if logical >= end || remaining == 0 {
			continue
		}
		n := min(remaining, end-logical)
		plan = append(plan, streamExtent{int64(uint64(r.LCN)*cs + logical - start), int(n)})
		logical += n
		remaining -= n
	}
	return plan, nil
}

// writeStreamPlan pads the planned region with zeros after b is exhausted.
// A fixed buffer bounds memory when clearing a large former logical tail.
func (f *FS) writeStreamPlan(plan []streamExtent, b []byte) error {
	buf := make([]byte, 64*1024)
	for _, part := range plan {
		for part.length > 0 {
			chunk := buf[:min(part.length, len(buf))]
			clear(chunk)
			copied := copy(chunk, b)
			b = b[copied:]
			n, err := f.img.WriteAt(chunk, part.offset)
			if err != nil {
				return fmt.Errorf("ntfs: physical write: %w", err)
			}
			if n != len(chunk) {
				return fmt.Errorf("ntfs: physical write: %w", io.ErrShortWrite)
			}
			part.offset += int64(n)
			part.length -= n
		}
	}
	return nil
}

func (f *FS) streamMirrorRange() (uint64, uint64, error) {
	cs := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
	length := max(uint64(4)*uint64(f.fileRecordSize), cs)
	if cs == 0 || f.img.Size() < 0 || f.mftMirrorCluster > uint64(f.img.Size())/cs {
		return 0, 0, errors.New("ntfs: MFT mirror exceeds image bounds")
	}
	start := f.mftMirrorCluster * cs
	if length > uint64(f.img.Size())-start {
		return 0, 0, errors.New("ntfs: MFT mirror exceeds image bounds")
	}
	return start, start + length, nil
}

// prepareStreamRecord does all record and mapping checks before payload writes.
// Only 512-byte fixup strides are supported; other sector geometries are not
// inferred. Mirrored records are rejected rather than leaving $MFTMirr stale.
func (e *Entry) prepareStreamRecord(b []byte) ([]byte, []byte, []streamExtent, error) {
	f := e.fs
	r, err := parseMFTRecord(b)
	if err != nil {
		return nil, nil, nil, err
	}
	ids := make(map[uint16]bool)
	for _, a := range r.attributes {
		if ids[a.attributeID] {
			return nil, nil, nil, errors.New("ntfs: malformed FILE record duplicate attribute instance ID")
		}
		ids[a.attributeID] = true
	}
	cs := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
	if f.bytesPerSector != 512 || len(b) != int(f.fileRecordSize) || len(b)%512 != 0 {
		return nil, nil, nil, errors.New("ntfs: unsupported FILE write fixup geometry")
	}
	mirrored := max(uint64(4), cs/uint64(f.fileRecordSize))
	if e.recordNumber < mirrored {
		return nil, nil, nil, errors.New("ntfs: FILE write requires unsupported $MFTMirr synchronization")
	}
	usa, count := int(binary.LittleEndian.Uint16(b[4:])), int(binary.LittleEndian.Uint16(b[6:]))
	if count != len(b)/512+1 || usa < 48 || usa%2 != 0 || usa+count*2 > int(r.header.firstAttributeOffset) || usa+count*2 > 510 || int(r.header.allocatedSize) != len(b) {
		return nil, nil, nil, errors.New("ntfs: malformed FILE record update sequence array")
	}
	if e.recordNumber > ^uint64(0)/uint64(f.fileRecordSize) {
		return nil, nil, nil, errors.New("ntfs: MFT offset overflow")
	}
	logical := e.recordNumber * uint64(f.fileRecordSize)
	if logical > f.mftDataSize || uint64(len(b)) > f.mftDataSize-logical {
		return nil, nil, nil, errors.New("ntfs: FILE write exceeds MFT data size")
	}
	plan, err := f.streamWritePlan(f.mftRuns, logical, len(b))
	if err != nil {
		return nil, nil, nil, err
	}
	mirrorStart, mirrorEnd, err := f.streamMirrorRange()
	if err != nil {
		return nil, nil, nil, err
	}
	for _, part := range plan {
		if uint64(part.offset) < cs || uint64(part.offset) < mirrorEnd && mirrorStart < uint64(part.offset)+uint64(part.length) {
			return nil, nil, nil, errors.New("ntfs: FILE write overlaps boot region or $MFTMirr")
		}
	}
	expected := append([]byte(nil), b...)
	sequence := binary.LittleEndian.Uint16(b[usa:]) + 1
	if sequence == 0 || sequence == 0xffff {
		sequence = 1
	}
	binary.LittleEndian.PutUint16(expected[usa:], sequence)
	for i := 1; i < count; i++ {
		copy(expected[usa+i*2:usa+i*2+2], b[i*512-2:i*512])
	}
	protected := append([]byte(nil), expected...)
	for i := 1; i < count; i++ {
		binary.LittleEndian.PutUint16(protected[i*512-2:], sequence)
	}
	return protected, expected, plan, nil
}

func (e *Entry) writeStreamRecord(b []byte) error {
	protected, expected, plan, err := e.prepareStreamRecord(b)
	if err != nil {
		return err
	}
	return e.commitStreamRecord(protected, expected, plan)
}

func (e *Entry) commitStreamRecord(protected, expected []byte, plan []streamExtent) error {
	if err := e.fs.writeStreamPlan(plan, protected); err != nil {
		return err
	}
	got, err := e.fs.readMFTRecord(e.recordNumber)
	if err != nil {
		return fmt.Errorf("ntfs: verify FILE write: %w", err)
	}
	if !bytes.Equal(got, expected) {
		return errors.New("ntfs: FILE write verification mismatch")
	}
	return nil
}

func (e *Entry) replaceNonresident(b []byte, r mftRecord, index int, data []byte) error {
	a := r.attributes[index]
	if a.flags&0x8000 != 0 {
		return errors.New("ntfs: sparse stream mutation unsupported")
	}
	capacity, err := e.fs.streamRuns(a.runs, true)
	if err != nil {
		return err
	}
	off := attributeOffset(r, index)
	if a.lowestVCN != 0 || a.allocatedSize != capacity || binary.LittleEndian.Uint16(b[off+34:]) != 0 {
		return errors.New("ntfs: unsupported nonresident allocation/extent/compression unit")
	}
	if uint64(len(data)) > capacity {
		return errors.New("ntfs: nonresident growth exceeds existing allocation; cluster allocation unsupported")
	}
	length := max(uint64(len(data)), a.dataSize)
	if length > uint64(^uint(0)>>1) {
		return errors.New("ntfs: stream is too large")
	}
	plan, err := e.fs.streamWritePlan(a.runs, 0, int(length))
	if err != nil {
		return err
	}
	// Refuse malformed ADS mappings into filesystem metadata or another
	// attribute in this record. No volume-wide allocation ownership is inferred.
	mirrorStart, mirrorEnd, err := e.fs.streamMirrorRange()
	if err != nil {
		return err
	}
	otherRuns := append([]dataRun(nil), e.fs.mftRuns...)
	for j, other := range r.attributes {
		if j != index && other.nonResident {
			if _, err := e.fs.streamRuns(other.runs, false); err != nil {
				return err
			}
			otherRuns = append(otherRuns, other.runs...)
		}
	}
	for _, run := range a.runs {
		cs := uint64(e.fs.bytesPerSector) * uint64(e.fs.sectorsPerCluster)
		start, end := uint64(run.LCN)*cs, (uint64(run.LCN)+run.Clusters)*cs
		if start < cs || start < mirrorEnd && mirrorStart < end {
			return errors.New("ntfs: stream allocation overlaps metadata")
		}
		for _, other := range otherRuns {
			if !other.Sparse && uint64(run.LCN) < uint64(other.LCN)+other.Clusters && uint64(other.LCN) < uint64(run.LCN)+run.Clusters {
				return errors.New("ntfs: stream allocation overlaps another attribute or MFT")
			}
		}
	}
	binary.LittleEndian.PutUint64(b[off+48:], uint64(len(data)))
	binary.LittleEndian.PutUint64(b[off+56:], uint64(len(data)))
	protected, expected, recordPlan, err := e.prepareStreamRecord(b)
	if err != nil {
		return err
	}
	// Clear the former logical tail on shrink; allocation and runlist stay intact.
	if err := e.fs.writeStreamPlan(plan, data); err != nil {
		return err
	}
	return e.commitStreamRecord(protected, expected, recordPlan)
}
