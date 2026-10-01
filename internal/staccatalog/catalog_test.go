package staccatalog

import (
	"context"
	"fmt"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"path/filepath"
	"testing"
)

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "stac.duckdb"), "abc123", 10000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func testItem(id string, x float64) stacmodel.Document {
	d, _ := stacmodel.Decode([]byte(fmt.Sprintf(`{"type":"Feature","stac_version":"1.1.0","id":%q,"collection":"scenes","geometry":{"type":"Point","coordinates":[%g,0]},"bbox":[%g,0,%g,0],"properties":{"datetime":"2025-01-01T00:00:00.123456789Z"},"assets":{},"links":[{"rel":"collection","href":"https://example.org/collections/scenes"}]}`, id, x, x, x)))
	return d
}
func TestAtomicGenerationAndIsolation(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	for _, ws := range []string{"one", "two"} {
		if err := c.PutCollection(ctx, ws, Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
			t.Fatal(err)
		}
	}
	j, err := c.CreateJob(ctx, "one", "scenes", "import", "uploading", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Stage(ctx, j, []stacmodel.Document{testItem("a", 179), testItem("b", -179), testItem("c", 0)}); err != nil {
		t.Fatal(err)
	}
	page, err := c.Search(ctx, "one", Search{}, []string{"scenes"})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("staged data leaked: %+v %v", page, err)
	}
	if err = c.SetJobStatus(ctx, "one", j.ID, "ready", ""); err != nil {
		t.Fatal(err)
	}
	if err = c.Publish(ctx, "one", j.ID, false, nil); err != nil {
		t.Fatal(err)
	}
	page, err = c.Search(ctx, "one", Search{BBox: []float64{170, -1, -170, 1}, Limit: 1}, []string{"scenes"})
	if err != nil || len(page.Items) != 1 || page.Next == "" {
		t.Fatalf("antimeridian page: %+v %v", page, err)
	}
	next := Search{BBox: []float64{170, -1, -170, 1}, Limit: 1, Token: page.Next}
	page, err = c.Search(ctx, "one", next, []string{"scenes"})
	if err != nil || len(page.Items) != 1 || page.Items[0].String("id") != "b" {
		t.Fatalf("next page %+v %v", page, err)
	}
	if _, err = c.Search(ctx, "two", next, []string{"scenes"}); err == nil {
		t.Fatal("cross-workspace token accepted")
	}
	page, err = c.Search(ctx, "two", Search{}, []string{"scenes"})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("cross-workspace inventory leak")
	}
	col, err := c.GetCollection(ctx, "one", "scenes")
	if err != nil || col.ItemCount != 3 {
		t.Fatalf("derived metadata: %+v %v", col, err)
	}
}
func TestRefreshConflictAndExactIntersection(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	_ = c.PutCollection(ctx, "one", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true)
	if err := c.PutItem(ctx, "one", "scenes", testItem("a", 0), false); err != nil {
		t.Fatal(err)
	}
	geometry, _ := stacmodel.Decode([]byte(`{"type":"Polygon","coordinates":[[[-2,-2],[2,-2],[2,2],[-2,2],[-2,-2]],[[-1,-1],[-1,1],[1,1],[1,-1],[-1,-1]]]}`))
	p, err := c.Search(ctx, "one", Search{Intersects: geometry}, []string{"scenes"})
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("polygon hole not honored: %v %v", p, err)
	}
	j, err := c.CreateJob(ctx, "one", "scenes", "refresh", "running", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Stage(ctx, j, []stacmodel.Document{testItem("b", 1)}); err != nil {
		t.Fatal(err)
	}
	col, _ := c.GetCollection(ctx, "one", "scenes")
	col.Public = true
	if err = c.PutCollection(ctx, "one", *col, false); err != nil {
		t.Fatal(err)
	}
	if err = c.Publish(ctx, "one", j.ID, false, nil); err != ErrConflict {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	if _, err = c.GetItem(ctx, "one", "scenes", "a"); err != nil {
		t.Fatal("last good generation lost")
	}
}

func TestBBoxIncludesItemsWithoutGeometry(t *testing.T) {
	ctx := context.Background()
	c := testCatalog(t)
	for _, ws := range []string{"one", "two"} {
		if err := c.PutCollection(ctx, ws, Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scenes", "other", nil)}, true); err != nil {
			t.Fatal(err)
		}
		doc := testItem("b", 0)
		doc["geometry"] = nil
		delete(doc, "bbox")
		if err := c.PutItem(ctx, ws, "scenes", doc, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.PutItem(ctx, "one", "scenes", testItem("a", 179), false); err != nil {
		t.Fatal(err)
	}
	for _, bbox := range [][]float64{{170, -1, -170, 1}, {170, -1, 10, -170, 1, 20}} {
		q := Search{BBox: bbox, Limit: 1}
		page, err := c.Search(ctx, "one", q, []string{"scenes"})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("bbox page: %+v %v", page, err)
		}
		if len(bbox) == 4 {
			if page.Items[0].String("id") != "a" || page.Next == "" {
				t.Fatalf("spatial branch: %+v", page)
			}
			q.Token = page.Next
			page, err = c.Search(ctx, "one", q, []string{"scenes"})
		}
		if err != nil || len(page.Items) != 1 || page.Items[0].String("id") != "b" || page.Next != "" {
			t.Fatalf("nonspatial branch: %+v %v", page, err)
		}
	}
	geometry, _ := stacmodel.Decode([]byte(`{"type":"Point","coordinates":[0,0]}`))
	page, err := c.Search(ctx, "one", Search{Intersects: geometry}, []string{"scenes"})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("intersects must require a geometry: %+v %v", page, err)
	}
}
