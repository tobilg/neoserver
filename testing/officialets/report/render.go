package report

import (
	"archive/zip"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

//go:embed templates/* assets/*
var files embed.FS

var templates = template.Must(template.New("reports").Funcs(template.FuncMap{
	"statusClass": func(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "-")) },
	"refName":     func(s string) string { return strings.TrimPrefix(strings.TrimPrefix(s, "refs/heads/"), "refs/tags/") },
	"short": func(s string) string {
		if len(s) > 12 {
			return s[:12]
		}
		return s
	},
	"kind": func(s string) string {
		if s == "official-derived" {
			return "Official-derived"
		}
		return "Stock official"
	},
}).ParseFS(files, "templates/*.html"))

type page struct {
	Title     string
	Canonical string
	Root      string
	Report    Report
	Profile   *Profile
	Archive   bool
	Entries   []archiveEntry
}
type archiveEntry struct {
	Title  string
	Href   string
	Report Report
}

// Generate writes a portable artifact: site/ contains only generated pages and
// download archives; inputs/ retains the data needed for a trusted re-render.
func Generate(input, output, manifestPath string, run Run, prefix string) (Report, error) {
	doc, err := manifest.Load(manifestPath)
	if err != nil {
		return Report{}, err
	}
	r, err := Load(input, doc, run)
	if err != nil {
		return r, err
	}
	if prefix != "" && prefix != "latest" && !(strings.HasPrefix(prefix, "releases/") && version.MatchString(strings.TrimPrefix(prefix, "releases/"))) {
		return r, fmt.Errorf("invalid publication prefix")
	}
	r.Prefix = prefix
	if within(output, input) || within(output, manifestPath) {
		return r, fmt.Errorf("output must not contain input evidence or its manifest")
	}
	for key, p := range doc.Profiles {
		if within(SuiteRoot(input, p.Suite, p.EvidenceKind, strings.Split(key, "/")[1]), output) {
			return r, fmt.Errorf("output must not be inside a suite's evidence directory")
		}
	}
	if err := prepareOutput(output); err != nil {
		return r, err
	}
	site := filepath.Join(output, "site")
	if err := os.MkdirAll(filepath.Join(site, "evidence"), 0o755); err != nil {
		return r, err
	}
	if err := copyAssets(site); err != nil {
		return r, err
	}
	if err := os.MkdirAll(filepath.Join(output, "inputs"), 0o755); err != nil {
		return r, err
	}
	if err := writeJSON(filepath.Join(output, "inputs", "run.json"), run); err != nil {
		return r, err
	}
	if err := copyFile(manifestPath, filepath.Join(output, "inputs", "manifest.json")); err != nil {
		return r, err
	}
	seen := map[string]bool{}
	for _, p := range r.Profiles {
		if p.Evidence == "" || seen[p.Evidence] {
			continue
		}
		seen[p.Evidence] = true
		root := SuiteRoot(input, p.Suite, p.Kind, p.Name)
		dest := filepath.Join(output, "inputs", artifactName(p.Suite, p.Kind, p.Name))
		if err := copyTree(root, dest); err != nil {
			return r, err
		}
		if err := zipTree(dest, filepath.Join(site, "evidence", p.Evidence)); err != nil {
			return r, err
		}
	}
	canonical := Origin + "/"
	if prefix != "" {
		canonical += prefix + "/"
	}
	if err := render(filepath.Join(site, "index.html"), page{Title: "Conformance report", Canonical: canonical, Root: "./", Report: r}); err != nil {
		return r, err
	}
	for _, p := range r.Profiles {
		path := "profiles/" + p.Key + "/"
		if err := render(filepath.Join(site, filepath.FromSlash(path), "index.html"), page{Title: p.Protocol + " · " + p.Name, Canonical: canonical + path, Root: "../../../", Report: r, Profile: &p}); err != nil {
			return r, err
		}
	}
	if err := writeJSON(filepath.Join(site, "report.json"), r); err != nil {
		return r, err
	}
	return r, nil
}

func within(parent, child string) bool {
	parent, err := filepath.Abs(parent)
	if err != nil {
		return true
	}
	child, err = filepath.Abs(child)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Only clean directories previously created by this command. This prevents
// stale evidence surviving a re-render without deleting an arbitrary output.
func prepareOutput(output string) error {
	const marker = ".etsreport-output"
	entries, err := os.ReadDir(output)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(output, marker)); err != nil {
			return fmt.Errorf("output is not empty and is not an etsreport bundle: %s", output)
		}
		for _, name := range []string{"site", "inputs"} {
			if err := os.RemoveAll(filepath.Join(output, name)); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, marker), []byte("Generated conformance report bundle\n"), 0o644)
}

