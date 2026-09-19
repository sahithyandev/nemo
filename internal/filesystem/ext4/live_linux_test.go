//go:build linux

package ext4

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

func TestLiveNamedStreamRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a file 世界.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := openLiveEntry(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := entry.WriteStream("user.nemo-test", []byte("payload")); errors.Is(err, syscall.ENOTSUP) {
		t.Skip("temporary filesystem does not support user xattrs")
	} else if err != nil {
		t.Fatal(err)
	}
	got, err := entry.ReadStream("user.nemo-test")
	if err != nil || string(got) != "payload" {
		t.Fatalf("read stream: %q, %v", got, err)
	}
	names, err := entry.NamedStreams()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "user.nemo-test" {
		t.Fatalf("stream names: %v", names)
	}
	if err := entry.DeleteStream("user.nemo-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := entry.ReadStream("user.nemo-test"); !errors.Is(err, syscall.ENODATA) {
		t.Fatalf("expected missing xattr: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "original" {
		t.Fatalf("file content changed: %q, %v", content, err)
	}
	if err := entry.WriteStream("security.invalid", []byte("x")); err == nil || !strings.Contains(err.Error(), "only user") {
		t.Fatalf("expected namespace rejection: %v", err)
	}
}

func TestLiveTimestampRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timestamp.txt")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := openLiveEntry(path, false)
	if err != nil {
		t.Fatal(err)
	}
	access := time.Date(2020, 1, 2, 3, 4, 5, 123456789, time.UTC)
	modified := time.Date(2021, 2, 3, 4, 5, 6, 987654321, time.UTC)
	if err := entry.SetTimestamp(filesystem.TimeAccessed, access); err != nil {
		t.Fatal(err)
	}
	if err := entry.SetTimestamp(filesystem.TimeModified, modified); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		field filesystem.TimeField
		want  time.Time
	}{{filesystem.TimeAccessed, access}, {filesystem.TimeModified, modified}} {
		got, err := entry.Timestamp(tc.field)
		if err != nil || !got.Equal(tc.want) {
			t.Fatalf("%s: got %s, want %s: %v", tc.field, got, tc.want, err)
		}
	}
	if ok, err := entry.SupportsTimestamp(filesystem.TimeCreated); err != nil || ok {
		t.Fatalf("creation support: %v, %v", ok, err)
	}
	if err := entry.SetTimestamp(filesystem.TimeCreated, access); err == nil || !strings.Contains(err.Error(), "creation time") {
		t.Fatalf("expected creation time error: %v", err)
	}
}

func TestLiveRejectsSymlinksAndOtherFilesystems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regular")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openLiveEntry(link, false); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection: %v", err)
	}
	if _, err := OpenLive(path); err != nil && !strings.Contains(err.Error(), "not on ext4") {
		t.Fatalf("unexpected filesystem error: %v", err)
	}
	entry, err := openLiveEntry(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(link, path); err != nil {
		t.Fatal(err)
	}
	if err := entry.WriteStream("user.nemo-test", []byte("x")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected replaced target to be rejected: %v", err)
	}
}
