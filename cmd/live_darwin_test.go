//go:build darwin

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/technique"
)

// TestOpenLiveTargetRouting confirms openLiveTarget only reaches for the raw
// device when the caller explicitly asks for slack-space: every other
// technique (and an unqualified "") stays on the ordinary path-backed
// Entry, with no image attached.
func TestOpenLiveTargetRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tech := range []string{"", technique.NamedStream, technique.Timestomp} {
		opened, err := openLiveTarget(path, tech, false)
		if err != nil {
			t.Fatalf("openLiveTarget(tech=%q): %v", tech, err)
		}
		if opened.filesystem == nil {
			t.Fatalf("openLiveTarget(tech=%q): nil filesystem", tech)
		}
		if opened.image != nil {
			t.Fatalf("openLiveTarget(tech=%q): image = %v, want nil (no device opened)", tech, opened.image)
		}
	}

	// slack-space does reach for the device. Root's read-only open of the
	// current volume's device may succeed; either way it must not silently
	// fall back to a path-backed target.
	_, err := openLiveTarget(path, technique.SlackSpace, false)
	if os.Geteuid() != 0 && err == nil {
		t.Fatal("openLiveTarget(slack-space) as non-root: expected a privilege error, got nil")
	}
	if err != nil && os.Geteuid() != 0 {
		if !strings.Contains(err.Error(), "sudo") && !strings.Contains(err.Error(), "permission") {
			t.Fatalf("openLiveTarget(slack-space) error = %v, want it to mention sudo or permission", err)
		}
	}
}

// TestOpenLiveTargetSlackWriteFailsWhenMounted pins the documented macOS
// limit: a write-mode open of the container device always fails while its
// volume is mounted (EBUSY), or with a privilege error first if not root.
// Live slack-space hide can never succeed against a mounted volume.
func TestOpenLiveTargetSlackWriteFailsWhenMounted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := openLiveTarget(path, technique.SlackSpace, true)
	if err == nil {
		t.Fatal("openLiveTarget(slack-space, write=true) on a mounted volume: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "busy") && !strings.Contains(err.Error(), "sudo") && !strings.Contains(err.Error(), "permission") {
		t.Fatalf("error = %v, want it to mention busy/sudo/permission", err)
	}
}

// TestHideLiveNamedStreamProducesOneCustodyRecord exercises hide's live path
// end to end against a real file (the real openLiveTarget, not a fake), and
// confirms exactly one custody record is produced. This is the "no live
// mutation bypasses custody logging" acceptance criterion: nothing about
// the live path writes to the target without also going through
// dependencies.logCustody.
func TestHideLiveNamedStreamProducesOneCustodyRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(payload, []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := defaultHideDependencies()
	logged := 0
	var record custody.Record
	deps.logCustody = func(r custody.Record) error { logged++; record = r; return nil }

	out, err := runHideCmd(t, deps, path, "-t", "named-stream", "-d", payload, "--stream-name", "nemo.test")
	if err != nil {
		t.Fatalf("hide: %v", err)
	}
	if logged != 1 {
		t.Fatalf("logCustody called %d times, want 1", logged)
	}
	var emitted custody.Record
	if err := json.Unmarshal([]byte(out), &emitted); err != nil {
		t.Fatalf("decode custody record: %v\noutput: %s", err, out)
	}
	if emitted != record {
		t.Fatalf("echoed record %+v != logged record %+v", emitted, record)
	}
	if record.Technique != "named-stream" || record.Target != path || record.Detail != "nemo.test" {
		t.Fatalf("unexpected custody record: %+v", record)
	}
}

// TestClearLiveTimestompProducesOneCustodyRecord is the clear-side twin of
// TestHideLiveNamedStreamProducesOneCustodyRecord: a live timestomp clear
// against a real file, exactly one custody record.
func TestClearLiveTimestompProducesOneCustodyRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := defaultClearDependencies()
	logged := 0
	deps.logCustody = func(custody.Record) error { logged++; return nil }

	stamp := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := runClearCmd(deps, path, "-t", "timestomp", "--field=modified", "--timestamp="+stamp); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if logged != 1 {
		t.Fatalf("logCustody called %d times, want 1", logged)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := time.Parse(time.RFC3339, stamp)
	if info.ModTime().Unix() != want.Unix() {
		t.Fatalf("modtime = %v, want %v", info.ModTime(), want)
	}
}

// runHideCmd mirrors runClearCmd/runDetectCmd's shape for hide.
func runHideCmd(t *testing.T, deps hideDependencies, args ...string) (string, error) {
	t.Helper()
	command := newHideCommand(deps)
	var out strings.Builder
	command.SetOut(&out)
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}
