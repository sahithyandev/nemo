package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
)

func TestLiveFeatureMatrixExplainsLinuxRestrictions(t *testing.T) {
	output := new(bytes.Buffer)
	err := writeLiveFeatureMatrix(output, []filesystem.Detector{
		{Type: filesystem.TypeNTFS}, {Type: filesystem.TypeEXT4}, {Type: filesystem.TypeAPFS},
	}, "linux")
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(rows) != 10 {
		t.Fatalf("matrix row count: %d\n%s", len(rows), output)
	}
	for _, want := range []string{"LIMITS", "user.* xattrs", "created/changed writes unavailable", "raw-device permissions required", "unavailable on linux"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("matrix missing %q:\n%s", want, output)
		}
	}
	if !strings.HasPrefix(rows[1], "apfs") || !strings.HasPrefix(rows[4], "ext4") || !strings.HasPrefix(rows[7], "ntfs") {
		t.Fatalf("matrix order is unstable:\n%s", output)
	}
}

func TestNativeFeatureRestrictions(t *testing.T) {
	for _, tc := range []struct {
		kind               filesystem.Type
		tech, goos, status string
	}{
		{filesystem.TypeEXT4, technique.NamedStream, "linux", "supported"},
		{filesystem.TypeEXT4, technique.Timestomp, "linux", "partial"},
		{filesystem.TypeEXT4, technique.SlackSpace, "linux", "read-only"},
		{filesystem.TypeAPFS, technique.Timestomp, "darwin", "partial"},
		{filesystem.TypeNTFS, technique.NamedStream, "windows", "read-only"},
		{filesystem.TypeEXT4, technique.NamedStream, "windows", "unsupported"},
	} {
		got, limits := liveFeature(tc.kind, tc.tech, tc.goos)
		if got != tc.status || limits == "" {
			t.Errorf("%s/%s/%s = %s, %q; want %s with limits", tc.kind, tc.tech, tc.goos, got, limits, tc.status)
		}
	}
}

func TestFeaturesRejectsUnknownMode(t *testing.T) {
	command := newFeaturesCommand(func() []filesystem.Detector { return nil })
	command.SetArgs([]string{"--mode=other"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "image or live") {
		t.Fatalf("invalid mode: %v", err)
	}
}

func TestLiveFeatureMatrixReturnsOutputError(t *testing.T) {
	if err := writeLiveFeatureMatrix(errorWriter{}, nil, "linux"); err == nil {
		t.Fatal("output failure ignored")
	}
}
