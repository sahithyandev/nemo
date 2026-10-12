//go:build linux

package ext4

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/sahithyandev/nemo/internal/filesystem"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
	"golang.org/x/sys/unix"
)

type liveSlackMount struct {
	id                        uint64
	major, minor              uint32
	root, point, source, kind string
}

// OpenLiveSlack reads the verified ext4 block device behind a mounted target.
// Mounted raw writes are always refused. Reads are not a consistent snapshot:
// unflushed changes and concurrent filesystem activity can invalidate results.
func OpenLiveSlack(target string, write bool, wrap func(imagepkg.Image, func() error) (imagepkg.Image, func() error)) (filesystem.FileSystem, imagepkg.Image, func() error, error) {
	if write {
		return nil, nil, nil, LiveSlackError()
	}
	fd, stat, err := openLiveFD(target, true)
	if err != nil {
		return nil, nil, nil, err
	}
	defer syscall.Close(fd)
	var sx unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &sx); err != nil {
		return nil, nil, nil, fmt.Errorf("ext4 live slack: cannot verify target mount identity with statx: %w", err)
	}
	if sx.Mask&unix.STATX_MNT_ID == 0 {
		return nil, nil, nil, errors.New("ext4 live slack: kernel does not report target mount identity")
	}
	resolved, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ext4 live slack: resolve opened target: %w", err)
	}
	// A deleted descriptor has no safely resolvable on-disk pathname.
	if strings.HasSuffix(resolved, " (deleted)") {
		return nil, nil, nil, errors.New("ext4 live slack: target was deleted or has an ambiguous deleted pathname")
	}
	mounts, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ext4 live slack: read mountinfo: %w", err)
	}
	records, parseErr := parseLiveSlackMounts(mounts)
	_ = mounts.Close()
	if parseErr != nil {
		return nil, nil, nil, parseErr
	}
	mount, err := selectLiveSlackMount(records, resolved, sx.Mnt_id, unix.Major(uint64(stat.Dev)), unix.Minor(uint64(stat.Dev)))
	if err != nil {
		return nil, nil, nil, err
	}
	return openLiveSlackMount(target, resolved, mount, stat.Ino, openVerifiedSlackDevice, wrap)
}

