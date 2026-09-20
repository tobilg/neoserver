package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

func TestEnvironmentPreservesTestedMergeAndPRHead(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "tobilg/neoserver")
	t.Setenv("GITHUB_RUN_ID", "123")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	t.Setenv("GITHUB_SHA", "tested-merge-sha")
	t.Setenv("GITHUB_REF", "refs/pull/42/merge")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("ETS_REPORT_SELECTED", `["wms13"]`)
	t.Setenv("ETS_REPORT_JOBS", `{"official-conformance":{"result":"failure"}}`)
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte(`{"pull_request":{"head":{"sha":"pr-head-sha"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_PATH", path)
	r, err := environment(manifest.Document{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Commit != "tested-merge-sha" || r.PRHead != "pr-head-sha" || r.Attempt != 2 || r.Jobs["official-conformance"] != "failure" {
		t.Fatalf("lost run identity: %+v", r)
	}
}

func TestUnavailableSelectionDiffersFromDocumentationOnly(t *testing.T) {
	t.Setenv("GITHUB_EVENT_PATH", "")
	t.Setenv("ETS_REPORT_JOBS", "")
	for _, selected := range []string{"", "[]", "invalid-json"} {
		t.Setenv("ETS_REPORT_SELECTED", selected)
		r, err := environment(manifest.Document{})
		if selected == "invalid-json" {
			if err == nil {
				t.Fatal("invalid selection accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if r.SelectionKnown != (selected == "[]") {
			t.Fatalf("wrong selection state for %q", selected)
		}
	}
}
