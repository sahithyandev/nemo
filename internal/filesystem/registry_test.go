package filesystem_test

import (
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
	"github.com/sahithyandev/nemo/internal/image"
)

func TestOpenSelectsMatchingDetector(t *testing.T) {
	img := fakefs.NewImage(16)
	copy(img.Data, []byte("CLI01"))
	want := fakefs.New("/target")
	filesystem.Register(filesystem.Detector{
		Type: filesystem.TypeEXT4,
		Sniff: func(signature []byte) bool {
			return strings.HasPrefix(string(signature), "CLI01")
		},
		New: func(got image.Image) (filesystem.FileSystem, error) {
			if got != img {
				t.Fatalf("constructor received unexpected image %T", got)
			}
			return want, nil
		},
	})

	got, err := filesystem.Open(img)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("unexpected filesystem: got %T, want %T", got, want)
	}
}

func TestOpenRejectsUnknownSignature(t *testing.T) {
	img := fakefs.NewImage(16)
	copy(img.Data, []byte("NO-MATCH"))

	_, err := filesystem.Open(img)
	if err == nil || !strings.Contains(err.Error(), "unrecognized image format") {
		t.Fatalf("unexpected error: %v", err)
	}
}