func parseLiveSlackMounts(r io.Reader) ([]liveSlackMount, error) {
	var mounts []liveSlackMount
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) < sep+4 {
			return nil, errors.New("ext4 live slack: malformed mountinfo record")
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ext4 live slack: invalid mount ID: %w", err)
		}
		device := strings.Split(fields[2], ":")
		if len(device) != 2 {
			return nil, errors.New("ext4 live slack: invalid mount device number")
		}
		major, err := strconv.ParseUint(device[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("ext4 live slack: invalid device major: %w", err)
		}
		minor, err := strconv.ParseUint(device[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("ext4 live slack: invalid device minor: %w", err)
		}
		root, err := unescapeMountPath(fields[3])
		if err != nil {
			return nil, err
		}
		point, err := unescapeMountPath(fields[4])
		if err != nil {
			return nil, err
		}
		source, err := unescapeMountPath(fields[sep+2])
		if err != nil {
			return nil, err
		}
		if !filepath.IsAbs(root) || !filepath.IsAbs(point) {
			return nil, errors.New("ext4 live slack: mount root and point must be absolute")
		}
		mounts = append(mounts, liveSlackMount{id: id, major: uint32(major), minor: uint32(minor), root: root, point: point, source: source, kind: fields[sep+1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ext4 live slack: read mountinfo: %w", err)
	}
	return mounts, nil
}

func unescapeMountPath(s string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			out.WriteByte(s[i])
			continue
		}
		if i+3 >= len(s) {
			return "", errors.New("ext4 live slack: truncated mountinfo escape")
		}
		escape := s[i+1 : i+4]
		switch escape {
		case "040":
			out.WriteByte(' ')
		case "011":
			out.WriteByte('\t')
		case "012":
			out.WriteByte('\n')
		case "134":
			out.WriteByte('\\')
		default:
			return "", fmt.Errorf("ext4 live slack: unsupported mountinfo escape %q", escape)
		}
		i += 3
	}
	return out.String(), nil
}

func mountRelativePath(m liveSlackMount, target string) (string, error) {
	if !filepath.IsAbs(target) {
		return "", errors.New("ext4 live slack: target path must be absolute")
	}
	rel, err := filepath.Rel(m.point, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("ext4 live slack: %q is outside mount %q", target, m.point)
	}
	return filepath.Join(m.root, rel), nil
}

func selectLiveSlackMount(mounts []liveSlackMount, target string, mountID uint64, major, minor uint32) (liveSlackMount, error) {
	var selected *liveSlackMount
	for i := range mounts {
		m := &mounts[i]
		if _, err := mountRelativePath(*m, target); err != nil {
			continue
		}
		// statx identifies the opened file's mount, including stacked/bind mounts.
		if m.id != mountID {
			continue
		}
		if selected == nil || len(m.point) > len(selected.point) {
			selected = m
		}
	}
	if selected == nil {
		return liveSlackMount{}, errors.New("ext4 live slack: opened target mount is absent from mountinfo; retry after mount changes")
	}
	if selected.kind != "ext4" || selected.major != major || selected.minor != minor {
		return liveSlackMount{}, errors.New("ext4 live slack: target mount filesystem/device identity mismatch")
	}
	if !filepath.IsAbs(selected.source) {
		return liveSlackMount{}, errors.New("ext4 live slack: mount source is not an absolute block-device path")
	}
	return *selected, nil
}

type slackDeviceOpener func(liveSlackMount) (imagepkg.Image, func() error, error)

// openVerifiedSlackDevice verifies the opened descriptor, then reopens that same
// descriptor through procfs for image.OpenReadOnly; path replacement cannot select
// a different device between verification and the raw read.
func openVerifiedSlackDevice(m liveSlackMount) (imagepkg.Image, func() error, error) {
	// Device aliases (e.g. /dev/mapper/name) are common; resolve them before the
	// no-follow open and validate the resulting descriptor, not just its path.
	source, err := filepath.EvalSymlinks(m.source)
	if err != nil {
		return nil, nil, classifySlackDeviceError(m.source, err)
	}
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, classifySlackDeviceError(m.source, err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, nil, fmt.Errorf("ext4 live slack: stat device: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFBLK || unix.Major(uint64(stat.Rdev)) != m.major || unix.Minor(uint64(stat.Rdev)) != m.minor {
		return nil, nil, errors.New("ext4 live slack: mount source is not the matching block device")
	}
	raw, err := imagepkg.OpenReadOnly(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return nil, nil, classifySlackDeviceError(m.source, err)
	}
	return raw, raw.Close, nil
}

func classifySlackDeviceError(source string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("ext4 live slack: read-only access to block device %q denied; grant device read permission or run with appropriate privileges: %w", source, err)
	}
	return fmt.Errorf("ext4 live slack: open block device %q for reading: %w", source, err)
}

func openLiveSlackMount(target, resolved string, mount liveSlackMount, inode uint64, opener slackDeviceOpener, wrap func(imagepkg.Image, func() error) (imagepkg.Image, func() error)) (filesystem.FileSystem, imagepkg.Image, func() error, error) {
	raw, closeFn, err := opener(mount)
	if err != nil {
		return nil, nil, nil, err
	}
	img := imagepkg.ReadOnly(raw)
	if wrap != nil {
		img, closeFn = wrap(img, closeFn)
	}
	// Enforce read-only even if a custom wrapper exposes WriteAt.
	img = imagepkg.ReadOnly(img)
	fsi, err := New(img)
	if err != nil {
		_ = closeFn()
		return nil, nil, nil, fmt.Errorf("ext4 live slack: parse mounted device (view may be stale): %w", err)
	}
	path, err := mountRelativePath(mount, resolved)
	if err != nil {
		_ = closeFn()
		return nil, nil, nil, err
	}
	entry, err := fsi.Open(path)
	if err != nil {
		_ = closeFn()
		return nil, nil, nil, fmt.Errorf("ext4 live slack: find target on device (view may be stale): %w", err)
	}
	if uint64(entry.(*Entry).inode) != inode {
		_ = closeFn()
		return nil, nil, nil, errors.New("ext4 live slack: mounted target inode differs from raw device; retry on a stable or offline filesystem")
	}
	return &liveSlackFS{underlying: fsi, mount: mount, target: target, resolved: resolved, root: entry}, img, closeFn, nil
}

type liveSlackFS struct {
	underlying       filesystem.FileSystem
	mount            liveSlackMount
	target, resolved string
	root             filesystem.Entry
}

func (f *liveSlackFS) Type() filesystem.Type  { return f.underlying.Type() }
func (f *liveSlackFS) Root() filesystem.Entry { return f.root }
func (f *liveSlackFS) Open(target string) (filesystem.Entry, error) {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	// The original argument may include parent symlinks. Only the already
	// descriptor-verified target can use its canonical path without re-resolving.
	if target == f.target {
		absolute = f.resolved
	}
	path, err := mountRelativePath(f.mount, absolute)
	if err != nil {
		return nil, err
	}
	return f.underlying.Open(path)
}
