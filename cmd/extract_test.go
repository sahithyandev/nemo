package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
)

// fakeExtractDeps returns dependencies whose openImage hands back the given
// fake filesystem (read-only), so extract runs without a real image.
func fakeExtractDeps(fs *fakefs.FS) extractDependencies {
	return extractDependencies{
		openImage: func(string) (openedTarget, error) {
			return openedTarget{filesystem: fs, image: imagepkg.ReadOnly(fs.Img)}, nil
		},
		openLive: func(string, string, bool) (openedTarget, error) {
			return openedTarget{}, errors.New("unexpected live open")
		},
		writeFile: func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) },
	}
}

func runExtractCmd(t *testing.T, deps extractDependencies, args ...string) (string, error) {
	t.Helper()
	command := newExtractCommand(deps)
	out := new(bytes.Buffer)
	command.SetOut(out)
	command.SetErr(out)
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

func TestExtractHelpListsDocumentedArgumentsAndOptions(t *testing.T) {
	out, err := runExtractCmd(t, defaultExtractDependencies(), "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"extract <target>", "--technique", "-t", "--image", "-i", "--stream-name", "--output", "-o"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestExtractNamedStreamToStdout(t *testing.T) {
	fs := fakefs.New("/target")
	if err := fs.Entry("/target").WriteStream("secret", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	out, err := runExtractCmd(t, fakeExtractDeps(fs), "/target", "-t", "named-stream", "--stream-name", "secret", "--image", "x")
	if err != nil {
		t.Fatal(err)
	}
	if out != "payload" {
		t.Fatalf("unexpected stdout %q", out)
	}
}

func TestExtractNamedStreamToOutputFile(t *testing.T) {
	fs := fakefs.New("/target")
	if err := fs.Entry("/target").WriteStream("secret", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "recovered.bin")
	deps := fakeExtractDeps(fs)
	deps.writeFile = func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }

	stdout, err := runExtractCmd(t, deps, "/target", "-t", "named-stream", "--stream-name", "secret", "--image", "x", "-o", outPath)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout when writing to a file, got %q", stdout)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("unexpected file contents %q", got)
	}
}

func TestExtractOutputExistingFileErrors(t *testing.T) {
	fs := fakefs.New("/target")
	if err := fs.Entry("/target").WriteStream("secret", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "recovered.bin")
	if err := os.WriteFile(outPath, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runExtractCmd(t, fakeExtractDeps(fs), "/target", "-t", "named-stream", "--stream-name", "secret", "--image", "x", "-o", outPath)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if got, _ := os.ReadFile(outPath); string(got) != "keep me" {
		t.Fatalf("existing file was modified: %q", got)
	}
}

func TestExtractOutputSameAsTargetErrors(t *testing.T) {
	fs := fakefs.New("/target")
	if err := fs.Entry("/target").WriteStream("secret", []byte("payload")); err != nil {
		t.Fatal(err)
	}

	_, err := runExtractCmd(t, fakeExtractDeps(fs), "/target", "-t", "named-stream", "--stream-name", "secret", "--image", "x", "-o", "/target")
	if err == nil || !strings.Contains(err.Error(), "same file as the target") {
		t.Fatalf("expected same-as-target error, got %v", err)
	}
}

func TestExtractOutputSameAsImageErrors(t *testing.T) {
	fs := fakefs.New("/target")
	if err := fs.Entry("/target").WriteStream("secret", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(t.TempDir(), "disk.img")

	_, err := runExtractCmd(t, fakeExtractDeps(fs), "/target", "-t", "named-stream", "--stream-name", "secret", "--image", imgPath, "-o", imgPath)
	if err == nil || !strings.Contains(err.Error(), "same file as --image") {
		t.Fatalf("expected same-as-image error, got %v", err)
	}
}

func TestLooksBinary(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"plain text", []byte("hello\nworld\n"), false},
		{"tabs and crlf", []byte("a\tb\r\n"), false},
		{"nul byte", []byte("hello\x00world"), true},
		{"control byte", []byte("hello\x01world"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := looksBinary(c.data); got != c.want {
				t.Errorf("looksBinary(%q) = %v, want %v", c.data, got, c.want)
			}
		})
	}
}

func TestIsTerminalFalseForNonFileWriters(t *testing.T) {
	if isTerminal(new(bytes.Buffer)) {
		t.Fatal("a bytes.Buffer is never a terminal")
	}
}

func TestExtractSlackSpaceRecoversPayload(t *testing.T) {
	fs := fakefs.New("/slack.bin")
	fs.Entry("/slack.bin").Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fs.Img.Data))}}
	slack, _ := technique.Get(technique.SlackSpace)
	if _, err := slack.Hide(fs.Entry("/slack.bin"), technique.Request{Data: []byte("hidden!"), Image: fs.Img}); err != nil {
		t.Fatal(err)
	}

	out, err := runExtractCmd(t, fakeExtractDeps(fs), "/slack.bin", "-t", "slack-space", "--image", "x")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hidden!" {
		t.Fatalf("unexpected recovered payload %q", out)
	}
}

