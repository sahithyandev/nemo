// Package custody creates auditable records for mutating operations.
package custody

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Record is the stable, machine-readable result of a successful write.
type Record struct {
	Operation string    `json:"operation"`
	Technique string    `json:"technique"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	Timestamp time.Time `json:"timestamp"`
}

// NewRecord creates a record and hashes the bytes supplied to the operation.
func NewRecord(operation, technique, target, detail string, bytes int64, written []byte, at time.Time) Record {
	sum := sha256.Sum256(written)
	return Record{
		Operation: operation,
		Technique: technique,
		Target:    target,
		Detail:    detail,
		Bytes:     bytes,
		SHA256:    hex.EncodeToString(sum[:]),
		Timestamp: at.UTC(),
	}
}

// Write emits one JSON Lines custody record.
func Write(writer io.Writer, record Record) error {
	return json.NewEncoder(writer).Encode(record)
}

// DefaultLogPath returns the path to the current user's Nemo custody log.
func DefaultLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nemo", "logs", "custody.jsonl"), nil
}

// AppendRecord appends one JSON Lines custody record to path.
func AppendRecord(path string, record Record) error {
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// Persist appends a record to the current user's Nemo custody log.
func Persist(record Record) error {
	path, err := DefaultLogPath()
	if err != nil {
		return err
	}
	return AppendRecord(path, record)
}
