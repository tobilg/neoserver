package report

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

func fixture(t *testing.T) (string, string, manifest.Document, Run) {
	t.Helper()
	root := t.TempDir()
	doc := manifest.Document{SchemaVersion: 2, Suites: map[string]manifest.Suite{"wms13": {Image: "ogccite/wms@sha256:abc"}}, Profiles: map[string]manifest.Profile{
		"wms13/core": {Suite: "wms13", EvidenceKind: "official"},
	}}
	path := filepath.Join(root, "manifest.json")
	writeTestJSON(t, path, doc)
	m := Metadata{Suite: "wms13-core", EvidenceKind: "official", Image: doc.Suites["wms13"].Image, Commit: "abc123",
		StartedAt: time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC), CompletedAt: time.Date(2026, 9, 19, 10, 0, 5, 0, time.UTC),
		Result: Result{Format: "ctl", Counts: Counts{Total: 2, Passed: 1, Skipped: 1}, Leaf: Counts{Total: 2, Passed: 1, Skipped: 1}, SkipCategories: map[string]int{"unclassified": 1}}}
	dir := filepath.Join(root, "conformance/wms13/core")
	writeTestJSON(t, filepath.Join(dir, "metadata.json"), m)
	writeTestFile(t, filepath.Join(dir, "junit.xml"), `<testsuite tests="2" failures="0" skipped="1"><testcase name="duplicate &lt;script&gt;alert(1)&lt;/script&gt;" classname="assertion.example"/><testcase name="duplicate &lt;script&gt;alert(1)&lt;/script&gt;" classname="assertion.example"><skipped type="unclassified" message="&lt;img src=x onerror=alert(1)&gt;"/></testcase></testsuite>`)
	writeTestFile(t, filepath.Join(dir, "result.xml"), "<execution>original raw bytes\n</execution>\n")
	run := Run{SchemaVersion: 1, Repository: "tobilg/neoserver", Commit: "abc123", SelectionKnown: true, Selected: []string{"wms13"}}
	return root, path, doc, run
}

func writeTestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(data))
}

