package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sahithyandev/nemo/internal/filesystem"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
	"github.com/spf13/cobra"
)

type extractOptions struct {
	technique  string
	image      string
	streamName string
	output     string
}

type extractDependencies struct {
	openImage func(string) (openedTarget, error)
	openLive  func(target, technique string, write bool) (openedTarget, error)
	writeFile func(path string, data []byte) error
}

func defaultExtractDependencies() extractDependencies {
	return extractDependencies{
		// Read-only, like detect: the image is opened read-only and never
		// wrapped in custody, so extract cannot write to the target.
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
		openLive: openLiveTarget,
		writeFile: func(path string, data []byte) error {
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			if _, err := f.Write(data); err != nil {
				_ = f.Close()
				return err
			}
			return f.Close()
		},
	}
}

func newExtractCommand(dependencies extractDependencies) *cobra.Command {
	options := extractOptions{}
	command := &cobra.Command{
		Use:   "extract <target>",
		Short: "Recover a previously hidden payload from a target",
		Long: "Read a payload hidden with named-stream or slack-space back out and write it " +
			"to a file or standard output. extract is read-only: it never writes to the target " +
			"and never touches the custody log. Supplying --image selects image mode; omitting " +
			"it selects live mode.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runExtract(command, args[0], options, dependencies)
		},
		SilenceUsage: true,
	}

	flags := command.Flags()
	flags.StringVarP(&options.technique, "technique", "t", "", "technique to extract: named-stream or slack-space (required)")
	flags.StringVarP(&options.image, "image", "i", "", "raw disk image path (selects image mode)")
	flags.StringVar(&options.streamName, "stream-name", "", "stream name to read (required for named-stream)")
	flags.StringVarP(&options.output, "output", "o", "", "file to write the recovered payload to (default: stdout)")

	return command
}

func runExtract(command *cobra.Command, target string, options extractOptions, dependencies extractDependencies) error {
	selected, err := validateExtract(command, target, options)
	if err != nil {
		return err
	}

	var opened openedTarget
	if command.Flags().Changed("image") {
		opened, err = dependencies.openImage(options.image)
	} else {
		opened, err = dependencies.openLive(target, options.technique, false)
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

	entry, err := opened.filesystem.Open(target)
	if err != nil {
		return fmt.Errorf("open target %q: %w", target, err)
	}

	payload, err := selected.Extract(entry, technique.Request{
		StreamName: options.streamName,
		Image:      opened.image,
	})
	if err != nil {
		return fmt.Errorf("extract %q with %s: %w", target, options.technique, err)
	}

	if options.output == "" {
		if _, err := command.OutOrStdout().Write(payload); err != nil {
			return fmt.Errorf("write payload to stdout: %w", err)
		}
		return nil
	}
	if err := dependencies.writeFile(options.output, payload); err != nil {
		return fmt.Errorf("write payload to %q: %w", options.output, err)
	}
	return nil
}

func validateExtract(command *cobra.Command, target string, options extractOptions) (technique.Technique, error) {
	if options.technique == "" {
		return nil, errors.New("--technique is required")
	}
	// timestomp overwrites a timestamp field in place and stores no retrievable
	// payload, so there is nothing to extract; reject it before opening the target.
	if options.technique == technique.Timestomp {
		return nil, errors.New("timestomp has no retrievable payload to extract")
	}
	selected, err := technique.Get(options.technique)
	if err != nil {
		return nil, err
	}
	if command.Flags().Changed("image") && options.image == "" {
		return nil, errors.New("--image requires a non-empty path")
	}

	if options.output != "" {
		if _, err := os.Stat(options.output); err == nil {
			return nil, fmt.Errorf("output file %q already exists; refusing to overwrite", options.output)
		}
		// -o must never land on the target or (in image mode) the image itself;
		// O_EXCL alone doesn't catch this if the path spelling differs (relative
		// vs absolute, symlink, etc.) from one that happens not to exist yet.
		if samePath(options.output, target) {
			return nil, fmt.Errorf("--output %q must not be the same file as the target", options.output)
		}
		if command.Flags().Changed("image") && samePath(options.output, options.image) {
			return nil, fmt.Errorf("--output %q must not be the same file as --image", options.output)
		}
	}

	switch options.technique {
	case technique.NamedStream:
		if options.streamName == "" {
			return nil, errors.New("--stream-name is required for named-stream")
		}
	case technique.SlackSpace:
		if command.Flags().Changed("stream-name") {
			return nil, errors.New("--stream-name is incompatible with slack-space")
		}
	}

	return selected, nil
}

// samePath reports whether a and b name the same file, comparing absolute
// paths since the two flags are often spelled differently (relative vs
// absolute) even when they point at the same place.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
}
