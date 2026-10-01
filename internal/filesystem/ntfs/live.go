package ntfs

import (
	"fmt"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

// LiveWrap applies the caller's image policy before parsing. The returned
// close function owns the device, just as in APFS live slack-space mode.
type LiveWrap func(image.Image, func() error) (image.Image, func() error)

func parseLive(raw image.Image, closeRaw func() error, wrap LiveWrap) (filesystem.FileSystem, image.Image, func() error, error) {
	img := image.ReadOnly(raw)
	closeFn := closeRaw
	if wrap != nil {
		img, closeFn = wrap(img, closeRaw)
		if img == nil || closeFn == nil {
			if closeFn != nil {
				_ = closeFn()
			} else {
				_ = closeRaw()
			}
			return nil, nil, nil, fmt.Errorf("ntfs: invalid live image wrapper")
		}
	}
	img = image.ReadOnly(img)
	fs, err := New(img)
	if err != nil {
		_ = closeFn()
		return nil, nil, nil, fmt.Errorf("ntfs: parse live volume: %w", err)
	}
	return fs, img, closeFn, nil
}
