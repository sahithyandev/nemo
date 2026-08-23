// Package custody creates auditable records for mutating operations.
package custody

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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
