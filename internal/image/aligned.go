//go:build darwin

// Read-only access to a raw (character) block device, used for live
// slack-space detect on macOS. The buffered device (RawImage, elsewhere in
// this package) is refused by the kernel for both read and write while its
// volume is mounted, because opening it creates a second, independent view
// into blocks the mount's own buffer cache already manages. The raw device
// bypasses that cache entirely, at the cost of requiring every read to land
// on a sector boundary: AlignedRawDevice's ReadAt handles that by rounding
// each request out to the device's block size and copying back only the
// bytes actually asked for.
package image

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// dkiocGetBlockSize and dkiocGetBlockCount are DKIOCGETBLOCKSIZE and
// DKIOCGETBLOCKCOUNT from <sys/disk.h>, hand-encoded via that header's
// _IOR('d', N, T) macro since golang.org/x/sys/unix exposes neither.
const (
	dkiocGetBlockSize  = 0x40046418 // _IOR('d', 24, uint32_t)
	dkiocGetBlockCount = 0x40086419 // _IOR('d', 25, uint64_t)
)

// AlignedRawDevice is a read-only Image backed by a raw character device
// (e.g. /dev/rdisk5), satisfying arbitrary byte-range reads by rounding
// each one out to the device's sector size.
type AlignedRawDevice struct {
	file      *os.File
	path      string
	size      int64
	blockSize int64
}

var _ Image = (*AlignedRawDevice)(nil)

// OpenRawReadOnly opens path, a raw character device, for read-only,
// block-aligned access. The device's block size and block count come from
// DKIOCGETBLOCKSIZE/DKIOCGETBLOCKCOUNT: unlike a regular file, a raw device
// doesn't report a usable size through Stat.
func OpenRawReadOnly(path string) (*AlignedRawDevice, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}

	fd := int(file.Fd())
	blockSize, err := unix.IoctlGetInt(fd, dkiocGetBlockSize)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("get block size of %q: %w", path, err)
	}
	if blockSize <= 0 {
		file.Close()
		return nil, fmt.Errorf("%q reports a non-positive block size (%d)", path, blockSize)
	}
	blockCount, err := unix.IoctlGetInt(fd, dkiocGetBlockCount)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("get block count of %q: %w", path, err)
	}
	if blockCount <= 0 {
		file.Close()
		return nil, fmt.Errorf("%q reports a non-positive block count (%d)", path, blockCount)
	}

	return &AlignedRawDevice{
		file:      file,
		path:      path,
		size:      int64(blockSize) * int64(blockCount),
		blockSize: int64(blockSize),
	}, nil
}

// ReadAt reads len(p) bytes starting at off. The underlying device read is
// rounded out to a sector-aligned range; only the requested sub-slice is
// copied into p. Tolerates a short read at the end of the device, like
// RawImage.ReadAt.
func (a *AlignedRawDevice) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("read %q at negative offset %d", a.path, off)
	}
	if len(p) == 0 {
		return 0, nil
	}

	alignedOff := (off / a.blockSize) * a.blockSize
	alignedEnd := ((off + int64(len(p)) + a.blockSize - 1) / a.blockSize) * a.blockSize

	buf := make([]byte, alignedEnd-alignedOff)
	n, err := a.file.ReadAt(buf, alignedOff)
	if err != nil && n == 0 {
		return 0, err
	}
	buf = buf[:n]

	start := off - alignedOff
	if start >= int64(len(buf)) {
		return 0, io.EOF
	}
	copied := copy(p, buf[start:])
	if copied < len(p) {
		return copied, io.EOF
	}
	return copied, nil
}

// WriteAt always fails: AlignedRawDevice exists only for live slack-space
// detect, never hide or clear.
func (a *AlignedRawDevice) WriteAt([]byte, int64) (int, error) {
	return 0, errors.New("apfs: AlignedRawDevice is read-only")
}

func (a *AlignedRawDevice) Size() int64  { return a.size }
func (a *AlignedRawDevice) Path() string { return a.path }
func (a *AlignedRawDevice) Close() error { return a.file.Close() }
