package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
)

func TestFeaturesPrintsDeterministicRegisteredMatrix(t *testing.T) {
	detectors := []filesystem.Detector{
		{Type: filesystem.TypeNTFS, Techniques: []string{technique.SlackSpace}},
		{Type: filesystem.TypeEXT4, Techniques: []string{technique.Timestomp, technique.NamedStream}},
		{Type: filesystem.TypeAPFS},
	}

	output := new(bytes.Buffer)
	command := newFeaturesCommand(func() []filesystem.Detector { return detectors })
	command.SetOut(output)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	wantRows := [][]string{
		{"FILESYSTEM", "TECHNIQUE", "STATUS"},
		{"apfs", "named-stream", "unsupported"},
		{"apfs", "slack-space", "unsupported"},
		{"apfs", "timestomp", "unsupported"},
		{"ext4", "named-stream", "supported"},
		{"ext4", "slack-space", "unsupported"},
		{"ext4", "timestomp", "supported"},
		{"ntfs", "named-stream", "unsupported"},
		{"ntfs", "slack-space", "supported"},
		{"ntfs", "timestomp", "unsupported"},
	}
	assertFeatureRows(t, output.String(), wantRows)
}

func TestFeaturesPrintsEmptyMatrixWithoutError(t *testing.T) {
	output := new(bytes.Buffer)
	command := newFeaturesCommand(func() []filesystem.Detector { return nil })
	command.SetOut(output)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	assertFeatureRows(t, output.String(), [][]string{{"FILESYSTEM", "TECHNIQUE", "STATUS"}})
}

func TestFeaturesHelpMatchesDocumentedInterface(t *testing.T) {
	output := new(bytes.Buffer)
	command := newFeaturesCommand(func() []filesystem.Detector { return nil })
	command.SetOut(output)
	command.SetArgs([]string{"--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	help := output.String()
	for _, expected := range []string{"features", "registered filesystem", "named streams", "slack space", "timestomping", "No image or target is required"} {
		if !strings.Contains(help, expected) {
			t.Errorf("help does not contain %q:\n%s", expected, help)
		}
	}
}

func TestFeaturesRejectsArguments(t *testing.T) {
	command := newFeaturesCommand(func() []filesystem.Detector { return nil })
	command.SetArgs([]string{"disk.img"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected argument rejection, got %v", err)
	}
}

func TestFeaturesReturnsOutputFailure(t *testing.T) {
	command := newFeaturesCommand(func() []filesystem.Detector {
		return []filesystem.Detector{{Type: filesystem.TypeEXT4}}
	})
	command.SetOut(errorWriter{})
	command.SetArgs(nil)
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "write feature matrix") {
		t.Fatalf("expected output error, got %v", err)
	}
}

func assertFeatureRows(t *testing.T, output string, want [][]string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != len(want) {
		t.Fatalf("unexpected row count: got %d, want %d\n%s", len(lines), len(want), output)
	}
	for i, line := range lines {
		got := strings.Fields(line)
		if strings.Join(got, "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d: got %v, want %v", i, got, want[i])
		}
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}
