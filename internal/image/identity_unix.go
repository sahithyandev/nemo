//go:build unix

package image

import (
	"fmt"
	"syscall"
)

// BackupIdentity identifies the opened backing object across reopen and path
// aliases. Content writes do not change it; copies and replacements do.
func (r *RawImage) BackupIdentity() (string, error) {
	info, err := r.file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("backing file has no device/inode identity")
	}
	size := info.Size()
	if info.Mode().IsRegular() == false {
		size = r.size
	}
	filesystem, err := r.filesystemIdentity()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("file-unix-v1:%d:%d:%d", stat.Dev, stat.Ino, size) + filesystem, nil
}
