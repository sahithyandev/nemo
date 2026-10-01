package ntfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf16"
)

const (
	mftRecordHeaderSize            = 48
	residentAttributeHeaderSize    = 24
	nonResidentAttributeHeaderSize = 64

	attributeTypeStandardInformation = uint32(0x10)
	attributeTypeFileName            = uint32(0x30)
	attributeTypeData                = uint32(0x80)
	attributeTypeIndexRoot           = uint32(0x90)
	attributeTypeIndexAllocation     = uint32(0xa0)
	attributeTypeBitmap              = uint32(0xb0)
	attributeTypeAttributeList       = uint32(0x20)
	attributeTypeEnd                 = uint32(0xffffffff)
)

type mftRecordHeader struct {
	firstAttributeOffset uint16
	flags                uint16
	usedSize             uint32
	allocatedSize        uint32
	baseRecordReference  uint64
	recordNumber         uint32
}

type attributeHeader struct {
	typeCode           uint32
	length             uint32
	flags              uint16
	attributeID        uint16
	name               string
	valueLength        uint32
	valueOffset        uint16
	nonResident        bool
	lowestVCN          uint64
	highestVCN         uint64
	mappingPairsOffset uint16
	allocatedSize      uint64
	dataSize           uint64
	initializedSize    uint64
	runs               []dataRun
	value              []byte
}

type standardInformation struct {
	createdTime  uint64
	modifiedTime uint64
	mftTime      uint64
	accessedTime uint64
	fileFlags    uint32
}

type fileNameAttribute struct {
	parentReference uint64
	createdTime     uint64
	modifiedTime    uint64
	mftTime         uint64
	accessedTime    uint64
	allocatedSize   uint64
	realSize        uint64
	fileFlags       uint32
	namespace       uint8
	name            string
}

type dataAttribute struct {
	attributeID uint16
	flags       uint16
	resident    bool
	dataSize    uint64
	runs        []dataRun
}

type mftRecord struct {
	header              mftRecordHeader
	attributes          []attributeHeader
	standardInformation *standardInformation
	fileNames           []fileNameAttribute
	data                *dataAttribute
	indexRoot           []byte
	indexAllocation     *attributeHeader
	bitmap              *attributeHeader
}

func parseMFTRecord(record []byte) (mftRecord, error) {
	header, err := parseMFTRecordHeader(record)
	if err != nil {
		return mftRecord{}, err
	}

	parsed := mftRecord{header: header}
	for offset := int(header.firstAttributeOffset); ; {
		if offset+4 > int(header.usedSize) {
			return mftRecord{}, errors.New("ntfs: MFT record has no attribute end marker")
		}
		typeCode := binary.LittleEndian.Uint32(record[offset:])
		if typeCode == attributeTypeEnd {
			return parsed, nil
		}

		attribute, value, err := parseAttribute(record[:header.usedSize], offset)
		if err != nil {
			return mftRecord{}, err
		}
		parsed.attributes = append(parsed.attributes, attribute)

		switch attribute.typeCode {
		case attributeTypeAttributeList:
			return mftRecord{}, errors.New("ntfs: ATTRIBUTE_LIST attributes are unsupported")
		case attributeTypeStandardInformation:
			if parsed.standardInformation != nil {
				return mftRecord{}, errors.New("ntfs: duplicate STANDARD_INFORMATION attribute")
			}
			information, err := parseStandardInformation(value)
			if err != nil {
				return mftRecord{}, err
			}
			parsed.standardInformation = &information
		case attributeTypeFileName:
			fileName, err := parseFileName(value)
			if err != nil {
				return mftRecord{}, err
			}
			parsed.fileNames = append(parsed.fileNames, fileName)
		case attributeTypeData:
			if attribute.name != "" {
				break
			}
			if parsed.data != nil {
				return mftRecord{}, errors.New("ntfs: duplicate unnamed DATA attribute")
			}
			parsed.data = &dataAttribute{
				attributeID: attribute.attributeID,
				flags:       attribute.flags,
				resident:    !attribute.nonResident,
				dataSize:    attribute.dataSize,
				runs:        attribute.runs,
			}
		case attributeTypeIndexRoot:
			if attribute.nonResident {
				return mftRecord{}, errors.New("ntfs: non-resident INDEX_ROOT is invalid")
			}
			parsed.indexRoot = append([]byte(nil), value...)
		case attributeTypeIndexAllocation:
			a := attribute
			parsed.indexAllocation = &a
		case attributeTypeBitmap:
			a := attribute
			parsed.bitmap = &a
		}
		offset += int(attribute.length)
	}
}

