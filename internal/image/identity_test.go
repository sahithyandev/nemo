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
