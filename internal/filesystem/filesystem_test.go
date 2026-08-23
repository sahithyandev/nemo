package filesystem_test

import (
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

// These tests exercise the filesystem package's contracts via the fakefs
// implementation; fakefs's own tests cover its behavior in depth.

func TestFakeFileSystemSatisfiesContract(t *testing.T) {
	fs := fakefs.New("/", "/test.txt")
	fs.FSType = filesystem.TypeNTFS

	if fs.Type() != filesystem.TypeNTFS {
		t.Fatalf("expected filesystem type %s, got %s", filesystem.TypeNTFS, fs.Type())
	}

	if fs.Root() == nil || fs.Root().Path() != "/" {
		t.Fatal("expected root entry at /")
	}

	entry, err := fs.Open("/test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if entry.Path() != "/test.txt" {
		t.Fatalf("expected /test.txt, got %s", entry.Path())
	}
}

func TestFakeFileSystemOpenMissingEntry(t *testing.T) {
	fs := fakefs.New()

	entry, err := fs.Open("/missing.txt")
	if err == nil {
		t.Fatal("expected error for missing entry")
	}

	if entry != nil {
		t.Fatal("expected nil entry for missing path")
	}
}
