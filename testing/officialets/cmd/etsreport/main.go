// etsreport renders existing conformance evidence; it never runs the suites.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
	"github.com/tobilg/neoserver/testing/officialets/report"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	input := flag.String("input", "test-results", "local test-results or downloaded artifact directory")
	output := flag.String("output", "test-results/conformance-dashboard", "output bundle directory (site/ and inputs/)")
	manifestPath := flag.String("manifest", "testing/officialets/versions.lock.json", "manifest from the tested checkout")
	context := flag.String("context", "", "versioned run context JSON; defaults to GitHub environment or local evidence")
	summary := flag.String("summary", "", "append Markdown summary to this file")
	summaryOnly := flag.Bool("summary-only", false, "write summary without generating HTML or archives")
	suite := flag.String("suite", "", "limit summary to one suite")
	kind := flag.String("kind", "", "limit summary to official or official-derived")
	prefix := flag.String("prefix", "", "published path: latest or releases/vVERSION")
	index := flag.String("index", "", "regenerate the archive index in an existing site directory")
	flag.Parse()
	if *index != "" {
		return report.WriteIndex(*index)
	}
	doc, err := manifest.Load(*manifestPath)
	if err != nil {
		return err
	}
	var run report.Run
	if *context != "" {
		run, err = report.ReadRun(*context)
	} else {
		run, err = environment(doc)
	}
	if err != nil {
		return err
	}
	var r report.Report
	if *summaryOnly {
		r, err = report.Load(*input, doc, run)
	} else {
		r, err = report.Generate(*input, *output, *manifestPath, run, *prefix)
	}
	if err != nil {
		return err
	}
	if *summary != "" {
		f, err := os.OpenFile(*summary, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		err = report.Summary(f, r, *suite, *kind)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	return report.Summary(os.Stdout, r, *suite, *kind)
}

func environment(doc manifest.Document) (report.Run, error) {
	r := report.Run{SchemaVersion: 1, Repository: os.Getenv("GITHUB_REPOSITORY"), ID: os.Getenv("GITHUB_RUN_ID"),
		Commit: os.Getenv("GITHUB_SHA"), Ref: os.Getenv("GITHUB_REF"), Event: os.Getenv("GITHUB_EVENT_NAME"),
		CreatedAt: time.Now().UTC().Format(time.RFC3339), SelectionKnown: true, Selected: []string{}}
	r.Attempt, _ = strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT"))
	r.Number, _ = strconv.Atoi(os.Getenv("GITHUB_RUN_NUMBER"))
	if r.ID != "" {
		r.URL = "https://github.com/" + r.Repository + "/actions/runs/" + r.ID
	}
	if r.Repository == "" {
		r.Repository = "tobilg/neoserver"
	}
	if raw, present := os.LookupEnv("ETS_REPORT_SELECTED"); present {
		if raw == "" {
			r.SelectionKnown = false
		} else if err := json.Unmarshal([]byte(raw), &r.Selected); err != nil {
			return r, fmt.Errorf("invalid selected suites: %w", err)
		}
	} else {
		for suite := range doc.Suites {
			r.Selected = append(r.Selected, suite)
		}
	}
	if raw := os.Getenv("ETS_REPORT_JOBS"); raw != "" {
		var jobs map[string]struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal([]byte(raw), &jobs); err != nil {
			return r, err
		}
		r.Jobs = map[string]string{}
		for name, job := range jobs {
			r.Jobs[name] = job.Result
		}
	}
	if path := os.Getenv("GITHUB_EVENT_PATH"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return r, err
		}
		var event struct {
			PullRequest struct {
				Head struct {
					SHA string `json:"sha"`
				} `json:"head"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return r, err
		}
		r.PRHead = event.PullRequest.Head.SHA
	}
	sort.Strings(r.Selected)
	return r, nil
}
