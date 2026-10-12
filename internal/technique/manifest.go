package technique

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/sahithyandev/nemo/internal/image"
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
	var pending error // a parse error held until we know it is not the last line
	for line := 1; scanner.Scan(); line++ {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		if pending != nil {
			// A later record means the bad line was not a torn tail.
			return nil, pending
		}
		var b Backup
		if err := json.Unmarshal(raw, &b); err != nil {
			pending = fmt.Errorf("manifest line %d: %w", line, err)
			continue
		}
		out = append(out, b)
	}
	// A single unparseable final line is a hide that was killed mid-append;
	// the records before it are still good.
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

var memoryIdentitySession = rand.Text()

// BackupImageIdentity binds backups to the backing object rather than only the
// target path inside it. File identities use the opened file's device/inode
// (or Windows volume/file index) and size; path aliases identify the same object. In-memory images have process-local identities and
// cannot accidentally match persisted file records. An unavailable identity
// never qualifies a record for restoration.
func BackupImageIdentity(img image.Image) string {
	if img == nil {
		return ""
	}
	if wrapped, ok := img.(interface{ UnderlyingImage() image.Image }); ok {
		return BackupImageIdentity(wrapped.UnderlyingImage())
	}
	if backing, ok := img.(interface{ BackupIdentity() (string, error) }); ok {
		identity, err := backing.BackupIdentity()
		if err != nil {
			return ""
		}
		return identity
	}

	return fmt.Sprintf("memory-v1:%s:%T:%p:%d", memoryIdentitySession, img, img, img.Size())
}

// LatestImageBackup excludes legacy and foreign-image records. Frame digest
// validation remains in Clear so a stale same-location backup cannot restore
// bytes over a different payload.
func LatestImageBackup(backups []Backup, technique, target, location, identity string) (Backup, bool) {
	if identity == "" {
		return Backup{}, false
	}
	var found Backup
	var ok bool
	for _, b := range backups {
		if b.Technique == technique && b.Target == target && b.Location == location && b.ImageIdentity == identity && b.FrameSHA256 != "" {
			found, ok = b, true
		}
	}
	return found, ok
}
