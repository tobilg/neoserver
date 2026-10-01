package report

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

type ValidationCheck struct {
	Version  string     `json:"version"`
	ExitCode *int       `json:"exit_code"`
	Commands [][]string `json:"commands"`
	Log      string     `json:"log"`
	Tool     string     `json:"-"`
	Output   string     `json:"-"`
	Warnings []Advisory `json:"-"`
}

type Advisory struct {
	Category    string
	Message     string
	Explanation string
}

type stacEvidence struct {
	SchemaVersion int                        `json:"schema_version"`
	Root          string                     `json:"root"`
	Commit        string                     `json:"neoserver_commit"`
	Image         string                     `json:"neoserver_image_id"`
	StartedAt     time.Time                  `json:"started_at"`
	CompletedAt   time.Time                  `json:"completed_at"`
	Checks        map[string]ValidationCheck `json:"checks"`
	ExitCodes     map[string]int             `json:"exit_codes,omitempty"`
}

var stacChecks = []struct{ name, tool, title, kind, scope string }{
	{"api", "stac-api-validator", "STAC API 1.0", "community-validator", "Core, Collections, Features and Item Search, including pagination, against the workspace STAC fixture."},
	{"documents", "stac-validator", "STAC 1.1 documents", "community-validator", "Core schema validation of the root Catalog, Collections and fixture Items using pinned schemas. This does not assert support for every STAC extension."},
	{"client", "pystac-client", "PySTAC client interoperability", "client-interoperability", "Repository checks using PySTAC: GET and POST pagination return the same 300 unique Items; oversized limits are clamped."},
}

func loadSTAC(input string, doc manifest.Document, r *Report) error {
	expected := doc.STACValidation
	if expected == nil {
		return nil
	}
	if _, ok := doc.Suites[expected.Suite]; !ok || expected.Suite != "ogcapi-features10" || len(expected.Tools) != len(stacChecks) {
		return fmt.Errorf("invalid STAC validation manifest")
	}
	for _, check := range stacChecks {
		if expected.Tools[check.tool] == "" {
			return fmt.Errorf("missing STAC validator version: %s", check.tool)
		}
	}
	root := SuiteRoot(input, expected.Suite, "community-validator", "")
	var evidence stacEvidence
	readErr := readJSON(filepath.Join(root, "results.json"), &evidence)
	for _, check := range stacChecks {
		p := Profile{Key: "stac/" + check.name, Name: check.tool, Suite: expected.Suite,
			Protocol: check.title, Kind: check.kind, Scope: check.scope, STAC: true, Status: "Not run", Duration: "—", Validation: &ValidationCheck{Tool: check.tool}}
		if r.Run.SelectionKnown && !slices.Contains(r.Run.Selected, p.Suite) {
			r.Profiles = append(r.Profiles, p)
			continue
		}
		p.Status = "Incomplete"
		p.Metadata = Metadata{Commit: evidence.Commit, CandidateImageID: evidence.Image,
			StartedAt: evidence.StartedAt, CompletedAt: evidence.CompletedAt, Arguments: map[string][]string{"root": {evidence.Root}}}
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			p.Evidence = "stac-validation.zip"
		}
		value := evidence.Checks[check.tool]
		value.Tool = check.tool
		p.Validation = &value
		switch {
		case readErr != nil:
			p.Issue = "Missing or invalid STAC results.json: " + readErr.Error()
		case evidence.SchemaVersion != 1:
			p.Issue = "Unsupported or unversioned STAC evidence; rerun validators to record provenance."
		case evidence.Commit == "" || evidence.Commit == "unknown" || r.Run.Commit != "" && evidence.Commit != r.Run.Commit:
			p.Issue = "STAC evidence commit is missing or does not match the tested checkout."
		case evidence.Image == "" || evidence.Image == "unknown" || evidence.Root == "":
			p.Issue = "STAC endpoint or server image provenance is missing."
		case evidence.StartedAt.IsZero() || evidence.CompletedAt.Before(evidence.StartedAt):
			p.Issue = "STAC execution timestamps are missing or invalid."
		case value.Version != expected.Tools[check.tool] || value.ExitCode == nil || len(value.Commands) == 0 || value.Log != check.tool+".log":
			p.Issue = "STAC check is missing, invalid, or does not match the pinned tool version."
		case evidence.ExitCodes != nil && (len(evidence.ExitCodes) != len(stacChecks) || evidence.ExitCodes[check.tool] != *value.ExitCode):
			p.Issue = "STAC exit code summaries disagree."
		default:
			output, err := os.ReadFile(filepath.Join(root, value.Log))
			if err != nil || len(output) == 0 {
				p.Issue = "Missing or empty raw validator log: " + value.Log
			} else {
				value.Output = string(output)
				p.Status = "Passed"
				if check.tool == "stac-api-validator" {
					var complete, errors bool
					value.Warnings, complete, errors = apiAdvisories(value.Output)
					if !complete {
						p.Status, p.Issue = "Incomplete", "API validator log has no complete warning/error summary."
					} else if errors {
						p.Status = "Failed"
					} else if len(value.Warnings) > 0 {
						p.Status = "Passed with warnings"
					}
				}
				if *value.ExitCode != 0 {
					p.Status = "Failed"
				}
			}
		}
		// The community checks and inherited ETS profile must describe the same
		// server when both records are present. Never combine different builds.
		for _, inherited := range r.Profiles {
			if inherited.Key == "ogcapi-features10/stac" && inherited.Metadata.CandidateImageID != "" && evidence.Image != "" &&
				(inherited.Metadata.CandidateImageID != evidence.Image || inherited.Metadata.Commit != evidence.Commit) {
				p.Status, p.Issue = worse(p.Status, "Incomplete"), "STAC validator and inherited OGC evidence identify different server builds."
			}
		}
		if !evidence.StartedAt.IsZero() && !evidence.CompletedAt.Before(evidence.StartedAt) {
			p.Duration = evidence.CompletedAt.Sub(evidence.StartedAt).Round(time.Second).String()
		}
		if date := evidence.CompletedAt.UTC().Format(time.RFC3339); !evidence.CompletedAt.IsZero() && date > r.Date {
			r.Date = date
		}
		r.Status = worse(r.Status, p.Status)
		r.Profiles = append(r.Profiles, p)
	}
	return nil
}

