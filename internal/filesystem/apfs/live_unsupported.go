//go:build !darwin

// Live mode's implementation lives in live_darwin.go, since it needs
// macOS-only syscalls (xattr, setattrlist). Every other platform gets these
// clear "unsupported here" stubs, so a Linux/Windows build stays clean
// without a live filesystem.FileSystem to construct.
package apfs

import (
	"fmt"
	"runtime"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
)

func OpenLive(path string) (filesystem.FileSystem, error) {
	return nil, fmt.Errorf("apfs live mode requires macOS (this is %s): %w", runtime.GOOS, filesystem.ErrUnsupported)
}

func OpenLiveSlack(path string, write bool, wrap func(*image.RawImage) (image.Image, func() error)) (filesystem.FileSystem, image.Image, func() error, error) {
	return nil, nil, nil, fmt.Errorf("apfs live mode requires macOS (this is %s): %w", runtime.GOOS, filesystem.ErrUnsupported)
}
