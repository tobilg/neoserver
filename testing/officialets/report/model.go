// Package report turns existing ETS evidence into static reports. It does not
// reinterpret TEAM Engine XML or change the conformance qualification rules.
package report

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

const Origin = "https://conformance.neoserver.cloud"

type Run struct {
	SchemaVersion  int               `json:"schema_version"`
	Repository     string            `json:"repository"`
	ID             string            `json:"run_id"`
	Attempt        int               `json:"run_attempt"`
	Number         int               `json:"run_number"`
	Commit         string            `json:"commit"`
	PRHead         string            `json:"pr_head,omitempty"`
	Ref            string            `json:"ref"`
	Event          string            `json:"event"`
	URL            string            `json:"url,omitempty"`
	CreatedAt      string            `json:"created_at,omitempty"`
	Selected       []string          `json:"selected_suites"`
	SelectionKnown bool              `json:"selection_known"`
	Jobs           map[string]string `json:"jobs,omitempty"`
	Release        string            `json:"release,omitempty"`
	Prerelease     bool              `json:"prerelease,omitempty"`
}

type Counts struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type Result struct {
	Counts
	Format         string         `json:"format"`
	Leaf           Counts         `json:"leaf"`
	Wrapper        Counts         `json:"wrapper"`
	Infrastructure Counts         `json:"infrastructure"`
	SkipCategories map[string]int `json:"skip_categories,omitempty"`
}

type Metadata struct {
	Suite            string              `json:"suite"`
	EvidenceKind     string              `json:"evidence_kind"`
	Image            string              `json:"image"`
	BaseImage        string              `json:"base_image,omitempty"`
	DerivedImageID   string              `json:"derived_image_id,omitempty"`
	PatchSet         string              `json:"patch_set,omitempty"`
	PatchSHA256      string              `json:"patch_sha256,omitempty"`
	Commit           string              `json:"neoserver_commit"`
	CandidateImageID string              `json:"neoserver_image_id,omitempty"`
	StartedAt        time.Time           `json:"started_at"`
	CompletedAt      time.Time           `json:"completed_at"`
	Arguments        map[string][]string `json:"arguments"`
	Result           Result              `json:"result"`
	Error            string              `json:"error,omitempty"`
}

type Case struct {
	ID       string
	Name     string
	Class    string
	Kind     string
	Status   string
	Message  string
	Category string
}

type Profile struct {
	Key      string
	Suite    string
	Name     string
	Protocol string
	Kind     string
	Status   string
	Issue    string
	Metadata Metadata
	Cases    []Case
	Evidence string
	Duration string
}

type Report struct {
	Run      Run       `json:"run"`
	Profiles []Profile `json:"-"`
	Status   string    `json:"status"`
	Date     string    `json:"date"`
	Prefix   string    `json:"-"`
}

