package technique

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManifestAppendLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ManifestName)

	entries := []Backup{
		{Technique: SlackSpace, Target: "/a", Location: "10-29", Original: []byte{0xAB, 0x00, 0xFF}},
		{Technique: Timestomp, Target: "/b", Location: "modified", Timestamp: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{Technique: SlackSpace, Target: "/a", Location: "10-29", Original: []byte("second")},
	}
	for _, e := range entries {
		if err := AppendManifest(path, e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if string(got[0].Original) != "\xab\x00\xff" || !got[1].Timestamp.Equal(entries[1].Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// LatestBackup returns the last matching entry.
	latest, ok := LatestBackup(got, SlackSpace, "/a", "10-29")
	if !ok || string(latest.Original) != "second" {
		t.Fatalf("LatestBackup returned %+v (ok=%v)", latest, ok)
	}
	if _, ok := LatestBackup(got, SlackSpace, "/missing", ""); ok {
		t.Fatal("LatestBackup matched a target that is not in the manifest")
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	_, err := LoadManifest(filepath.Join(t.TempDir(), "nope.jsonl"))
	if !os.IsNotExist(err) {
		t.Fatalf("want not-exist error, got %v", err)
	}
}
