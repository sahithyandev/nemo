//go:build linux

package ext4

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"golang.org/x/sys/unix"
)

func TestLiveChangeTimeReadableButNotWritable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host")
	if err := os.WriteFile(path, []byte("host content"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := openLiveEntry(path, false)
	if err != nil {
		t.Fatal(err)
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	got, err := entry.Timestamp(filesystem.TimeChanged)
	want := time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec).UTC()
	if err != nil || !got.Equal(want) {
		t.Fatalf("change time = %v, %v; want %v", got, err, want)
	}
	for _, field := range []filesystem.TimeField{filesystem.TimeCreated, filesystem.TimeChanged} {
		if supported, err := entry.SupportsTimestamp(field); supported || err != nil {
			t.Fatalf("%s write capability = %v, %v", field, supported, err)
		}
		if err := entry.SetTimestamp(field, time.Unix(100, 0)); !errors.Is(err, filesystem.ErrUnsupported) {
			t.Fatalf("%s write should be unsupported: %v", field, err)
		}
	}
	if err := syscall.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	if !time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec).Equal(want) {
		t.Fatal("unsupported write changed inode metadata")
	}
}

func TestLiveCreationTimeStatx(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := openLiveEntry(path, false)
	if err != nil {
		t.Fatal(err)
	}
	var stat unix.Statx_t
	err = unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME, &stat)
	got, readErr := entry.Timestamp(filesystem.TimeCreated)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || (err == nil && stat.Mask&unix.STATX_BTIME == 0) {
		if !errors.Is(readErr, filesystem.ErrUnsupported) {
			t.Fatalf("missing birth time should be unsupported: %v", readErr)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	want := time.Unix(stat.Btime.Sec, int64(stat.Btime.Nsec)).UTC()
	if readErr != nil || !got.Equal(want) {
		t.Fatalf("birth time = %v, %v; want %v", got, readErr, want)
	}
}

func TestLiveTimestampUpdatePreservesOtherField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := openLiveEntry(path, false)
	if err != nil {
		t.Fatal(err)
	}
	access := time.Unix(123456789, 123456789)
	modified := time.Unix(987654321, 987654321)
	if err := entry.SetTimestamp(filesystem.TimeAccessed, access); err != nil {
		t.Fatal(err)
	}
	if err := entry.SetTimestamp(filesystem.TimeModified, modified); err != nil {
		t.Fatal(err)
	}
	got, err := entry.Timestamp(filesystem.TimeAccessed)
	if err != nil || !got.Equal(access) {
		t.Fatalf("modification update changed access time: %v, %v", got, err)
	}
	if err := entry.SetTimestamp(filesystem.TimeAccessed, access.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err = entry.Timestamp(filesystem.TimeModified)
	if err != nil || !got.Equal(modified) {
		t.Fatalf("access update changed modification time: %v, %v", got, err)
	}
	if _, err := entry.Timestamp("invalid"); err == nil {
		t.Fatal("invalid timestamp field accepted")
	}
}
