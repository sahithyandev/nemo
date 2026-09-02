package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
	"github.com/spf13/cobra"
)

type hideOptions struct {
	technique  string
	image      string
	data       string
	streamName string
	field      string
	timestamp  string
	manifest   string
}

type openedTarget struct {
	filesystem filesystem.FileSystem
	image      imagepkg.Image
	close      func() error
}

type hideDependencies struct {
	openImage    func(string) (openedTarget, error)
	openLive     func(string) (openedTarget, error)
	readFile     func(string) ([]byte, error)
	now          func() time.Time
	writeCustody func(io.Writer, custody.Record) error
	appendBackup func(string, technique.Backup) error
}

func defaultHideDependencies() hideDependencies {
	return hideDependencies{
		openImage: func(path string) (openedTarget, error) {
			img, err := imagepkg.Open(path)
			if err != nil {
				return openedTarget{}, fmt.Errorf("open image %q: %w", path, err)
			}
			fs, err := filesystem.Open(img)
			if err != nil {
				_ = img.Close()
				return openedTarget{}, err
			}
			return openedTarget{filesystem: fs, image: img, close: img.Close}, nil
		},
		openLive: func(string) (openedTarget, error) {
			return openedTarget{}, errors.New("live mode is unavailable: no native filesystem implementation is registered")
		},
		readFile:     os.ReadFile,
		now:          time.Now,
		writeCustody: custody.Write,
		appendBackup: technique.AppendManifest,
	}
}

func newHideCommand(dependencies hideDependencies) *cobra.Command {
	options := hideOptions{}
	command := &cobra.Command{
		Use:   "hide <target>",
		Short: "Hide data against a filesystem target",
		Long: "Hide payload data in a named stream or slack space, or alter a target timestamp. " +
			"Supplying --image selects image mode; omitting it selects live mode.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runHide(command, args[0], options, dependencies)
		},
		SilenceUsage: true,
	}

	flags := command.Flags()
	flags.StringVarP(&options.technique, "technique", "t", "", "hiding technique: named-stream, slack-space, or timestomp (required)")
	flags.StringVarP(&options.image, "image", "i", "", "raw disk image path (selects image mode)")
	flags.StringVarP(&options.data, "data", "d", "", "payload file path (required for named-stream and slack-space)")
	flags.StringVar(&options.streamName, "stream-name", "", "stream name (required for named-stream)")
	flags.StringVar(&options.field, "field", "", "timestamp field: created, modified, or accessed (required for timestomp)")
	flags.StringVar(&options.timestamp, "timestamp", "", "RFC 3339 timestamp value (required for timestomp)")
	flags.StringVar(&options.manifest, "manifest", technique.ManifestName, "path to the backup manifest (records overwritten slack bytes so clear can restore them)")

	return command
}

func runHide(command *cobra.Command, target string, options hideOptions, dependencies hideDependencies) error {
	selected, timestamp, err := validateHide(command, options)
	if err != nil {
		return err
	}

	var payload []byte
	if options.data != "" {
		payload, err = dependencies.readFile(options.data)
		if err != nil {
			return fmt.Errorf("read payload %q: %w", options.data, err)
		}
	}

	var opened openedTarget
	if command.Flags().Changed("image") {
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

	entry, err := opened.filesystem.Open(target)
	if err != nil {
		return fmt.Errorf("open target %q: %w", target, err)
	}
	result, err := selected.Hide(entry, technique.Request{
		Data:       payload,
		StreamName: options.streamName,
		Field:      filesystem.TimeField(options.field),
		Timestamp:  timestamp,
		Image:      opened.image,
		Backup: func(b technique.Backup) error {
			return dependencies.appendBackup(options.manifest, b)
		},
	})
	if err != nil {
		return fmt.Errorf("hide %q with %s: %w", target, options.technique, err)
	}

	written := payload
	if selected.Name() == technique.Timestomp {
		written = []byte(result.Detail)
	}
	record := custody.NewRecord("hide", result.Technique, result.Target, result.Detail, result.Bytes, written, dependencies.now())
	if err := dependencies.writeCustody(command.OutOrStdout(), record); err != nil {
		return fmt.Errorf("write custody record: %w", err)
	}
	return nil
}

func validateHide(command *cobra.Command, options hideOptions) (technique.Technique, time.Time, error) {
	if options.technique == "" {
		return nil, time.Time{}, errors.New("--technique is required")
	}
	selected, err := technique.Get(options.technique)
	if err != nil {
		return nil, time.Time{}, err
	}
	if command.Flags().Changed("image") && options.image == "" {
		return nil, time.Time{}, errors.New("--image requires a non-empty path")
	}

	switch options.technique {
	case technique.NamedStream:
		if options.data == "" {
			return nil, time.Time{}, errors.New("--data is required for named-stream")
		}
		if options.streamName == "" {
			return nil, time.Time{}, errors.New("--stream-name is required for named-stream")
		}
		if command.Flags().Changed("field") || command.Flags().Changed("timestamp") {
			return nil, time.Time{}, errors.New("--field and --timestamp are incompatible with named-stream")
		}
	case technique.SlackSpace:
		if options.data == "" {
			return nil, time.Time{}, errors.New("--data is required for slack-space")
		}
		if command.Flags().Changed("stream-name") || command.Flags().Changed("field") || command.Flags().Changed("timestamp") {
			return nil, time.Time{}, errors.New("--stream-name, --field, and --timestamp are incompatible with slack-space")
		}
	case technique.Timestomp:
		if options.field == "" {
			return nil, time.Time{}, errors.New("--field is required for timestomp")
		}
		if !validTimeField(options.field) {
			return nil, time.Time{}, errors.New("--field must be created, modified, or accessed")
		}
		if options.timestamp == "" {
			return nil, time.Time{}, errors.New("--timestamp is required for timestomp")
		}
		if command.Flags().Changed("data") || command.Flags().Changed("stream-name") {
			return nil, time.Time{}, errors.New("--data and --stream-name are incompatible with timestomp")
		}
		timestamp, err := time.Parse(time.RFC3339, options.timestamp)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("--timestamp must use RFC 3339 format: %w", err)
		}
		return selected, timestamp, nil
	}

	return selected, time.Time{}, nil
}

func validTimeField(field string) bool {
	switch filesystem.TimeField(field) {
	case filesystem.TimeCreated, filesystem.TimeModified, filesystem.TimeAccessed:
		return true
	default:
		return false
	}
}
