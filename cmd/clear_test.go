package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
	"github.com/sahithyandev/nemo/internal/technique"
)

func fakeClearDeps(f *fakefs.FS) clearDependencies {
	return clearDependencies{
		openImage:    func(string) (openedTarget, error) { return openedTarget{filesystem: f, image: f.Img}, nil },
		openLive:     func(string) (openedTarget, error) { return openedTarget{filesystem: f, image: f.Img}, nil },
		loadManifest: func(string) ([]technique.Backup, error) { return nil, os.ErrNotExist },
		now:          func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) },
		logCustody:   func(custody.Record) error { return nil },
		echoCustody:  custody.Write,
	}
}

func runClearCmd(deps clearDependencies, args ...string) (string, error) {
	command := newClearCommand(deps)
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(io.Discard)
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

func TestClearHelpAndRegistration(t *testing.T) {
	out, err := runClearCmd(defaultClearDependencies(), "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"clear <target>", "--technique", "--image", "--stream-name", "--field", "--timestamp", "--manifest"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
	command, _, err := rootCmd.Find([]string{"clear"})
	if err != nil || command.Name() != "clear" {
		t.Fatalf("clear not registered: %v", err)
	}
}

func TestClearValidationBeforeOpening(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "accepts 1 arg"},
		{[]string{"/a", "/b"}, "accepts 1 arg"},
		{[]string{"/a"}, "--technique is required"},
		{[]string{"/a", "-t", "unknown"}, "unknown technique"},
		{[]string{"/a", "-t", "named-stream"}, "--stream-name is required"},
		{[]string{"/a", "-t", "named-stream", "--stream-name=s", "--field=modified"}, "incompatible"},
		{[]string{"/a", "-t", "named-stream", "--stream-name=s", "--manifest=m"}, "incompatible"},
		{[]string{"/a", "-t", "slack-space", "--image="}, "non-empty path"},
		{[]string{"/a", "-t", "slack-space", "--manifest="}, "non-empty path"},
		{[]string{"/a", "-t", "slack-space", "--stream-name=s"}, "incompatible"},
		{[]string{"/a", "-t", "slack-space", "--timestamp=x"}, "incompatible"},
		{[]string{"/a", "-t", "timestomp"}, "--field is required"},
		{[]string{"/a", "-t", "timestomp", "--field=x"}, "created, modified, or accessed"},
		{[]string{"/a", "-t", "timestomp", "--field=modified"}, "--timestamp is required"},
		{[]string{"/a", "-t", "timestomp", "--field=modified", "--timestamp=yesterday"}, "RFC 3339"},
		{[]string{"/a", "-t", "timestomp", "--field=modified", "--timestamp=0001-01-01T00:00:00Z"}, "non-zero"},
		{[]string{"/a", "-t", "timestomp", "--field=modified", "--timestamp=2026-01-01T00:00:00Z", "--stream-name=s"}, "incompatible"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			deps := clearDependencies{}
			unexpected := func(string) (openedTarget, error) { t.Fatal("opened before validation"); return openedTarget{}, nil }
			deps.openLive, deps.openImage = unexpected, unexpected
			deps.loadManifest = func(string) ([]technique.Backup, error) { t.Fatal("manifest read before validation"); return nil, nil }
			_, err := runClearCmd(deps, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestClearNamedStreamModesAndCustody(t *testing.T) {
	for _, imageMode := range []bool{false, true} {
		f := fakefs.New("/target")
		f.Entry("/target").Streams = map[string][]byte{"secret": []byte("payload"), "keep": []byte("keep")}
		deps := fakeClearDeps(f)
		closed, opened, persisted := 0, 0, 0
		var record custody.Record
		open := func(path string) (openedTarget, error) {
			opened++
			want := "/target"
			if imageMode {
				want = "disk.img"
			}
			if path != want {
				t.Fatalf("open path = %q", path)
			}
			return openedTarget{filesystem: f, image: f.Img, close: func() error { closed++; return nil }}, nil
		}
		unexpected := func(string) (openedTarget, error) { t.Fatal("wrong mode"); return openedTarget{}, nil }
		deps.openLive, deps.openImage = open, unexpected
		args := []string{"/target", "-t", "named-stream", "--stream-name=secret"}
		if imageMode {
			deps.openLive, deps.openImage = unexpected, open
			args = append(args, "-i", "disk.img")
		}
		deps.loadManifest = func(string) ([]technique.Backup, error) { t.Fatal("named-stream read manifest"); return nil, nil }
		deps.logCustody = func(r custody.Record) error { persisted++; record = r; return nil }
		out, err := runClearCmd(deps, args...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Entry("/target").ReadStream("secret"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("stream not deleted")
		}
		if got, _ := f.Entry("/target").ReadStream("keep"); string(got) != "keep" {
			t.Fatal("other stream changed")
		}
		var emitted custody.Record
		if err := json.Unmarshal([]byte(out), &emitted); err != nil {
			t.Fatal(err)
		}
		want := custody.NewRecord("clear", technique.NamedStream, "/target", "secret", 0, nil, deps.now())
		if record != want || emitted != record || persisted != 1 || closed != 1 || opened != 1 {
			t.Fatalf("unexpected flow: %+v, opens=%d closes=%d logs=%d", record, opened, closed, persisted)
		}
	}
}

func setupClearSlack(t *testing.T) (*fakefs.FS, technique.Backup) {
	t.Helper()
	f := fakefs.New("/target")
	f.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 32, Length: 64}}
	copy(f.Img.Data[32:], bytes.Repeat([]byte{0x7b}, 64))
	selected, err := technique.Get(technique.SlackSpace)
	if err != nil {
		t.Fatal(err)
	}
	var backup technique.Backup
	_, err = selected.Hide(f.Entry("/target"), technique.Request{Image: f.Img, Data: []byte("payload"), Backup: func(b technique.Backup) error { backup = b; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return f, backup
}

func TestClearSlackRestoreAndZero(t *testing.T) {
	for _, restore := range []bool{false, true} {
		f, backup := setupClearSlack(t)
		deps := fakeClearDeps(f)
		args := []string{"/target", "-t", "slack-space", "-i", "disk.img"}
		want := make([]byte, len(backup.Original))
		if restore {
			manifest := filepath.Join(t.TempDir(), technique.ManifestName)
			old := backup
			old.Original = bytes.Repeat([]byte{1}, len(backup.Original))
			wrong := backup
			wrong.Location = "100-119"
			wrong.Original = bytes.Repeat([]byte{2}, len(backup.Original))
			for _, b := range []technique.Backup{old, backup, wrong} {
				if err := technique.AppendManifest(manifest, b); err != nil {
					t.Fatal(err)
				}
			}
			deps.loadManifest = technique.LoadManifest
			args = append(args, "--manifest", manifest)
			want = backup.Original
		}
		var record custody.Record
		deps.logCustody = func(r custody.Record) error { record = r; return nil }
		if _, err := runClearCmd(deps, args...); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(f.Img.Data[32:32+len(want)], want) {
			t.Fatal("incorrect slack bytes")
		}
		if !bytes.Equal(f.Img.Data[32+len(want):96], bytes.Repeat([]byte{0x7b}, 64-len(want))) {
			t.Fatal("unrelated slack modified")
		}
		expected := custody.NewRecord("clear", technique.SlackSpace, "/target", backup.Location, 7, want, deps.now())
		if record != expected {
			t.Fatalf("audit = %+v, want %+v", record, expected)
		}
	}
}

func TestClearTimestomp(t *testing.T) {
	f := fakefs.New("/target")
	deps := fakeClearDeps(f)
	const stamp = "2026-08-23T12:00:00+05:30"
	out, err := runClearCmd(deps, "/target", "-t", "timestomp", "--field=modified", "--timestamp="+stamp)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := time.Parse(time.RFC3339, stamp)
	if !f.Entry("/target").Times[filesystem.TimeModified].Equal(want) {
		t.Fatal("timestamp not restored")
	}
	var got custody.Record
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	expected := custody.NewRecord("clear", technique.Timestomp, "/target", "modified="+stamp, 0, []byte("modified="+stamp), deps.now())
	if got != expected {
		t.Fatalf("audit = %+v", got)
	}
}

// Embedding only the generic interfaces hides the fake's optional capabilities.
type clearUnsupportedFS struct{ filesystem.FileSystem }
type clearUnsupportedEntry struct{ filesystem.Entry }

func (f clearUnsupportedFS) Open(path string) (filesystem.Entry, error) {
	e, err := f.FileSystem.Open(path)
	return clearUnsupportedEntry{e}, err
}

func TestClearUnsupportedCapabilities(t *testing.T) {
	for _, args := range [][]string{
		{"-t", "named-stream", "--stream-name=s"},
		{"-t", "slack-space"},
		{"-t", "timestomp", "--field=created", "--timestamp=2026-01-01T00:00:00Z"},
	} {
		f := fakefs.New("/target")
		deps := fakeClearDeps(f)
		closed := false
		deps.openLive = func(string) (openedTarget, error) {
			return openedTarget{filesystem: clearUnsupportedFS{f}, close: func() error { closed = true; return nil }}, nil
		}
		deps.logCustody = func(custody.Record) error { t.Fatal("logged failed clear"); return nil }
		out, err := runClearCmd(deps, append([]string{"/target"}, args...)...)
		if !errors.Is(err, technique.ErrUnsupported) || !strings.Contains(err.Error(), "clear") || out != "" || !closed {
			t.Fatalf("error = %v, output = %q, closed = %v", err, out, closed)
		}
	}
}

func TestClearFailuresAndCleanup(t *testing.T) {
	sentinel := errors.New("injected failure")
	for _, stage := range []string{"open", "nil filesystem", "entry", "clear", "persist", "echo"} {
		t.Run(stage, func(t *testing.T) {
			f := fakefs.New("/target")
			f.Entry("/target").Streams = map[string][]byte{"s": []byte("x")}
			deps := fakeClearDeps(f)
			closed, logs, echoes := 0, 0, 0
			deps.openLive = func(string) (openedTarget, error) {
				if stage == "open" {
					return openedTarget{}, sentinel
				}
				var fs filesystem.FileSystem = f
				if stage == "nil filesystem" {
					fs = nil
				}
				if stage == "entry" {
					fs = fakefs.New()
				}
				if stage == "clear" {
					delete(f.Entry("/target").Streams, "s")
				}
				return openedTarget{filesystem: fs, close: func() error { closed++; return nil }}, nil
			}
			deps.logCustody = func(custody.Record) error {
				logs++
				if stage == "persist" {
					return sentinel
				}
				return nil
			}
			deps.echoCustody = func(io.Writer, custody.Record) error { echoes++; return sentinel }
			_, err := runClearCmd(deps, "/target", "-t", "named-stream", "--stream-name=s")
			if err == nil {
				t.Fatal("expected error")
			}
			if stage == "open" || stage == "persist" || stage == "echo" {
				if !errors.Is(err, sentinel) {
					t.Fatalf("lost error cause: %v", err)
				}
			}
			wantCloses := 1
			if stage == "open" {
				wantCloses = 0
			}
			wantLogs, wantEchoes := 0, 0
			if stage == "persist" || stage == "echo" {
				wantLogs = 1
			}
			if stage == "echo" {
				wantEchoes = 1
			}
			if closed != wantCloses || logs != wantLogs || echoes != wantEchoes {
				t.Fatalf("closes/logs/echoes = %d/%d/%d", closed, logs, echoes)
			}
		})
	}
}

func TestClearManifestFailuresDoNotMutate(t *testing.T) {
	for _, mode := range []string{"missing explicit", "unreadable", "no match", "wrong location", "nil original", "wrong size"} {
		t.Run(mode, func(t *testing.T) {
			f, backup := setupClearSlack(t)
			before := append([]byte(nil), f.Img.Data...)
			deps := fakeClearDeps(f)
			deps.loadManifest = func(string) ([]technique.Backup, error) {
				switch mode {
				case "missing explicit":
					return nil, os.ErrNotExist
				case "unreadable":
					return nil, os.ErrPermission
				case "no match":
					return nil, nil
				case "wrong location":
					backup.Location = "100-119"
				case "nil original":
					backup.Original = nil
				case "wrong size":
					backup.Original = []byte{1}
				}
				return []technique.Backup{backup}, nil
			}
			deps.logCustody = func(custody.Record) error { t.Fatal("logged failed restoration"); return nil }
			out, err := runClearCmd(deps, "/target", "-t", "slack-space", "--manifest=backup.jsonl")
			if err == nil || out != "" || !bytes.Equal(before, f.Img.Data) {
				t.Fatalf("unsafe failure: %v", err)
			}
		})
	}
}
