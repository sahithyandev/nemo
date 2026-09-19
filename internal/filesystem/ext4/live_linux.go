//go:build linux

package ext4

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

const linuxExt4Magic = 0xef53

// LiveFS uses Linux file APIs against a mounted ext4 filesystem.
type LiveFS struct{ root *LiveEntry }

type LiveEntry struct {
	path        string
	isDir       bool
	requireExt4 bool
}

var _ filesystem.FileSystem = (*LiveFS)(nil)
var _ filesystem.Entry = (*LiveEntry)(nil)
var _ filesystem.NamedStreamCapable = (*LiveEntry)(nil)
var _ filesystem.TimestompCapable = (*LiveEntry)(nil)

// OpenLive validates the target's mounted filesystem before returning a live view.
func OpenLive(target string) (filesystem.FileSystem, error) {
	entry, err := openLiveEntry(target, true)
	if err != nil {
		return nil, err
	}
	return &LiveFS{root: entry}, nil
}

func (*LiveFS) Type() filesystem.Type    { return filesystem.TypeEXT4 }
func (f *LiveFS) Root() filesystem.Entry { return f.root }
func (*LiveFS) Open(path string) (filesystem.Entry, error) {
	return openLiveEntry(path, true)
}

func openLiveEntry(path string, requireExt4 bool) (*LiveEntry, error) {
	fd, stat, err := openLiveFD(path, requireExt4)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	return &LiveEntry{path: path, isDir: stat.Mode&syscall.S_IFMT == syscall.S_IFDIR, requireExt4: requireExt4}, nil
}

// Opening with O_NOFOLLOW rejects the final symlink, including during a path race.
// Metadata calls below use /proc/self/fd to keep the operation on that opened inode.
func openLiveFD(path string, requireExt4 bool) (int, syscall.Stat_t, error) {
	var stat syscall.Stat_t
	if path == "" {
		return -1, stat, errors.New("ext4 live mode: target path is empty")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return -1, stat, fmt.Errorf("ext4 live mode: symlink targets are unsupported: %q", path)
		}
		return -1, stat, fmt.Errorf("ext4 live mode: open %q: %w", path, err)
	}
	if err := syscall.Fstat(fd, &stat); err != nil {
		syscall.Close(fd)
		return -1, stat, fmt.Errorf("ext4 live mode: stat %q: %w", path, err)
	}
	if kind := stat.Mode & syscall.S_IFMT; kind != syscall.S_IFREG && kind != syscall.S_IFDIR {
		syscall.Close(fd)
		return -1, stat, fmt.Errorf("ext4 live mode: only regular files and directories are supported: %q", path)
	}
	if requireExt4 {
		var info syscall.Statfs_t
		if err := syscall.Fstatfs(fd, &info); err != nil {
			syscall.Close(fd)
			return -1, stat, fmt.Errorf("ext4 live mode: inspect filesystem for %q: %w", path, err)
		}
		if uint64(info.Type) != linuxExt4Magic {
			syscall.Close(fd)
			return -1, stat, fmt.Errorf("ext4 live mode: %q is not on ext4", path)
		}
	}
	return fd, stat, nil
}

