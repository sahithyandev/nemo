package main

import (
	"os/exec"
	"testing"
)

// TestMain installs the versioned pre-commit hook as a side effect of
// `go test`, so it's set up even for contributors who skip `make`.
func TestMain(m *testing.M) {
	exec.Command("git", "config", "core.hooksPath", ".githooks").Run()
	m.Run()
}
