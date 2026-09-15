package apfs

import (
	"strings"
	"testing"
)

// TestMultiLevelTree loads apfs-manyfiles, whose root directory holds enough
// entries to force the filesystem B-tree past a single node, and confirms
// both the root node's shape and that traversal actually descends correctly.
func TestMultiLevelTree(t *testing.T) {
	fs := openFS(t, loadImage(t, "apfs-manyfiles"))

	root, err := fs.fsTree.readNode(fs.fsTree.rootPaddr)
	if err != nil {
		t.Fatalf("readNode(root): %v", err)
	}
	if root.leaf {
		t.Fatalf("root node is a leaf, want an internal node (tree should have grown past one node)")
	}
	if root.level == 0 {
		t.Fatalf("root node level = 0, want > 0")
	}

	e := entryFor(t, fs, "/f1000")
	if e.IsDir() {
		t.Fatalf("/f1000: reported as a directory")
	}

	children, err := fs.Root().Children()
	if err != nil {
		t.Fatalf("Root().Children(): %v", err)
	}
	// The standard populate() set plus 2000 empty files f0000..f1999.
	if len(children) < 2000 {
		t.Fatalf("len(children) = %d, want >= 2000", len(children))
	}
}

// TestBlockSize16K loads apfs-16k, the only fixture formatted with a
// non-default block size, and confirms both the reported size and that
// ordinary traversal still works at that size.
func TestBlockSize16K(t *testing.T) {
	fs := openFS(t, loadImage(t, "apfs-16k"))
	if fs.blockSize != 16384 {
		t.Fatalf("blockSize = %d, want 16384", fs.blockSize)
	}
	entryFor(t, fs, "/hello.txt")
}

// TestMultiVolume loads apfs-multivol, a container with two volumes, and
// confirms nemo mounts the first volume in nx_fs_oid (NEMO, carrying the
// standard populate() set) rather than the second (NEMO2).
func TestMultiVolume(t *testing.T) {
	fs := openFS(t, loadImage(t, "apfs-multivol"))
	if fs.volume.name != "NEMO" {
		t.Fatalf("volume name = %q, want %q", fs.volume.name, "NEMO")
	}
	entryFor(t, fs, "/hello.txt")
	if _, err := fs.Open("/second.txt"); err == nil {
		t.Fatalf("Open(/second.txt): expected error (that file lives on NEMO2), got nil")
	}
}

// TestEncryptedRefused loads apfs-encrypted, a real encrypted APSB volume,
// and confirms New refuses it rather than misreading unreadable content.
func TestEncryptedRefused(t *testing.T) {
	if _, err := New(loadImage(t, "apfs-encrypted")); err == nil {
		t.Fatalf("New(apfs-encrypted): expected an error, got nil")
	} else if !strings.Contains(err.Error(), "encrypt") {
		t.Fatalf("New(apfs-encrypted): err = %v, want it to mention encryption", err)
	}
}
