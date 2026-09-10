package apfs

import (
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
)

// openFS parses img as APFS or fails the test.
func openFS(t *testing.T, img *image.RawImage) *FS {
	t.Helper()
	f, err := New(img)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return f.(*FS)
}

func entryFor(t *testing.T, f *FS, path string) *Entry {
	t.Helper()
	e, err := f.Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	return e.(*Entry)
}

// TestXattrReadExisting reads the xattr the fixture generator wrote:
// `xattr -w user.nemo.test nemo xattr.txt`.
func TestXattrReadExisting(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			img := loadImage(t, name)
			e := entryFor(t, openFS(t, img), "/xattr.txt")

			names, err := e.NamedStreams()
			if err != nil {
				t.Fatalf("NamedStreams: %v", err)
			}
			if !contains(names, "user.nemo.test") {
				t.Fatalf("NamedStreams = %v, want it to include user.nemo.test", names)
			}

			v, err := e.ReadStream("user.nemo.test")
			if err != nil {
				t.Fatalf("ReadStream: %v", err)
			}
			if string(v) != "nemo" {
				t.Fatalf("value = %q, want %q", v, "nemo")
			}
		})
	}
}

func TestXattrReadMissing(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	e := entryFor(t, openFS(t, img), "/xattr.txt")
	if _, err := e.ReadStream("user.nemo.absent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadStream(missing) err = %v, want fs.ErrNotExist", err)
	}
}

// TestXattrInlineRoundTrip writes a new embedded xattr, reopens the image, and
// confirms both the new value and the untouched pre-existing one.
func TestXattrInlineRoundTrip(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			img := loadImage(t, name)

			e := entryFor(t, openFS(t, img), "/hello.txt")
			payload := []byte("the quick brown fox")
			if err := e.WriteStream("user.nemo.hidden", payload); err != nil {
				t.Fatalf("WriteStream: %v", err)
			}

			// Reopen from disk: this re-verifies every node's checksum.
			e2 := entryFor(t, openFS(t, img), "/hello.txt")
			got, err := e2.ReadStream("user.nemo.hidden")
			if err != nil {
				t.Fatalf("ReadStream after reopen: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("value = %q, want %q", got, payload)
			}

			// The unrelated xattr on xattr.txt must be byte-identical.
			other, err := entryFor(t, openFS(t, img), "/xattr.txt").ReadStream("user.nemo.test")
			if err != nil {
				t.Fatalf("ReadStream(unrelated): %v", err)
			}
			if string(other) != "nemo" {
				t.Fatalf("unrelated xattr changed to %q", other)
			}
		})
	}
}

// TestXattrReplaceValue replaces an existing embedded value with a shorter and
// then a longer one.
func TestXattrReplaceValue(t *testing.T) {
	img := loadImage(t, "apfs-gpt")

	entryFor(t, openFS(t, img), "/xattr.txt").mustWrite(t, "user.nemo.test", []byte("x"))
	if v, _ := entryFor(t, openFS(t, img), "/xattr.txt").ReadStream("user.nemo.test"); string(v) != "x" {
		t.Fatalf("after shrink: value = %q, want %q", v, "x")
	}

	long := bytes.Repeat([]byte("abcd"), 20)
	entryFor(t, openFS(t, img), "/xattr.txt").mustWrite(t, "user.nemo.test", long)
	if v, _ := entryFor(t, openFS(t, img), "/xattr.txt").ReadStream("user.nemo.test"); !bytes.Equal(v, long) {
		t.Fatalf("after grow: value len = %d, want %d", len(v), len(long))
	}
}

func TestXattrDelete(t *testing.T) {
	img := loadImage(t, "apfs-gpt")

	e := entryFor(t, openFS(t, img), "/xattr.txt")
	e.mustWrite(t, "user.nemo.keep", []byte("keep me"))

	if err := entryFor(t, openFS(t, img), "/xattr.txt").DeleteStream("user.nemo.test"); err != nil {
		t.Fatalf("DeleteStream: %v", err)
	}

	e2 := entryFor(t, openFS(t, img), "/xattr.txt")
	if _, err := e2.ReadStream("user.nemo.test"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deleted xattr still readable: %v", err)
	}
	if v, err := e2.ReadStream("user.nemo.keep"); err != nil || string(v) != "keep me" {
		t.Fatalf("sibling xattr = (%q, %v), want (%q, nil)", v, err, "keep me")
	}
}

func TestXattrDeleteMissing(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	e := entryFor(t, openFS(t, img), "/hello.txt")
	if err := e.DeleteStream("user.nemo.absent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("DeleteStream(missing) = %v, want fs.ErrNotExist", err)
	}
}

