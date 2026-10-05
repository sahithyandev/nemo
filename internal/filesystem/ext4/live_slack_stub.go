//go:build !linux

package ext4

import (
	"fmt"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

func OpenLiveSlack(string, bool, func(image.Image, func() error) (image.Image, func() error)) (filesystem.FileSystem, image.Image, func() error, error) {
	return nil, nil, nil, fmt.Errorf("ext4 live slack-space requires Linux: %w", filesystem.ErrUnsupported)
}
