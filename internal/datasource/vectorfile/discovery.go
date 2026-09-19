package vectorfile

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/gdalcap"
)

// DiscoverLayers discovers available layers from the vector file.
// For single-layer formats (Shapefile, GeoJSON), returns one layer.
// For multi-layer formats (GeoPackage), returns all feature layers.
func (ds *DataSource) DiscoverLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	// If a specific layer is configured, only return that layer
	if ds.cfg.Layer != "" {
		return ds.discoverSingleLayer(ctx, ds.cfg.Layer)
	}

	// For multi-layer formats, try to discover all layers
	if ds.isMultiLayerFormat() {
		return ds.discoverMultiLayers(ctx)
	}

	// For single-layer formats, derive layer name from filename
	layerName := deriveLayerName(ds.cfg.Path)
	return ds.discoverSingleLayer(ctx, layerName)
}

// discoverSingleLayer discovers a single layer
func (ds *DataSource) discoverSingleLayer(ctx context.Context, layerName string) ([]*datasource.DiscoveredLayer, error) {
	// Get columns for the layer
	columns, err := ds.getColumns(ctx, layerName)
	if err != nil {
		return nil, fmt.Errorf("get columns for layer %s: %w", layerName, err)
	}

	// Find geometry column
	geomCol := ds.findGeometryColumn(columns)
	if geomCol == "" {
		return nil, fmt.Errorf("no geometry column found in layer %s", layerName)
	}

	// Find ID column
	idCol := ds.findIDColumn(columns)

	// Detect geometry type and SRID
	geomType := ds.detectGeometryType(ctx, layerName, geomCol)
	srid := ds.detectSRID(ctx, layerName, geomCol)
	if srid == 0 {
		srid = ds.cfg.SRID
	}

	return []*datasource.DiscoveredLayer{
		{
			Name:           layerName,
			Title:          layerName,
			GeometryColumn: geomCol,
			GeometryType:   geomType,
			SRID:           srid,
			IDColumn:       idCol,
		},
	}, nil
}

// discoverMultiLayers discovers all layers from a multi-layer format like GeoPackage
func (ds *DataSource) discoverMultiLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	// For GeoPackage, query the gpkg_contents table to find all feature layers
	if ds.format == "geopackage" {
		return ds.discoverGeoPackageLayers(ctx)
	}

	// Fallback: try to read the file directly and return single layer
	layerName := deriveLayerName(ds.cfg.Path)
	return ds.discoverSingleLayer(ctx, layerName)
}

// discoverGeoPackageLayers discovers layers from a GeoPackage file
func (ds *DataSource) discoverGeoPackageLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	// gpkg_contents is a metadata table, not an OGR feature layer. Enumerate
	// actual layers through a GPKG-only open, retaining the driver allowlist
	// even when an upload is disguised with a .gpkg extension.
	if !gdalcap.Get().GeoPackage {
		return nil, fmt.Errorf("GeoPackage discovery requires the GPKG driver")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dataset, err := godal.Open(ds.cfg.Path, godal.VectorOnly(), godal.Drivers("GPKG"))
	if err != nil {
		return nil, fmt.Errorf("open GeoPackage metadata: %w", err)
	}
	defer dataset.Close()
	var layers []*datasource.DiscoveredLayer
	for _, source := range dataset.Layers() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if source.Type() == godal.GTNone {
			continue
		}
		identifier, description := source.Metadata("IDENTIFIER"), source.Metadata("DESCRIPTION")
		srid, _ := strconv.Atoi(source.SpatialRef().AuthorityCode(""))
		layer, err := ds.discoverGeoPackageLayer(ctx, source.Name(), &identifier, &description, &srid)
		if err != nil {
			return nil, fmt.Errorf("discover GeoPackage layer %s: %w", source.Name(), err)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

// discoverGeoPackageLayer discovers a single layer from a GeoPackage
func (ds *DataSource) discoverGeoPackageLayer(ctx context.Context, tableName string, identifier, description *string, srsID *int) (*datasource.DiscoveredLayer, error) {
	// Get columns for the layer
	columns, err := ds.getColumns(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("get columns for layer %s: %w", tableName, err)
	}

	// Find geometry column
	geomCol := ds.findGeometryColumn(columns)
	if geomCol == "" {
		return nil, fmt.Errorf("no geometry column found in layer %s", tableName)
	}

	// Find ID column
	idCol := ds.findIDColumn(columns)

	// Detect geometry type
	geomType := ds.detectGeometryType(ctx, tableName, geomCol)

	// Use SRID from gpkg_contents if available, otherwise detect
	srid := 0
	if srsID != nil {
		srid = *srsID
	}
	if srid == 0 {
		srid = ds.detectSRID(ctx, tableName, geomCol)
	}
	if srid == 0 {
		srid = ds.cfg.SRID
	}

	// Build title and description
	title := tableName
	if identifier != nil && *identifier != "" {
		title = *identifier
	}

	desc := ""
	if description != nil {
		desc = *description
	}

	return &datasource.DiscoveredLayer{
		Name:           tableName,
		Title:          title,
		Description:    desc,
		GeometryColumn: geomCol,
		GeometryType:   geomType,
		SRID:           srid,
		IDColumn:       idCol,
	}, nil
}

// deriveLayerName derives a layer name from a file path
func deriveLayerName(path string) string {
	base := filepath.Base(path)
	// Remove extension
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	// Clean up name
	base = strings.ReplaceAll(base, "-", "_")
	base = strings.ReplaceAll(base, " ", "_")
	return base
}
