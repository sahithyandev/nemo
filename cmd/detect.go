package cmd

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/sahithyandev/nemo/internal/filesystem"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
	"github.com/spf13/cobra"
)

type detectOptions struct {
	technique string
	image     string
}

type detectDependencies struct {
	openImage func(string) (openedTarget, error)
	openLive  func(string) (openedTarget, error)
}

func defaultDetectDependencies() detectDependencies {
	return detectDependencies{
		openImage: func(path string) (openedTarget, error) {
			img, err := imagepkg.OpenReadOnly(path)
			if err != nil {
				return openedTarget{}, fmt.Errorf("open image %q: %w", path, err)
			}
			ro := imagepkg.ReadOnly(img)
			fs, err := filesystem.Open(ro)
			if err != nil {
				_ = img.Close()
				return openedTarget{}, err
			}
			return openedTarget{filesystem: fs, image: ro, close: img.Close}, nil
		},
		openLive: func(string) (openedTarget, error) {
			return openedTarget{}, errors.New("live mode is unavailable: no native filesystem implementation is registered")
		},
	}
}

// detectTechniques is the default set scanned when --technique is not given.
var detectTechniques = []string{technique.NamedStream, technique.SlackSpace, technique.Timestomp}

func newDetectCommand(dependencies detectDependencies) *cobra.Command {
	options := detectOptions{}
	command := &cobra.Command{
		Use:   "detect [target]",
		Short: "Scan a target or whole image for hidden data",
		Long: "Scan a file, a directory subtree, or (in image mode, with no target) an entire " +
			"image for data hidden with the supported techniques. detect is read-only: it never " +
			"writes to the target.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runDetect(command, target, options, dependencies)
		},
	}

	flags := command.Flags()
	flags.StringVarP(&options.technique, "technique", "t", "", "restrict the scan to one technique: named-stream, slack-space, or timestomp (default: all three)")
	flags.StringVarP(&options.image, "image", "i", "", "raw disk image path (selects image mode)")

	return command
}

func runDetect(command *cobra.Command, target string, options detectOptions, dependencies detectDependencies) error {
	imageMode := command.Flags().Changed("image")

	var selected []technique.Technique
	explicit := options.technique != ""
	if explicit {
		one, err := technique.Get(options.technique)
		if err != nil {
			return err
		}
		selected = []technique.Technique{one}
	} else {
		for _, name := range detectTechniques {
			one, _ := technique.Get(name)
			selected = append(selected, one)
		}
	}

	if imageMode && options.image == "" {
		return errors.New("--image requires a non-empty path")
	}
	if target == "" && !imageMode {
		return errors.New("a whole-filesystem scan requires image mode; give a target or --image")
	}

	var opened openedTarget
	var err error
	if imageMode {
		opened, err = dependencies.openImage(options.image)
	} else {
		opened, err = dependencies.openLive(target)
	}
	if err != nil {
		return err
	}
	if opened.close != nil {
		defer opened.close()
	}
	if opened.filesystem == nil {
		return errors.New("open target: filesystem implementation returned nil")
	}

	var root filesystem.Entry
	if target == "" {
		root = opened.filesystem.Root()
	} else {
		root, err = opened.filesystem.Open(target)
		if err != nil {
			return fmt.Errorf("open target %q: %w", target, err)
		}
	}

	// unsupported[i] tracks whether technique i hit ErrUnsupported on every
	// entry visited. With an explicit --technique, an all-unsupported scan is
	// an error; the default scan just skips (timestomp Detect is unsupported
	// by design and would otherwise abort every plain `nemo detect`).
	unsupported := make([]bool, len(selected))
	for i := range unsupported {
		unsupported[i] = true
	}

	req := technique.Request{Image: opened.image}
	var findings []reportedFinding
	err = walkEntries(root, func(entry filesystem.Entry) error {
		for i, tech := range selected {
			got, derr := tech.Detect(entry, req)
			if errors.Is(derr, technique.ErrUnsupported) {
				continue
			}
			unsupported[i] = false
			if derr != nil {
				return fmt.Errorf("detect %s on %q: %w", tech.Name(), entry.Path(), derr)
			}
			for _, f := range got {
				findings = append(findings, reportedFinding{target: entry.Path(), finding: f})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if explicit && unsupported[0] {
		return fmt.Errorf("%s is unsupported on this filesystem", options.technique)
	}

	return writeFindings(command.OutOrStdout(), findings)
}

type reportedFinding struct {
	target  string
	finding technique.Finding
}

// walkEntries visits entry and every descendant reachable through Children(),
// mirroring io/fs.WalkDir's shape.
func walkEntries(entry filesystem.Entry, visit func(filesystem.Entry) error) error {
	if err := visit(entry); err != nil {
		return err
	}
	children, err := entry.Children()
	if err != nil {
		return fmt.Errorf("list children of %q: %w", entry.Path(), err)
	}
	for _, child := range children {
		if err := walkEntries(child, visit); err != nil {
			return err
		}
	}
	return nil
}

func writeFindings(output io.Writer, findings []reportedFinding) error {
	if len(findings) == 0 {
		return nil
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "TECHNIQUE\tTARGET\tLOCATION\tSIZE"); err != nil {
		return fmt.Errorf("write findings: %w", err)
	}
	for _, r := range findings {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%d\n", r.finding.Technique, r.target, r.finding.Location, r.finding.Size); err != nil {
			return fmt.Errorf("write findings: %w", err)
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write findings: %w", err)
	}
	return nil
}
