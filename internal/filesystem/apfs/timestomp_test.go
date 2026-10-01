package apfs

import (
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
)

var allTimeFields = []filesystem.TimeField{
	filesystem.TimeCreated,
	filesystem.TimeModified,
	filesystem.TimeChanged,
	filesystem.TimeAccessed,
}

// TestTimestompRoundTrip sets each field in turn and confirms it reads back
// correctly after the image is reopened (which re-verifies every node's
// checksum), and that the other three fields are untouched.
func TestTimestompRoundTrip(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			for _, field := range allTimeFields {
				t.Run(string(field), func(t *testing.T) {
					img := loadImage(t, name)
					e := entryFor(t, openFS(t, img), "/hello.txt")

					before := make(map[filesystem.TimeField]time.Time)
					for _, f := range allTimeFields {
						v, err := e.Timestamp(f)
						if err != nil {
							t.Fatalf("Timestamp(%q) before write: %v", f, err)
						}
						before[f] = v
					}

					// Sub-second precision, since that's the point of the
					// nanosecond-resolution field.
					want := time.Date(2011, 9, 9, 1, 46, 40, 123456789, time.UTC)
					if err := e.SetTimestamp(field, want); err != nil {
						t.Fatalf("SetTimestamp(%q): %v", field, err)
					}

					e2 := entryFor(t, openFS(t, img), "/hello.txt")
					got, err := e2.Timestamp(field)
					if err != nil {
						t.Fatalf("Timestamp(%q) after write: %v", field, err)
					}
					if !got.Equal(want) {
						t.Fatalf("%s = %s, want %s", field, got, want)
					}

					for _, f := range allTimeFields {
						if f == field {
							continue
						}
						v, err := e2.Timestamp(f)
						if err != nil {
							t.Fatalf("Timestamp(%q) sibling: %v", f, err)
						}
						if !v.Equal(before[f]) {
							t.Fatalf("sibling field %s changed: %s -> %s", f, before[f], v)
						}
					}

					// The write must not have corrupted the volume: the tree
					// is still walkable and the fixture's other files are
					// still there.
					children, err := openFS(t, img).Root().Children()
					if err != nil {
						t.Fatalf("Children after timestomp: %v", err)
					}
					if !contains(entryPaths(children), "/xattr.txt") {
						t.Fatalf("Children after timestomp = %v, missing /xattr.txt", entryPaths(children))
					}
				})
			}
		})
	}
}

// TestTimestompDirectory confirms a directory's INODE record, not just a
// regular file's, can be stomped.
func TestTimestompDirectory(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	root := entryFor(t, openFS(t, img), "/")

	want := time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)
	if err := root.SetTimestamp(filesystem.TimeModified, want); err != nil {
		t.Fatalf("SetTimestamp on root: %v", err)
	}

	root2 := entryFor(t, openFS(t, img), "/")
	got, err := root2.Timestamp(filesystem.TimeModified)
	if err != nil {
		t.Fatalf("Timestamp on root: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("root modified = %s, want %s", got, want)
	}
}

func TestTimestompInvalidField(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	e := entryFor(t, openFS(t, img), "/hello.txt")
	if err := e.SetTimestamp(filesystem.TimeField("bogus"), time.Now()); err == nil {
		t.Fatal("SetTimestamp with an invalid field succeeded, want an error")
	}
}

func TestTimestompRejectsPreEpoch(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	e := entryFor(t, openFS(t, img), "/hello.txt")
	err := e.SetTimestamp(filesystem.TimeModified, time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("SetTimestamp with a pre-epoch timestamp succeeded, want an error")
	}
}

// TestTimestompTechniqueRoundTrip drives the timestomp technique end to end,
// covering the filesystem.TimestompCapable assertion in the technique layer.
func TestTimestompTechniqueRoundTrip(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	tech, err := technique.Get(technique.Timestomp)
	if err != nil {
		t.Fatalf("technique.Get: %v", err)
	}

	e := entryFor(t, openFS(t, img), "/hello.txt")
	want := time.Date(2007, 3, 4, 5, 6, 7, 0, time.UTC)
	if _, err := tech.Hide(e, technique.Request{Field: filesystem.TimeCreated, Timestamp: want}); err != nil {
		t.Fatalf("Hide: %v", err)
	}

	e2 := entryFor(t, openFS(t, img), "/hello.txt")
	got, err := e2.Timestamp(filesystem.TimeCreated)
	if err != nil {
		t.Fatalf("Timestamp after Hide: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("created = %s, want %s", got, want)
	}

	original := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := tech.Clear(e2, technique.Request{Field: filesystem.TimeCreated, Timestamp: original}); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	e3 := entryFor(t, openFS(t, img), "/hello.txt")
	got, err = e3.Timestamp(filesystem.TimeCreated)
	if err != nil {
		t.Fatalf("Timestamp after Clear: %v", err)
	}
	if !got.Equal(original) {
		t.Fatalf("created after Clear = %s, want %s", got, original)
	}
}

func entryPaths(entries []filesystem.Entry) []string {
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path()
	}
	return paths
}