func parseAttribute(record []byte, offset int) (attributeHeader, []byte, error) {
	if offset < 0 || offset+16 > len(record) {
		return attributeHeader{}, nil, errors.New("ntfs: truncated attribute header")
	}
	length := binary.LittleEndian.Uint32(record[offset+4 : offset+8])
	if length < 16 || length%8 != 0 || uint64(offset)+uint64(length) > uint64(len(record)) {
		return attributeHeader{}, nil, fmt.Errorf("ntfs: invalid attribute length %d at offset %d", length, offset)
	}
	if record[offset+8] == 0 {
		return parseResidentAttribute(record, offset)
	}
	if record[offset+8] != 1 || length < nonResidentAttributeHeaderSize {
		return attributeHeader{}, nil, errors.New("ntfs: invalid non-resident attribute header")
	}
	a := record[offset : offset+int(length)]
	nameLength, nameOffset := int(a[9]), int(binary.LittleEndian.Uint16(a[10:12]))
	name, err := decodeAttributeName(a, nameOffset, nameLength)
	if err != nil {
		return attributeHeader{}, nil, err
	}
	mappingOffset := binary.LittleEndian.Uint16(a[32:34])
	if mappingOffset < nonResidentAttributeHeaderSize || int(mappingOffset) >= len(a) {
		return attributeHeader{}, nil, errors.New("ntfs: mapping pairs exceed attribute bounds")
	}
	lowest, highest := binary.LittleEndian.Uint64(a[16:24]), binary.LittleEndian.Uint64(a[24:32])
	runs, err := parseDataRuns(a[mappingOffset:], lowest, highest)
	if err != nil {
		return attributeHeader{}, nil, err
	}
	h := attributeHeader{typeCode: binary.LittleEndian.Uint32(a), length: length, flags: binary.LittleEndian.Uint16(a[12:14]), attributeID: binary.LittleEndian.Uint16(a[14:16]), name: name, nonResident: true, lowestVCN: lowest, highestVCN: highest, mappingPairsOffset: mappingOffset, allocatedSize: binary.LittleEndian.Uint64(a[40:48]), dataSize: binary.LittleEndian.Uint64(a[48:56]), initializedSize: binary.LittleEndian.Uint64(a[56:64]), runs: runs}
	if h.initializedSize > h.dataSize || h.dataSize > h.allocatedSize {
		return attributeHeader{}, nil, errors.New("ntfs: invalid non-resident attribute sizes")
	}
	return h, nil, nil
}

func parseMFTRecordHeader(record []byte) (mftRecordHeader, error) {
	if len(record) < mftRecordHeaderSize {
		return mftRecordHeader{}, errors.New("ntfs: truncated MFT record header")
	}
	if string(record[:len(fileRecordMagic)]) != fileRecordMagic {
		return mftRecordHeader{}, errors.New("ntfs: invalid FILE record signature")
	}

	usedSize := binary.LittleEndian.Uint32(record[24:28])
	allocatedSize := binary.LittleEndian.Uint32(record[28:32])
	firstAttributeOffset := binary.LittleEndian.Uint16(record[20:22])
	if allocatedSize > uint32(len(record)) || usedSize > allocatedSize {
		return mftRecordHeader{}, fmt.Errorf("ntfs: invalid MFT record sizes: used %d, allocated %d, buffer %d", usedSize, allocatedSize, len(record))
	}
	if firstAttributeOffset < mftRecordHeaderSize || firstAttributeOffset%8 != 0 || uint32(firstAttributeOffset)+4 > usedSize {
		return mftRecordHeader{}, fmt.Errorf("ntfs: invalid first attribute offset %d", firstAttributeOffset)
	}

	updateSequenceOffset := binary.LittleEndian.Uint16(record[4:6])
	updateSequenceCount := binary.LittleEndian.Uint16(record[6:8])
	if updateSequenceCount != 0 {
		sequenceBytes := uint32(updateSequenceCount) * 2
		if updateSequenceOffset < mftRecordHeaderSize || uint32(updateSequenceOffset)+sequenceBytes > usedSize {
			return mftRecordHeader{}, errors.New("ntfs: update sequence array exceeds MFT record bounds")
		}
	}

	return mftRecordHeader{
		firstAttributeOffset: firstAttributeOffset,
		flags:                binary.LittleEndian.Uint16(record[22:24]),
		usedSize:             usedSize,
		allocatedSize:        allocatedSize,
		baseRecordReference:  binary.LittleEndian.Uint64(record[32:40]),
		recordNumber:         binary.LittleEndian.Uint32(record[44:48]),
	}, nil
}

