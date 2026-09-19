//go:build darwin

// Live mode on macOS: named streams and timestomp operate directly on a
// path through xattr and setattrlist syscalls (OpenLive), no privilege or
// volume parsing required. Slack space is different: the unused tail bytes
// of an allocated block aren't exposed by any path-based API, so it needs
// the raw block device backing the mounted volume (OpenLiveSlack), which
// the kernel only opens read-write for an unmounted device. See
// docs/architecture/live-mode.md and the "Live mode" section of
// docs/file-systems/apfs.md for what this can and can't do.
package apfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
	"golang.org/x/sys/unix"
)

// OpenLive constructs a live, path-backed FileSystem rooted at path. It
// requires no special privilege: named-stream and timestomp operations are
// ordinary syscalls against a mounted volume. It refuses a path that isn't
// on an APFS volume.
func OpenLive(path string) (filesystem.FileSystem, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("apfs: open live target %q: %w", path, err)
	}
	stat, err := statfs(path)
	if err != nil {
		return nil, fmt.Errorf("apfs: statfs %q: %w", path, err)
	}
	if fstype := cString(stat.Fstypename[:]); fstype != "apfs" {
		return nil, fmt.Errorf("apfs: %q is on a %q filesystem, not apfs", path, fstype)
	}
	return &liveFS{root: path}, nil
}

// OpenLiveSlack opens the raw block device backing path's mounted APFS
// container for slack-space access. write selects a read-write or
// read-only open of the device; macOS refuses a read-write open while the
// device's volume is mounted (EBUSY), so write is only ever satisfiable
// against an unmounted device. See docs/file-systems/apfs.md.
//
// wrap lets the caller apply its own image-wrapping policy (custody
// logging for a write, a read-only wrapper for a scan) to the freshly
// opened device before this package parses it, so a live mutation cannot
// bypass custody logging by construction: the raw device is never handed
// back unwrapped.
func OpenLiveSlack(path string, write bool, wrap func(*image.RawImage) (image.Image, func() error)) (filesystem.FileSystem, image.Image, func() error, error) {
	stat, err := statfs(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("apfs: statfs %q: %w", path, err)
	}
	device, volume, err := deviceAndVolume(stat)
	if err != nil {
		return nil, nil, nil, err
	}

	openDevice := image.OpenReadOnly
	if write {
		openDevice = image.Open
	}
	raw, err := openDevice(device)
	if err != nil {
		return nil, nil, nil, classifyDeviceError(device, err)
	}

	wrapped, closeFn := wrap(raw)
	fs, err := NewVolume(wrapped, volume)
	if err != nil {
		_ = closeFn()
		return nil, nil, nil, fmt.Errorf("apfs: open live slack space on %q (device %s): %w", path, device, err)
	}
	return fs, wrapped, closeFn, nil
}

func statfs(path string) (unix.Statfs_t, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	return stat, err
}

