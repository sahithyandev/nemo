package custody

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestNewRecordHashesWrittenBytesAndWriteEmitsJSONLine(t *testing.T) {
	at := time.Date(2026, time.August, 23, 17, 30, 0, 0, time.FixedZone("LKT", 5*60*60+30*60))
	written := []byte("hidden payload")
	record := NewRecord("hide", "named-stream", "/target", "secret", int64(len(written)), written, at)
	sum := sha256.Sum256(written)

	if record.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected hash %q", record.SHA256)
	}
	if record.Timestamp.Location() != time.UTC || !record.Timestamp.Equal(at) {
		t.Fatalf("timestamp was not normalized to UTC: %v", record.Timestamp)
	}

	var output bytes.Buffer
	if err := Write(&output, record); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 || output.Bytes()[output.Len()-1] != '\n' {
		t.Fatalf("expected JSON Lines output, got %q", output.String())
	}
	var decoded Record
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Operation != "hide" || decoded.Target != "/target" || decoded.SHA256 != record.SHA256 || !decoded.Timestamp.Equal(record.Timestamp) {
		t.Fatalf("unexpected decoded record: %+v", decoded)
	}
}
