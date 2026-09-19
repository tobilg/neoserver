package rastermosaic

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/store"
)

func TestMosaicDiscoveryRenderingAndDimensionSelection(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "..", "testing", "fixtures", "raster", "rectified-grid-coverage.tif"))
	if err != nil {
		t.Fatal(err)
	}
	pathpolicy.Configure([]string{fixture})
	zero := 0.0
	connection, _ := json.Marshal(store.RasterMosaicConnectionInfo{Name: "series", Granules: []store.RasterMosaicGranule{{Path: fixture, Time: "2026-01-01T00:00:00Z", Elevation: &zero}, {Path: fixture, Time: "2026-01-02T00:00:00Z", Priority: 1}}})
	backend, err := NewFromService(&store.Service{ID: "m", Name: "series", Type: store.ServiceTypeRasterMosaic, ConnectionInfo: connection})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	mosaic := backend.(*DataSource)
	items, err := mosaic.DiscoverCoverages(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if items[0].Descriptor == nil || len(items[0].Descriptor.Axes) != 4 || items[0].Descriptor.Axes[2].Kind != datasource.CoverageAxisTime ||
		items[0].Descriptor.Axes[3].Kind != datasource.CoverageAxisElevation || items[0].Descriptor.Axes[3].Coordinates[0].Number == nil || *items[0].Descriptor.Axes[3].Coordinates[0].Number != 0 {
		t.Fatalf("unexpected multidimensional descriptor: %+v", items[0].Descriptor)
	}
	grid, err := mosaic.RenderCoverage(context.Background(), "mosaic", datasource.CoverageRenderRequest{TargetCRS: "EPSG:32611", BBox: items[0].Info.Envelope, Width: 8, Height: 6, Bands: []int{1}, Resampling: "nearest", Time: "2026-01-02T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(grid.Bands) != 1 || len(grid.Bands[0]) != 48 {
		t.Fatalf("grid=%+v", grid)
	}
	instant, _ := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
	result, err := mosaic.ExecuteCoverage(context.Background(), "mosaic", datasource.CoverageQuery{
		DomainSubsets: []datasource.CoverageDomainSubset{{Axis: "time", Low: datasource.CoverageAxisValue{Time: &instant}, High: datasource.CoverageAxisValue{Time: &instant}, Slice: true}},
		TargetGrid:    &datasource.CoverageTargetGrid{CRS: items[0].Info.CRS, BBox: items[0].Info.Envelope, Width: 8, Height: 6, Bands: []int{1}, Resampling: "nearest"},
	})
	if err != nil || result.Grid == nil || len(result.Grid.Bands[0]) != 48 {
		t.Fatalf("WCS dimension slice failed: result=%+v err=%v", result, err)
	}
	if _, err = mosaic.RenderCoverage(context.Background(), "mosaic", datasource.CoverageRenderRequest{TargetCRS: "EPSG:32611", BBox: items[0].Info.Envelope, Width: 8, Height: 6, Bands: []int{1}, Time: "2030-01-01T00:00:00Z"}); err == nil {
		t.Fatal("expected empty selection error")
	}
	if _, err = mosaic.ExecuteCoverage(context.Background(), "mosaic", datasource.CoverageQuery{
		MaxSourceGranules: 1,
		TargetGrid:        &datasource.CoverageTargetGrid{CRS: items[0].Info.CRS, BBox: items[0].Info.Envelope, Width: 8, Height: 6, Bands: []int{1}},
	}); err == nil {
		t.Fatal("expected WCS source-granule limit error")
	}
	SetMaxRenderGranules(1)
	t.Cleanup(func() { SetMaxRenderGranules(256) })
	if _, err = mosaic.RenderCoverage(context.Background(), "mosaic", datasource.CoverageRenderRequest{TargetCRS: "EPSG:32611", BBox: items[0].Info.Envelope, Width: 8, Height: 6, Bands: []int{1}, Time: "2026-01-01T00:00:00Z,2026-01-02T00:00:00Z"}); err == nil {
		t.Fatal("expected per-render granule limit error")
	}
}
