//go:build !windows

package ntfs

import (
	"fmt"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
	"runtime"
)

// OpenLive is unavailable outside Windows; image mode remains portable.
func OpenLive(path string, write bool, wrap LiveWrap) (filesystem.FileSystem, image.Image, func() error, error) {
	return nil, nil, nil, fmt.Errorf("ntfs live mode requires Windows (this is %s): %w", runtime.GOOS, filesystem.ErrUnsupported)
}