var slug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var version = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`)

var protocolNames = map[string]string{
	"wms13": "WMS 1.3", "wfs20": "WFS 2.0", "wcs20": "WCS 2.0",
	"wmts10": "WMTS 1.0", "ogcapi-features10": "OGC API · Features 1.0", "ogcapi-tiles10": "OGC API · Tiles 1.0",
}

func ReadRun(path string) (Run, error) {
	var r Run
	if err := readJSON(path, &r); err != nil {
		return r, err
	}
	if r.SchemaVersion != 1 {
		return r, fmt.Errorf("unsupported report context schema %d", r.SchemaVersion)
	}
	return r, nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func Load(input string, doc manifest.Document, run Run) (Report, error) {
	r := Report{Run: run, Status: "Not run"}
	keys := make([]string, 0, len(doc.Profiles))
	for key, p := range doc.Profiles {
		parts := strings.Split(key, "/")
		if len(parts) != 2 || !slug.MatchString(parts[0]) || !slug.MatchString(parts[1]) || parts[0] != p.Suite ||
			(p.EvidenceKind != "official" && p.EvidenceKind != "official-derived") {
			return r, fmt.Errorf("invalid manifest profile %q", key)
		}
		if _, ok := doc.Suites[p.Suite]; !ok {
			return r, fmt.Errorf("unknown suite %q", p.Suite)
		}
		keys = append(keys, key)
	}
	for _, suite := range run.Selected {
		if _, ok := doc.Suites[suite]; !ok {
			return r, fmt.Errorf("unknown selected suite %q", suite)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := doc.Profiles[keys[i]], doc.Profiles[keys[j]]
		if a.EvidenceKind != b.EvidenceKind {
			return a.EvidenceKind == "official"
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		expected := doc.Profiles[key]
		p := Profile{Key: key, Suite: expected.Suite, Name: strings.Split(key, "/")[1], Kind: expected.EvidenceKind,
			Protocol: protocolNames[expected.Suite], Status: "Not run", Duration: "—"}
		if p.Protocol == "" {
			p.Protocol = p.Suite
		}
		if run.SelectionKnown && !slices.Contains(run.Selected, p.Suite) {
			r.Profiles = append(r.Profiles, p)
			continue
		}
		p.Status = "Incomplete"
		root := SuiteRoot(input, expected.Suite, expected.EvidenceKind, p.Name)
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			p.Evidence = artifactName(p.Suite, p.Kind, p.Name) + ".zip"
		}
		dir := root
		if p.Kind == "official" {
			dir = filepath.Join(root, p.Name)
		}
		err := readJSON(filepath.Join(dir, "metadata.json"), &p.Metadata)
		if err != nil {
			p.Issue = "Missing or invalid metadata.json: " + err.Error()
		} else {
			p.Cases, err = readJUnit(filepath.Join(dir, "junit.xml"))
			if err != nil {
				p.Issue = "Missing or invalid junit.xml: " + err.Error()
			} else {
				p.Issue = validateProfile(p, expected, doc.Suites[p.Suite], run)
				if p.Issue == "" && p.Metadata.Error == "" {
					info, rawErr := os.Stat(filepath.Join(dir, "result.xml"))
					if rawErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
						p.Issue = "Missing or empty raw result.xml evidence."
					}
				}
				if p.Issue == "" {
					p.Status = "Passed"
					if p.Metadata.Result.Leaf.Skipped > 0 || p.Metadata.Result.Infrastructure.Skipped > 0 {
						p.Status = "Passed with skips"
					}
					if p.Metadata.Error != "" || p.Metadata.Result.Failed > 0 || p.Metadata.Result.Leaf.Failed > 0 || p.Metadata.Result.Wrapper.Failed > 0 || p.Metadata.Result.Infrastructure.Failed > 0 {
						p.Status = "Failed"
					}
					for _, c := range p.Cases {
						if c.Status == "Failed" {
							p.Status = "Failed"
						}
					}
				}
			}
		}
		if !p.Metadata.StartedAt.IsZero() && !p.Metadata.CompletedAt.Before(p.Metadata.StartedAt) {
			p.Duration = p.Metadata.CompletedAt.Sub(p.Metadata.StartedAt).Round(time.Second).String()
		}
		date := p.Metadata.CompletedAt.UTC().Format(time.RFC3339)
		if !p.Metadata.CompletedAt.IsZero() && date > r.Date {
			r.Date = date
		}
		r.Status = worse(r.Status, p.Status)
		r.Profiles = append(r.Profiles, p)
	}
	if !run.SelectionKnown {
		r.Status = worse(r.Status, "Incomplete")
	}
	for _, status := range run.Jobs {
		if status == "failure" || status == "cancelled" {
			r.Status = worse(r.Status, "Incomplete")
		}
	}
	return r, nil
}

func validateProfile(p Profile, expected manifest.Profile, suite manifest.Suite, run Run) string {
	m := p.Metadata
	if m.Suite != strings.ReplaceAll(p.Key, "/", "-") || m.EvidenceKind != p.Kind || m.Image != suite.Image {
		return "Evidence does not match the source suite manifest."
	}
	if run.Commit != "" && m.Commit != run.Commit {
		return "Evidence commit does not match the tested checkout."
	}
	if p.Kind == "official-derived" && (m.PatchSet != expected.PatchSet || m.PatchSHA256 != expected.PatchSHA256 || m.DerivedImageID == "") {
		return "Derived suite provenance does not match the source manifest."
	}
	for _, c := range []Counts{m.Result.Counts, m.Result.Leaf, m.Result.Wrapper, m.Result.Infrastructure} {
		if c.Passed < 0 || c.Failed < 0 || c.Skipped < 0 || c.Total != c.Passed+c.Failed+c.Skipped {
			return "Invalid counts in metadata.json."
		}
	}
	if m.Error == "" && (m.Result.Format != "ctl" && m.Result.Format != "testng" || m.Result.Leaf.Total == 0 || m.Result.Leaf.Passed == 0) {
		return "No completed assertion evidence."
	}
	var leaf Counts
	for _, c := range p.Cases {
		if c.Kind != "assertion" {
			continue
		}
		leaf.Total++
		switch c.Status {
		case "Passed":
			leaf.Passed++
		case "Failed":
			leaf.Failed++
		case "Skipped":
			leaf.Skipped++
		}
	}
	if leaf != m.Result.Leaf {
		return "JUnit assertion counts disagree with metadata.json."
	}
	return ""
}

type junit struct {
	XMLName  xml.Name `xml:"testsuite"`
	Tests    int      `xml:"tests,attr"`
	Failures int      `xml:"failures,attr"`
	Errors   int      `xml:"errors,attr"`
	Skipped  int      `xml:"skipped,attr"`
	Cases    []struct {
		Name    string        `xml:"name,attr"`
		Class   string        `xml:"classname,attr"`
		Failure *junitMessage `xml:"failure"`
		Error   *junitMessage `xml:"error"`
		Skipped *junitMessage `xml:"skipped"`
	} `xml:"testcase"`
}
type junitMessage struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

func readJUnit(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var j junit
	if err := xml.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	if j.Tests != len(j.Cases) || j.Tests == 0 {
		return nil, fmt.Errorf("invalid or empty testcase count")
	}
	cases := make([]Case, 0, len(j.Cases))
	var failed, skipped, errors int
	for i, entry := range j.Cases {
		c := Case{ID: fmt.Sprintf("case-%d", i+1), Name: entry.Name, Class: entry.Class, Status: "Passed", Kind: "assertion"}
		if strings.HasPrefix(entry.Class, "infrastructure.") {
			c.Kind = "infrastructure"
		}
		message := entry.Failure
		if entry.Skipped != nil {
			skipped++
			c.Status = "Skipped"
			message = entry.Skipped
			c.Category = entry.Skipped.Type
			if c.Category == "" {
				c.Category = "unclassified"
			}
		}
		if entry.Failure != nil {
			failed++
			c.Status = "Failed"
			message = entry.Failure
		}
		if entry.Error != nil {
			errors++
			c.Status = "Failed"
			message = entry.Error
		}
		if message != nil {
			c.Message = strings.TrimSpace(message.Message + "\n" + message.Body)
		}
		cases = append(cases, c)
	}
	if j.Failures != failed || j.Errors != errors || j.Skipped != skipped {
		return nil, fmt.Errorf("JUnit totals disagree with testcase outcomes")
	}
	return cases, nil
}

func artifactName(suite, kind, profile string) string {
	if kind == "official-derived" {
		return "official-derived-ets-" + suite + "-" + profile
	}
	return "official-ets-" + suite
}

// SuiteRoot accepts both local test-results and actions/download-artifact's
// default directory layout, keeping stock and derived evidence separate.
func SuiteRoot(input, suite, kind, profile string) string {
	artifact := filepath.Join(input, artifactName(suite, kind, profile))
	if _, err := os.Stat(artifact); err == nil {
		return artifact
	}
	if kind == "official-derived" {
		return filepath.Join(input, "conformance-derived", suite, profile)
	}
	return filepath.Join(input, "conformance", suite)
}

func worse(a, b string) string {
	order := map[string]int{"Not run": 0, "Passed": 1, "Passed with skips": 2, "Incomplete": 3, "Failed": 4}
	if order[b] > order[a] {
		return b
	}
	return a
}
