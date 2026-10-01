package report

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

const advisoryLog = "Warnings:\n- [Collections] stac-check recommendations: <script>alert(1)</script>\n- [Collections] stac-check recommendations: <script>alert(1)</script>\n- [Item Search] GET Search within bbox=20,20,21,21 to validate ids does not override all other parameters returned 0 results\nErrors: none\n"

func stacFixture(t *testing.T) (string, string, manifest.Document, Run, stacEvidence) {
	t.Helper()
	root, path, doc, run := fixture(t)
	doc.Suites["ogcapi-features10"] = manifest.Suite{Image: "official-features"}
	doc.STACValidation = &manifest.STACValidation{Suite: "ogcapi-features10", Tools: map[string]string{
		"stac-api-validator": "0.6.8", "stac-validator": "4.1.2", "pystac-client": "0.9.0",
	}}
	writeTestJSON(t, path, doc)
	run.Selected = append(run.Selected, "ogcapi-features10")
	start := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	e := stacEvidence{SchemaVersion: 1, Root: "http://server/workspaces/demo/stac", Commit: run.Commit, Image: "sha256:server", StartedAt: start, CompletedAt: start.Add(time.Minute), Checks: map[string]ValidationCheck{}}
	for tool, version := range doc.STACValidation.Tools {
		code := 0
		e.Checks[tool] = ValidationCheck{Version: version, ExitCode: &code, Commands: [][]string{{tool, "validate"}}, Log: tool + ".log"}
		output := "Validation passed.\n"
		if tool == "stac-api-validator" {
			output = advisoryLog
		}
		writeTestFile(t, filepath.Join(root, "conformance/stac", tool+".log"), output)
	}
	writeTestJSON(t, filepath.Join(root, "conformance/stac/results.json"), e)
	return root, path, doc, run, e
}

func TestSTACIndependentOutcomesAndPortableEvidence(t *testing.T) {
	for _, downloaded := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "downloaded"}[downloaded], func(t *testing.T) {
			root, path, _, run, _ := stacFixture(t)
			if downloaded {
				if err := os.Rename(filepath.Join(root, "conformance/stac"), filepath.Join(root, "stac-validation")); err != nil {
					t.Fatal(err)
				}
			}
			output := t.TempDir()
			r, err := Generate(root, output, path, run, "")
			if err != nil {
				t.Fatal(err)
			}
			profiles := r.STACProfiles()
			if len(profiles) != 3 || profiles[0].Status != "Passed with warnings" || profiles[1].Status != "Passed" || profiles[2].Status != "Passed" {
				t.Fatalf("wrong STAC outcomes: %+v", profiles)
			}
			if profiles[0].Metadata.Result.Format != "" || len(profiles[0].Validation.Warnings) != 3 {
				t.Fatal("warnings must not become assertion counts or be deduplicated")
			}
			page, err := os.ReadFile(filepath.Join(output, "site/profiles/stac/api/index.html"))
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"Community validator", "0.6.8", "Metadata recommendation", "Test coverage limitation", "3 occurrences", "stac-validation.zip", "sha256:server", "&lt;script&gt;"} {
				if !bytes.Contains(page, []byte(text)) {
					t.Errorf("missing %q", text)
				}
			}
			if bytes.Contains(page, []byte("<script>alert")) {
				t.Fatal("unsafe warning HTML")
			}
			z, err := zip.OpenReader(filepath.Join(output, "site/evidence/stac-validation.zip"))
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			if len(z.File) != 4 {
				t.Fatalf("incomplete evidence bundle: %v", z.File)
			}
			for _, f := range z.File {
				raw, err := f.Open()
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(raw)
				raw.Close()
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile(filepath.Join(SuiteRoot(root, "", "community-validator", ""), f.Name))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("changed evidence bytes: %s", f.Name)
				}
			}
			rerendered, err := Generate(filepath.Join(output, "inputs"), t.TempDir(), filepath.Join(output, "inputs/manifest.json"), run, "")
			if err != nil || rerendered.STACProfiles()[0].Status != "Passed with warnings" {
				t.Fatalf("portable rerender failed: %v", err)
			}
			var summary bytes.Buffer
			if err := Summary(&summary, r, "ogcapi-features10", "official-and-stac"); err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"stac/api", "stac/documents", "stac/client", "Test coverage limitation", "Advisory warnings"} {
				if !strings.Contains(summary.String(), text) {
					t.Errorf("summary missing %q", text)
				}
			}
		})
	}
}

