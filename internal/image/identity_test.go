//go:build unix || windows

package image

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupIdentityReopenAndCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, make([]byte, 512), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	want, err := first.BackupIdentity()
	if err != nil || want == "" {
		t.Fatalf("identity = %q, %v", want, err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.BackupIdentity()
	if err != nil || got != want {
		t.Fatalf("reopen = %q, %v; want %q", got, err, want)
	}
	if _, err := first.WriteAt([]byte{1}, 0); err != nil {
		t.Fatal(err)
	}
	got, err = first.BackupIdentity()
	if err != nil || got != want {
		t.Fatalf("write changed identity = %q, %v", got, err)
	}
	copyPath := path + "-copy"
	if err := os.WriteFile(copyPath, make([]byte, 512), 0600); err != nil {
		t.Fatal(err)
	}
	copied, err := Open(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	got, err = copied.BackupIdentity()
	if err != nil || got == want {
		t.Fatalf("copy identity = %q, %v", got, err)
	}
}

func TestBackupIdentityHardlinkAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, make([]byte, 512), 0600); err != nil {
		t.Fatal(err)
	}
	alias := path + "-link"
	if err := os.Link(path, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	want, err := first.BackupIdentity()
	if err != nil {
		t.Fatal(err)
	}
	got, err := second.BackupIdentity()
	if err != nil || got != want {
		t.Fatalf("hardlink = %q, %v; want %q", got, err, want)
	}
}

func TestBackupIdentityExt4GenerationAndTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ext4.img")
	data := make([]byte, 4096)
	// Minimal identity metadata, with two distinct immutable UUIDs.
	data[1024] = 1
	data[1028] = 1
	data[1080] = 0x53
	data[1081] = 0xef
	data[1128] = 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	first, err := img.BackupIdentity()
	if err != nil {
		t.Fatal(err)
	}
	data[1128] = 2
	// Truncate/rewrite preserves the file object, as cp over an existing image does.
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	second, err := img.BackupIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("foreign ext4 generation reused backup identity")
	}
	if err := os.Truncate(path, 1144); err != nil {
		t.Fatal(err)
	}
	if identity, err := img.BackupIdentity(); err == nil || identity != "" {
		t.Fatalf("truncated known ext4 identity = %q, %v", identity, err)
	}
}