// deviceAndVolume derives the container's raw device path and the target
// volume's name from a mounted path's statfs result. A mounted volume's
// device is reported as e.g. /dev/disk5s1 (the volume's own slice); the
// container device slack-space needs is /dev/disk5, its slice suffix
// stripped.
func deviceAndVolume(stat unix.Statfs_t) (device, volume string, err error) {
	from := cString(stat.Mntfromname[:])
	on := cString(stat.Mntonname[:])
	if from == "" {
		return "", "", errors.New("apfs: statfs returned no source device")
	}

	base := from
	if idx := strings.LastIndexByte(from, 's'); idx > strings.LastIndexByte(from, '/') && isDigits(from[idx+1:]) {
		base = from[:idx]
	}

	if on != "" && on != "/" {
		volume = filepath.Base(on)
	}
	return base, volume, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// classifyDeviceError turns a raw device open failure into a message that
// names the actual macOS condition, rather than a bare "permission denied"
// or "resource busy" with no next step.
func classifyDeviceError(device string, err error) error {
	switch {
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
		return fmt.Errorf("live slack-space needs raw access to %s: permission denied; re-run with sudo", device)
	case errors.Is(err, unix.EBUSY):
		return fmt.Errorf("%s is busy: macOS refuses write access to the device of a mounted volume; unmount it (diskutil unmountDisk) or use --image", device)
	case errors.Is(err, unix.ENOENT):
		return fmt.Errorf("apfs: derived device %s does not exist", device)
	default:
		return fmt.Errorf("apfs: open %s: %w", device, err)
	}
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// liveFS is a path-backed FileSystem: no volume parsing, every operation is
// a syscall against liveEntry's path.
type liveFS struct {
	root string
}

var _ filesystem.FileSystem = (*liveFS)(nil)

func (f *liveFS) Type() filesystem.Type { return filesystem.TypeAPFS }

func (f *liveFS) Root() filesystem.Entry {
	return newLiveEntry(f.root)
}

func (f *liveFS) Open(path string) (filesystem.Entry, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("apfs: open live target %q: %w", path, err)
	}
	return newLiveEntry(path), nil
}

// newLiveEntry stats path (following symlinks, matching image-mode's
// treatment of a target) to learn whether it's a directory.
func newLiveEntry(path string) *liveEntry {
	isDir := false
	if info, err := os.Stat(path); err == nil {
		isDir = info.IsDir()
	}
	return &liveEntry{path: path, isDir: isDir}
}

// liveEntry is a live, path-backed filesystem entry. It implements
// NamedStreamCapable and TimestompCapable through direct syscalls, and
// deliberately does not implement SlackSpaceCapable: slack space needs the
// raw device, which OpenLiveSlack handles separately.
type liveEntry struct {
	path  string
	isDir bool
}

var (
	_ filesystem.Entry              = (*liveEntry)(nil)
	_ filesystem.NamedStreamCapable = (*liveEntry)(nil)
	_ filesystem.TimestompCapable   = (*liveEntry)(nil)
)

func (e *liveEntry) Path() string { return e.path }
func (e *liveEntry) IsDir() bool  { return e.isDir }

func (e *liveEntry) Children() ([]filesystem.Entry, error) {
	if !e.isDir {
		return nil, nil
	}
	dirEntries, err := os.ReadDir(e.path)
	if err != nil {
		return nil, fmt.Errorf("apfs: list children of %q: %w", e.path, err)
	}
	out := make([]filesystem.Entry, 0, len(dirEntries))
	for _, d := range dirEntries {
		out = append(out, newLiveEntry(filepath.Join(e.path, d.Name())))
	}
	return out, nil
}

// NamedStreams lists the extended-attribute names on e, sorted. The
// resource fork, when present, is one of these names
// (com.apple.ResourceFork): macOS exposes it as an ordinary xattr.
func (e *liveEntry) NamedStreams() ([]string, error) {
	size, err := unix.Listxattr(e.path, nil)
	if err != nil {
		return nil, fmt.Errorf("apfs: list xattrs for %q: %w", e.path, err)
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	n, err := unix.Listxattr(e.path, buf)
	if err != nil {
		return nil, fmt.Errorf("apfs: list xattrs for %q: %w", e.path, err)
	}
	var names []string
	for _, part := range bytes.Split(buf[:n], []byte{0}) {
		if len(part) > 0 {
			names = append(names, string(part))
		}
	}
	sort.Strings(names)
	return names, nil
}

// ReadStream returns the value of the xattr name on e. A missing name is
// reported as fs.ErrNotExist, matching image-mode Entry.ReadStream.
func (e *liveEntry) ReadStream(name string) ([]byte, error) {
	size, err := unix.Getxattr(e.path, name, nil)
	if err != nil {
		return nil, fmt.Errorf("apfs: read xattr %q on %q: %w", name, e.path, xattrErr(err))
	}
	if size == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, size)
	n, err := unix.Getxattr(e.path, name, buf)
	if err != nil {
		return nil, fmt.Errorf("apfs: read xattr %q on %q: %w", name, e.path, xattrErr(err))
	}
	return buf[:n], nil
}

// WriteStream creates or replaces the xattr name on e.
func (e *liveEntry) WriteStream(name string, data []byte) error {
	if err := unix.Setxattr(e.path, name, data, 0); err != nil {
		return fmt.Errorf("apfs: write xattr %q on %q: %w", name, e.path, err)
	}
	return nil
}

// DeleteStream removes the xattr name from e.
func (e *liveEntry) DeleteStream(name string) error {
	if err := unix.Removexattr(e.path, name); err != nil {
		return fmt.Errorf("apfs: delete xattr %q on %q: %w", name, e.path, xattrErr(err))
	}
	return nil
}

// xattrErr maps macOS's ENOATTR onto fs.ErrNotExist, so callers can use the
// same errors.Is(..., fs.ErrNotExist) check they'd use image-mode.
func xattrErr(err error) error {
	if errors.Is(err, unix.ENOATTR) {
		return fs.ErrNotExist
	}
	return err
}

// SetTimestamp implements filesystem.TimestompCapable. modified and
// accessed go through os.Chtimes; created (birthtime) needs setattrlist,
// which stdlib doesn't expose. changed is kernel-controlled metadata-change
// time with no live userland API to set it directly, matching the
// filesystem.TimeChanged doc comment.
func (e *liveEntry) SetTimestamp(field filesystem.TimeField, t time.Time) error {
	switch field {
	case filesystem.TimeModified:
		if err := os.Chtimes(e.path, time.Time{}, t); err != nil {
			return fmt.Errorf("apfs: set modified time on %q: %w", e.path, err)
		}
		return nil
	case filesystem.TimeAccessed:
		if err := os.Chtimes(e.path, t, time.Time{}); err != nil {
			return fmt.Errorf("apfs: set accessed time on %q: %w", e.path, err)
		}
		return nil
	case filesystem.TimeCreated:
		if err := setBirthtime(e.path, t); err != nil {
			return fmt.Errorf("apfs: set created time on %q: %w", e.path, err)
		}
		return nil
	case filesystem.TimeChanged:
		return fmt.Errorf("apfs: live changed time cannot be set directly: %w", filesystem.ErrUnsupported)
	default:
		return fmt.Errorf("apfs: unsupported timestamp field %q", field)
	}
}

// Timestamp reads one timestamp field back from e, for tests and for
// callers (not part of filesystem.TimestompCapable, which is write-only).
func (e *liveEntry) Timestamp(field filesystem.TimeField) (time.Time, error) {
	info, err := os.Stat(e.path)
	if err != nil {
		return time.Time{}, fmt.Errorf("apfs: stat %q: %w", e.path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, fmt.Errorf("apfs: unexpected stat type for %q", e.path)
	}
	switch field {
	case filesystem.TimeCreated:
		return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec), nil
	case filesystem.TimeModified:
		return time.Unix(st.Mtimespec.Sec, st.Mtimespec.Nsec), nil
	case filesystem.TimeAccessed:
		return time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec), nil
	case filesystem.TimeChanged:
		return time.Unix(st.Ctimespec.Sec, st.Ctimespec.Nsec), nil
	default:
		return time.Time{}, fmt.Errorf("apfs: unsupported timestamp field %q", field)
	}
}

// setBirthtime sets a path's creation time via setattrlist(ATTR_CMN_CRTIME),
// which os.Chtimes has no equivalent for.
func setBirthtime(path string, t time.Time) error {
	list := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_CRTIME,
	}
	// attrBuf is a native struct timespec: two little-endian int64 fields
	// (seconds, nanoseconds), 16 bytes on every 64-bit Darwin arch.
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint64(buf[0:8], uint64(t.Unix()))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(t.Nanosecond()))
	return unix.Setattrlist(path, &list, buf, 0)
}
