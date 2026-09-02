package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
	"github.com/sahithyandev/nemo/internal/technique"
)

func TestHideHelpListsDocumentedArgumentsAndOptions(t *testing.T) {
	command := newHideCommand(defaultHideDependencies())
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetArgs([]string{"--help"})

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	help := output.String()
	for _, expected := range []string{
		"hide <target>", "--technique", "-t", "--image", "-i", "--data", "-d",
		"--stream-name", "--field", "--timestamp",
	} {
		if !strings.Contains(help, expected) {
			t.Errorf("help does not contain %q:\n%s", expected, help)
		}
	}
}

func TestHideRejectsMissingAndIncompatibleFlagsBeforeOpeningTarget(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "technique", args: []string{"/target"}, want: "--technique is required"},
		{name: "unknown", args: []string{"/target", "-t", "unknown"}, want: "unknown technique"},
		{name: "named stream data", args: []string{"/target", "-t", "named-stream", "--stream-name", "secret"}, want: "--data is required"},
		{name: "named stream name", args: []string{"/target", "-t", "named-stream", "--data", "payload"}, want: "--stream-name is required"},
		{name: "named stream incompatible", args: []string{"/target", "-t", "named-stream", "--data", "payload", "--stream-name", "secret", "--field", "modified"}, want: "incompatible"},
		{name: "slack data", args: []string{"/target", "-t", "slack-space"}, want: "--data is required"},
		{name: "slack incompatible", args: []string{"/target", "-t", "slack-space", "--data", "payload", "--stream-name", "secret"}, want: "incompatible"},
		{name: "timestomp field", args: []string{"/target", "-t", "timestomp", "--timestamp", "2026-08-23T12:00:00Z"}, want: "--field is required"},
		{name: "timestomp field value", args: []string{"/target", "-t", "timestomp", "--field", "changed", "--timestamp", "2026-08-23T12:00:00Z"}, want: "created, modified, or accessed"},
		{name: "timestomp timestamp", args: []string{"/target", "-t", "timestomp", "--field", "modified"}, want: "--timestamp is required"},
		{name: "timestomp format", args: []string{"/target", "-t", "timestomp", "--field", "modified", "--timestamp", "yesterday"}, want: "RFC 3339"},
		{name: "timestomp incompatible", args: []string{"/target", "-t", "timestomp", "--field", "modified", "--timestamp", "2026-08-23T12:00:00Z", "--data", "payload"}, want: "incompatible"},
		{name: "empty image", args: []string{"/target", "-t", "timestomp", "--field", "modified", "--timestamp", "2026-08-23T12:00:00Z", "--image="}, want: "non-empty path"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened := false
			dependencies := defaultHideDependencies()
			dependencies.openImage = func(string) (openedTarget, error) {
				opened = true
				return openedTarget{}, errors.New("unexpected open")
			}
			dependencies.openLive = func(string) (openedTarget, error) {
				opened = true
				return openedTarget{}, errors.New("unexpected open")
			}
			dependencies.readFile = func(string) ([]byte, error) {
				t.Fatal("payload read before flag validation")
				return nil, nil
			}

			command := newHideCommand(dependencies)
			command.SetArgs(test.args)
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
			if opened {
				t.Fatal("target was opened despite invalid flags")
			}
		})
	}
}