func TestXattrOversizeValueRejected(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	e := entryFor(t, openFS(t, img), "/hello.txt")
	err := e.WriteStream("user.nemo.big", make([]byte, xattrMaxEmbeddedSize+1))
	if err == nil || !strings.Contains(err.Error(), "data stream is required") {
		t.Fatalf("oversize write err = %v, want a 'data stream is required' error", err)
	}
}

// TestXattrExtentRead reads the two stream-backed values the fixture carries:
// an 8000-byte user xattr and a 6000-byte resource fork.
func TestXattrExtentRead(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			f := openFS(t, loadImage(t, name))

			big, err := entryFor(t, f, "/bigxattr.txt").ReadStream("user.nemo.big")
			if err != nil {
				t.Fatalf("ReadStream(user.nemo.big): %v", err)
			}
			if !bytes.Equal(big, bytes.Repeat([]byte("A"), 8000)) {
				t.Fatalf("user.nemo.big = %d bytes, want 8000 of 'A'", len(big))
			}

			rf, err := entryFor(t, f, "/rsrc.txt").ReadStream("com.apple.ResourceFork")
			if err != nil {
				t.Fatalf("ReadStream(resource fork): %v", err)
			}
			if !bytes.Equal(rf, bytes.Repeat([]byte("R"), 6000)) {
				t.Fatalf("resource fork = %d bytes, want 6000 of 'R'", len(rf))
			}
		})
	}
}

// TestXattrExtentWriteWithinAllocation overwrites a stream-backed value with a
// same-or-smaller payload, which fits the existing extents.
func TestXattrExtentWriteWithinAllocation(t *testing.T) {
	img := loadImage(t, "apfs-gpt")

	replacement := bytes.Repeat([]byte("Z"), 4096)
	entryFor(t, openFS(t, img), "/bigxattr.txt").mustWrite(t, "user.nemo.big", replacement)

	got, err := entryFor(t, openFS(t, img), "/bigxattr.txt").ReadStream("user.nemo.big")
	if err != nil {
		t.Fatalf("ReadStream after reopen: %v", err)
	}
	if !bytes.Equal(got, replacement) {
		t.Fatalf("value = %d bytes, want %d", len(got), len(replacement))
	}
}

func TestXattrExtentGrowRejected(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	e := entryFor(t, openFS(t, img), "/bigxattr.txt")
	err := e.WriteStream("user.nemo.big", make([]byte, 1<<20))
	if err == nil || !strings.Contains(err.Error(), "needs block allocation") {
		t.Fatalf("grow past allocation err = %v, want a 'needs block allocation' error", err)
	}
}

// TestXattrNodeFull confirms an insert that overflows the leaf fails cleanly
// and leaves the image readable.
func TestXattrNodeFull(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	e := entryFor(t, openFS(t, img), "/xattr.txt")
	err := e.WriteStream("user.nemo.toobig", make([]byte, xattrMaxEmbeddedSize))
	if err == nil || !strings.Contains(err.Error(), "node full") {
		t.Fatalf("err = %v, want a 'node full' error", err)
	}
	// The failed write must not have corrupted the tree.
	if v, err := entryFor(t, openFS(t, img), "/xattr.txt").ReadStream("user.nemo.test"); err != nil || string(v) != "nemo" {
		t.Fatalf("after rejected write: (%q, %v), want (%q, nil)", v, err, "nemo")
	}
}

// TestXattrTechniqueRoundTrip drives the named-stream technique end to end.
func TestXattrTechniqueRoundTrip(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	tech, err := technique.Get(technique.NamedStream)
	if err != nil {
		t.Fatalf("technique.Get: %v", err)
	}

	e := entryFor(t, openFS(t, img), "/hello.txt")
	payload := []byte("payload via technique layer")
	if _, err := tech.Hide(e, technique.Request{StreamName: "user.nemo.tech", Data: payload}); err != nil {
		t.Fatalf("Hide: %v", err)
	}

	e2 := entryFor(t, openFS(t, img), "/hello.txt")
	findings, err := tech.Detect(e2, technique.Request{})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var found *technique.Finding
	for i := range findings {
		if findings[i].Location == "user.nemo.tech" {
			found = &findings[i]
		}
	}
	if found == nil {
		t.Fatalf("Detect findings = %+v, want one for user.nemo.tech", findings)
	}
	if found.Size != int64(len(payload)) {
		t.Fatalf("finding size = %d, want %d", found.Size, len(payload))
	}
}

func (e *Entry) mustWrite(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := e.WriteStream(name, data); err != nil {
		t.Fatalf("WriteStream(%q): %v", name, err)
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
