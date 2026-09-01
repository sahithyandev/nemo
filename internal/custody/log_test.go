package custody

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestAppendRecordCreatesDirectoriesAndRoundTripsRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "logs", "custody.jsonl")
	record := NewRecord(
		"hide",
		"named-stream",
		"/target",
		"secret",
		14,
		[]byte("hidden payload"),
		time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC),
	)

	if err := AppendRecord(path, record); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(contents, []byte{'\n'}) != 1 || contents[len(contents)-1] != '\n' {
		t.Fatalf("expected exactly one JSON line, got %q", contents)
	}

	var decoded Record
	if err := json.Unmarshal(bytes.TrimSuffix(contents, []byte{'\n'}), &decoded); err != nil {
		t.Fatalf("decode appended record: %v", err)
	}
	if decoded != record {
		t.Fatalf("record did not survive round-trip:\nwant: %+v\n got: %+v", record, decoded)
	}
}

func TestAppendRecordPreservesRecordsInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "custody.jsonl")
	first := NewRecord("hide", "named-stream", "/first", "one", 3, []byte("one"), time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC))
	second := NewRecord("hide", "slack-space", "/second", "two", 3, []byte("two"), time.Date(2026, time.August, 23, 12, 1, 0, 0, time.UTC))

	if err := AppendRecord(path, first); err != nil {
		t.Fatal(err)
	}
	if err := AppendRecord(path, second); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var records []Record
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0] != first || records[1] != second {
		t.Fatalf("records were truncated or reordered:\nwant: %+v, %+v\n got: %+v", first, second, records)
	}
}

func TestAppendRecordReturnsErrorForInvalidDestination(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := AppendRecord(filepath.Join(parentFile, "custody.jsonl"), Record{})
	if err == nil {
		t.Fatal("expected invalid destination error")
	}
}

func TestPersistUsesDefaultLogPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	record := NewRecord("hide", "named-stream", "/target", "secret", 1, []byte("x"), time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC))

	path, err := DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, ".nemo", "logs", "custody.jsonl")
	if path != wantPath {
		t.Fatalf("expected default path %q, got %q", wantPath, path)
	}
	if err := Persist(record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("persisted log missing: %v", err)
	}
}