func TestHideNamedStreamLiveModeEndToEndWithCustodyRecord(t *testing.T) {
	fake := fakefs.New("/target")
	fixedTime := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.FixedZone("test", 5*60*60+30*60))
	dependencies := defaultHideDependencies()
	dependencies.readFile = func(path string) ([]byte, error) {
		if path != "payload.bin" {
			t.Fatalf("unexpected payload path %q", path)
		}
		return []byte("hidden payload"), nil
	}
	liveCalls := 0
	imageCalls := 0
	dependencies.openLive = func(target string) (openedTarget, error) {
		liveCalls++
		if target != "/target" {
			t.Fatalf("unexpected live target %q", target)
		}
		return openedTarget{filesystem: fake}, nil
	}
	dependencies.openImage = func(string) (openedTarget, error) {
		imageCalls++
		return openedTarget{filesystem: fake, image: fake.Img}, nil
	}
	dependencies.now = func() time.Time { return fixedTime }
	persistCalls := 0
	var persisted custody.Record
	dependencies.logCustody = func(record custody.Record) error {
		persistCalls++
		persisted = record
		return nil
	}

	output := new(bytes.Buffer)
	command := newHideCommand(dependencies)
	command.SetOut(output)
	command.SetArgs([]string{"/target", "--technique", "named-stream", "--data", "payload.bin", "--stream-name", "secret"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	if liveCalls != 1 || imageCalls != 0 {
		t.Fatalf("expected live mode only, got live=%d image=%d", liveCalls, imageCalls)
	}
	written, err := fake.Entry("/target").ReadStream("secret")
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "hidden payload" {
		t.Fatalf("unexpected named stream payload %q", written)
	}

	var record custody.Record
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode custody record: %v\noutput: %s", err, output.String())
	}
	sum := sha256.Sum256([]byte("hidden payload"))
	if record.Operation != "hide" || record.Technique != "named-stream" || record.Target != "/target" || record.Detail != "secret" || record.Bytes != 14 {
		t.Fatalf("unexpected custody record: %+v", record)
	}
	if record.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected hash %q", record.SHA256)
	}
	if !record.Timestamp.Equal(fixedTime) {
		t.Fatalf("unexpected timestamp %v", record.Timestamp)
	}
	if persistCalls != 1 {
		t.Fatalf("expected persistence once, got %d calls", persistCalls)
	}
	if persisted != record {
		t.Fatalf("persisted and emitted records differ:\npersisted: %+v\n  emitted: %+v", persisted, record)
	}
}

