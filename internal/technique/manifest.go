package technique

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// ManifestName is the default file hide appends backup records to and clear
// reads them back from. It is JSON Lines: one Backup per line. Backup.Original
// serialises as base64 (encoding/json's []byte default) and Backup.Timestamp
// as RFC 3339, so the manifest is greppable and diffable.
const ManifestName = "nemo-manifest.jsonl"

// AppendManifest appends one backup record to the manifest at path, creating
// it if needed. It is the persistence half of the Request.Backup contract:
// hide passes a closure calling this, clear replays it via LoadManifest +
// LatestBackup. A write failure here aborts the hide before any bytes are
// overwritten.
func AppendManifest(path string, b Backup) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	if err := json.NewEncoder(f).Encode(b); err != nil {
		f.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close manifest: %w", err)
	}
	return nil
}

// LoadManifest reads every backup record from the manifest at path. A missing
// file is an error the caller can check with os.IsNotExist.
func LoadManifest(path string) ([]Backup, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var out []Backup
	for line := 1; scanner.Scan(); line++ {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var b Backup
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("manifest line %d: %w", line, err)
		}
		out = append(out, b)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return out, nil
}

// LatestBackup returns the most recent record matching technique and target
// (and Location, when location is non-empty). Later entries win, so re-hiding
// a target and then clearing restores the bytes from the last hide.
func LatestBackup(backups []Backup, technique, target, location string) (Backup, bool) {
	var (
		found Backup
		ok    bool
	)
	for _, b := range backups {
		if b.Technique != technique || b.Target != target {
			continue
		}
		if location != "" && b.Location != location {
			continue
		}
		found, ok = b, true
	}
	return found, ok
}