// Keep each upstream occurrence, including duplicates, and never turn advisory
// messages into assertion counts. Unknown messages remain visible verbatim.
func apiAdvisories(output string) (warnings []Advisory, complete, errors bool) {
	inWarnings, seenWarnings := false, false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "Warnings:" || line == "Warnings: none":
			inWarnings, seenWarnings = line == "Warnings:", true
		case strings.HasPrefix(line, "Errors:"):
			complete, errors, inWarnings = seenWarnings, line != "Errors: none", false
		case inWarnings && strings.HasPrefix(line, "- "):
			a := Advisory{Category: "Advisory", Message: strings.TrimPrefix(line, "- "), Explanation: "Upstream advisory; consult the original log for context."}
			if strings.Contains(line, "stac-check recommendations:") {
				a.Category, a.Explanation = "Metadata recommendation", "Recommended descriptive metadata, such as collection summaries and link titles. Band summaries apply when band metadata exists. These recommendations are not schema validation errors."
			} else if strings.Contains(line, "to validate ids does not override all other parameters returned 0 results") {
				a.Category, a.Explanation = "Test coverage limitation", "The fixture has no Item in the validator's probe bounding box. The validator could not complete this ids/filter interaction check; this is a coverage gap, not evidence that the behavior passed."
			}
			warnings = append(warnings, a)
		}
	}
	return
}

func (r Report) STACProfiles() []Profile {
	profiles := r.profilesBySTAC(true)
	sort.SliceStable(profiles, func(i, j int) bool { return profiles[i].Kind != "official" && profiles[j].Kind == "official" })
	return profiles
}
func (r Report) OGCProfiles() []Profile { return r.profilesBySTAC(false) }
func (r Report) profilesBySTAC(stac bool) []Profile {
	var profiles []Profile
	for _, p := range r.Profiles {
		if p.STAC == stac {
			profiles = append(profiles, p)
		}
	}
	return profiles
}
