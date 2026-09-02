package technique

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

func TestGetAcceptsExactlyThreeNames(t *testing.T) {
	for _, name := range []string{NamedStream, SlackSpace, Timestomp} {
		if _, err := Get(name); err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
	}
	if _, err := Get("shadow-copy"); err == nil {
		t.Fatal("Get accepted an unknown technique")
	}
}

func TestNamedStreamHideDetectClearRoundTrip(t *testing.T) {
	fake := fakefs.New("/target")
	entry, err := fake.Open("/target")
	if err != nil {
		t.Fatal(err)
	}
	tech, _ := Get(NamedStream)

	result, err := tech.Hide(entry, Request{StreamName: "secret", Data: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Technique != NamedStream || result.Target != "/target" || result.Detail != "secret" || result.Bytes != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}

	findings, err := tech.Detect(entry, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Location != "secret" || findings[0].Size != 7 {
		t.Fatalf("unexpected findings: %+v", findings)
	}

	if _, err := tech.Clear(entry, Request{StreamName: "secret"}); err != nil {
		t.Fatal(err)
	}
	findings, err = tech.Detect(entry, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("stream still present after clear: %+v", findings)
	}
}

func TestNamedStreamClearRequiresStreamName(t *testing.T) {
	fake := fakefs.New("/target")
	entry, _ := fake.Open("/target")
	tech, _ := Get(NamedStream)
	if _, err := tech.Clear(entry, Request{}); err == nil {
		t.Fatal("expected error for missing stream name")
	}
}

func TestNamedStreamHideRequiresStreamName(t *testing.T) {
	fake := fakefs.New("/target")
	entry, _ := fake.Open("/target")
	tech, _ := Get(NamedStream)
	if _, err := tech.Hide(entry, Request{Data: []byte("payload")}); err == nil {
		t.Fatal("expected error for missing stream name")
	}
	if findings, _ := tech.Detect(entry, Request{}); len(findings) != 0 {
		t.Fatalf("stream written despite empty name: %+v", findings)
	}
}

func TestReadRegionRejectsOutOfRangeLength(t *testing.T) {
	fake := fakefs.New("/target")
	if _, err := readRegion(fake.Img, -1, 0); err == nil {
		t.Fatal("expected error for negative region length")
	}
}

func TestSlackSpaceHideDetectClearRoundTrip(t *testing.T) {
	fake := fakefs.New("/target")
	// Pre-fill both regions with ordinary residual bytes.
	for i := range fake.Img.Data {
		fake.Img.Data[i] = 0xAB
	}
	fake.Entry("/target").Slack = []filesystem.SlackRegion{
		{Offset: 10, Length: 4},   // too small once framed
		{Offset: 40, Length: 128}, // fits
	}
	entry, _ := fake.Open("/target")
	tech, _ := Get(SlackSpace)

	var backups []Backup
	req := Request{
		Data:  []byte("payload"),
		Image: fake.Img,
		Backup: func(b Backup) error {
			backups = append(backups, b)
			return nil
		},
	}

	result, err := tech.Hide(entry, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Detail != "40-59" || result.Bytes != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(backups) != 1 || len(backups[0].Original) != 19 || backups[0].Original[0] != 0xAB {
		t.Fatalf("backup not recorded before write: %+v", backups)
	}

	findings, err := tech.Detect(entry, Request{Image: fake.Img})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Location != "40-59" || findings[0].Size != 7 {
		t.Fatalf("unexpected findings: %+v", findings)
	}

	// A wrong-length restore slice is rejected, not silently zero-filled.
	if _, err := tech.Clear(entry, Request{Image: fake.Img, Restore: []byte("too short")}); err == nil {
		t.Fatal("expected wrong-length restore to be rejected")
	}

	// Clear restoring the original residual bytes.
	res, err := tech.Clear(entry, Request{Image: fake.Img, Restore: backups[0].Original})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Restored {
		t.Fatalf("Result.Restored is false after a restore-from-backup clear: %+v", res)
	}
	findings, err = tech.Detect(entry, Request{Image: fake.Img})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("payload still detected after clear: %+v", findings)
	}
	for _, b := range fake.Img.Data[40:59] {
		if b != 0xAB {
			t.Fatalf("original residual bytes not restored")
		}
	}
}

func TestSlackSpaceDetectIgnoresResidualBytes(t *testing.T) {
	fake := fakefs.New("/target")
	region := filesystem.SlackRegion{Offset: 40, Length: 64}
	fake.Entry("/target").Slack = []filesystem.SlackRegion{region}
	entry, _ := fake.Open("/target")
	tech, _ := Get(SlackSpace)

	cases := map[string][]byte{
		"noise":           []byte("just some leftover file data here"),
		"magic no crc":    append([]byte("NEMO\x05\x00\x00\x00\x00\x00\x00\x00"), []byte("hello")...),
		"truncated frame": []byte("NEMO\x10"),
	}
	for name, bytes := range cases {
		for i := range fake.Img.Data {
			fake.Img.Data[i] = 0
		}
		copy(fake.Img.Data[region.Offset:], bytes)
		findings, err := tech.Detect(entry, Request{Image: fake.Img})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(findings) != 0 {
			t.Fatalf("%s: residual bytes reported as a finding: %+v", name, findings)
		}
	}
}

func TestSlackSpaceHideBackupErrorAborts(t *testing.T) {
	fake := fakefs.New("/target")
	fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 40, Length: 128}}
	entry, _ := fake.Open("/target")
	tech, _ := Get(SlackSpace)

	_, err := tech.Hide(entry, Request{
		Data:   []byte("payload"),
		Image:  fake.Img,
		Backup: func(Backup) error { return errors.New("disk full") },
	})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, b := range fake.Img.Data[40:60] {
		if b != 0 {
			t.Fatal("slack was written despite backup failure")
		}
	}
}

