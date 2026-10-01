package staccatalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/stacmodel"
)

func TestImportPreviewDuplicatesAndCancellation(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	if err := c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
		t.Fatal(err)
	}
	item := testItem("a", 1)
	item.Object("assets")["data"] = stacmodel.Document{"href": "scene.tif"}
	importFile := func(raw string) *Job {
		t.Helper()
		j, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Import(ctx, j, strings.NewReader(raw), "https://example.org/data/"); err != nil {
			t.Fatal(err)
		}
		return j
	}
	j := importFile(`{"type":"FeatureCollection","features":[` + jsonString(item) + `]}`)
	preview, err := c.PreviewJob(ctx, "one", j.ID)
	if err != nil || len(preview) != 1 {
		t.Fatalf("preview: %v %v", preview, err)
	}
	if preview[0].Object("assets").Object("data").String("href") != "https://example.org/data/scene.tif" {
		t.Fatal("relative href not resolved")
	}
	if _, err = c.GetItem(ctx, "one", "scenes", "a"); err != ErrNotFound {
		t.Fatalf("unpublished Item visible: %v", err)
	}
	if err = c.Publish(ctx, "one", j.ID, false, nil); err != nil {
		t.Fatal(err)
	}
	item.Object("properties")["description"] = "replacement"
	duplicate := importFile(jsonString(item) + "\n")
	if err = c.Publish(ctx, "one", duplicate.ID, false, nil); err == nil {
		t.Fatal("duplicate accepted without upsert")
	}
	if err = c.Publish(ctx, "one", duplicate.ID, true, nil); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetItem(ctx, "one", "scenes", "a")
	if err != nil || got.Object("properties").String("description") != "replacement" {
		t.Fatalf("upsert: %v %v", got, err)
	}
	cancelled := importFile(jsonString(testItem("b", 2)))
	if err = c.Cancel(ctx, "one", cancelled.ID); err != nil {
		t.Fatal(err)
	}
	if err = c.Publish(ctx, "one", cancelled.ID, false, nil); err == nil {
		t.Fatal("cancelled job published")
	}
	if err = c.Prune(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLinkedOverridesSurviveRefreshWithoutFreezingSourceProperties(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	if err := c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
		t.Fatal(err)
	}
	if err := c.PutBinding(ctx, "one", "scenes", &Binding{ID: "binding", ServiceID: "source", ResourceID: "layer", ResourceKind: "layer", Mode: "mapped"}, 1); err != nil {
		t.Fatal(err)
	}
	publish := func(value string) {
		t.Helper()
		j, err := c.CreateJob(ctx, "one", "scenes", "refresh", "running", nil)
		if err != nil {
			t.Fatal(err)
		}
		d := testItem("a", 0)
		d.Object("properties")["platform"] = value
		if err = c.Stage(ctx, j, []stacmodel.Document{d}); err != nil {
			t.Fatal(err)
		}
		if err = c.Publish(ctx, "one", j.ID, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	publish("before")
	d, err := c.GetItem(ctx, "one", "scenes", "a")
	if err != nil {
		t.Fatal(err)
	}
	d.Object("properties")["description"] = "user text"
	if err = c.PutItem(ctx, "one", "scenes", d, true); err != nil {
		t.Fatal(err)
	}
	publish("after")
	d, err = c.GetItem(ctx, "one", "scenes", "a")
	if err != nil {
		t.Fatal(err)
	}
	if d.Object("properties").String("description") != "user text" || d.Object("properties").String("platform") != "after" {
		t.Fatalf("override/source ownership lost: %v", d)
	}
	d.Object("properties")["datetime"] = "2020-01-01T00:00:00Z"
	if err = c.PutItem(ctx, "one", "scenes", d, true); err == nil {
		t.Fatal("source acquisition time overridden")
	}
}

func TestRestartRetainsReadyImportAndFailsIncompleteUpload(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "stac.duckdb")
	c, err := Open(path, "abc123", 10000)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
		t.Fatal(err)
	}
	ready, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Import(ctx, ready, strings.NewReader(jsonString(testItem("ready", 1))), ""); err != nil {
		t.Fatal(err)
	}
	interrupted, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Stage(ctx, interrupted, []stacmodel.Document{testItem("partial", 2)}); err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(path, "abc123", 10000)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	failed, err := c.GetJob(ctx, "one", interrupted.ID)
	if err != nil || failed.Status != "failed" {
		t.Fatalf("interrupted job: %+v %v", failed, err)
	}
	if err = c.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if err = c.Publish(ctx, "one", ready.ID, false, nil); err != nil {
		t.Fatal(err)
	}
	page, err := c.Search(ctx, "one", Search{}, []string{"scenes"})
	if err != nil || len(page.Items) != 1 || page.Items[0].String("id") != "ready" {
		t.Fatalf("recovered inventory: %+v %v", page, err)
	}
}

func TestRetargetingPreservesOriginalPublication(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	if err := c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
		t.Fatal(err)
	}
	original := &Binding{ID: "binding", ServiceID: "private-source", ResourceID: "layer", ResourceKind: "layer", Mode: "mapped"}
	if err := c.PutBinding(ctx, "one", "scenes", original, 1); err != nil {
		t.Fatal(err)
	}
	j, err := c.CreateJob(ctx, "one", "scenes", "refresh", "running", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Stage(ctx, j, []stacmodel.Document{testItem("private-item", 0)}); err != nil {
		t.Fatal(err)
	}
	if err = c.Publish(ctx, "one", j.ID, false, nil); err != nil {
		t.Fatal(err)
	}
	col, err := c.GetCollection(ctx, "one", "scenes")
	if err != nil {
		t.Fatal(err)
	}
	replacement := *original
	replacement.ServiceID = "public-source"
	if err = c.PutBinding(ctx, "one", "scenes", &replacement, col.Revision); err == nil {
		t.Fatal("old Items could inherit replacement source access")
	}
	preserved, err := c.GetCollection(ctx, "one", "scenes")
	if err != nil || preserved.Binding.ServiceID != original.ServiceID || preserved.ItemCount != 1 {
		t.Fatalf("publication not preserved: %+v %v", preserved, err)
	}
	if _, err = c.GetItem(ctx, "one", "scenes", "private-item"); err != nil {
		t.Fatal(err)
	}
}
