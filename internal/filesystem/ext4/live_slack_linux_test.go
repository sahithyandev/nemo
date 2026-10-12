//go:build linux

package ext4

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
)

func TestLiveSlackMountinfoEscapesAndBindRoot(t *testing.T) {
	text := "20 1 8:2 / / rw - ext4 /dev/sda2 rw\n" +
		"21 20 8:2 /subdir /mnt/bind\\040space rw shared:2 - ext4 /dev/disk\\134alias rw\n" +
		"22 21 8:3 / /mnt/bind\\040space/nested rw - ext4 /dev/sda3 rw\n"
	mounts, err := parseLiveSlackMounts(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	m, err := selectLiveSlackMount(mounts, "/mnt/bind space/nested/file", 22, 8, 3)
	if err != nil || m.id != 22 {
		t.Fatalf("nested mount = %+v, %v", m, err)
	}
	m, err = selectLiveSlackMount(mounts, "/mnt/bind space/nested.txt", 21, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	if m.source != `/dev/disk\alias` {
		t.Fatalf("source = %q", m.source)
	}
	path, err := mountRelativePath(m, "/mnt/bind space/nested.txt")
	if err != nil || path != "/subdir/nested.txt" {
		t.Fatalf("bind target = %q, %v", path, err)
	}
	for _, outside := range []string{"/mnt/bind space-other/file", "/mnt/bind space/../outside", "/elsewhere", "relative"} {
		if _, err := mountRelativePath(m, outside); err == nil {
			t.Fatalf("accepted outside target %q", outside)
		}
	}
	path, err = mountRelativePath(m, m.point)
	if err != nil || path != "/subdir" {
		t.Fatalf("bind root = %q, %v", path, err)
	}
	for _, tc := range []struct {
		id           uint64
		major, minor uint32
	}{{99, 8, 2}, {21, 8, 3}} {
		if _, err := selectLiveSlackMount(mounts, "/mnt/bind space/file", tc.id, tc.major, tc.minor); err == nil {
			t.Fatal("accepted mismatched mount/device identity")
		}
	}
	mounts[1].kind = "tmpfs"
	if _, err := selectLiveSlackMount(mounts, "/mnt/bind space/file", 21, 8, 2); err == nil {
		t.Fatal("accepted non-ext4 mount")
	}
}

func TestLiveSlackMountinfoRejectsMalformedRecords(t *testing.T) {
	for _, text := range []string{
		"garbage", "1 0 bad / / rw - ext4 /dev/sda rw", "x 0 8:2 / / rw - ext4 /dev/sda rw",
		"1 0 8:x / / rw - ext4 /dev/sda rw", "1 0 8:2 relative / rw - ext4 /dev/sda rw",
		`1 0 8:2 / /mnt/\999 rw - ext4 /dev/sda rw`, `1 0 8:2 / /mnt/\04 rw - ext4 /dev/sda rw`,
	} {
		if _, err := parseLiveSlackMounts(strings.NewReader(text)); err == nil {
			t.Fatalf("accepted malformed mountinfo %q", text)
		}
	}
	got, err := unescapeMountPath(`/x\040y\011z\012a\134b`)
	if err != nil || got != "/x y\tz\na\\b" {
		t.Fatalf("decoded path = %q, %v", got, err)
	}
}

func TestLiveSlackMountInjectedImageIsReadOnlyAndMapsBindTarget(t *testing.T) {
	img := syntheticImage()
	mount := liveSlackMount{root: "/subdir", point: "/mnt/bind", source: "/dev/example", kind: "ext4"}
	closes := 0
	opener := func(liveSlackMount) (imagepkg.Image, func() error, error) {
		return img, func() error { closes++; return nil }, nil
	}
	// A wrapper that returns the raw writable image cannot disable the guard.
	wrapper := func(_ imagepkg.Image, closeFn func() error) (imagepkg.Image, func() error) { return img, closeFn }
	fs, raw, closeFn, err := openLiveSlackMount("/mnt/bind/nested.txt", "/mnt/bind/nested.txt", mount, 5, opener, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Root().Path() != "/subdir/nested.txt" {
		t.Fatalf("root = %q", fs.Root().Path())
	}
	entry, err := fs.Open("/mnt/bind/nested.txt")
	if err != nil || entry.Path() != "/subdir/nested.txt" {
		t.Fatalf("Open = %v, %v", entry, err)
	}
	before := bytes.Clone(img.data)
	if _, err := raw.WriteAt([]byte{1}, 0); !errors.Is(err, imagepkg.ErrReadOnly) {
		t.Fatalf("raw write error = %v", err)
	}
	if err := entry.(filesystem.NamedStreamCapable).WriteStream("user.test", []byte("no")); !errors.Is(err, imagepkg.ErrReadOnly) {
		t.Fatalf("entry write error = %v", err)
	}
	if !bytes.Equal(before, img.data) {
		t.Fatal("read-only mounted view changed image")
	}
	if _, err := fs.Open("/mnt/bind-other/nested.txt"); err == nil {
		t.Fatal("prefix sibling accepted")
	}
	if closes != 0 {
		t.Fatal("device closed before successful result")
	}
	if err := closeFn(); err != nil || closes != 1 {
		t.Fatalf("close count=%d err=%v", closes, err)
	}
}

func TestLiveSlackOpenClosesOnParserPathAndInodeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		inode        uint64
		corrupt      bool
	}{
		{"invalid image", "/mnt/hello.txt", 3, true},
		{"outside mount", "/mnt-other/hello.txt", 3, false},
		{"missing raw path", "/mnt/missing", 3, false},
		{"stale inode", "/mnt/hello.txt", 9, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := syntheticImage()
			if tc.corrupt {
				img.data[1024+0x38] = 0
			}
			closes := 0
			opener := func(liveSlackMount) (imagepkg.Image, func() error, error) {
				return img, func() error { closes++; return nil }, nil
			}
			_, _, _, err := openLiveSlackMount(tc.target, tc.target, liveSlackMount{point: "/mnt", root: "/"}, tc.inode, opener, nil)
			if err == nil || closes != 1 {
				t.Fatalf("error=%v closes=%d", err, closes)
			}
		})
	}
}

func TestLiveSlackDeviceRefusesRegularFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, syntheticImage().data, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := openVerifiedSlackDevice(liveSlackMount{source: path})
	if err == nil || !strings.Contains(err.Error(), "matching block device") {
		t.Fatalf("regular file device error=%v", err)
	}
}

func TestLiveSlackMountedWritesRefusedBeforeOpeningTarget(t *testing.T) {
	_, _, _, err := OpenLiveSlack("/does-not-exist", true, nil)
	if !errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("mounted write error = %v", err)
	}
}

func TestLiveSlackReadOnlyDisposableImageOpener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ext4.img")
	original := syntheticImage().data
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	closed := false
	opener := func(liveSlackMount) (imagepkg.Image, func() error, error) {
		raw, err := imagepkg.OpenReadOnly(path)
		if err != nil {
			return nil, nil, err
		}
		return raw, func() error { closed = true; return raw.Close() }, nil
	}
	fs, img, closeFn, err := openLiveSlackMount("/mnt/hello.txt", "/mnt/hello.txt", liveSlackMount{root: "/", point: "/mnt"}, 3, opener, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Type() != filesystem.TypeEXT4 {
		t.Fatalf("Type=%s", fs.Type())
	}
	if _, err := img.WriteAt([]byte("forbidden"), 0); !errors.Is(err, imagepkg.ErrReadOnly) {
		t.Fatalf("write error=%v", err)
	}
	if err := closeFn(); err != nil || !closed {
		t.Fatalf("close=%v closed=%v", err, closed)
	}
	if _, err := img.ReadAt(make([]byte, 4), 0); err == nil {
		t.Fatal("closed device still readable")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("image changed; err=%v", err)
	}
}
