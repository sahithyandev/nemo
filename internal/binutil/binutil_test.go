package binutil

import (
	"encoding/binary"
	"testing"
)

func TestUint(t *testing.T) {
	b := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	tests := []struct {
		name    string
		off     int
		size    int
		order   binary.ByteOrder
		want    uint64
		wantErr error
	}{
		{"le size1", 0, 1, binary.LittleEndian, 0x01, nil},
		{"be size1", 0, 1, binary.BigEndian, 0x01, nil},
		{"le size2", 0, 2, binary.LittleEndian, 0x0201, nil},
		{"be size2", 0, 2, binary.BigEndian, 0x0102, nil},
		{"le size3", 0, 3, binary.LittleEndian, 0x030201, nil},
		{"be size3", 0, 3, binary.BigEndian, 0x010203, nil},
		{"le size4", 0, 4, binary.LittleEndian, 0x04030201, nil},
		{"be size4", 0, 4, binary.BigEndian, 0x01020304, nil},
		{"le size6", 0, 6, binary.LittleEndian, 0x060504030201, nil},
		{"be size6", 0, 6, binary.BigEndian, 0x010203040506, nil},
		{"le size8", 0, 8, binary.LittleEndian, 0x0807060504030201, nil},
		{"be size8", 0, 8, binary.BigEndian, 0x0102030405060708, nil},
		{"size0", 0, 0, binary.LittleEndian, 0, ErrSize},
		{"size9", 0, 9, binary.LittleEndian, 0, ErrSize},
		{"negative off", -1, 1, binary.LittleEndian, 0, ErrRange},
		{"off past end", 8, 1, binary.LittleEndian, 0, ErrRange},
		{"size past end", 6, 4, binary.LittleEndian, 0, ErrRange},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Uint(b, tc.off, tc.size, tc.order)
			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got 0x%x, want 0x%x", got, tc.want)
			}
		})
	}
}
