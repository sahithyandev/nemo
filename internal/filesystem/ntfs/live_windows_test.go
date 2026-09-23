//go:build windows

package ntfs

import (
	"bytes"
	"errors"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLivePaths(t *testing.T) {
	for _, p := range []string{`C:\`, `c:\dir\file.txt`, `D:/file`, `\\.\C:`, `\\?\Volume{01234567-89ab-cdef-0123-456789abcdef}\`} {
		if err := validateLivePath(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"", "relative", `C:relative`, `\rooted`, `\\server\share`, `\\.\PhysicalDrive0`, `\\.\C:\file`, `\\?\C:\file`, `C:\file:stream`, "C:\\nul\x00", `\\?\Volume{bad}`, `C:\*.txt`} {
		f, img, closeFn, err := OpenLive(p, false, nil)
		if !errors.Is(err, fs.ErrInvalid) || f != nil || img != nil || closeFn != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
}

func TestLiveWritesUnsupported(t *testing.T) {
	f, img, closeFn, err := OpenLive(`\\.\C:`, true, func(image.Image, func() error) (image.Image, func() error) {
		t.Fatal("write reached wrapper")
		return nil, nil
	})
	if !errors.Is(err, filesystem.ErrUnsupported) || f != nil || img != nil || closeFn != nil {
		t.Fatal(err)
	}
}

func TestResolveLiveVolume(t *testing.T) {
	dir := t.TempDir()
	device, mount, err := resolveLiveVolume(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !volumeGUID.MatchString(device) || mount == "" {
		t.Fatalf("device=%q mount=%q", device, mount)
	}
	if _, _, err := resolveLiveVolume(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestVolumeImageReadsAndParser(t *testing.T) {
	data := syntheticNTFSImage().data
	p := filepath.Join(t.TempDir(), "volume.img")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	v := &volumeImage{file: f, size: int64(len(data)), sector: 4096}
	defer v.Close()
	for _, tc := range []struct {
		off    int64
		length int
	}{{0, 512}, {13, 5000}, {int64(len(data) - 9), 15}, {int64(len(data)), 1}} {
		buf := make([]byte, tc.length)
		n, err := v.ReadAt(buf, tc.off)
		want := min(tc.length, len(data)-int(tc.off))
		if n != want || !bytes.Equal(buf[:n], data[tc.off:tc.off+int64(n)]) {
			t.Fatalf("read %+v: n=%d", tc, n)
		}
		if want < tc.length && !errors.Is(err, io.EOF) || want == tc.length && err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.ReadAt(make([]byte, 1), -1); !errors.Is(err, fs.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := v.WriteAt([]byte{1}, 0); !errors.Is(err, image.ErrReadOnly) {
		t.Fatal(err)
	}
	parsed, err := New(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Open("/Dir/file.txt"); err != nil {
		t.Fatal(err)
	}
}