func parseResidentAttribute(record []byte, offset int) (attributeHeader, []byte, error) {
	if offset < 0 || offset+residentAttributeHeaderSize > len(record) {
		return attributeHeader{}, nil, errors.New("ntfs: truncated attribute header")
	}
	length := binary.LittleEndian.Uint32(record[offset+4 : offset+8])
	if length < residentAttributeHeaderSize || length%8 != 0 || uint64(offset)+uint64(length) > uint64(len(record)) {
		return attributeHeader{}, nil, fmt.Errorf("ntfs: invalid attribute length %d at offset %d", length, offset)
	}
	if record[offset+8] != 0 {
		return attributeHeader{}, nil, fmt.Errorf("ntfs: non-resident attribute %#x is unsupported", binary.LittleEndian.Uint32(record[offset:offset+4]))
	}

	nameLength := int(record[offset+9])
	nameOffset := int(binary.LittleEndian.Uint16(record[offset+10 : offset+12]))
	name, err := decodeAttributeName(record[offset:offset+int(length)], nameOffset, nameLength)
	if err != nil {
		return attributeHeader{}, nil, err
	}
	valueLength := binary.LittleEndian.Uint32(record[offset+16 : offset+20])
	valueOffset := binary.LittleEndian.Uint16(record[offset+20 : offset+22])
	if valueOffset < residentAttributeHeaderSize || uint64(valueOffset)+uint64(valueLength) > uint64(length) {
		return attributeHeader{}, nil, errors.New("ntfs: resident attribute value exceeds attribute bounds")
	}

	header := attributeHeader{
		typeCode:    binary.LittleEndian.Uint32(record[offset : offset+4]),
		length:      length,
		flags:       binary.LittleEndian.Uint16(record[offset+12 : offset+14]),
		attributeID: binary.LittleEndian.Uint16(record[offset+14 : offset+16]),
		name:        name,
		valueLength: valueLength,
		valueOffset: valueOffset,
		dataSize:    uint64(valueLength),
	}
	start := offset + int(valueOffset)
	header.value = append([]byte(nil), record[start:start+int(valueLength)]...)
	return header, record[start : start+int(valueLength)], nil
}

type dataRun struct {
	VCN      uint64
	LCN      int64
	Clusters uint64
	Sparse   bool
}