func TestExtractSlackSpaceNoFrameErrors(t *testing.T) {
	fs := fakefs.New("/slack.bin")
	fs.Entry("/slack.bin").Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fs.Img.Data))}}
	out, err := runExtractCmd(t, fakeExtractDeps(fs), "/slack.bin", "-t", "slack-space", "--image", "x")
	if err == nil {
		t.Fatalf("expected an error for an unframed region, got %q", out)
	}
	if !strings.Contains(err.Error(), "no framed slack payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestExtractLiveModeCallsOpenLiveReadOnly covers the live-mode path (no
// --image), which fakeExtractDeps otherwise never exercises: it asserts
// extract requests openLive with write=false, for both techniques.
func TestExtractLiveModeCallsOpenLiveReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"named-stream", []string{"/target", "-t", "named-stream", "--stream-name", "secret"}},
		{"slack-space", []string{"/slack.bin", "-t", "slack-space"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.args[0]
			fs := fakefs.New(target)
			if tc.name == "named-stream" {
				if err := fs.Entry(target).WriteStream("secret", []byte("payload")); err != nil {
					t.Fatal(err)
				}
			} else {
				fs.Entry(target).Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fs.Img.Data))}}
				slack, _ := technique.Get(technique.SlackSpace)
				if _, err := slack.Hide(fs.Entry(target), technique.Request{Data: []byte("payload"), Image: fs.Img}); err != nil {
					t.Fatal(err)
				}
			}

			var gotTarget, gotTech string
			var gotWrite bool
			deps := extractDependencies{
				openImage: func(string) (openedTarget, error) {
					t.Fatal("image mode opened despite no --image")
					return openedTarget{}, nil
				},
				openLive: func(target, tech string, write bool) (openedTarget, error) {
					gotTarget, gotTech, gotWrite = target, tech, write
					return openedTarget{filesystem: fs, image: imagepkg.ReadOnly(fs.Img)}, nil
				},
			}

			out, err := runExtractCmd(t, deps, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if out != "payload" {
				t.Fatalf("unexpected recovered payload %q", out)
			}
			if gotTarget != target {
				t.Fatalf("openLive target = %q, want %q", gotTarget, target)
			}
			tech := tc.args[2]
			if gotTech != tech {
				t.Fatalf("openLive technique = %q, want %q", gotTech, tech)
			}
			if gotWrite {
				t.Fatal("extract must request openLive with write=false")
			}
		})
	}
}

func TestExtractRejectsBadFlagsBeforeOpeningTarget(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "technique", args: []string{"/target"}, want: "--technique is required"},
		{name: "timestomp", args: []string{"/target", "-t", "timestomp"}, want: "no retrievable payload"},
		{name: "unknown", args: []string{"/target", "-t", "shadow-copy"}, want: "unknown technique"},
		{name: "named stream name", args: []string{"/target", "-t", "named-stream"}, want: "--stream-name is required"},
		{name: "slack incompatible", args: []string{"/target", "-t", "slack-space", "--stream-name", "secret"}, want: "incompatible"},
		{name: "empty image", args: []string{"/target", "-t", "named-stream", "--stream-name", "secret", "--image="}, want: "non-empty path"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened := false
			deps := extractDependencies{
				openImage: func(string) (openedTarget, error) {
					opened = true
					return openedTarget{}, errors.New("unexpected open")
				},
				openLive: func(string, string, bool) (openedTarget, error) {
					opened = true
					return openedTarget{}, errors.New("unexpected open")
				},
			}
			_, err := runExtractCmd(t, deps, test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
			if opened {
				t.Fatal("target was opened despite invalid flags")
			}
		})
	}
}

func TestExtractPerformsNoWrites(t *testing.T) {
	fs := fakefs.New("/slack.bin")
	fs.Entry("/slack.bin").Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fs.Img.Data))}}
	slack, _ := technique.Get(technique.SlackSpace)
	if _, err := slack.Hide(fs.Entry("/slack.bin"), technique.Request{Data: []byte("hidden!"), Image: fs.Img}); err != nil {
		t.Fatal(err)
	}

	before := append([]byte(nil), fs.Img.Data...)
	// fakeExtractDeps wraps fs.Img in image.ReadOnly, so a stray WriteAt would
	// surface as an error rather than corrupt bytes silently.
	if _, err := runExtractCmd(t, fakeExtractDeps(fs), "/slack.bin", "-t", "slack-space", "--image", "x"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, fs.Img.Data) {
		t.Fatal("extract mutated the image")
	}
}
