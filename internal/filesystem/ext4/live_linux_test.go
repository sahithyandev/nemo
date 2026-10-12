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

func TestLiveChildrenSupportsUnconditionalTraversal(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "nested")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "file.txt")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	// Bypass only the mount-type guard so this contract test also runs on tmpfs.
	entry, err := openLiveEntry(root, false)
	if err != nil {
		t.Fatal(err)
	}
	var visited []string
	var walk func(filesystem.Entry)
	walk = func(current filesystem.Entry) {
		visited = append(visited, current.Path())
		// Match cmd.walkEntries: ask every entry for children, including files.
		children, err := current.Children()
		if err != nil {
			t.Fatalf("Children(%q): %v", current.Path(), err)
		}
		for _, child := range children {
			walk(child)
		}
	}
	walk(entry)
	if len(visited) != 3 || visited[0] != root || visited[1] != directory || visited[2] != file {
		t.Fatalf("visited = %v", visited)
	}
}

func TestLiveChildrenSkipUnsupportedEntriesWithWarnings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regular"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("regular", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	names := []string{"link", "pipe"}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: filepath.Join(dir, "socket")}); err != nil {
		if !errors.Is(err, syscall.EPERM) {
			t.Fatal(err)
		}
		t.Log("socket bind forbidden in sandbox; testing symlink and FIFO")
	} else {
		names = append(names, "socket")
	}
	entry, err := openLiveEntry(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		children, err := entry.Children()
		if err != nil {
			t.Fatal(err)
		}
		if len(children) != 1 || filepath.Base(children[0].Path()) != "regular" {
			t.Fatalf("children: %v", children)
		}
		warnings := entry.TraversalWarnings()
		if len(warnings) != len(names) {
			t.Fatalf("warnings: %v", warnings)
		}
		for _, err := range warnings {
			if !errors.Is(err, filesystem.ErrUnsupported) {
				t.Fatalf("warning does not identify unsupported child: %v", err)
			}
		}
	}
	for _, name := range names {
		if _, err := openLiveEntry(filepath.Join(dir, name), false); !errors.Is(err, filesystem.ErrUnsupported) {
			t.Fatalf("explicit %s target: %v", name, err)
		}
	}
}

func TestLiveChildrenKeepOrdinaryErrorsFatal(t *testing.T) {
	entry := &LiveEntry{path: filepath.Join(t.TempDir(), "missing"), isDir: true}
	if _, err := entry.Children(); err == nil || errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("missing directory should fail: %v", err)
	}
}
