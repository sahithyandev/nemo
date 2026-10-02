//go:build !linux

package ext4

import (
	"errors"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

func OpenLive(string) (filesystem.FileSystem, error) {
	return nil, errors.New("ext4 live mode is supported only on Linux")
}

func LiveSlackError() error {
	return errors.New("ext4 live slack-space is supported only on Linux")
}