func (e *LiveEntry) Path() string { return e.path }
func (e *LiveEntry) IsDir() bool  { return e.isDir }
func (e *LiveEntry) Children() ([]filesystem.Entry, error) {
	if !e.isDir {
		return nil, fmt.Errorf("ext4 live mode: %q is not a directory", e.path)
	}
	items, err := os.ReadDir(e.path)
	if err != nil {
		return nil, err
	}
	children := make([]filesystem.Entry, 0, len(items))
	for _, item := range items {
		child, err := openLiveEntry(filepath.Join(e.path, item.Name()), e.requireExt4)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, nil
}

func (e *LiveEntry) withFD(fn func(string, syscall.Stat_t) error) error {
	if e == nil {
		return errors.New("ext4 live mode: nil entry")
	}
	fd, stat, err := openLiveFD(e.path, e.requireExt4)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	return fn("/proc/self/fd/"+strconv.Itoa(fd), stat)
}

func (e *LiveEntry) NamedStreams() ([]string, error) {
	var names []string
	err := e.withFD(func(path string, _ syscall.Stat_t) error {
		for attempt := 0; attempt < 3; attempt++ {
			size, err := syscall.Listxattr(path, nil)
			if err != nil {
				return err
			}
			if size == 0 {
				names = []string{}
				return nil
			}
			buf := make([]byte, size)
			n, err := syscall.Listxattr(path, buf)
			if errors.Is(err, syscall.ERANGE) {
				continue
			}
			if err != nil {
				return err
			}
			for _, name := range strings.Split(strings.TrimSuffix(string(buf[:n]), "\x00"), "\x00") {
				if strings.HasPrefix(name, "user.") {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			return nil
		}
		return errors.New("xattr list changed during read")
	})
	return names, err
}

func validateLiveXattr(name string) error {
	if _, _, err := splitXattrName(name); err != nil {
		return err
	}
	if !strings.HasPrefix(name, "user.") {
		return fmt.Errorf("ext4 live mode: only user.* xattrs are supported: %q", name)
	}
	return nil
}

func (e *LiveEntry) ReadStream(name string) ([]byte, error) {
	if err := validateLiveXattr(name); err != nil {
		return nil, err
	}
	var data []byte
	err := e.withFD(func(path string, _ syscall.Stat_t) error {
		for attempt := 0; attempt < 3; attempt++ {
			size, err := syscall.Getxattr(path, name, nil)
			if err != nil {
				return err
			}
			if size == 0 {
				data = []byte{}
				return nil
			}
			buf := make([]byte, size)
			n, err := syscall.Getxattr(path, name, buf)
			if errors.Is(err, syscall.ERANGE) {
				continue
			}
			if err != nil {
				return err
			}
			data = buf[:n]
			return nil
		}
		return errors.New("xattr changed during read")
	})
	return data, err
}

func (e *LiveEntry) WriteStream(name string, data []byte) error {
	if err := validateLiveXattr(name); err != nil {
		return err
	}
	return e.withFD(func(path string, _ syscall.Stat_t) error { return syscall.Setxattr(path, name, data, 0) })
}

func (e *LiveEntry) DeleteStream(name string) error {
	if err := validateLiveXattr(name); err != nil {
		return err
	}
	return e.withFD(func(path string, _ syscall.Stat_t) error { return syscall.Removexattr(path, name) })
}

func (e *LiveEntry) SupportsTimestamp(field filesystem.TimeField) (bool, error) {
	switch field {
	case filesystem.TimeAccessed, filesystem.TimeModified:
		return true, nil
	case filesystem.TimeCreated:
		return false, nil
	default:
		return false, fmt.Errorf("ext4 live mode: unknown timestamp field %q", field)
	}
}

func (e *LiveEntry) Timestamp(field filesystem.TimeField) (time.Time, error) {
	if supported, err := e.SupportsTimestamp(field); err != nil {
		return time.Time{}, err
	} else if !supported {
		return time.Time{}, errors.New("ext4 live mode: creation time cannot be read with the supported Linux file APIs")
	}
	var value time.Time
	err := e.withFD(func(_ string, stat syscall.Stat_t) error {
		if field == filesystem.TimeAccessed {
			value = time.Unix(stat.Atim.Sec, stat.Atim.Nsec).UTC()
		} else {
			value = time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec).UTC()
		}
		return nil
	})
	return value, err
}

func (e *LiveEntry) SetTimestamp(field filesystem.TimeField, value time.Time) error {
	if supported, err := e.SupportsTimestamp(field); err != nil {
		return err
	} else if !supported {
		return errors.New("ext4 live mode: creation time cannot be set with Linux file APIs")
	}
	return e.withFD(func(path string, stat syscall.Stat_t) error {
		times := []syscall.Timespec{stat.Atim, stat.Mtim}
		replacement := syscall.Timespec{Sec: value.Unix(), Nsec: int64(value.Nanosecond())}
		if field == filesystem.TimeAccessed {
			times[0] = replacement
		} else {
			times[1] = replacement
		}
		return syscall.UtimesNano(path, times)
	})
}

// LiveSlackError explains why raw block slack is unavailable in live mode.
func LiveSlackError() error {
	if os.Geteuid() != 0 {
		return errors.New("ext4 live slack-space requires root privileges for raw-device access")
	}
	return errors.New("ext4 live slack-space is unsupported on a mounted filesystem: raw writes can corrupt ext4 metadata or cached data; use a disposable offline image")
}
