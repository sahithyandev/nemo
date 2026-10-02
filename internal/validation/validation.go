// Package validation scores explicit, locally supplied image cases without mutation.
package validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/sahithyandev/nemo/internal/filesystem"
	_ "github.com/sahithyandev/nemo/internal/filesystem/apfs"
	_ "github.com/sahithyandev/nemo/internal/filesystem/ext4"
	_ "github.com/sahithyandev/nemo/internal/filesystem/ntfs"
	"github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
)

type Manifest struct {
	Version    int    `json:"version"`
	Provenance string `json:"provenance"`
	Cases      []Case `json:"cases"`
}
type Case struct {
	ID         string    `json:"id"`
	Image      string    `json:"image"`
	Filesystem string    `json:"filesystem"`
	Target     string    `json:"target"`
	Technique  string    `json:"technique"`
	Expected   []Finding `json:"expected"`
}
type Finding struct {
	Location string `json:"location"`
	Size     int64  `json:"size"`
}
type Result struct {
	ID         string    `json:"id"`
	Filesystem string    `json:"filesystem"`
	Technique  string    `json:"technique"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail,omitempty"`
	Expected   []Finding `json:"expected"`
	Actual     []Finding `json:"actual"`
}
type Totals struct {
	Pass        int `json:"pass"`
	Fail        int `json:"fail"`
	Missing     int `json:"missing"`
	Unsupported int `json:"unsupported"`
	Error       int `json:"error"`
}
type Report struct {
	Version int               `json:"version"`
	Results []Result          `json:"results"`
	Totals  Totals            `json:"totals"`
	Groups  map[string]Totals `json:"groups"`
}

func Parse(r io.Reader) (Manifest, error) {
	var m Manifest
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return m, errors.New("manifest must contain exactly one JSON value")
	}
	return m, m.Validate()
}
func (m Manifest) Validate() error {
	if m.Version != 1 || strings.TrimSpace(m.Provenance) == "" || len(m.Cases) == 0 {
		return errors.New("manifest requires version 1, provenance and nonempty cases")
	}
	seen := map[string]bool{}
	for _, c := range m.Cases {
		if c.ID == "" || seen[c.ID] {
			return fmt.Errorf("empty or duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		if c.Image == "" || strings.ContainsAny(c.Image, "\\:") || path.IsAbs(c.Image) || path.Clean(c.Image) != c.Image || c.Image == ".." || strings.HasPrefix(c.Image, "../") {
			return fmt.Errorf("case %s: image must be a clean relative slash path", c.ID)
		}
		if c.Target == "" || !path.IsAbs(c.Target) || path.Clean(c.Target) != c.Target {
			return fmt.Errorf("case %s: target must be a clean absolute image path", c.ID)
		}
		if c.Filesystem != "ext4" && c.Filesystem != "ntfs" && c.Filesystem != "apfs" {
			return fmt.Errorf("case %s: unknown filesystem", c.ID)
		}
		if _, err := technique.Get(c.Technique); err != nil {
			return err
		}
		if c.Expected == nil {
			return fmt.Errorf("case %s: expected must be an explicit array (empty means no findings)", c.ID)
		}
		for _, f := range c.Expected {
			if f.Location == "" || f.Size < 0 {
				return fmt.Errorf("case %s: invalid expected finding", c.ID)
			}
		}
	}
	return nil
}

// Equal compares exact multisets, preserving duplicate counts and rejecting extra findings.
func Equal(a, b []Finding) bool {
	a = append([]Finding{}, a...)
	b = append([]Finding{}, b...)
	sortFindings(a)
	sortFindings(b)
	return reflect.DeepEqual(a, b)
}
func sortFindings(a []Finding) {
	sort.Slice(a, func(i, j int) bool {
		if a[i].Location == a[j].Location {
			return a[i].Size < a[j].Size
		}
		return a[i].Location < a[j].Location
	})
}

// Run opens an os.Root to prevent image paths and symlinks escaping the supplied directory.
func Run(rootPath string, m Manifest) (Report, error) {
	report := Report{Version: 1, Results: []Result{}, Groups: map[string]Totals{}}
	if err := m.Validate(); err != nil {
		return report, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return report, err
	}
	defer root.Close()
	for _, c := range m.Cases {
		r := runCase(root, c)
		report.Results = append(report.Results, r)
		report.Totals.add(r.Status)
		key := c.Filesystem + "/" + c.Technique
		total := report.Groups[key]
		total.add(r.Status)
		report.Groups[key] = total
	}
	return report, nil
}
func (t *Totals) add(status string) {
	switch status {
	case "pass":
		t.Pass++
	case "fail":
		t.Fail++
	case "missing":
		t.Missing++
	case "unsupported":
		t.Unsupported++
	default:
		t.Error++
	}
}
func (r Report) Complete() bool {
	return r.Totals.Fail+r.Totals.Missing+r.Totals.Unsupported+r.Totals.Error == 0 && r.Totals.Pass > 0
}

type readImage struct {
	*os.File
	size int64
}

func (i readImage) Size() int64                        { return i.size }
func (i readImage) Path() string                       { return i.Name() }
func (i readImage) WriteAt([]byte, int64) (int, error) { return 0, os.ErrPermission }

func runCase(root *os.Root, c Case) Result {
	r := Result{ID: c.ID, Filesystem: c.Filesystem, Technique: c.Technique, Status: "error", Expected: c.Expected, Actual: []Finding{}}
	f, err := root.Open(filepath.FromSlash(c.Image))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.Status = "missing"
		}
		r.Detail = err.Error()
		return r
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		r.Detail = err.Error()
		return r
	}
	if !info.Mode().IsRegular() {
		r.Detail = "image must be a regular file"
		return r
	}
	img := image.ReadOnly(readImage{File: f, size: info.Size()})
	fs, err := filesystem.Open(img)
	if err != nil {
		r.Detail = err.Error()
		return r
	}
	if string(fs.Type()) != c.Filesystem {
		r.Detail = fmt.Sprintf("filesystem mismatch: expected %s, got %s", c.Filesystem, fs.Type())
		return r
	}
	// No timestamp anomaly detector exists; do not count its empty result as a pass.
	if c.Technique == technique.Timestomp {
		r.Status = "unsupported"
		r.Detail = "timestamp anomaly detection is not implemented"
		return r
	}
	entry, err := fs.Open(c.Target)
	if err != nil {
		r.Detail = fmt.Sprintf("open target: %v", err)
		return r
	}
	if entry.IsDir() {
		r.Detail = "case target must be a file; enumerate targets explicitly"
		return r
	}
	tech, _ := technique.Get(c.Technique)
	found, err := tech.Detect(entry, technique.Request{Image: img})
	if err != nil {
		if errors.Is(err, technique.ErrUnsupported) {
			r.Status = "unsupported"
		}
		r.Detail = err.Error()
		return r
	}
	for _, f := range found {
		r.Actual = append(r.Actual, Finding{Location: f.Location, Size: f.Size})
	}
	sortFindings(r.Actual)
	if Equal(c.Expected, r.Actual) {
		r.Status = "pass"
	} else {
		r.Status = "fail"
		r.Detail = "actual findings differ from expected (exact location and size multiset)"
	}
	return r
}
