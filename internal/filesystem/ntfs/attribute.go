package ntfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

const (
	mftRecordHeaderSize         = 48
	residentAttributeHeaderSize = 24

	attributeTypeStandardInformation = uint32(0x10)
	attributeTypeFileName            = uint32(0x30)
	attributeTypeData                = uint32(0x80)
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
	typeCode    uint32
	length      uint32
	flags       uint16
	attributeID uint16
	name        string
	valueLength uint32
	valueOffset uint16
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
}

type mftRecord struct {
	header              mftRecordHeader
	attributes          []attributeHeader
	standardInformation *standardInformation
	fileNames           []fileNameAttribute
	data                *dataAttribute
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

		attribute, value, err := parseResidentAttribute(record[:header.usedSize], offset)
		if err != nil {
			return mftRecord{}, err
		}
		parsed.attributes = append(parsed.attributes, attribute)

		switch attribute.typeCode {
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
				return mftRecord{}, errors.New("ntfs: named DATA attributes are unsupported")
			}
			if parsed.data != nil {
				return mftRecord{}, errors.New("ntfs: duplicate unnamed DATA attribute")
			}
			parsed.data = &dataAttribute{
				attributeID: attribute.attributeID,
				flags:       attribute.flags,
				resident:    true,
				dataSize:    uint64(attribute.valueLength),
			}
		}
		offset += int(attribute.length)
	}
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
	}
	start := offset + int(valueOffset)
	return header, record[start : start+int(valueLength)], nil
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