func render(path string, p page) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = templates.ExecuteTemplate(f, "page", p)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func copyAssets(site string) error {
	if err := os.MkdirAll(filepath.Join(site, "assets"), 0o755); err != nil {
		return err
	}
	entries, err := files.ReadDir("assets")
	if err != nil {
		return err
	}
	for _, e := range entries {
		data, err := files.ReadFile("assets/" + e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(site, "assets", e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular evidence file %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing evidence symlink %s", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

func zipTree(src, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	w := zip.NewWriter(f)
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		entry, err := w.CreateHeader(&zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate})
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	})
	zipErr, fileErr := w.Close(), f.Close()
	if err != nil {
		return err
	}
	if zipErr != nil {
		return zipErr
	}
	return fileErr
}

// WriteIndex preserves the independently generated latest/release directories.
func WriteIndex(site string) error {
	var entries []archiveEntry
	paths, err := filepath.Glob(filepath.Join(site, "releases", "*", "report.json"))
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	paths = append([]string{filepath.Join(site, "latest", "report.json")}, paths...)
	for _, path := range paths {
		var r Report
		if err := readJSON(path, &r); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		rel, err := filepath.Rel(site, filepath.Dir(path))
		if err != nil {
			return err
		}
		title := r.Run.Release
		if title == "" {
			title = "Latest · main"
		}
		if r.Run.Prerelease {
			title += " · prerelease"
		}
		entries = append(entries, archiveEntry{Title: title, Href: filepath.ToSlash(rel) + "/index.html", Report: r})
	}
	if err := copyAssets(site); err != nil {
		return err
	}
	return render(filepath.Join(site, "index.html"), page{Title: "Conformance", Canonical: Origin + "/", Root: "./", Archive: true, Entries: entries})
}

func markdown(s string) string {
	s = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "|", "&#124;", "`", "&#96;", "[", "&#91;", "]", "&#93;", "\r", " ", "\n", " ").Replace(s)
	if len([]rune(s)) > 350 {
		s = string([]rune(s)[:350]) + "…"
	}
	return s
}

func Summary(w io.Writer, r Report, suite, kind string) error {
	var b strings.Builder
	status := r.Status
	if suite != "" || kind != "" {
		status = "Not run"
		for _, p := range r.Profiles {
			if (suite == "" || p.Suite == suite) && (kind == "" || p.Kind == kind) {
				status = worse(status, p.Status)
			}
		}
	}
	fmt.Fprintf(&b, "## OGC conformance · %s\n\n", status)
	fmt.Fprintln(&b, "| Profile | Evidence | Status | Assertions passed / failed / skipped | Infrastructure failed / skipped |\n|---|---|---|---:|---:|")
	var examples []string
	for _, p := range r.Profiles {
		if suite != "" && p.Suite != suite || kind != "" && p.Kind != kind {
			continue
		}
		counts := "—"
		if p.Metadata.Result.Format != "" {
			c := p.Metadata.Result.Leaf
			counts = fmt.Sprintf("%d / %d / %d", c.Passed, c.Failed, c.Skipped)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d / %d |\n", markdown(p.Key), p.Kind, p.Status, counts, p.Metadata.Result.Infrastructure.Failed, p.Metadata.Result.Infrastructure.Skipped)
		if p.Issue != "" {
			examples = append(examples, p.Key+": "+p.Issue)
		}
		if p.Metadata.Error != "" {
			examples = append(examples, p.Key+": "+p.Metadata.Error)
		}
		for _, c := range p.Cases {
			if c.Status == "Failed" {
				examples = append(examples, p.Key+" / "+c.Name+": "+c.Message)
			}
		}
	}
	fmt.Fprintln(&b, "\nCounts are per profile; stock official and derived evidence remain separate.")
	if !r.Run.SelectionKnown {
		fmt.Fprintln(&b, "\nSuite selection was unavailable; expected evidence is incomplete.")
	}
	if r.Run.SelectionKnown && len(r.Run.Selected) == 0 {
		fmt.Fprintln(&b, "\nNo suites were selected for this change.")
	}
	for _, p := range r.Profiles {
		if suite != "" && p.Suite != suite || kind != "" && p.Kind != kind {
			continue
		}
		if len(p.Metadata.Result.SkipCategories) == 0 {
			continue
		}
		var categories []string
		for category, n := range p.Metadata.Result.SkipCategories {
			categories = append(categories, fmt.Sprintf("%s: %d", category, n))
		}
		sort.Strings(categories)
		fmt.Fprintf(&b, "\n%s skips: %s.\n", markdown(p.Key), markdown(strings.Join(categories, "; ")))
	}
	if len(examples) > 0 {
		fmt.Fprintln(&b, "\nDiagnostics:")
		for _, s := range examples[:min(5, len(examples))] {
			fmt.Fprintf(&b, "\n- %s\n", markdown(s))
		}
	}
	fmt.Fprintf(&b, "\n[Public dashboard](%s/) · Full details and raw evidence are in this run’s artifacts.\n", Origin)
	_, err := io.WriteString(w, b.String())
	return err
}
