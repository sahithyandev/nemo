package ntfs

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func residentTestAttribute(typeCode uint32, value []byte, name string) []byte {
	encodedName := utf16.Encode([]rune(name))
	nameBytes := len(encodedName) * 2
	valueOffset := residentAttributeHeaderSize + nameBytes
	valueOffset = (valueOffset + 7) &^ 7
	length := (valueOffset + len(value) + 7) &^ 7
	b := make([]byte, length)
	binary.LittleEndian.PutUint32(b[0:4], typeCode)
	binary.LittleEndian.PutUint32(b[4:8], uint32(length))
	b[9] = byte(len(encodedName))
	if len(encodedName) != 0 {
		binary.LittleEndian.PutUint16(b[10:12], residentAttributeHeaderSize)
		for i, codeUnit := range encodedName {
			binary.LittleEndian.PutUint16(b[residentAttributeHeaderSize+i*2:], codeUnit)
		}
	}
	binary.LittleEndian.PutUint16(b[14:16], 7)
	binary.LittleEndian.PutUint32(b[16:20], uint32(len(value)))
	binary.LittleEndian.PutUint16(b[20:22], uint16(valueOffset))
	copy(b[valueOffset:], value)
	return b
}

func syntheticMFTRecord(attributes ...[]byte) []byte {
	record := make([]byte, 1024)
	copy(record[:4], fileRecordMagic)
	binary.LittleEndian.PutUint16(record[20:22], 56)
	binary.LittleEndian.PutUint16(record[22:24], 1)
	binary.LittleEndian.PutUint32(record[28:32], uint32(len(record)))
	binary.LittleEndian.PutUint32(record[44:48], 42)

	offset := 56
	for _, attribute := range attributes {
		copy(record[offset:], attribute)
		offset += len(attribute)
	}
	binary.LittleEndian.PutUint32(record[offset:offset+4], attributeTypeEnd)
	binary.LittleEndian.PutUint32(record[24:28], uint32(offset+4))
	return record
}

func TestParseMFTRecordResidentAttributes(t *testing.T) {
	standardValue := make([]byte, 48)
	binary.LittleEndian.PutUint64(standardValue[0:8], 11)
	binary.LittleEndian.PutUint64(standardValue[8:16], 22)
	binary.LittleEndian.PutUint64(standardValue[16:24], 33)
	binary.LittleEndian.PutUint64(standardValue[24:32], 44)
	binary.LittleEndian.PutUint32(standardValue[32:36], 0x20)

	fileNameValue := make([]byte, 66+len("test")*2)
	binary.LittleEndian.PutUint64(fileNameValue[0:8], 5)
	binary.LittleEndian.PutUint64(fileNameValue[40:48], 4096)
	binary.LittleEndian.PutUint64(fileNameValue[48:56], 1234)
	fileNameValue[64] = 4
	fileNameValue[65] = 1
	for i, codeUnit := range utf16.Encode([]rune("test")) {
		binary.LittleEndian.PutUint16(fileNameValue[66+i*2:], codeUnit)
	}

	record := syntheticMFTRecord(
		residentTestAttribute(attributeTypeStandardInformation, standardValue, ""),
		residentTestAttribute(attributeTypeFileName, fileNameValue, ""),
		residentTestAttribute(attributeTypeData, []byte{1, 2, 3}, ""),
	)
	parsed, err := parseMFTRecord(record)
	if err != nil {
		t.Fatalf("parseMFTRecord() error = %v", err)
	}
	if parsed.header.recordNumber != 42 || len(parsed.attributes) != 3 {
		t.Fatalf("header record = %d, attributes = %d", parsed.header.recordNumber, len(parsed.attributes))
	}
	if parsed.standardInformation == nil || parsed.standardInformation.createdTime != 11 || parsed.standardInformation.fileFlags != 0x20 {
		t.Fatalf("STANDARD_INFORMATION = %#v", parsed.standardInformation)
	}
	if len(parsed.fileNames) != 1 || parsed.fileNames[0].name != "test" || parsed.fileNames[0].parentReference != 5 || parsed.fileNames[0].realSize != 1234 {
		t.Fatalf("FILE_NAME = %#v", parsed.fileNames)
	}
	if parsed.data == nil || !parsed.data.resident || parsed.data.dataSize != 3 {
		t.Fatalf("DATA = %#v", parsed.data)
	}
}

func TestParseMFTRecordRejectsInvalidRecordHeader(t *testing.T) {
	tests := []struct {
		name string
		edit func([]byte)
		want string
	}{
		{"signature", func(b []byte) { copy(b[:4], "BAAD") }, "signature"},
		{"used size", func(b []byte) { binary.LittleEndian.PutUint32(b[24:28], 2048) }, "record sizes"},
		{"attribute offset", func(b []byte) { binary.LittleEndian.PutUint16(b[20:22], 49) }, "attribute offset"},
		{"update sequence", func(b []byte) {
			binary.LittleEndian.PutUint16(b[4:6], 1000)
			binary.LittleEndian.PutUint16(b[6:8], 20)
		}, "update sequence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := syntheticMFTRecord()
			tt.edit(record)
			_, err := parseMFTRecord(record)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseMFTRecord() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseMFTRecordRejectsMalformedAttributes(t *testing.T) {
	tests := []struct {
		name string
		attr func() []byte
		edit func([]byte)
		want string
	}{
		{"short length", func() []byte { return residentTestAttribute(attributeTypeData, nil, "") }, func(b []byte) { binary.LittleEndian.PutUint32(b[4:8], 8) }, "attribute length"},
		{"value bounds", func() []byte { return residentTestAttribute(attributeTypeData, nil, "") }, func(b []byte) { binary.LittleEndian.PutUint32(b[16:20], 100) }, "value exceeds"},
		{"non-resident", func() []byte { return residentTestAttribute(attributeTypeData, nil, "") }, func(b []byte) { b[8] = 1 }, "non-resident"},
		{"named data", func() []byte { return residentTestAttribute(attributeTypeData, nil, "stream") }, func([]byte) {}, "named DATA"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attribute := tt.attr()
			tt.edit(attribute)
			_, err := parseMFTRecord(syntheticMFTRecord(attribute))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseMFTRecord() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseMFTRecordRejectsTruncatedKnownValues(t *testing.T) {
	tests := []struct {
		name     string
		typeCode uint32
		value    []byte
		want     string
	}{
		{"standard information", attributeTypeStandardInformation, make([]byte, 47), "STANDARD_INFORMATION"},
		{"file name", attributeTypeFileName, make([]byte, 65), "FILE_NAME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMFTRecord(syntheticMFTRecord(residentTestAttribute(tt.typeCode, tt.value, "")))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseMFTRecord() error = %v, want %q", err, tt.want)
			}
		})
	}
}
