package cmd

import (
	"fmt"
	"io"
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
	return &cobra.Command{
		Use:   "features",
		Short: "Print the filesystem feature matrix",
		Long: "Print whether each registered filesystem supports named streams, " +
			"slack space, and timestomping. No image or target is required.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return writeFeatureMatrix(command.OutOrStdout(), detectors())
		},
	}
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
