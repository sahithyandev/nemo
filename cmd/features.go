package cmd

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"text/tabwriter"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
	"github.com/spf13/cobra"
)

var featureTechniques = []string{
	technique.NamedStream,
	technique.SlackSpace,
	technique.Timestomp,
}

func newFeaturesCommand(detectors func() []filesystem.Detector) *cobra.Command {
	mode := "image"
	command := &cobra.Command{
		Use:   "features",
		Short: "Print the filesystem feature matrix",
		Long: "Print whether each registered filesystem supports named streams, " +
			"slack space, and timestomping. No image or target is required.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			switch mode {
			case "image":
				return writeFeatureMatrix(command.OutOrStdout(), detectors())
			case "live":
				return writeLiveFeatureMatrix(command.OutOrStdout(), detectors(), runtime.GOOS)
			default:
				return fmt.Errorf("--mode must be image or live")
			}
		},
	}
	command.Flags().StringVar(&mode, "mode", "image", "feature mode: image or live (live reports this operating system)")
	return command
}

func writeLiveFeatureMatrix(output io.Writer, detectors []filesystem.Detector, goos string) error {
	sorted := append([]filesystem.Detector(nil), detectors...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Type < sorted[j].Type })
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "FILESYSTEM\tTECHNIQUE\tSTATUS\tLIMITS"); err != nil {
		return fmt.Errorf("write live feature matrix: %w", err)
	}
	for _, detector := range sorted {
		for _, name := range featureTechniques {
			status, limits := liveFeature(detector.Type, name, goos)
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", detector.Type, name, status, limits); err != nil {
				return fmt.Errorf("write live feature matrix: %w", err)
			}
		}
	}
	return table.Flush()
}

func liveFeature(kind filesystem.Type, name, goos string) (string, string) {
	switch {
	case kind == filesystem.TypeEXT4 && goos == "linux":
		switch name {
		case technique.NamedStream:
			return "supported", "user.* xattrs; ext4 mounts only"
		case technique.Timestomp:
			return "partial", "accessed/modified writes; created/changed writes unavailable"
		case technique.SlackSpace:
			return "read-only", "raw-device permissions required; mounted writes refused"
		}
	case kind == filesystem.TypeAPFS && goos == "darwin":
		switch name {
		case technique.NamedStream:
			return "supported", "APFS mounts only"
		case technique.Timestomp:
			return "partial", "accessed/modified/created writes; changed writes unavailable"
		case technique.SlackSpace:
			return "read-only", "raw-device permissions required; mounted writes refused"
		}
	case kind == filesystem.TypeNTFS && goos == "windows":
		return "read-only", "administrator volume access; live mutation unavailable"
	}
	return "unsupported", "unavailable on " + goos
}

func writeFeatureMatrix(output io.Writer, detectors []filesystem.Detector) error {
	sorted := append([]filesystem.Detector(nil), detectors...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Type < sorted[j].Type
	})

	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "FILESYSTEM\tTECHNIQUE\tSTATUS"); err != nil {
		return fmt.Errorf("write feature matrix: %w", err)
	}
	for _, detector := range sorted {
		supported := make(map[string]struct{}, len(detector.Techniques))
		for _, name := range detector.Techniques {
			supported[name] = struct{}{}
		}
		for _, name := range featureTechniques {
			status := "unsupported"
			if _, ok := supported[name]; ok {
				status = "supported"
			}
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", detector.Type, name, status); err != nil {
				return fmt.Errorf("write feature matrix: %w", err)
			}
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write feature matrix: %w", err)
	}
	return nil
}
