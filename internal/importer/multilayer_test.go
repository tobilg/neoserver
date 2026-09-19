package importer

import (
	"context"
	"os"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestMultiLayerUploadCanBeReincludedAndPublished(t *testing.T) {
	m, _, ws := revisionFixture(t)
	ctx := context.Background()
	file, err := os.Open("../../web/admin/e2e/fixtures/two-layers.gpkg")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	job, err := m.CreateUpload(ctx, ws, "multi", "two-layers.gpkg", "test", file)
	if err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	if job.Status != store.ImportAwaitingPlan || job.Discovery == nil || len(job.Discovery.Layers) != 2 {
		t.Fatalf("discovery: %+v", job)
	}
	plan := store.ImportPlan{ServiceName: "multi"}
	for _, source := range job.Discovery.Layers {
		plan.Layers = append(plan.Layers, store.ImportLayerPlan{SourceLayer: source.Name, PublicID: source.Name, GeometryColumn: source.GeometryColumn, SourceSRID: source.SRID, TargetSRID: source.SRID, Enabled: true})
	}
	first := plan
	first.Layers = first.Layers[:1]
	for _, revision := range []store.ImportPlan{first, plan} {
		if _, err := m.SetPlan(ctx, job.ID, revision); err != nil {
			t.Fatal(err)
		}
		job = runRevisionJob(t, m)
		if job.Status != store.ImportReadyToPublish {
			t.Fatalf("transform: %+v", job)
		}
	}
	job, err = m.Publish(ctx, job.ID)
	if err != nil || job.Status != store.ImportPublished || len(job.Plan.Layers) != 2 {
		t.Fatalf("publish: %+v %v", job, err)
	}
}

func TestPublishedImportLayersCarryNativeExtent(t *testing.T) {
	m, catalog, ws := revisionFixture(t)
	ctx := context.Background()
	file, err := os.Open("../../testing/tutorial/places.geojson")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	job, err := m.CreateUpload(ctx, ws, "places", "places.geojson", "test", file)
	if err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	if job.Status != store.ImportAwaitingPlan || job.Discovery == nil || len(job.Discovery.Layers) != 1 {
		t.Fatalf("discovery: %+v", job)
	}
	source := job.Discovery.Layers[0]
	plan := store.ImportPlan{ServiceName: "places", Layers: []store.ImportLayerPlan{{SourceLayer: source.Name, PublicID: "places", GeometryColumn: source.GeometryColumn, SourceSRID: source.SRID, TargetSRID: 4326, Enabled: true}}}
	if _, err := m.SetPlan(ctx, job.ID, plan); err != nil {
		t.Fatal(err)
	}
	if job = runRevisionJob(t, m); job.Status != store.ImportReadyToPublish {
		t.Fatalf("transform: %+v", job)
	}
	if job, err = m.Publish(ctx, job.ID); err != nil || job.Status != store.ImportPublished {
		t.Fatalf("publish: %+v %v", job, err)
	}
	services, err := catalog.ListServices(ctx, ws)
	if err != nil || len(services) != 1 {
		t.Fatalf("services: %v %v", services, err)
	}
	layers, err := catalog.ListLayers(ctx, services[0].ID)
	if err != nil || len(layers) != 1 {
		t.Fatalf("layers: %v %v", layers, err)
	}
	extent := layers[0].NativeExtent
	if extent == nil || extent.SRID != 4326 {
		t.Fatalf("expected a 4326 extent, got %+v", extent)
	}
	// Berlin, Paris and London must all be inside the stored envelope.
	for _, point := range [][2]float64{{13.405, 52.52}, {2.3522, 48.8566}, {-0.1276, 51.5072}} {
		if point[0] < extent.MinX-0.01 || point[0] > extent.MaxX+0.01 || point[1] < extent.MinY-0.01 || point[1] > extent.MaxY+0.01 {
			t.Fatalf("extent %+v does not contain %v", extent, point)
		}
	}
}