func TestHideSelectsImageModeWhenImageFlagIsPresent(t *testing.T) {
	fake := fakefs.New("/target")
	dependencies := defaultHideDependencies()
	dependencies.readFile = func(string) ([]byte, error) { return []byte("x"), nil }
	dependencies.openLive = func(string) (openedTarget, error) {
		t.Fatal("live mode selected despite --image")
		return openedTarget{}, nil
	}
	dependencies.openImage = func(path string) (openedTarget, error) {
		if path != "disk.img" {
			t.Fatalf("unexpected image path %q", path)
		}
		return openedTarget{filesystem: fake, image: fake.Img}, nil
	}
	dependencies.logCustody = func(custody.Record) error { return nil }
	dependencies.echoCustody = func(io.Writer, custody.Record) error { return nil }

	command := newHideCommand(dependencies)
	command.SetArgs([]string{"/target", "-t", "named-stream", "-d", "payload", "--stream-name", "secret", "--image", "disk.img"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestHideExecutesSlackSpaceAndTimestomp(t *testing.T) {
	t.Run("slack-space", func(t *testing.T) {
		fake := fakefs.New("/target")
		fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 32, Length: 64}}
		dependencies := defaultHideDependencies()
		dependencies.readFile = func(string) ([]byte, error) { return []byte("payload"), nil }
		dependencies.openImage = func(string) (openedTarget, error) {
			return openedTarget{filesystem: fake, image: fake.Img}, nil
		}
		dependencies.logCustody = func(custody.Record) error { return nil }
		dependencies.echoCustody = func(io.Writer, custody.Record) error { return nil }
		manifestPath := filepath.Join(t.TempDir(), technique.ManifestName)

		command := newHideCommand(dependencies)
		command.SetArgs([]string{"/target", "-t", "slack-space", "-d", "payload.bin", "--image", "disk.img", "--manifest", manifestPath})
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		// payload is written framed: 12-byte header, then the bytes.
		if got := string(fake.Img.Data[44:51]); got != "payload" {
			t.Fatalf("unexpected slack payload %q", got)
		}
		// The overwritten bytes were recorded to the manifest before the write.
		backups, err := technique.LoadManifest(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := technique.LatestBackup(backups, technique.SlackSpace, "/target", "")
		if !ok || len(got.Original) != 19 {
			t.Fatalf("unexpected manifest backup: %+v (ok=%v)", got, ok)
		}
	})

	t.Run("timestomp", func(t *testing.T) {
		fake := fakefs.New("/target")
		dependencies := defaultHideDependencies()
		dependencies.openLive = func(string) (openedTarget, error) {
			return openedTarget{filesystem: fake}, nil
		}
		dependencies.logCustody = func(custody.Record) error { return nil }
		dependencies.echoCustody = func(io.Writer, custody.Record) error { return nil }

		command := newHideCommand(dependencies)
		command.SetArgs([]string{"/target", "-t", "timestomp", "--field", "modified", "--timestamp", "2026-08-23T12:00:00Z"})
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
		if got := fake.Entry("/target").Times[filesystem.TimeModified]; !got.Equal(want) {
			t.Fatalf("unexpected modified time %v", got)
		}
	})
}

func TestHideDoesNotEmitCustodyRecordWhenOperationFails(t *testing.T) {
	dependencies := defaultHideDependencies()
	dependencies.readFile = func(string) ([]byte, error) { return []byte("x"), nil }
	dependencies.openLive = func(string) (openedTarget, error) {
		return openedTarget{filesystem: fakefs.New()}, nil
	}
	recorded := false
	persisted := false
	dependencies.logCustody = func(custody.Record) error {
		persisted = true
		return nil
	}
	dependencies.echoCustody = func(io.Writer, custody.Record) error {
		recorded = true
		return nil
	}

	command := newHideCommand(dependencies)
	command.SetArgs([]string{"/missing", "-t", "named-stream", "-d", "payload", "--stream-name", "secret"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected operation failure")
	}
	if recorded {
		t.Fatal("custody record emitted for failed operation")
	}
	if persisted {
		t.Fatal("custody record persisted for failed operation")
	}
}

func TestHideReturnsPersistenceFailureWithoutEmittingRecord(t *testing.T) {
	fake := fakefs.New("/target")
	dependencies := defaultHideDependencies()
	dependencies.readFile = func(string) ([]byte, error) { return []byte("payload"), nil }
	dependencies.openLive = func(string) (openedTarget, error) {
		return openedTarget{filesystem: fake}, nil
	}
	persistCalls := 0
	dependencies.logCustody = func(custody.Record) error {
		persistCalls++
		return errors.New("disk unavailable")
	}
	dependencies.echoCustody = func(io.Writer, custody.Record) error {
		t.Fatal("custody record emitted after persistence failure")
		return nil
	}

	command := newHideCommand(dependencies)
	command.SetArgs([]string{"/target", "-t", "named-stream", "-d", "payload", "--stream-name", "secret"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "append custody log: disk unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
	if persistCalls != 1 {
		t.Fatalf("expected persistence once, got %d calls", persistCalls)
	}
}

func TestHidePayloadReadFailureDoesNotOpenOrWrite(t *testing.T) {
	dependencies := defaultHideDependencies()
	dependencies.readFile = func(string) ([]byte, error) { return nil, errors.New("read failed") }
	dependencies.openLive = func(string) (openedTarget, error) {
		t.Fatal("target opened after payload read failure")
		return openedTarget{}, nil
	}
	dependencies.logCustody = func(custody.Record) error {
		t.Fatal("custody record persisted after payload read failure")
		return nil
	}
	dependencies.echoCustody = func(io.Writer, custody.Record) error {
		t.Fatal("custody record written after payload read failure")
		return nil
	}

	command := newHideCommand(dependencies)
	command.SetArgs([]string{"/target", "-t", "named-stream", "-d", "missing", "--stream-name", "secret"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "read payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}
