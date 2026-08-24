package filesystem_test

import (
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
	"github.com/sahithyandev/nemo/internal/image"
)

func fakeDetector(typ filesystem.Type, prefix string, fs filesystem.FileSystem, techniques []string) filesystem.Detector {
	return filesystem.Detector{
		Type: typ,
		Sniff: func(signature []byte) bool {
			return strings.HasPrefix(string(signature), prefix)
		},
		New: func(image.Image) (filesystem.FileSystem, error) {
			return fs, nil
		},
		Techniques: techniques,
	}
}

func TestOpenSelectsMatchingDetector(t *testing.T) {
	filesystem.ResetRegistry()

	img := fakefs.NewImage(16)
	copy(img.Data, []byte("CLI01"))
	want := fakefs.New("/target")
	filesystem.Register(filesystem.Detector{
		Type: filesystem.TypeEXT4,
		Sniff: func(signature []byte) bool {
			return strings.HasPrefix(string(signature), "CLI01")
		},
		New: func(got image.Image) (filesystem.FileSystem, error) {
			if got != img {
				t.Fatalf("constructor received unexpected image %T", got)
			}
			return want, nil
		},
	})

	got, err := filesystem.Open(img)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("unexpected filesystem: got %T, want %T", got, want)
	}
}

func TestOpenSelectsAmongTwoFakeSignatures(t *testing.T) {
	filesystem.ResetRegistry()

	wantA := fakefs.New("/a")
	wantB := fakefs.New("/b")
	filesystem.Register(fakeDetector(filesystem.TypeEXT4, "FAKE-A", wantA, nil))
	filesystem.Register(fakeDetector(filesystem.TypeNTFS, "FAKE-B", wantB, nil))

	imgA := fakefs.NewImage(16)
	copy(imgA.Data, []byte("FAKE-A"))
	gotA, err := filesystem.Open(imgA)
	if err != nil {
		t.Fatal(err)
	}
	if gotA != wantA {
		t.Fatalf("signature FAKE-A: got %T, want %T", gotA, wantA)
	}

	imgB := fakefs.NewImage(16)
	copy(imgB.Data, []byte("FAKE-B"))
	gotB, err := filesystem.Open(imgB)
	if err != nil {
		t.Fatal(err)
	}
	if gotB != wantB {
		t.Fatalf("signature FAKE-B: got %T, want %T", gotB, wantB)
	}
}

func TestOpenRejectsUnknownSignature(t *testing.T) {
	filesystem.ResetRegistry()
	filesystem.Register(fakeDetector(filesystem.TypeEXT4, "FAKE-A", fakefs.New("/a"), nil))

	img := fakefs.NewImage(16)
	copy(img.Data, []byte("NO-MATCH"))

	_, err := filesystem.Open(img)
	if err == nil || !strings.Contains(err.Error(), "unrecognized image format") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "ext4") {
		t.Fatalf("expected error to name registered types, got: %v", err)
	}
}

func TestOpenRejectsAmbiguousSignature(t *testing.T) {
	filesystem.ResetRegistry()

	var newACalled, newBCalled bool
	filesystem.Register(filesystem.Detector{
		Type:  filesystem.TypeEXT4,
		Sniff: func([]byte) bool { return true },
		New: func(image.Image) (filesystem.FileSystem, error) {
			newACalled = true
			return fakefs.New("/a"), nil
		},
	})
	filesystem.Register(filesystem.Detector{
		Type:  filesystem.TypeNTFS,
		Sniff: func([]byte) bool { return true },
		New: func(image.Image) (filesystem.FileSystem, error) {
			newBCalled = true
			return fakefs.New("/b"), nil
		},
	})

	img := fakefs.NewImage(16)
	_, err := filesystem.Open(img)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "ext4") || !strings.Contains(err.Error(), "ntfs") {
		t.Fatalf("expected error to name both candidates, got: %v", err)
	}
	if newACalled || newBCalled {
		t.Fatal("New must not be called on ambiguous match")
	}
}

func TestRegisterPanicsOnDuplicateType(t *testing.T) {
	filesystem.ResetRegistry()
	filesystem.Register(fakeDetector(filesystem.TypeEXT4, "FAKE-A", fakefs.New("/a"), nil))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
		if !strings.Contains(r.(string), "ext4") {
			t.Fatalf("expected panic message to name the type, got: %v", r)
		}
	}()
	filesystem.Register(fakeDetector(filesystem.TypeEXT4, "FAKE-A2", fakefs.New("/a2"), nil))
}

func TestRegisterPanicsOnInvalidDetector(t *testing.T) {
	filesystem.ResetRegistry()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on detector with nil Sniff")
		}
	}()
	filesystem.Register(filesystem.Detector{
		Type: filesystem.TypeEXT4,
		New: func(image.Image) (filesystem.FileSystem, error) {
			return fakefs.New("/a"), nil
		},
	})
}

func TestDetectorsExposeTechniques(t *testing.T) {
	filesystem.ResetRegistry()
	filesystem.Register(fakeDetector(filesystem.TypeEXT4, "FAKE-A", fakefs.New("/a"), []string{"named-stream"}))

	all := filesystem.Detectors()
	if len(all) != 1 || len(all[0].Techniques) != 1 || all[0].Techniques[0] != "named-stream" {
		t.Fatalf("expected Techniques to round-trip through Detectors(), got: %+v", all)
	}
}