func TestGeneratePortableEvidenceAndEscapedCases(t *testing.T) {
	input, path, _, run := fixture(t)
	output := t.TempDir()
	r, err := Generate(input, output, path, run, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "Passed with skips" || len(r.Profiles[0].Cases) != 2 {
		t.Fatalf("unexpected report: %+v", r)
	}
	page, err := os.ReadFile(filepath.Join(output, "site/profiles/wms13/core/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`id="case-1"`, `id="case-2"`, "unclassified", "../../../assets/report.css", Origin + "/latest/profiles/wms13/core/"} {
		if !strings.Contains(string(page), expected) {
			t.Errorf("missing %q", expected)
		}
	}
	if bytes.Contains(page, []byte("<script>alert")) || bytes.Contains(page, []byte("<img src=x")) {
		t.Fatal("evidence executed as HTML")
	}
	z, err := zip.OpenReader(filepath.Join(output, "site/evidence/official-ets-wms13.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, name := range []string{"core/result.xml", "core/metadata.json", "core/junit.xml"} {
		found := false
		for _, f := range z.File {
			if f.Name != name {
				continue
			}
			found = true
			r, _ := f.Open()
			archived, _ := io.ReadAll(r)
			r.Close()
			original, _ := os.ReadFile(filepath.Join(input, "conformance/wms13", name))
			if !bytes.Equal(archived, original) {
				t.Errorf("archive changed %s", name)
			}
		}
		if !found {
			t.Errorf("missing archive file %s", name)
		}
	}
	context, err := ReadRun(filepath.Join(output, "inputs/run.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Generate(filepath.Join(output, "inputs"), t.TempDir(), filepath.Join(output, "inputs/manifest.json"), context, "")
	if err != nil {
		t.Fatalf("portable inputs cannot be re-rendered: %v", err)
	}
}

func TestIncompleteEvidenceNeverPasses(t *testing.T) {
	for _, mode := range []string{"missing", "missing-raw", "bad-json", "bad-xml", "mismatched-counts", "wrong-commit", "wrong-image", "unknown-selection", "job-failure"} {
		t.Run(mode, func(t *testing.T) {
			root, _, doc, run := fixture(t)
			meta := filepath.Join(root, "conformance/wms13/core/metadata.json")
			var m Metadata
			if err := readJSON(meta, &m); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing":
				os.Remove(meta)
			case "missing-raw":
				os.Remove(filepath.Join(root, "conformance/wms13/core/result.xml"))
			case "bad-json":
				writeTestFile(t, meta, "broken")
			case "bad-xml":
				writeTestFile(t, filepath.Join(root, "conformance/wms13/core/junit.xml"), "<broken>")
			case "mismatched-counts":
				m.Result.Leaf = Counts{Total: 2, Passed: 2}
				writeTestJSON(t, meta, m)
			case "wrong-commit":
				run.Commit = "different"
			case "wrong-image":
				m.Image = "wrong"
				writeTestJSON(t, meta, m)
			case "unknown-selection":
				run.SelectionKnown = false
			case "job-failure":
				run.Jobs = map[string]string{"Official ETS": "failure"}
			}
			r, err := Load(root, doc, run)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "Incomplete" {
				t.Errorf("got %s", r.Status)
			}
		})
	}
}

func TestNoSelectionAndFilteredSummary(t *testing.T) {
	root, _, doc, run := fixture(t)
	run.Selected = []string{}
	r, err := Load(root, doc, run)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "Not run" || r.Profiles[0].Status != "Not run" {
		t.Fatal("unselected tests must not pass")
	}
	var out bytes.Buffer
	if err := Summary(&out, r, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No suites were selected") {
		t.Fatal(out.String())
	}
	run.Selected = []string{"wms13"}
	r, _ = Load(root, doc, run)
	r.Status = "Incomplete" // Another suite's missing evidence must not taint this job summary.
	out.Reset()
	Summary(&out, r, "wms13", "official")
	if !strings.Contains(out.String(), "OGC conformance · Passed with skips") {
		t.Fatal(out.String())
	}
}

func TestTestNGAndInfrastructureFailure(t *testing.T) {
	root, _, doc, run := fixture(t)
	dir := filepath.Join(root, "conformance/wms13/core")
	var m Metadata
	readJSON(filepath.Join(dir, "metadata.json"), &m)
	m.Result.Format = "testng"
	m.Result.Counts = Counts{Total: 3, Passed: 1, Failed: 1, Skipped: 1}
	m.Result.Infrastructure = Counts{Total: 1, Failed: 1}
	m.Error = "Controller setup failed"
	writeTestJSON(t, filepath.Join(dir, "metadata.json"), m)
	writeTestFile(t, filepath.Join(dir, "junit.xml"), `<testsuite tests="3" failures="1" skipped="1"><testcase name="test" classname="assertion.example"/><testcase name="conditional" classname="assertion.example"><skipped type="unclassified"/></testcase><testcase name="setup" classname="infrastructure.example"><failure message="setup failed"/></testcase></testsuite>`)
	r, err := Load(root, doc, run)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "Failed" || r.Profiles[0].Cases[2].Kind != "infrastructure" || r.Profiles[0].Metadata.Result.Total != 3 {
		t.Fatalf("bad infrastructure result: %+v", r)
	}
}

func TestDerivedProvenance(t *testing.T) {
	for _, pair := range [][2]string{{"wcs20", "interpolation"}, {"wfs20", "core202"}} {
		suite, profile := pair[0], pair[1]
		t.Run(suite+"/"+profile, func(t *testing.T) {
			root, _, doc, run := fixture(t)
			doc.Suites[suite] = doc.Suites["wms13"]
			doc.Profiles[suite+"/"+profile] = manifest.Profile{Suite: suite, EvidenceKind: "official-derived", PatchSet: "patch", PatchSHA256: "sha"}
			run.Selected = append(run.Selected, suite)
			var m Metadata
			readJSON(filepath.Join(root, "conformance/wms13/core/metadata.json"), &m)
			m.Suite = suite + "-" + profile
			m.EvidenceKind = "official-derived"
			m.PatchSet = "patch"
			m.PatchSHA256 = "sha"
			m.DerivedImageID = "sha256:derived"
			dir := filepath.Join(root, "conformance-derived", suite, profile)
			writeTestJSON(t, filepath.Join(dir, "metadata.json"), m)
			data, _ := os.ReadFile(filepath.Join(root, "conformance/wms13/core/junit.xml"))
			writeTestFile(t, filepath.Join(dir, "junit.xml"), string(data))
			writeTestFile(t, filepath.Join(dir, "result.xml"), "<execution/>")
			r, err := Load(root, doc, run)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "Passed with skips" || r.Profiles[1].Kind != "official-derived" {
				t.Fatal("derived profile missing")
			}
			// Actions downloads use one artifact directory per derived profile.
			artifact := filepath.Join(root, artifactName(suite, "official-derived", profile))
			if err := os.Rename(dir, artifact); err != nil {
				t.Fatal(err)
			}
			dir = artifact
			r, err = Load(root, doc, run)
			if err != nil || r.Status != "Passed with skips" {
				t.Fatalf("artifact layout: %v, %s", err, r.Status)
			}
			m.PatchSHA256 = "wrong"
			writeTestJSON(t, filepath.Join(dir, "metadata.json"), m)
			r, _ = Load(root, doc, run)
			if r.Status != "Incomplete" {
				t.Fatal("derived mismatch accepted")
			}
		})
	}
}

func TestRejectPathsAndSymlinks(t *testing.T) {
	root, path, doc, run := fixture(t)
	doc.Profiles["wms13/../../escape"] = manifest.Profile{Suite: "wms13", EvidenceKind: "official"}
	if _, err := Load(root, doc, run); err == nil {
		t.Fatal("unsafe profile accepted")
	}
	if err := os.Symlink(path, filepath.Join(root, "conformance/wms13/link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(root, t.TempDir(), path, run, "latest"); err == nil {
		t.Fatal("evidence symlink accepted")
	}
}

func TestArchiveIndexRetainsVersions(t *testing.T) {
	root := t.TempDir()
	for _, version := range []string{"v0.1.1", "v0.1.2-beta"} {
		writeTestJSON(t, filepath.Join(root, "releases", version, "report.json"), Report{Run: Run{Release: version, Prerelease: strings.Contains(version, "beta")}, Status: "Passed"})
	}
	writeTestJSON(t, filepath.Join(root, "latest/report.json"), Report{Status: "Failed"})
	if err := WriteIndex(root); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(root, "index.html"))
	for _, expected := range []string{"v0.1.1", "v0.1.2-beta", "prerelease", "latest/index.html", "Failed"} {
		if !bytes.Contains(page, []byte(expected)) {
			t.Errorf("missing %s", expected)
		}
	}
}

func TestRegenerationDropsStaleOutputAndProtectsInputs(t *testing.T) {
	root, path, _, run := fixture(t)
	out := t.TempDir()
	if _, err := Generate(root, out, path, run, ""); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(out, "site/evidence/stale.zip"), "stale")
	if _, err := Generate(root, out, path, run, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "site/evidence/stale.zip")); !os.IsNotExist(err) {
		t.Fatal("stale evidence survived")
	}
	if _, err := Generate(filepath.Join(out, "inputs"), out, filepath.Join(out, "inputs/manifest.json"), run, ""); err == nil {
		t.Fatal("in-place rewrite would destroy input")
	}
	if _, err := os.Stat(filepath.Join(out, "inputs/manifest.json")); err != nil {
		t.Fatal("source manifest was removed")
	}
	if _, err := Generate(root, filepath.Join(root, "conformance/wms13/output"), path, run, ""); err == nil {
		t.Fatal("recursive evidence copy was allowed")
	}
	unrelated := t.TempDir()
	writeTestFile(t, filepath.Join(unrelated, "important.txt"), "keep")
	if _, err := Generate(root, unrelated, path, run, ""); err == nil {
		t.Fatal("unrelated output directory was overwritten")
	}
}
