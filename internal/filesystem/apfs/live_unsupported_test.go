//go:build !darwin

package apfs

import (
	"errors"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

func TestOpenLiveUnsupportedOffDarwin(t *testing.T) {
	if _, err := OpenLive("/tmp"); !errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("OpenLive: err = %v, want filesystem.ErrUnsupported", err)
	}
}

func TestOpenLiveSlackUnsupportedOffDarwin(t *testing.T) {
	_, _, _, err := OpenLiveSlack("/tmp", false, nil)
	if !errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("OpenLiveSlack: err = %v, want filesystem.ErrUnsupported", err)
	}
}
