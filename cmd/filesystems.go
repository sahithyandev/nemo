package cmd

import (
	// Filesystem implementations register their detectors during package init.
	_ "github.com/sahithyandev/nemo/internal/filesystem/apfs"
)
