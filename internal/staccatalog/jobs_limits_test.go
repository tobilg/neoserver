package staccatalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/stacmodel"
)

func TestPendingImportsAreLimitedPerWorkspace(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	for _, ws := range []string{"one", "two"} {
		if err := c.PutCollection(ctx, ws, Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < MaxPendingImportsPerWorkspace; i++ {
		j, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Import(ctx, j, strings.NewReader(jsonString(testItem("item", 1))), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil); err == nil {
		t.Fatal("import limit not enforced")
	}
	if _, err := c.CreateJob(ctx, "two", "scenes", "import", "uploading", nil); err != nil {
		t.Fatalf("staged imports in one workspace blocked another: %v", err)
	}
	if err := c.PutBinding(ctx, "one", "scenes", &Binding{ID: "binding", ServiceID: "source", ResourceID: "layer", ResourceKind: "layer", Mode: "mapped"}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateJob(ctx, "one", "scenes", "refresh", "queued", nil); err != nil {
		t.Fatalf("staged imports blocked a refresh: %v", err)
	}
}

func TestStagedImportsDoNotBlockRefreshStaging(t *testing.T) {
	ctx := context.Background()
	c, err := Open(filepath.Join(t.TempDir(), "stac.duckdb"), "abc123", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, id := range []string{"imported", "linked"} {
		if err = c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection(id, id, id, "other", nil)}, true); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		j, err := c.CreateJob(ctx, "one", "imported", "import", "uploading", nil)
		if err != nil {
			t.Fatal(err)
		}
		item := testItem("item", 1)
		item["collection"] = "imported"
		if err = c.Stage(ctx, j, []stacmodel.Document{item}); err != nil {
			t.Fatal(err)
		}
	}
	extra, err := c.CreateJob(ctx, "one", "imported", "import", "uploading", nil)
	if err != nil {
		t.Fatal(err)
	}
	item := testItem("extra", 1)
	item["collection"] = "imported"
	if err = c.Stage(ctx, extra, []stacmodel.Document{item}); err == nil {
		t.Fatal("import staging capacity not enforced")
	}
	if err = c.PutBinding(ctx, "one", "linked", &Binding{ID: "binding", ServiceID: "source", ResourceID: "layer", ResourceKind: "layer", Mode: "mapped"}, 1); err != nil {
		t.Fatal(err)
	}
	refresh, err := c.CreateJob(ctx, "one", "linked", "refresh", "running", nil)
	if err != nil {
		t.Fatal(err)
	}
	item = testItem("source", 1)
	item["collection"] = "linked"
	if err = c.Stage(ctx, refresh, []stacmodel.Document{item}); err != nil {
		t.Fatalf("staged imports blocked a refresh: %v", err)
	}
}

func TestPruneExpiresStaleStagedImports(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	if err := c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
		t.Fatal(err)
	}
	stale, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Import(ctx, stale, strings.NewReader(jsonString(testItem("stale", 1))), ""); err != nil {
		t.Fatal(err)
	}
	fresh, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Import(ctx, fresh, strings.NewReader(jsonString(testItem("fresh", 1))), ""); err != nil {
		t.Fatal(err)
	}
	if _, err = c.db.ExecContext(ctx, "UPDATE stac_jobs SET updated_at=current_timestamp - INTERVAL 25 HOUR WHERE id=?", stale.ID); err != nil {
		t.Fatal(err)
	}
	if err = c.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	expired, err := c.GetJob(ctx, "one", stale.ID)
	if err != nil || expired.Status != "cancelled" {
		t.Fatalf("stale import not expired: %+v %v", expired, err)
	}
	var staged int
	if err = c.db.QueryRowContext(ctx, "SELECT count(*) FROM stac_items WHERE generation=?", stale.ID).Scan(&staged); err != nil || staged != 0 {
		t.Fatalf("expired staging retained: %d %v", staged, err)
	}
	if err = c.Publish(ctx, "one", fresh.ID, false, nil); err != nil {
		t.Fatalf("fresh import expired: %v", err)
	}
}

func TestRefreshRecordsSourceFingerprintAndSchemaUpgrades(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "stac.duckdb")
	c, err := Open(path, "abc123", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
		t.Fatal(err)
	}
	if err = c.PutBinding(ctx, "one", "scenes", &Binding{ID: "binding", ServiceID: "source", ResourceID: "layer", ResourceKind: "layer", Mode: "mapped"}, 1); err != nil {
		t.Fatal(err)
	}
	j, err := c.CreateJob(ctx, "one", "scenes", "refresh", "running", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.PublishRefresh(ctx, "one", j.ID, nil, "fingerprint"); err != nil {
		t.Fatal(err)
	}
	col, err := c.GetCollection(ctx, "one", "scenes")
	if err != nil || col.SourceFingerprint != "fingerprint" {
		t.Fatalf("fingerprint not recorded: %+v %v", col, err)
	}
	// Recreate a schema 1 database and check that reopening upgrades it.
	if _, err = c.db.Exec("ALTER TABLE stac_collections DROP COLUMN source_fingerprint"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.db.Exec("DELETE FROM stac_schema WHERE version=2"); err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(path, "abc123", 100)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	col, err = c.GetCollection(ctx, "one", "scenes")
	if err != nil || col.SourceFingerprint != "" || col.Binding == nil {
		t.Fatalf("upgraded Collection: %+v %v", col, err)
	}
	var version int
	if err = c.db.QueryRow("SELECT max(version) FROM stac_schema").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version %d %v", version, err)
	}
}
