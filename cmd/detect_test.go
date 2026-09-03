package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
)

// fakeDetectDeps returns dependencies whose openImage hands back the given
// fake filesystem, so detect can be exercised without a real image.
func fakeDetectDeps(fs *fakefs.FS) detectDependencies {
	return detectDependencies{
		openImage: func(string) (openedTarget, error) {
			return openedTarget{filesystem: fs, image: imagepkg.ReadOnly(fs.Img)}, nil
		},
		openLive: func(string) (openedTarget, error) {
			return openedTarget{}, errors.New("unexpected live open")
		},
	}
}

func runDetectCmd(t *testing.T, deps detectDependencies, args ...string) (string, error) {
	t.Helper()
	command := newDetectCommand(deps)
	out := new(bytes.Buffer)
	command.SetOut(out)
	command.SetErr(out)
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

// detectLineHas reports whether some output line contains every one of parts.
func detectLineHas(out string, parts ...string) bool {
	for _, line := range strings.Split(out, "\n") {
		ok := true
		for _, p := range parts {
			if !strings.Contains(line, p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestDetectHelpListsDocumentedArgumentsAndOptions(t *testing.T) {
	out, err := runDetectCmd(t, defaultDetectDependencies(), "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"detect [target]", "--technique", "-t", "--image", "-i"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestDetectNoTargetVisitsAllDescendants(t *testing.T) {
	fs := fakefs.New("/a/b/c.txt", "/a/d.txt", "/e.txt")
	for _, p := range []string{"/a/b/c.txt", "/a/d.txt", "/e.txt"} {
		if err := fs.Entry(p).WriteStream("secret", []byte("hi")); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runDetectCmd(t, fakeDetectDeps(fs), "--image", "x", "-t", "named-stream")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/a/b/c.txt", "/a/d.txt", "/e.txt"} {
		if !strings.Contains(out, p) {
			t.Errorf("scan did not visit %s:\n%s", p, out)
		}
	}
}

func TestDetectTargetLimitsScan(t *testing.T) {
	fs := fakefs.New("/a/b/c.txt", "/a/d.txt", "/e.txt")
	for _, p := range []string{"/a/b/c.txt", "/e.txt"} {
		if err := fs.Entry(p).WriteStream("secret", []byte("hi")); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runDetectCmd(t, fakeDetectDeps(fs), "--image", "x", "-t", "named-stream", "/a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/a/b/c.txt") {
		t.Errorf("expected /a/b/c.txt finding:\n%s", out)
	}
	if strings.Contains(out, "/e.txt") {
		t.Errorf("target /a should not have scanned /e.txt:\n%s", out)
	}
}

func TestDetectDefaultScansAllTechniques(t *testing.T) {
	fs := fakefs.New("/stream.txt", "/slack.bin")
	if err := fs.Entry("/stream.txt").WriteStream("secret", []byte("abcd")); err != nil {
		t.Fatal(err)
	}
	slackEntry := fs.Entry("/slack.bin")
	slackEntry.Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fs.Img.Data))}}
	slack, _ := technique.Get(technique.SlackSpace)
	if _, err := slack.Hide(slackEntry, technique.Request{Data: []byte("hidden!"), Image: fs.Img}); err != nil {
		t.Fatal(err)
	}

	out, err := runDetectCmd(t, fakeDetectDeps(fs), "--image", "x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "named-stream") || !strings.Contains(out, "slack-space") {
		t.Errorf("default scan missed a technique:\n%s", out)
	}
	// technique, path, location and size all present on one line
	if !detectLineHas(out, "slack-space", "/slack.bin", "0-19", "7") {
		t.Errorf("slack finding missing a column:\n%s", out)
	}
	if !detectLineHas(out, "named-stream", "/stream.txt", "secret", "4") {
		t.Errorf("named-stream finding missing a column:\n%s", out)
	}
}

// bareEntry implements only filesystem.Entry, no capability interfaces.
type bareEntry struct{}

func (bareEntry) Path() string                          { return "/" }
func (bareEntry) IsDir() bool                           { return true }
func (bareEntry) Children() ([]filesystem.Entry, error) { return nil, nil }
func (bareEntry) NamedStreams() ([]string, error)       { return nil, nil }

type bareFS struct{}

func (bareFS) Type() filesystem.Type                 { return filesystem.TypeUnknown }
func (bareFS) Root() filesystem.Entry                { return bareEntry{} }
func (bareFS) Open(string) (filesystem.Entry, error) { return bareEntry{}, nil }

func TestDetectExplicitUnsupportedTechniqueErrors(t *testing.T) {
	deps := detectDependencies{
		openImage: func(string) (openedTarget, error) {
			return openedTarget{filesystem: bareFS{}, image: imagepkg.ReadOnly(fakefs.NewImage(16))}, nil
		},
	}
	_, err := runDetectCmd(t, deps, "--image", "x", "-t", "named-stream")
	if err == nil || !strings.Contains(err.Error(), "named-stream is unsupported") {
		t.Fatalf("want unsupported error, got %v", err)
	}
}

func TestDetectPerformsNoWrites(t *testing.T) {
	fs := fakefs.New("/stream.txt", "/slack.bin")
	if err := fs.Entry("/stream.txt").WriteStream("secret", []byte("abcd")); err != nil {
		t.Fatal(err)
	}
	fs.Entry("/slack.bin").Slack = []filesystem.SlackRegion{{Offset: 0, Length: int64(len(fs.Img.Data))}}

	before := append([]byte(nil), fs.Img.Data...)
	// fakeDetectDeps wraps fs.Img in image.ReadOnly, so a stray WriteAt would
	// surface as an error rather than corrupt bytes silently.
	if _, err := runDetectCmd(t, fakeDetectDeps(fs), "--image", "x"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, fs.Img.Data) {
		t.Fatal("detect mutated the image")
	}
}

func TestDetectNoFindingsIsSilentAndSucceeds(t *testing.T) {
	fs := fakefs.New("/a.txt")
	out, err := runDetectCmd(t, fakeDetectDeps(fs), "--image", "x", "-t", "named-stream")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected no output, got:\n%s", out)
	}
}

func TestDetectWholeFilesystemScanRequiresImageMode(t *testing.T) {
	_, err := runDetectCmd(t, defaultDetectDependencies())
	if err == nil || !strings.Contains(err.Error(), "image mode") {
		t.Fatalf("want image-mode error, got %v", err)
	}
}
