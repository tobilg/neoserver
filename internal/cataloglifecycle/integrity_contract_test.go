package cataloglifecycle

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestHealthyIntegrityReportHasEmptyIssueArray(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	coordinator := &Coordinator{deps: Dependencies{Catalog: catalog}}
	report, err := coordinator.AuditIntegrity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Issues json.RawMessage `json:"issues"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	if !report.Healthy || string(response.Issues) != "[]" {
		t.Fatalf("healthy integrity response = %s", encoded)
	}
}
