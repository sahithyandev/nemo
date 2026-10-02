package validation

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExt4SampleReadOnly(t *testing.T) {
	for _, tool := range []string{"sh", "mkfs.ext4", "debugfs", "truncate"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("integration fixture requires %s", tool)
		}
	}
	root := filepath.Join(t.TempDir(), "sample")
	if out, err := exec.Command("sh", "../../docs/validation/create-sample.sh", root).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(root, "sample.ext4"))
	if err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(data)
	f, err := os.Open("../../docs/validation/synthetic-ext4-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(root, m)
	if err != nil || !report.Complete() || report.Totals.Pass != 3 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	m.Cases[0].Expected[0].Size++
	report, err = Run(root, m)
	if err != nil || report.Totals.Fail != 1 {
		t.Fatalf("expected mismatch: %+v %v", report, err)
	}
	m.Cases[0].Technique = "timestomp"
	report, err = Run(root, m)
	if err != nil || report.Totals.Unsupported != 1 {
		t.Fatalf("unsupported: %+v %v", report, err)
	}
	m.Cases[0].Technique = "named-stream"
	m.Cases[0].Target = "/absent"
	report, err = Run(root, m)
	if err != nil || report.Totals.Error != 1 {
		t.Fatalf("missing target: %+v %v", report, err)
	}
	m.Cases[0].Filesystem = "ntfs"
	report, err = Run(root, m)
	if err != nil || report.Totals.Error != 1 {
		t.Fatalf("filesystem mismatch: %+v %v", report, err)
	}
	data, err = os.ReadFile(filepath.Join(root, "sample.ext4"))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(data) != before {
		t.Fatal("detection modified image")
	}
}
