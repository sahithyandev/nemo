//go:build !windows

package ntfs

import (
	"errors"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
	"testing"
)

func TestOpenLiveUnsupported(t *testing.T) {
	for _, write := range []bool{false, true} {
		for _, path := range []string{"", `C:\file.txt`, `\\.\C:`} {
			f, img, closeFn, err := OpenLive(path, write, func(image.Image, func() error) (image.Image, func() error) {
				t.Fatal("wrapper called")
				return nil, nil
			})
			if !errors.Is(err, filesystem.ErrUnsupported) || f != nil || img != nil || closeFn != nil {
				t.Fatalf("OpenLive(%q,%v): %v", path, write, err)
			}
		}
	}
}
