//go:build linux

package cmd

import (
	"errors"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
)

func TestLinuxLiveSlackWritesRefused(t *testing.T) {
	// The router must reject mounted writes before device or target lookup.
	_, err := openLiveTarget("/missing", technique.SlackSpace, true)
	if !errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("live slack write error = %v", err)
	}
}