func TestSTACMissingOrMismatchedEvidenceNeverPasses(t *testing.T) {
	for _, mode := range []string{"missing", "legacy", "commit", "image", "timestamps", "tool-version", "check", "exit-code", "commands", "log-path", "missing-log", "empty-log", "truncated-log", "mixed-build"} {
		t.Run(mode, func(t *testing.T) {
			root, _, doc, run, evidence := stacFixture(t)
			check := evidence.Checks["stac-api-validator"]
			switch mode {
			case "legacy":
				evidence.SchemaVersion = 0
			case "commit":
				evidence.Commit = "different"
			case "image":
				evidence.Image = ""
			case "timestamps":
				evidence.CompletedAt = evidence.StartedAt.Add(-time.Minute)
			case "tool-version":
				check.Version = "9.9.9"
			case "exit-code":
				check.ExitCode = nil
			case "commands":
				check.Commands = nil
			case "log-path":
				check.Log = "../outside.log"
			case "mixed-build":
				doc.Profiles["ogcapi-features10/stac"] = manifest.Profile{Suite: "ogcapi-features10", EvidenceKind: "official"}
				writeTestJSON(t, filepath.Join(root, "conformance/ogcapi-features10/stac/metadata.json"), Metadata{CandidateImageID: "sha256:other", Commit: run.Commit})
			}
			evidence.Checks["stac-api-validator"] = check
			if mode == "check" {
				delete(evidence.Checks, "stac-api-validator")
			}
			path := filepath.Join(root, "conformance/stac/results.json")
			writeTestJSON(t, path, evidence)
			log := filepath.Join(root, "conformance/stac/stac-api-validator.log")
			switch mode {
			case "missing":
				os.Remove(path)
			case "missing-log":
				os.Remove(log)
			case "empty-log":
				writeTestFile(t, log, "")
			case "truncated-log":
				writeTestFile(t, log, "Warnings:\n- pending\n")
			}
			r, err := Load(root, doc, run)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range r.Profiles {
				if p.Key == "stac/api" && (p.Status != "Incomplete" || p.Issue == "") {
					t.Fatalf("invalid evidence passed: %+v", p)
				}
			}
		})
	}
}

func TestSTACFailuresSelectionAndHistoricalManifests(t *testing.T) {
	root, _, doc, run, e := stacFixture(t)
	for _, tool := range []string{"stac-api-validator", "stac-validator", "pystac-client"} {
		check := e.Checks[tool]
		code := 1
		check.ExitCode = &code
		e.Checks[tool] = check
	}
	writeTestJSON(t, filepath.Join(root, "conformance/stac/results.json"), e)
	r, err := Load(root, doc, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.STACProfiles() {
		if p.Status != "Failed" {
			t.Fatalf("failure hidden: %+v", p)
		}
	}
	run.Selected = []string{"wms13"}
	r, err = Load(root, doc, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.STACProfiles() {
		if p.Status != "Not run" {
			t.Fatalf("unselected evidence used: %+v", p)
		}
	}
	doc.STACValidation = nil
	r, err = Load(root, doc, run)
	if err != nil || len(r.STACProfiles()) != 0 {
		t.Fatalf("historical run acquired STAC requirements: %v", err)
	}
}

func TestAPIAdvisoryParsing(t *testing.T) {
	warnings, complete, errors := apiAdvisories(advisoryLog)
	if !complete || errors || len(warnings) != 3 || warnings[0].Category != "Metadata recommendation" || warnings[2].Category != "Test coverage limitation" {
		t.Fatalf("incorrect advisory parsing: %+v", warnings)
	}
	for _, log := range []string{"Warnings: none\nErrors: none\n", "Warnings:\n- Unknown advisory\nErrors: none\n"} {
		_, complete, errors := apiAdvisories(log)
		if !complete || errors {
			t.Fatal("valid summary rejected")
		}
	}
	_, complete, errors = apiAdvisories("Warnings: none\nErrors:\n- A conformance error\n")
	if !complete || !errors {
		t.Fatal("errors treated as warnings")
	}
}

func TestActionsDownloadLayoutsKeepSTACEvidenceSeparate(t *testing.T) {
	for _, single := range []bool{true, false} {
		t.Run(map[bool]string{true: "single-artifact", false: "multiple-artifacts"}[single], func(t *testing.T) {
			root, path, _, run, _ := stacFixture(t)
			destination := filepath.Join(root, "official-artifacts")
			if !single {
				destination = filepath.Join(destination, "official-ets-wms13")
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(root, "conformance/wms13"), destination); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(root, "conformance/stac"), filepath.Join(root, "stac-validation")); err != nil {
				t.Fatal(err)
			}
			output := t.TempDir()
			r, err := Generate(root, output, path, run, "")
			if err != nil {
				t.Fatal(err)
			}
			if r.OGCProfiles()[0].Status != "Passed with skips" || r.STACProfiles()[0].Status != "Passed with warnings" {
				t.Fatalf("downloaded evidence missing: %+v", r.Profiles)
			}
			z, err := zip.OpenReader(filepath.Join(output, "site/evidence/official-ets-wms13.zip"))
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			for _, file := range z.File {
				if strings.Contains(file.Name, "stac") {
					t.Fatalf("STAC logs leaked into official archive: %s", file.Name)
				}
			}
		})
	}
}
