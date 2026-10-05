package image

import (
	"fmt"
	"golang.org/x/sys/windows"
)

// BackupIdentity uses the opened handle's volume and file ID, avoiding path
// races and allowing symlink/hardlink aliases of the same backing file.
func (r *RawImage) BackupIdentity() (string, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(r.file.Fd()), &info); err != nil {
		return "", err
	}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if size == 0 {
		size = uint64(r.size)
	}
	return fmt.Sprintf("file-windows-v1:%d:%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow, size), nil
}
