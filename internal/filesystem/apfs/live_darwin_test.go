//go:build darwin

package apfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

// liveTarget creates an empty file in a fresh temp dir and returns its
// path along with the live Entry for it. t.TempDir() lives on the volume
// running the test suite, which is APFS on every supported macOS runner.
func liveTarget(t *testing.T) (string, *liveEntry) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("create target: %v", err)
	}
	return path, newLiveEntry(path)
}

func TestLiveXattrRoundTrip(t *testing.T) {
	_, e := liveTarget(t)

	if err := e.WriteStream("user.nemo.test", []byte("payload")); err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	// macOS itself may attach xattrs of its own (e.g. com.apple.provenance
	// on recent releases), so assert membership rather than an exact list.
	names, err := e.NamedStreams()
	if err != nil {
		t.Fatalf("NamedStreams: %v", err)
	}
	if !containsName(names, "user.nemo.test") {
		t.Fatalf("NamedStreams = %v, want it to contain user.nemo.test", names)
	}
	got, err := e.ReadStream("user.nemo.test")
	if err != nil {
		t.Fatalf("ReadStream: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("ReadStream = %q, want %q", got, "payload")
	}

	if err := e.DeleteStream("user.nemo.test"); err != nil {
		t.Fatalf("DeleteStream: %v", err)
	}
	if _, err := e.ReadStream("user.nemo.test"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadStream after delete: err = %v, want fs.ErrNotExist", err)
	}
	names, err = e.NamedStreams()
	if err != nil {
		t.Fatalf("NamedStreams after delete: %v", err)
	}
	if containsName(names, "user.nemo.test") {
		t.Fatalf("NamedStreams after delete = %v, still contains user.nemo.test", names)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestLiveXattrOverwriteReplaces(t *testing.T) {
	_, e := liveTarget(t)

	if err := e.WriteStream("user.nemo.test", []byte("first value, quite long")); err != nil {
		t.Fatalf("WriteStream (first): %v", err)
	}
	if err := e.WriteStream("user.nemo.test", []byte("second")); err != nil {
		t.Fatalf("WriteStream (second): %v", err)
	}
	got, err := e.ReadStream("user.nemo.test")
	if err != nil {
		t.Fatalf("ReadStream: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("ReadStream = %q, want %q (overwrite should replace, not append)", got, "second")
	}
}

func TestLiveXattrEmptyAndLargePayload(t *testing.T) {
	_, e := liveTarget(t)

	if err := e.WriteStream("user.nemo.empty", nil); err != nil {
		t.Fatalf("WriteStream(nil): %v", err)
	}
	got, err := e.ReadStream("user.nemo.empty")
	if err != nil {
		t.Fatalf("ReadStream(empty): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReadStream(empty) = %v, want empty", got)
	}

	large := make([]byte, 1<<20) // 1 MiB, crosses the small-value inline threshold
	for i := range large {
		large[i] = byte(i)
	}
	if err := e.WriteStream("user.nemo.large", large); err != nil {
		t.Fatalf("WriteStream(1MiB): %v", err)
	}
	got, err = e.ReadStream("user.nemo.large")
	if err != nil {
		t.Fatalf("ReadStream(1MiB): %v", err)
	}
	if len(got) != len(large) {
		t.Fatalf("ReadStream(1MiB) len = %d, want %d", len(got), len(large))
	}
	for i := range large {
		if got[i] != large[i] {
			t.Fatalf("ReadStream(1MiB) mismatch at byte %d", i)
		}
	}
}

// TestLiveXattrUnicodeNames exercises xattr round trips against filenames
// (and, for one case, an xattr name) drawn from encodings and character
// classes that have historically tripped up naive byte-oriented path code:
// NFC vs. NFD composition, a ZWJ emoji sequence, spaces and a colon.
func TestLiveXattrUnicodeNames(t *testing.T) {
	names := []struct {
		label string
		name  string
	}{
		{"nfc", "café.txt"},        // U+00E9 (precomposed é)
		{"nfd", "café.txt"},       // e + U+0301 (combining acute)
		{"emoji-zwj", "🕵️‍♀️.txt"}, // multi-codepoint ZWJ sequence
		{"spaces", "two words.txt"},
		{"colon", "a:b.txt"},
		{"cjk", "日本語.txt"},
	}
	for _, tc := range names {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatalf("create %q: %v", tc.name, err)
			}
			e := newLiveEntry(path)
			if err := e.WriteStream("user.nemo.test", []byte("payload")); err != nil {
				t.Fatalf("WriteStream on %q: %v", tc.name, err)
			}
			got, err := e.ReadStream("user.nemo.test")
			if err != nil {
				t.Fatalf("ReadStream on %q: %v", tc.name, err)
			}
			if string(got) != "payload" {
				t.Fatalf("ReadStream on %q = %q", tc.name, got)
			}
			if err := e.DeleteStream("user.nemo.test"); err != nil {
				t.Fatalf("DeleteStream on %q: %v", tc.name, err)
			}
		})
	}

	t.Run("unicode stream name", func(t *testing.T) {
		_, e := liveTarget(t)
		const streamName = "user.nemo.日本語"
		if err := e.WriteStream(streamName, []byte("payload")); err != nil {
			t.Fatalf("WriteStream: %v", err)
		}
		got, err := e.ReadStream(streamName)
		if err != nil {
			t.Fatalf("ReadStream: %v", err)
		}
		if string(got) != "payload" {
			t.Fatalf("ReadStream = %q", got)
		}
	})
}

// TestLiveXattrNFCAndNFDAddressSameFile pins APFS's normalization-insensitive
// lookup: writing under one composed form and reading back through the
// other must see the same xattr, so a future change in that behavior (or a
// test run on a filesystem that lacks it) shows up here instead of silently
// scattering data under two different names.
func TestLiveXattrNFCAndNFDAddressSameFile(t *testing.T) {
	dir := t.TempDir()
	nfc := filepath.Join(dir, "café.txt")
	nfd := filepath.Join(dir, "café.txt")

	if err := os.WriteFile(nfc, []byte("x"), 0o644); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := newLiveEntry(nfc).WriteStream("user.nemo.test", []byte("payload")); err != nil {
		t.Fatalf("WriteStream via NFC path: %v", err)
	}
	got, err := newLiveEntry(nfd).ReadStream("user.nemo.test")
	if err != nil {
		t.Fatalf("ReadStream via NFD path: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("ReadStream via NFD path = %q, want %q", got, "payload")
	}
}

func TestLiveResourceFork(t *testing.T) {
	path, e := liveTarget(t)
	payload := []byte("resource fork contents")

	if err := e.WriteStream("com.apple.ResourceFork", payload); err != nil {
		t.Fatalf("WriteStream(ResourceFork): %v", err)
	}
	got, err := os.ReadFile(path + "/..namedfork/rsrc")
	if err != nil {
		t.Fatalf("read resource fork via namedfork: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("resource fork contents = %q, want %q", got, payload)
	}
}

func TestLiveDirectoryTarget(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("create child: %v", err)
	}

	e := newLiveEntry(sub)
	if !e.IsDir() {
		t.Fatal("IsDir = false, want true")
	}
	if err := e.WriteStream("user.nemo.test", []byte("dir xattr")); err != nil {
		t.Fatalf("WriteStream on directory: %v", err)
	}
	got, err := e.ReadStream("user.nemo.test")
	if err != nil {
		t.Fatalf("ReadStream on directory: %v", err)
	}
	if string(got) != "dir xattr" {
		t.Fatalf("ReadStream on directory = %q", got)
	}

	children, err := e.Children()
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	wantChildren, err := os.ReadDir(sub)
	if err != nil {
		t.Fatalf("os.ReadDir: %v", err)
	}
	if len(children) != len(wantChildren) {
		t.Fatalf("Children() returned %d entries, os.ReadDir returned %d", len(children), len(wantChildren))
	}
}

func TestLiveSymlinkFollowsToTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	e := newLiveEntry(link)
	if err := e.WriteStream("user.nemo.test", []byte("via symlink")); err != nil {
		t.Fatalf("WriteStream via symlink: %v", err)
	}
	got, err := newLiveEntry(target).ReadStream("user.nemo.test")
	if err != nil {
		t.Fatalf("ReadStream on real target: %v", err)
	}
	if string(got) != "via symlink" {
		t.Fatalf("xattr written via symlink did not land on the real target: got %q", got)
	}
}

func TestLiveTimestompRoundTrip(t *testing.T) {
	// A timestamp safely in the future of the file's real birthtime, so
	// setting modified/accessed here can't trip the birthtime-lowering
	// quirk that TestLiveTimestompSettingModifiedBeforeBirthtimeLowersCreated
	// pins separately: that quirk only fires when the new time is *earlier*
	// than the current birthtime.
	want := time.Now().Add(48 * time.Hour).Truncate(time.Second)

	for _, field := range []filesystem.TimeField{filesystem.TimeCreated, filesystem.TimeModified, filesystem.TimeAccessed} {
		t.Run(string(field), func(t *testing.T) {
			_, e := liveTarget(t)

			before := make(map[filesystem.TimeField]time.Time)
			for _, f := range []filesystem.TimeField{filesystem.TimeCreated, filesystem.TimeModified, filesystem.TimeAccessed} {
				v, err := e.Timestamp(f)
				if err != nil {
					t.Fatalf("Timestamp(%q) before write: %v", f, err)
				}
				before[f] = v
			}

			if err := e.SetTimestamp(field, want); err != nil {
				t.Fatalf("SetTimestamp(%q): %v", field, err)
			}
			got, err := e.Timestamp(field)
			if err != nil {
				t.Fatalf("Timestamp(%q) after write: %v", field, err)
			}
			if got.Unix() != want.Unix() {
				t.Fatalf("Timestamp(%q) = %v, want %v", field, got, want)
			}

			for _, f := range []filesystem.TimeField{filesystem.TimeCreated, filesystem.TimeModified, filesystem.TimeAccessed} {
				if f == field {
					continue
				}
				v, err := e.Timestamp(f)
				if err != nil {
					t.Fatalf("Timestamp(%q) after unrelated write: %v", f, err)
				}
				if !v.Equal(before[f]) {
					t.Fatalf("Timestamp(%q) changed from %v to %v after setting only %q", f, before[f], v, field)
				}
			}
		})
	}
}

func TestLiveTimestompChangedIsUnsupported(t *testing.T) {
	_, e := liveTarget(t)
	err := e.SetTimestamp(filesystem.TimeChanged, time.Now())
	if !errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("SetTimestamp(changed): err = %v, want filesystem.ErrUnsupported", err)
	}
}