func parseDataRuns(mapping []byte, lowestVCN, highestVCN uint64) ([]dataRun, error) {
	if highestVCN < lowestVCN {
		return nil, errors.New("ntfs: invalid data-run VCN range")
	}
	vcn, lcn := lowestVCN, int64(0)
	var runs []dataRun
	for pos := 0; ; {
		if pos >= len(mapping) {
			return nil, errors.New("ntfs: truncated data runs")
		}
		descriptor := mapping[pos]
		pos++
		if descriptor == 0 {
			break
		}
		lenBytes, offBytes := int(descriptor&0x0f), int(descriptor>>4)
		if lenBytes == 0 || lenBytes > 8 || offBytes > 8 || pos+lenBytes+offBytes > len(mapping) {
			return nil, errors.New("ntfs: malformed data run")
		}
		clusters, err := decodeUnsignedLE(mapping[pos : pos+lenBytes])
		if err != nil || clusters == 0 {
			return nil, errors.New("ntfs: invalid data-run length")
		}
		pos += lenBytes
		if clusters > ^uint64(0)-vcn {
			return nil, errors.New("ntfs: data-run VCN overflow")
		}
		run := dataRun{VCN: vcn, Clusters: clusters, Sparse: offBytes == 0}
		if offBytes != 0 {
			delta := decodeSignedLE(mapping[pos : pos+offBytes])
			pos += offBytes
			if delta > 0 && lcn > math.MaxInt64-delta || delta < 0 && lcn < math.MinInt64-delta {
				return nil, errors.New("ntfs: data-run LCN overflow")
			}
			lcn += delta
			if lcn < 0 {
				return nil, errors.New("ntfs: data run has negative absolute LCN")
			}
			run.LCN = lcn
		}
		runs = append(runs, run)
		vcn += clusters
	}
	if len(runs) == 0 || vcn-1 != highestVCN {
		return nil, errors.New("ntfs: data runs do not match VCN range")
	}
	return runs, nil
}
func decodeUnsignedLE(b []byte) (uint64, error) {
	if len(b) > 8 {
		return 0, errors.New("overflow")
	}
	var v uint64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v, nil
}
func decodeSignedLE(b []byte) int64 {
	var v uint64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	if len(b) < 8 && b[len(b)-1]&0x80 != 0 {
		v |= ^uint64(0) << (uint(len(b)) * 8)
	}
	return int64(v)
}

func decodeAttributeName(attribute []byte, offset, codeUnits int) (string, error) {
	if codeUnits == 0 {
		return "", nil
	}
	byteLength := codeUnits * 2
	if offset < residentAttributeHeaderSize || offset > len(attribute) || byteLength > len(attribute)-offset {
		return "", errors.New("ntfs: attribute name exceeds attribute bounds")
	}
	encoded := make([]uint16, codeUnits)
	for i := range encoded {
		encoded[i] = binary.LittleEndian.Uint16(attribute[offset+i*2:])
	}
	return string(utf16.Decode(encoded)), nil
}

func parseStandardInformation(value []byte) (standardInformation, error) {
	if len(value) < 48 {
		return standardInformation{}, errors.New("ntfs: truncated STANDARD_INFORMATION attribute")
	}
	return standardInformation{
		createdTime:  binary.LittleEndian.Uint64(value[0:8]),
		modifiedTime: binary.LittleEndian.Uint64(value[8:16]),
		mftTime:      binary.LittleEndian.Uint64(value[16:24]),
		accessedTime: binary.LittleEndian.Uint64(value[24:32]),
		fileFlags:    binary.LittleEndian.Uint32(value[32:36]),
	}, nil
}

func parseFileName(value []byte) (fileNameAttribute, error) {
	if len(value) < 66 {
		return fileNameAttribute{}, errors.New("ntfs: truncated FILE_NAME attribute")
	}
	nameLength := int(value[64])
	if nameLength*2 > len(value)-66 {
		return fileNameAttribute{}, errors.New("ntfs: FILE_NAME value has a truncated name")
	}
	encoded := make([]uint16, nameLength)
	for i := range encoded {
		encoded[i] = binary.LittleEndian.Uint16(value[66+i*2:])
	}
	return fileNameAttribute{
		parentReference: binary.LittleEndian.Uint64(value[0:8]),
		createdTime:     binary.LittleEndian.Uint64(value[8:16]),
		modifiedTime:    binary.LittleEndian.Uint64(value[16:24]),
		mftTime:         binary.LittleEndian.Uint64(value[24:32]),
		accessedTime:    binary.LittleEndian.Uint64(value[32:40]),
		allocatedSize:   binary.LittleEndian.Uint64(value[40:48]),
		realSize:        binary.LittleEndian.Uint64(value[48:56]),
		fileFlags:       binary.LittleEndian.Uint32(value[56:60]),
		namespace:       value[65],
		name:            string(utf16.Decode(encoded)),
	}, nil
}
