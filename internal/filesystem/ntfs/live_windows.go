//go:build windows

package ntfs

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unsafe"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
	"golang.org/x/sys/windows"
)

var volumeGUID = regexp.MustCompile(`(?i)^\\\\\?\\Volume\{[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\}$`)

// OpenLive opens a local NTFS volume read-only and reuses the image parser.
// Accepted targets are absolute local paths, drive volumes (\\.\C:), and
// volume GUIDs. Physical disks, UNC paths and arbitrary devices are rejected.
// Writes are unsupported: the image parser must never modify a mounted volume.
// The caller must close the returned handle. Reads are not a snapshot.
func OpenLive(path string, write bool, wrap LiveWrap) (filesystem.FileSystem, image.Image, func() error, error) {
	if write {
		return nil, nil, nil, fmt.Errorf("ntfs: live writes require offline image mode: %w", filesystem.ErrUnsupported)
	}
	device, _, err := resolveLiveVolume(path)
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := openVolume(device)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ntfs: open volume %q (raw reads require administrator access): %w", device, err)
	}
	f, img, closeFn, err := parseLive(raw, raw.Close, wrap)
	if err != nil {
		return nil, nil, nil, err
	}
	return &liveVolumeFS{FileSystem: f, device: device}, img, closeFn, nil
}

func driveLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

func validateLivePath(p string) error {
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("ntfs: invalid live path: %w", fs.ErrInvalid)
	}
	if len(p) == 6 && strings.HasPrefix(p, `\\.\`) && driveLetter(p[4]) && p[5] == ':' {
		return nil
	}
	if volumeGUID.MatchString(strings.TrimSuffix(p, `\`)) {
		return nil
	}
	p = strings.ReplaceAll(p, "/", `\`)
	if len(p) < 3 || !driveLetter(p[0]) || p[1:3] != `:\` || strings.ContainsAny(p[3:], `:*?"<>|`) {
		return fmt.Errorf("ntfs: expected an absolute local path or volume device, got %q: %w", p, fs.ErrInvalid)
	}
	return nil
}

func resolveLiveVolume(p string) (device, mount string, err error) {
	if err = validateLivePath(p); err != nil {
		return
	}
	if strings.HasPrefix(p, `\\.\`) {
		p = p[4:] + `\`
	}
	if volumeGUID.MatchString(strings.TrimSuffix(p, `\`)) {
		return strings.TrimSuffix(p, `\`), "", nil
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return
	}
	if err = validateLivePath(p); err != nil {
		return
	}
	name, _ := windows.UTF16PtrFromString(p)
	buf := make([]uint16, 32768)
	if err = windows.GetVolumePathName(name, &buf[0], uint32(len(buf))); err != nil {
		return
	}
	mount = windows.UTF16ToString(buf)
	root, err := windows.UTF16PtrFromString(mount)
	if err != nil {
		return "", "", err
	}
	if err = windows.GetVolumeNameForVolumeMountPoint(root, &buf[0], uint32(len(buf))); err != nil {
		return
	}
	device = strings.TrimSuffix(windows.UTF16ToString(buf), `\`)
	if !volumeGUID.MatchString(device) {
		return "", "", fmt.Errorf("ntfs: invalid resolved volume: %w", fs.ErrInvalid)
	}
	return
}

type liveVolumeFS struct {
	filesystem.FileSystem
	device string
}

func (f *liveVolumeFS) Open(p string) (filesystem.Entry, error) {
	if strings.EqualFold(strings.TrimSuffix(p, `\`), f.device) {
		return f.Root(), nil
	}
	if err := validateLivePath(p); err != nil {
		return nil, err
	}
	if strings.HasPrefix(p, `\\.\`) {
		device, _, err := resolveLiveVolume(p)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(device, f.device) {
			return nil, fmt.Errorf("ntfs: path is on another volume: %w", fs.ErrInvalid)
		}
		return f.Root(), nil
	}
	// Re-resolve each OS path so junctions and nested mounts cannot silently
	// select an entry on the wrong volume.
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	device, mount, err := resolveLiveVolume(resolved)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(device, f.device) {
		return nil, fmt.Errorf("ntfs: path is on another volume: %w", fs.ErrInvalid)
	}
	rel, err := filepath.Rel(mount, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) {
		return nil, fmt.Errorf("ntfs: path escapes volume: %w", fs.ErrInvalid)
	}
	return f.FileSystem.Open(filepath.ToSlash(rel))
}

type volumeImage struct {
	file         *os.File
	size, sector int64
}

func openVolume(device string) (*volumeImage, error) {
	name, err := windows.UTF16PtrFromString(device)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), device)
	fail := func(err error) (*volumeImage, error) { _ = f.Close(); return nil, err }
	var length [8]byte
	var geometry [24]byte
	var n uint32
	// IOCTL_DISK_GET_LENGTH_INFO and IOCTL_DISK_GET_DRIVE_GEOMETRY.
	if err = windows.DeviceIoControl(h, 0x7405c, nil, 0, &length[0], 8, &n, nil); err != nil {
		return fail(err)
	}
	if n != 8 {
		return fail(io.ErrUnexpectedEOF)
	}
	if err = windows.DeviceIoControl(h, 0x70000, nil, 0, &geometry[0], 24, &n, nil); err != nil {
		return fail(err)
	}
	if n != 24 {
		return fail(io.ErrUnexpectedEOF)
	}
	size := int64(binary.LittleEndian.Uint64(length[:]))
	sector := int64(binary.LittleEndian.Uint32(geometry[20:]))
	if size <= 0 || sector < 512 || sector > 65536 || sector&(sector-1) != 0 || size%sector != 0 {
		return fail(fmt.Errorf("ntfs: invalid volume geometry"))
	}
	return &volumeImage{file: f, size: size, sector: sector}, nil
}

func (v *volumeImage) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	if len(p) == 0 {
		return 0, nil
	}
	if off >= v.size {
		return 0, io.EOF
	}
	// Use bounded, sector-aligned buffers (including their address) for
	// noncached volume I/O. Chunking also bounds allocation for large reads.
	total := 0
	for len(p) > 0 && off < v.size {
		start := off / v.sector * v.sector
		length := min(int64(1<<20), v.size-start)
		need := min(int64(len(p)), v.size-off) + off - start
		if need < length {
			length = (need + v.sector - 1) / v.sector * v.sector
		}
		storage := make([]byte, int(length+v.sector-1))
		shift := int((-uintptr(unsafe.Pointer(&storage[0]))) & uintptr(v.sector-1))
		buf := storage[shift : shift+int(length)]
		n, err := v.file.ReadAt(buf, start)
		if int64(n) <= off-start {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return total, err
		}
		copied := copy(p, buf[off-start:n])
		total += copied
		off += int64(copied)
		p = p[copied:]
		if err != nil && len(p) > 0 {
			return total, err
		}
	}
	if len(p) > 0 {
		return total, io.EOF
	}
	return total, nil
}

func (v *volumeImage) WriteAt([]byte, int64) (int, error) { return 0, image.ErrReadOnly }
func (v *volumeImage) Size() int64                        { return v.size }
func (v *volumeImage) Path() string                       { return v.file.Name() }
func (v *volumeImage) Close() error                       { return v.file.Close() }