// TestLiveTimestompSettingModifiedBeforeBirthtimeLowersCreated pins a real
// APFS/HFS+ quirk: setting mtime earlier than the current birthtime also
// pulls birthtime down to match, because APFS treats birthtime as "the
// earliest known point in the file's history". Deliberately asserted here
// so a macOS/APFS version that changes this surfaces as a test failure
// instead of a silently wrong assumption elsewhere.
func TestLiveTimestompSettingModifiedBeforeBirthtimeLowersCreated(t *testing.T) {
	_, e := liveTarget(t)

	createdBefore, err := e.Timestamp(filesystem.TimeCreated)
	if err != nil {
		t.Fatalf("Timestamp(created): %v", err)
	}

	past := createdBefore.Add(-24 * time.Hour)
	if err := e.SetTimestamp(filesystem.TimeModified, past); err != nil {
		t.Fatalf("SetTimestamp(modified): %v", err)
	}

	createdAfter, err := e.Timestamp(filesystem.TimeCreated)
	if err != nil {
		t.Fatalf("Timestamp(created) after: %v", err)
	}
	if !createdAfter.Before(createdBefore) {
		t.Fatalf("created time = %v, want it lowered below %v after mtime was set earlier", createdAfter, createdBefore)
	}
}

func TestOpenLiveMissingPath(t *testing.T) {
	_, err := OpenLive(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("OpenLive: expected error for a missing path, got nil")
	}
}

