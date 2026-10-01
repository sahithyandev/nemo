package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/technique"
	"github.com/spf13/cobra"
)

type clearOptions struct {
	technique  string
	image      string
	streamName string
	field      string
	timestamp  string
	manifest   string
}

type clearDependencies struct {
	openImage    func(string) (openedTarget, error)
	openLive     func(target, technique string, write bool) (openedTarget, error)
	loadManifest func(string) ([]technique.Backup, error)
	now          func() time.Time
	logCustody   func(custody.Record) error
	echoCustody  func(io.Writer, custody.Record) error
}

func defaultClearDependencies() clearDependencies {
	// Share the same image opening, live-mode availability, and audit paths.
	hide := defaultHideDependencies()
	return clearDependencies{
		openImage: hide.openImage, openLive: hide.openLive,
		loadManifest: technique.LoadManifest,
		now:          hide.now, logCustody: hide.logCustody, echoCustody: hide.echoCustody,
	}
}

func newClearCommand(dependencies clearDependencies) *cobra.Command {
	options := clearOptions{}
	command := &cobra.Command{
		Use:   "clear <target>",
		Short: "Clear hidden data or restore a filesystem timestamp",
		Long: "Delete a named stream, restore or zero a framed slack payload, or restore a timestamp. " +
			"Supplying --image selects image mode; omitting it selects live mode. " +
			"Slack space is restored from a matching manifest backup when available, otherwise zeroed. " +
			"An explicit --manifest requires a matching backup.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runClear(command, args[0], options, dependencies)
		},
		SilenceUsage: true,
	}
	flags := command.Flags()
	flags.StringVarP(&options.technique, "technique", "t", "", "technique: named-stream, slack-space, or timestomp (required)")
	flags.StringVarP(&options.image, "image", "i", "", "raw disk image path (selects image mode)")
	flags.StringVar(&options.streamName, "stream-name", "", "stream to delete (required for named-stream)")
	flags.StringVar(&options.field, "field", "", "timestamp field: created, modified, accessed, or changed (required for timestomp)")
	flags.StringVar(&options.timestamp, "timestamp", "", "original RFC 3339 timestamp to restore (required for timestomp)")
	flags.StringVar(&options.manifest, "manifest", technique.ManifestName, "backup manifest for slack restoration; explicit paths require a matching backup")
	return command
}

func runClear(command *cobra.Command, target string, options clearOptions, dependencies clearDependencies) error {
	selected, timestamp, err := validateClear(command, options)
	if err != nil {
		return err
	}
	var backups []technique.Backup
	if selected.Name() == technique.SlackSpace {
		backups, err = dependencies.loadManifest(options.manifest)
		if err != nil && !(os.IsNotExist(err) && !command.Flags().Changed("manifest")) {
			return fmt.Errorf("read manifest %q: %w", options.manifest, err)
		}
	}
	var opened openedTarget
	if command.Flags().Changed("image") {
		opened, err = dependencies.openImage(options.image)
	} else {
		opened, err = dependencies.openLive(target, options.technique, true)
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
	request := technique.Request{
		StreamName: options.streamName,
		Field:      filesystem.TimeField(options.field),
		Timestamp:  timestamp,
		Image:      opened.image,
	}
	if selected.Name() == technique.SlackSpace && (len(backups) > 0 || command.Flags().Changed("manifest")) {
		findings, err := selected.Detect(entry, request)
		if err != nil {
			return fmt.Errorf("inspect %q before clear: %w", target, err)
		}
		var backup technique.Backup
		var found bool
		if len(findings) > 0 {
			backup, found = technique.LatestBackup(backups, selected.Name(), entry.Path(), findings[0].Location)
		}
		if found {
			if backup.Original == nil {
				return errors.New("slack backup has no original bytes")
			}
			request.Restore = backup.Original
		} else if command.Flags().Changed("manifest") {
			return fmt.Errorf("no matching slack backup for %q in manifest %q", entry.Path(), options.manifest)
		}
	}
	var written []byte
	// Clear supplies the overwritten frame before mutation. This callback only
	// captures bytes for auditing; it does not append a clear to the hide manifest.
	request.Backup = func(b technique.Backup) error {
		if request.Restore != nil {
			written = request.Restore
		} else {
			written = make([]byte, len(b.Original))
		}
		return nil
	}
	result, err := selected.Clear(entry, request)
	if err != nil {
		return fmt.Errorf("clear %q with %s: %w", target, options.technique, err)
	}
	if selected.Name() == technique.Timestomp {
		written = []byte(result.Detail)
	}
	// Named-stream deletion has no payload written; its audit hash is of empty bytes.
	record := custody.NewRecord("clear", result.Technique, result.Target, result.Detail, result.Bytes, written, dependencies.now())
	if err := dependencies.logCustody(record); err != nil {
		return fmt.Errorf("append custody log: %w", err)
	}
	if err := dependencies.echoCustody(command.OutOrStdout(), record); err != nil {
		return fmt.Errorf("echo custody record: %w", err)
	}
	return nil
}

func validateClear(command *cobra.Command, options clearOptions) (technique.Technique, time.Time, error) {
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
	if command.Flags().Changed("manifest") && options.manifest == "" {
		return nil, time.Time{}, errors.New("--manifest requires a non-empty path")
	}
	switch options.technique {
	case technique.NamedStream:
		if options.streamName == "" {
			return nil, time.Time{}, errors.New("--stream-name is required for named-stream")
		}
		if command.Flags().Changed("field") || command.Flags().Changed("timestamp") || command.Flags().Changed("manifest") {
			return nil, time.Time{}, errors.New("--field, --timestamp, and --manifest are incompatible with named-stream")
		}
	case technique.SlackSpace:
		if command.Flags().Changed("stream-name") || command.Flags().Changed("field") || command.Flags().Changed("timestamp") {
			return nil, time.Time{}, errors.New("--stream-name, --field, and --timestamp are incompatible with slack-space")
		}
	case technique.Timestomp:
		if options.field == "" {
			return nil, time.Time{}, errors.New("--field is required for timestomp")
		}
		if !validTimeField(options.field) {
			return nil, time.Time{}, errors.New("--field must be created, modified, accessed, or changed")
		}
		if options.timestamp == "" {
			return nil, time.Time{}, errors.New("--timestamp is required for timestomp")
		}
		if command.Flags().Changed("stream-name") || command.Flags().Changed("manifest") {
			return nil, time.Time{}, errors.New("--stream-name and --manifest are incompatible with timestomp")
		}
		timestamp, err := time.Parse(time.RFC3339, options.timestamp)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("--timestamp must use RFC 3339 format: %w", err)
		}
		if timestamp.IsZero() {
			return nil, time.Time{}, errors.New("--timestamp must be non-zero")
		}
		return selected, timestamp, nil
	}
	return selected, time.Time{}, nil
}
