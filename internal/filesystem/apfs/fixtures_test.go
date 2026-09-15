package apfs

import "testing"

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