func TestOpenLiveNonAPFS(t *testing.T) {
	// /dev is devfs on every supported macOS version, never APFS.
	if _, err := OpenLive("/dev"); err == nil {
		t.Fatal("OpenLive(/dev): expected a non-apfs error, got nil")
	} else if !strings.Contains(err.Error(), "apfs") {
		t.Fatalf("OpenLive(/dev): err = %v, want it to mention apfs", err)
	}
}

func TestOpenLiveRootsAtTarget(t *testing.T) {
	path, _ := liveTarget(t)
	fs, err := OpenLive(path)
	if err != nil {
		t.Fatalf("OpenLive: %v", err)
	}
	entry, err := fs.Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	if entry.Path() != path {
		t.Fatalf("Open(%q).Path() = %q", path, entry.Path())
	}
	if root := fs.Root(); root.Path() != path {
		t.Fatalf("Root().Path() = %q, want %q", root.Path(), path)
	}
}

// TestLiveEntryHasNoSlackCapability confirms liveEntry deliberately does not
// implement filesystem.SlackSpaceCapable: live slack-space access needs the
// raw device (OpenLiveSlack), and must not silently appear available
// through the ordinary path-backed Entry.
func TestLiveEntryHasNoSlackCapability(t *testing.T) {
	var e filesystem.Entry = &liveEntry{}
	if _, ok := e.(filesystem.SlackSpaceCapable); ok {
		t.Fatal("liveEntry implements filesystem.SlackSpaceCapable, want it not to")
	}
}
