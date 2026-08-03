package ext4

import (
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

func TestNewXattrPreservesFullInodeBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		external bool
		refcount uint32
		valueLen int
		wantErr  string
	}{
		{"no external block", false, 0, 32, "requires an external block"},
		{"external fallback", true, 1, 32, ""},
		{"shared external block", true, 2, 32, "shared"},
		{"external capacity exceeded", true, 1, testBlockSize, "capacity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := syntheticImage()
			inodeOff := 5*testBlockSize + 2*256
			put16(img.data, inodeOff+128, 4)
			original := bytes.Repeat([]byte("s"), 72)
			body, err := encodeXattrs(256-132, 4, map[string][]byte{"security.selinux": original})
			if err != nil {
				t.Fatal(err)
			}
			copy(img.data[inodeOff+132:inodeOff+256], body)
			if tc.external {
				put32(img.data, inodeOff+104, 20)
				block, err := encodeXattrs(testBlockSize, 32, map[string][]byte{"trusted.original": []byte("keep")})
				if err != nil {
					t.Fatal(err)
				}
				put32(block, 4, tc.refcount)
				copy(img.data[20*testBlockSize:], block)
			}
			fsi, err := New(img)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := fsi.Open("/hello.txt")
			if err != nil {
				t.Fatal(err)
			}
			stream := entry.(filesystem.NamedStreamCapable)
			before := bytes.Clone(img.data)
			value := bytes.Repeat([]byte("n"), tc.valueLen)
			err = stream.WriteStream("user.new", value)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("WriteStream error = %v; want %q", err, tc.wantErr)
				}
				if !bytes.Equal(img.data, before) {
					t.Fatal("rejected write changed the image")
				}
				if _, err := stream.ReadStream("user.new"); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("rejected attribute read error = %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(img.data[inodeOff:inodeOff+256], before[inodeOff:inodeOff+256]) {
					t.Fatal("external fallback changed the inode body")
				}
				got, err := stream.ReadStream("user.new")
				if err != nil || !bytes.Equal(got, value) {
					t.Fatalf("new attribute = %q, %v", got, err)
				}
				// Deleting the newly added attribute must also preserve both regions.
				if err := stream.DeleteStream("user.new"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := stream.ReadStream("security.selinux")
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("original inode attribute = %q, %v", got, err)
			}
			if tc.external {
				got, err := stream.ReadStream("trusted.original")
				if err != nil || string(got) != "keep" {
					t.Fatalf("original external attribute = %q, %v", got, err)
				}
			}
		})
	}
}

func TestXattrReplacementCapacityFailurePreservesImage(t *testing.T) {
	for _, external := range []bool{false, true} {
		name := "inode body"
		if external {
			name = "external block"
		}
		t.Run(name, func(t *testing.T) {
			img := syntheticImage()
			inodeOff := 5*testBlockSize + 2*256
			put16(img.data, inodeOff+128, 4)
			size, header, offset := 256-132, 4, inodeOff+132
			if external {
				put32(img.data, inodeOff+104, 20)
				size, header, offset = testBlockSize, 32, 20*testBlockSize
			}
			attrs := map[string][]byte{"user.original": []byte("old"), "security.selinux": []byte("label")}
			region, err := encodeXattrs(size, header, attrs)
			if err != nil {
				t.Fatal(err)
			}
			copy(img.data[offset:offset+size], region)
			fsi, err := New(img)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := fsi.Open("/hello.txt")
			if err != nil {
				t.Fatal(err)
			}
			stream := entry.(filesystem.NamedStreamCapable)
			before := bytes.Clone(img.data)
			if err := stream.WriteStream("user.original", bytes.Repeat([]byte("x"), size)); err == nil || !strings.Contains(err.Error(), "capacity") {
				t.Fatalf("replacement error = %v; want capacity error", err)
			}
			if !bytes.Equal(before, img.data) {
				t.Fatal("rejected replacement changed the image")
			}
			for name, want := range attrs {
				got, err := stream.ReadStream(name)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("preserved %s = %q, %v", name, got, err)
				}
			}
		})
	}
}
