package apfs

import (
	"bytes"
	"encoding/binary"
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

// insertFileExtent inserts one synthetic FILE_EXTENT record for objID
// directly through the package's own unguarded btree insert path.
func insertFileExtent(t *testing.T, fs *FS, objID, logical, length, phys uint64) {
	t.Helper()
	key := make([]byte, 16)
	copy(key[0:8], encodeJKey(objID, objTypeFileExtent))
	binary.LittleEndian.PutUint64(key[8:16], logical)
	val := make([]byte, 24)
	binary.LittleEndian.PutUint64(val[0:8], length)
	binary.LittleEndian.PutUint64(val[8:16], phys)

	err := fs.fsTree.rewriteLeaf(key, func(recs []record, _ bool) ([]record, int, error) {
		idx := len(recs)
		for i := range recs {
			if oid, typ, kerr := decodeJKey(recs[i].key); kerr == nil && oid == objID && typ == objTypeFileExtent {
				if binary.LittleEndian.Uint64(recs[i].key[8:16]) > logical {
					idx = i
					break
				}
				continue
			}
			if fsKeyCompare(recs[i].key, key) > 0 {
				idx = i
				break
			}
		}
		newRecs := make([]record, 0, len(recs)+1)
		newRecs = append(newRecs, recs[:idx]...)
		newRecs = append(newRecs, record{key: key, val: val})
		newRecs = append(newRecs, recs[idx:]...)
		return newRecs, 0, nil
	})
	if err != nil {
		t.Fatalf("insertFileExtent(logical=%d): %v", logical, err)
	}
}

// TestFragmentedStream fabricates a stream-backed xattr whose data stream
// spans two non-contiguous FILE_EXTENT records, inserted directly through
// the package's own btree internals, and confirms extentsOf concatenates
// them correctly in read order. A real macOS fixture forced into this
// shape was tried first (fill the volume solid, delete every other block,
// write a multi-block value into what's left) and repeatedly came back
// with a single contiguous extent regardless — APFS's allocator apparently
// finds room elsewhere even when the intended free run is a fine
// checkerboard, so this exercises the same read path deterministically
// instead of depending on a specific allocator's behavior.
func TestFragmentedStream(t *testing.T) {
	fs := openFS(t, loadImage(t, "apfs-gpt"))
	e := entryFor(t, fs, "/bigxattr.txt")

	const objID = 999999999
	blockSize := uint64(fs.blockSize)
	total := int64(fs.img.Size())
	// Two blocks well before the tail (GPT backup header/table) and far
	// past any real content this fixture's small file set could reach.
	physA := uint64(total)/blockSize - 100
	physB := physA + 1

	first := bytes.Repeat([]byte("F"), int(blockSize))
	second := bytes.Repeat([]byte("S"), int(blockSize))
	if _, err := fs.img.WriteAt(first, int64(physA*blockSize)); err != nil {
		t.Fatalf("WriteAt physA: %v", err)
	}
	if _, err := fs.img.WriteAt(second, int64(physB*blockSize)); err != nil {
		t.Fatalf("WriteAt physB: %v", err)
	}

	// A new, otherwise-unused oid gets its own leaf on insert, which has
	// room. Retargeting user.nemo.big's own dstream record to it, below, is
	// an in-place replace (same record count, same 52-byte value shape) on
	// an existing record, so it doesn't need any of that headroom itself —
	// every one of this fixture set's existing leaves is already packed
	// too tight for the encodeLeaf write path (which can't split a node)
	// to add a new record to.
	insertFileExtent(t, fs, objID, 0, blockSize, physA)
	insertFileExtent(t, fs, objID, blockSize, blockSize, physB)

	ds := xattrDStream{objID: objID, size: 2 * blockSize, allocedSize: 2 * blockSize}
	if err := fs.replaceXattr(e.oid, "user.nemo.big", encodeStreamXattrVal(ds)); err != nil {
		t.Fatalf("replaceXattr: %v", err)
	}

	exts, err := fs.extentsOf(objID)
	if err != nil {
		t.Fatalf("extentsOf: %v", err)
	}
	if len(exts) != 2 {
		t.Fatalf("len(extents) = %d, want 2", len(exts))
	}

	got, err := e.ReadStream("user.nemo.big")
	if err != nil {
		t.Fatalf("ReadStream: %v", err)
	}
	want := append(append([]byte(nil), first...), second...)
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadStream returned %d bytes, want %d matching bytes", len(got), len(want))
	}
}

// TestSystemOwnedXattrRefused inserts a filesystem-owned xattr record (the
// flag real APFS sets on, e.g., com.apple.decmpfs) directly through the
// package's own unguarded btree insert path, then confirms the public
// WriteStream/DeleteStream API refuses to touch it. macOS's builtin
// ditto --hfsCompression, the natural way to get a real decmpfs xattr onto
// a fixture, turns out not to actually compress freshly written content on
// current macOS, so there's no reliable way to produce this on disk with
// only builtin tooling; a synthetic record exercises the same flag check.
func TestSystemOwnedXattrRefused(t *testing.T) {
	fs := openFS(t, loadImage(t, "apfs-gpt"))
	e := entryFor(t, fs, "/hello.txt")

	val := encodeEmbeddedXattrVal([]byte("x"))
	binary.LittleEndian.PutUint16(val[0:2], xattrDataEmbedded|xattrFileSystemOwned)
	if err := fs.insertXattr(e.oid, "com.apple.decmpfs", val); err != nil {
		t.Fatalf("insertXattr: %v", err)
	}

	names, err := e.NamedStreams()
	if err != nil {
		t.Fatalf("NamedStreams: %v", err)
	}
	if !contains(names, "com.apple.decmpfs") {
		t.Fatalf("NamedStreams = %v, want it to include com.apple.decmpfs", names)
	}

	if err := e.WriteStream("com.apple.decmpfs", []byte("y")); err == nil {
		t.Fatalf("WriteStream(com.apple.decmpfs): expected an error, got nil")
	}
	if err := e.DeleteStream("com.apple.decmpfs"); err == nil {
		t.Fatalf("DeleteStream(com.apple.decmpfs): expected an error, got nil")
	}
}
