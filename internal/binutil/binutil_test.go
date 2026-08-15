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

func TestInt(t *testing.T) {
	tests := []struct {
		name    string
		b       []byte
		size    int
		order   binary.ByteOrder
		want    int64
		wantErr error
	}{
		{"positive size1", []byte{0x7f}, 1, binary.LittleEndian, 0x7f, nil},
		{"negative size1", []byte{0xff}, 1, binary.LittleEndian, -1, nil},
		{"negative size2 le", []byte{0xfe, 0xff}, 2, binary.LittleEndian, -2, nil},
		{"negative size3 le", []byte{0xff, 0xff, 0xff}, 3, binary.LittleEndian, -1, nil},
		{"negative size3 be", []byte{0xff, 0xff, 0xff}, 3, binary.BigEndian, -1, nil},
		{"positive size3", []byte{0x01, 0x00, 0x00}, 3, binary.LittleEndian, 1, nil},
		{"negative size6", []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 6, binary.LittleEndian, -1, nil},
		{"negative size8", []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 8, binary.LittleEndian, -1, nil},
		{"size0", []byte{0x00}, 0, binary.LittleEndian, 0, ErrSize},
		{"range", []byte{0x00}, 2, binary.LittleEndian, 0, ErrRange},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Int(tc.b, 0, tc.size, tc.order)
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
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBits(t *testing.T) {
	tests := []struct {
		name    string
		v       uint64
		lo      int
		width   int
		want    uint64
		wantErr error
	}{
		{"lo0 width4", 0b1010, 0, 4, 0b1010, nil},
		{"crossing byte", 0b1_10000000, 7, 2, 0b11, nil},
		{"width64 lo0", ^uint64(0), 0, 64, ^uint64(0), nil},
		{"single bit", 0b0100, 2, 1, 1, nil},
		{"overflow", 1, 63, 2, 0, ErrRange},
		{"width0", 1, 0, 0, 0, ErrRange},
		{"negative lo", 1, -1, 1, 0, ErrRange},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Bits(tc.v, tc.lo, tc.width)
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
				t.Fatalf("got %b, want %b", got, tc.want)
			}
		})
	}
}
