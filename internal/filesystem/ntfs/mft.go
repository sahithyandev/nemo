package ntfs

import (
	"encoding/binary"
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
	if f.fileRecordSize < uint32(len(fileRecordMagic)) {
		return nil, fmt.Errorf("ntfs: invalid file record size %d", f.fileRecordSize)
	}

	recordSize := uint64(f.fileRecordSize)
	if number > ^uint64(0)/recordSize {
		return nil, fmt.Errorf("ntfs: MFT record %d offset overflow", number)
	}
	relative := number * recordSize
	b := make([]byte, f.fileRecordSize)
	var err error
	if number == 0 || len(f.mftRuns) == 0 {
		base, e := f.mftOffset()
		if e != nil {
			return nil, e
		}
		if relative > uint64(f.img.Size()-base) || recordSize > uint64(f.img.Size()-base)-relative {
			return nil, fmt.Errorf("ntfs: MFT record %d exceeds image bounds", number)
		}
		err = readImageExact(f.img, b, base+int64(relative))
	} else {
		err = f.readRunsAt(f.mftRuns, f.mftDataSize, relative, b)
	}
	if err != nil {
		return nil, fmt.Errorf("ntfs: read MFT record %d: %w", number, err)
	}
	if err = applyFixups(b, uint32(f.bytesPerSector), fileRecordMagic); err != nil {
		return nil, fmt.Errorf("ntfs: MFT record %d: %w", number, err)
	}
	return b, nil
}

func (f *FS) loadMFTRecord(number uint64) (mftRecord, error) {
	f.mftMu.Lock()
	defer f.mftMu.Unlock()
	if number != 0 && len(f.mftRuns) == 0 {
		zero, err := f.readMFTRecord(0)
		if err != nil {
			return mftRecord{}, err
		}
		parsed, err := parseMFTRecord(zero)
		if err != nil {
			return mftRecord{}, err
		}
		if parsed.data == nil || parsed.data.resident {
			return mftRecord{}, errors.New("ntfs: $MFT has no non-resident unnamed DATA attribute")
		}
		if err = f.validateRuns(parsed.data.runs); err != nil {
			return mftRecord{}, err
		}
		f.mftRuns = parsed.data.runs
		f.mftDataSize = parsed.data.dataSize
	}
	b, err := f.readMFTRecord(number)
	if err != nil {
		return mftRecord{}, err
	}
	return parseMFTRecord(b)
}

func applyFixups(record []byte, sectorSize uint32, magic string) error {
	if len(record) < 8 || string(record[:4]) != magic {
		return fmt.Errorf("ntfs: invalid %s signature", magic)
	}
	if sectorSize < 2 || len(record)%int(sectorSize) != 0 {
		return errors.New("ntfs: invalid fixup sector geometry")
	}
	off, count := int(binary.LittleEndian.Uint16(record[4:6])), int(binary.LittleEndian.Uint16(record[6:8]))
	sectors := len(record) / int(sectorSize)
	if count != sectors+1 || off < 8 || off > len(record) || count > (len(record)-off)/2 {
		return errors.New("ntfs: invalid update sequence array")
	}
	usa := record[off : off+count*2]
	for i := 0; i < sectors; i++ {
		trailer := (i+1)*int(sectorSize) - 2
		if record[trailer] != usa[0] || record[trailer+1] != usa[1] {
			return fmt.Errorf("ntfs: update sequence mismatch in sector %d", i)
		}
		copy(record[trailer:trailer+2], usa[2+i*2:4+i*2])
	}
	return nil
}

func readImageExact(img interface {
	ReadAt([]byte, int64) (int, error)
}, b []byte, off int64) error {
	n, err := img.ReadAt(b, off)
	if n == len(b) {
		return nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return err
}
func (f *FS) validateRuns(runs []dataRun) error {
	cs := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
	size := f.img.Size()
	if size < 0 {
		return errors.New("ntfs: invalid image size")
	}
	for _, r := range runs {
		if r.Sparse {
			continue
		}
		if uint64(r.LCN) > uint64(size)/cs || r.Clusters > uint64(size)/cs-uint64(r.LCN) {
			return errors.New("ntfs: data run exceeds image bounds")
		}
	}
	return nil
}
func (f *FS) readRunsAt(runs []dataRun, dataSize, logical uint64, out []byte) error {
	if uint64(len(out)) > dataSize || logical > dataSize-uint64(len(out)) {
		return io.ErrUnexpectedEOF
	}
	cs := uint64(f.bytesPerSector) * uint64(f.sectorsPerCluster)
	done := 0
	for done < len(out) {
		pos := logical + uint64(done)
		vcn := pos / cs
		within := pos % cs
		var found *dataRun
		for i := range runs {
			if vcn >= runs[i].VCN && vcn-runs[i].VCN < runs[i].Clusters {
				found = &runs[i]
				break
			}
		}
		if found == nil {
			return errors.New("ntfs: unmapped logical data")
		}
		avail := (found.Clusters-(vcn-found.VCN))*cs - within
		n := len(out) - done
		if uint64(n) > avail {
			n = int(avail)
		}
		if found.Sparse {
			clear(out[done : done+n])
		} else {
			physical := (uint64(found.LCN)+(vcn-found.VCN))*cs + within
			if physical > uint64(f.img.Size()) || uint64(n) > uint64(f.img.Size())-physical {
				return errors.New("ntfs: data run read exceeds image bounds")
			}
			if err := readImageExact(f.img, out[done:done+n], int64(physical)); err != nil {
				return err
			}
		}
		done += n
	}
	return nil
}
