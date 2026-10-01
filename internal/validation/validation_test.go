package validation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{Version: 1, Provenance: "test", Cases: []Case{{ID: "one", Image: "sample.ext4", Filesystem: "ext4", Target: "/sample.txt", Technique: "named-stream", Expected: []Finding{}}}}
}
func TestParse(t *testing.T) {
	m := validManifest()
	b, _ := json.Marshal(m)
	if _, err := Parse(bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{string(b) + " {}", strings.Replace(string(b), `"version":1`, `"version":2`, 1), strings.Replace(string(b), `"version":1`, `"version":1,"typo":1`, 1), strings.Replace(string(b), `"expected":[]`, `"expected":null`, 1)} {
		if _, err := Parse(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	for _, p := range []string{"../outside", "/absolute", "a/../b", `C:\outside`, `a\b`} {
		m := validManifest()
		m.Cases[0].Image = p
		if err := m.Validate(); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	m.Cases = append(m.Cases, m.Cases[0])
	if m.Validate() == nil {
		t.Fatal("accepted duplicate ids")
	}
}
func TestExactScoring(t *testing.T) {
	a := Finding{"user.a", 3}
	b := Finding{"user.b", 4}
	for _, tc := range []struct {
		a, b []Finding
		want bool
	}{{nil, []Finding{}, true}, {[]Finding{a, b}, []Finding{b, a}, true}, {[]Finding{a}, []Finding{a, b}, false}, {[]Finding{a, a}, []Finding{a}, false}, {[]Finding{a}, []Finding{{"user.a", 4}}, false}} {
		if got := Equal(tc.a, tc.b); got != tc.want {
			t.Errorf("Equal(%v,%v)=%v", tc.a, tc.b, got)
		}
	}
}
func TestRunErrors(t *testing.T) {
	root := t.TempDir()
	m := validManifest()
	r, err := Run(root, m)
	if err != nil || r.Totals.Missing != 1 || r.Complete() {
		t.Fatalf("missing: %+v %v", r, err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.ext4"), []byte("garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = Run(root, m)
	if err != nil || r.Totals.Error != 1 {
		t.Fatalf("invalid image: %+v %v", r, err)
	}
	if _, err = Run(filepath.Join(root, "absent"), m); err == nil {
		t.Fatal("missing root accepted")
	}
}
func TestRootRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "sample.ext4")); err != nil {
		t.Skip(err)
	}
	r, err := Run(root, validManifest())
	if err != nil || r.Totals.Error != 1 {
		t.Fatalf("escape: %+v %v", r, err)
	}
}
func TestTotals(t *testing.T) {
	var total Totals
	for _, s := range []string{"pass", "fail", "missing", "unsupported", "error"} {
		total.add(s)
	}
	if total != (Totals{1, 1, 1, 1, 1}) {
		t.Fatal(total)
	}
}