func TestSlackSpaceHideInsufficientSpace(t *testing.T) {
	fake := fakefs.New("/target")
	fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 10, Length: 4}}
	entry, _ := fake.Open("/target")
	tech, _ := Get(SlackSpace)
	if _, err := tech.Hide(entry, Request{Data: []byte("payload"), Image: fake.Img}); err == nil {
		t.Fatal("expected insufficient-space error")
	}
}

func TestSlackSpaceHideRejectsRegionPastImageEnd(t *testing.T) {
	fake := fakefs.New("/target") // 1KiB image
	fake.Entry("/target").Slack = []filesystem.SlackRegion{{Offset: 1020, Length: 128}}
	entry, _ := fake.Open("/target")
	tech, _ := Get(SlackSpace)
	_, err := tech.Hide(entry, Request{Data: []byte("payload"), Image: fake.Img})
	if err == nil || !strings.Contains(err.Error(), "past the image end") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTimestompHideAndClear(t *testing.T) {
	fake := fakefs.New("/target")
	entry, _ := fake.Open("/target")
	tech, _ := Get(Timestomp)
	stomped := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	original := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)

	if _, err := tech.Hide(entry, Request{Field: filesystem.TimeModified, Timestamp: stomped}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Entry("/target").Times[filesystem.TimeModified]; !got.Equal(stomped) {
		t.Fatalf("unexpected modified time %v", got)
	}

	// Detect can't see it — no timestamp reader on the capability.
	findings, err := tech.Detect(entry, Request{})
	if err != nil || findings != nil {
		t.Fatalf("timestomp detect: %+v, %v", findings, err)
	}

	if _, err := tech.Clear(entry, Request{}); err == nil {
		t.Fatal("clear without a timestamp should fail")
	}
	if _, err := tech.Clear(entry, Request{Field: filesystem.TimeModified, Timestamp: original}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Entry("/target").Times[filesystem.TimeModified]; !got.Equal(original) {
		t.Fatalf("timestamp not restored: %v", got)
	}
}

func TestTimestompRejectsInvalidFieldAndZeroTime(t *testing.T) {
	fake := fakefs.New("/target")
	entry, _ := fake.Open("/target")
	tech, _ := Get(Timestomp)
	ts := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)

	if _, err := tech.Hide(entry, Request{Field: "bogus", Timestamp: ts}); err == nil {
		t.Fatal("Hide accepted an invalid time field")
	}
	if _, err := tech.Hide(entry, Request{Field: filesystem.TimeModified}); err == nil {
		t.Fatal("Hide accepted a zero timestamp")
	}
	if _, err := tech.Clear(entry, Request{Field: "bogus", Timestamp: ts}); err == nil {
		t.Fatal("Clear accepted an invalid time field")
	}
	if _, ok := fake.Entry("/target").Times[""]; ok {
		t.Fatal("invalid field was written through to the filesystem")
	}
}

func TestOperationsReturnErrUnsupported(t *testing.T) {
	for _, name := range []string{NamedStream, SlackSpace, Timestomp} {
		tech, _ := Get(name)
		req := Request{StreamName: "s", Timestamp: time.Now(), Image: nil}
		for op, call := range map[string]func() error{
			"Hide":   func() error { _, e := tech.Hide(basicEntry{}, req); return e },
			"Detect": func() error { _, e := tech.Detect(basicEntry{}, req); return e },
			"Clear":  func() error { _, e := tech.Clear(basicEntry{}, req); return e },
		} {
			if err := call(); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("%s.%s: want ErrUnsupported, got %v", name, op, err)
			}
		}
	}
}

type basicEntry struct{}

func (basicEntry) Path() string { return "/target" }
func (basicEntry) IsDir() bool  { return false }
func (basicEntry) Children() ([]filesystem.Entry, error) {
	return nil, errors.New("not a directory")
}
func (basicEntry) NamedStreams() ([]string, error) { return nil, fs.ErrNotExist }
