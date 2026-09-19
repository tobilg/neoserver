package tiles

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/project"
	"github.com/paulmach/orb/simplify"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

// MVTGenerator generates Mapbox Vector Tiles (MVT) from data sources.
type MVTGenerator struct {
	outputName       string // Public MVT identity, independent of source lookup.
	extent           int    // MVT extent, typically 4096
	buffer           int    // Tile buffer in pixels, typically 256
	clipGeom         bool
	maxFeatures      int
	maxVertices      int
	maxTileBytes     int
	statementTimeout time.Duration
}

// NewMVTGenerator creates a new MVT generator.
func NewMVTGenerator(extent int) *MVTGenerator {
	if extent <= 0 {
		extent = 4096
	}
	return &MVTGenerator{
		extent:           extent,
		buffer:           256,
		clipGeom:         true,
		maxFeatures:      50000,
		maxVertices:      5000000,
		maxTileBytes:     10 << 20,
		statementTimeout: 30 * time.Second,
	}
}

// SetLimits applies server-owned hard ceilings to tile generation.
func (g *MVTGenerator) SetLimits(features, vertices, bytes, statementTimeoutMS int) {
	if features > 0 {
		g.maxFeatures = features
	}
	if vertices > 0 {
		g.maxVertices = vertices
	}
	if bytes > 0 {
		g.maxTileBytes = bytes
	}
	if statementTimeoutMS > 0 {
		g.statementTimeout = time.Duration(statementTimeoutMS) * time.Millisecond
	}
}

func (g *MVTGenerator) GenerateTileWithLimits(ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, tms string, z, x, y int, features, vertices, bytes int, outputName ...string) ([]byte, error) {
	limited := *g
	if len(outputName) > 0 {
		limited.outputName = outputName[0]
	}
	if features > 0 {
		limited.maxFeatures = min(limited.maxFeatures, features)
	}
	if vertices > 0 {
		limited.maxVertices = min(limited.maxVertices, vertices)
	}
	if bytes > 0 {
		limited.maxTileBytes = min(limited.maxTileBytes, bytes)
	}
	return limited.GenerateTile(ctx, ds, layer, tms, z, x, y)
}

func (g *MVTGenerator) GenerateSQLViewTileWithLimits(ctx context.Context, ds datasource.SQLViewDataSource, config *datasource.SQLViewConfig, layer *datasource.LayerInfo, tms string, z, x, y int, features, vertices, bytes int, outputName ...string) ([]byte, error) {
	limited := *g
	if len(outputName) > 0 {
		limited.outputName = outputName[0]
	}
	if features > 0 {
		limited.maxFeatures = min(limited.maxFeatures, features)
	}
	if vertices > 0 {
		limited.maxVertices = min(limited.maxVertices, vertices)
	}
	if bytes > 0 {
		limited.maxTileBytes = min(limited.maxTileBytes, bytes)
	}
	bounds, err := TileBBoxWGS84(tms, z, x, y)
	if err != nil {
		return nil, err
	}
	params := datasource.QueryParams{BBox: &datasource.BBox{MinX: bounds.MinX, MinY: bounds.MinY, MaxX: bounds.MaxX, MaxY: bounds.MaxY}, BBoxSRID: 4326, OutputSRID: GetTMSSRID(tms), Limit: limited.maxFeatures + 1}
	rows, err := ds.QuerySQLView(ctx, config, params)
	if err != nil {
		return nil, fmt.Errorf("query SQL view for tile: %w", err)
	}
	nativeBounds, err := TileBBox(tms, z, x, y)
	if err != nil {
		return nil, err
	}
	return limited.encodeFeaturesToMVT(rows, layer.Name, nativeBounds)
}

// GenerateTile generates an MVT tile for the given layer.
func (g *MVTGenerator) GenerateTile(ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, tms string, z, x, y int) ([]byte, error) {
	// Get tile bounds in the native CRS of the TMS
	bounds, err := TileBBox(tms, z, x, y)
	if err != nil {
		return nil, err
	}

	// Determine which query method to use based on datasource type
	switch ds.Type() {
	case store.ServiceTypePostGIS:
		if tms != TMSWebMercatorQuad {
			return g.generateDuckDBTileFallback(ctx, ds, layer, tms, z, x, y, bounds)
		}
		return g.generatePostGISTile(ctx, ds, layer, tms, z, x, y, bounds)
	case store.ServiceTypeDuckDB, store.ServiceTypeGeoParquet, store.ServiceTypeVectorFile:
		return g.generateDuckDBTile(ctx, ds, layer, tms, z, x, y, bounds)
	default:
		return nil, fmt.Errorf("MVT generation not supported for data source type: %s", ds.Type())
	}
}

// generatePostGISTile generates an MVT tile using PostGIS ST_AsMVT.
func (g *MVTGenerator) generatePostGISTile(ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, tms string, z, x, y int, bounds *TileBounds) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, g.statementTimeout)
	defer cancel()
	// Get the underlying pgx pool
	type pooler interface {
		Pool() *pgxpool.Pool
	}
	poolDS, ok := ds.(pooler)
	if !ok {
		return nil, fmt.Errorf("PostGIS datasource does not expose connection pool")
	}
	pool := poolDS.Pool()

	// Build the MVT query
	sql, args := g.buildPostGISMVTQuery(layer, tms, z, x, y, bounds)

	var mvtData []byte
	var featureCount, vertexCount int
	err := pool.QueryRow(ctx, sql, args...).Scan(&mvtData, &featureCount, &vertexCount)
	if err != nil {
		return nil, fmt.Errorf("MVT query failed: %w", err)
	}

	if featureCount > g.maxFeatures {
		return nil, fmt.Errorf("tile feature limit exceeded: %d > %d", featureCount, g.maxFeatures)
	}
	if vertexCount > g.maxVertices {
		return nil, fmt.Errorf("tile vertex limit exceeded: %d > %d", vertexCount, g.maxVertices)
	}
	if len(mvtData) > g.maxTileBytes {
		return nil, fmt.Errorf("tile output limit exceeded: %d > %d bytes", len(mvtData), g.maxTileBytes)
	}
	return mvtData, nil
}

// buildPostGISMVTQuery builds a PostGIS query to generate MVT tiles.
func (g *MVTGenerator) buildPostGISMVTQuery(layer *datasource.LayerInfo, tms string, z, x, y int, bounds *TileBounds) (string, []any) {
	schema := quoteIdent(layer.Schema)
	table := quoteIdent(strings.TrimPrefix(layer.Name, layer.Schema+"."))
	geomCol := quoteIdent(layer.GeometryColumn)
	layerName := layer.Name
	if strings.Contains(layerName, ".") {
		parts := strings.SplitN(layerName, ".", 2)
		layerName = parts[1]
	}
	if g.outputName != "" {
		layerName = g.outputName
	}

	targetSRID := GetTMSSRID(tms)

	// Build property columns (cast incompatible types)
	propCols := g.buildPropertyColumns(layer)

	// Build the MVT query using ST_TileEnvelope and ST_AsMVTGeom
	sql := fmt.Sprintf(`
WITH
tile_bounds AS (
    SELECT ST_TileEnvelope($1, $2, $3) AS geom
),
features AS (
    SELECT
        %s AS id,
        ST_AsMVTGeom(
            ST_Transform(t.%s, %d),
            (SELECT geom FROM tile_bounds),
            %d, %d, %t
        ) AS geom
        %s
    FROM %s.%s t, tile_bounds
    WHERE ST_Intersects(
        ST_Transform(t.%s, %d),
        tile_bounds.geom
    )
	LIMIT %d
)
SELECT ST_AsMVT(features.*, $4, %d, 'geom', 'id') AS mvt,
       COUNT(*)::integer,
       COALESCE(SUM(ST_NPoints(geom)), 0)::integer
FROM features
WHERE geom IS NOT NULL
`,
		g.buildIDExpr(layer),
		geomCol, targetSRID,
		g.extent, g.buffer, g.clipGeom,
		propCols,
		schema, table,
		geomCol, targetSRID,
		g.maxFeatures+1,
		g.extent,
	)

	args := []any{z, x, y, layerName}
	return sql, args
}

// generateDuckDBTile generates an MVT tile using DuckDB spatial extension.
func (g *MVTGenerator) generateDuckDBTile(ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, tms string, z, x, y int, bounds *TileBounds) ([]byte, error) {
	return g.generateDuckDBTileFallback(ctx, ds, layer, tms, z, x, y, bounds)
}

// generateDuckDBTileFallback generates MVT by fetching features and encoding manually.
// This is a fallback for when native ST_AsMVT is not available.
func (g *MVTGenerator) generateDuckDBTileFallback(ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, tms string, z, x, y int, bounds *TileBounds) ([]byte, error) {
	// Get WGS84 bounds for querying
	wgsBounds, err := TileBBoxWGS84(tms, z, x, y)
	if err != nil {
		return nil, err
	}

	// Query features within the tile bounds
	params := datasource.QueryParams{
		BBox: &datasource.BBox{
			MinX: wgsBounds.MinX,
			MinY: wgsBounds.MinY,
			MaxX: wgsBounds.MaxX,
			MaxY: wgsBounds.MaxY,
		},
		BBoxSRID:   4326,
		OutputSRID: GetTMSSRID(tms),
		Limit:      g.maxFeatures + 1,
		Offset:     0,
	}

	features, err := ds.Query(ctx, layer.Name, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query features for tile: %w", err)
	}

	// Convert features to MVT
	return g.encodeFeaturesToMVT(features, layer.Name, bounds)
}

// encodeFeaturesToMVT encodes features already projected into bounds' CRS.
func (g *MVTGenerator) encodeFeaturesToMVT(features []json.RawMessage, layerName string, bounds *TileBounds) ([]byte, error) {
	if len(features) > g.maxFeatures {
		return nil, fmt.Errorf("tile feature limit exceeded: %d > %d", len(features), g.maxFeatures)
	}
	collection := geojson.NewFeatureCollection()
	vertices := 0
	for _, raw := range features {
		feature, err := geojson.UnmarshalFeature(raw)
		if err != nil {
			return nil, fmt.Errorf("decode GeoJSON feature: %w", err)
		}
		if feature.Geometry == nil {
			continue
		}
		vertices += geometryVertices(feature.Geometry)
		if vertices > g.maxVertices {
			return nil, fmt.Errorf("tile vertex limit exceeded: %d > %d", vertices, g.maxVertices)
		}
		normalizeMVTProperties(feature.Properties)
		collection.Append(feature)
	}

	if g.outputName != "" {
		layerName = g.outputName
	} else {
		layerName = strings.TrimPrefix(layerName, "public.")
	}
	layer := mvt.NewLayer(layerName, collection)
	layer.Extent = uint32(g.extent)
	width, height := bounds.MaxX-bounds.MinX, bounds.MaxY-bounds.MinY
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid tile bounds")
	}
	for _, feature := range layer.Features {
		feature.Geometry = project.Geometry(feature.Geometry, func(point orb.Point) orb.Point {
			return orb.Point{
				(point[0] - bounds.MinX) / width * float64(g.extent),
				(bounds.MaxY - point[1]) / height * float64(g.extent),
			}
		})
	}
	buffer := float64(g.buffer)
	layer.Clip(orb.Bound{Min: orb.Point{-buffer, -buffer}, Max: orb.Point{float64(g.extent) + buffer, float64(g.extent) + buffer}})
	layer.Simplify(simplify.DouglasPeucker(1))
	layer.RemoveEmpty(1, 1)
	data, err := mvt.Marshal(mvt.Layers{layer})
	if err != nil {
		return nil, fmt.Errorf("encode MVT: %w", err)
	}
	if len(data) > g.maxTileBytes {
		return nil, fmt.Errorf("tile output limit exceeded: %d > %d bytes", len(data), g.maxTileBytes)
	}
	return data, nil
}

func normalizeMVTProperties(properties geojson.Properties) {
	for key, value := range properties {
		switch value.(type) {
		case nil, string, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		default:
			encoded, err := json.Marshal(value)
			if err != nil {
				delete(properties, key)
				continue
			}
			properties[key] = string(encoded)
		}
	}
}

func geometryVertices(geometry orb.Geometry) int {
	switch value := geometry.(type) {
	case orb.Point:
		return 1
	case orb.MultiPoint:
		return len(value)
	case orb.LineString:
		return len(value)
	case orb.MultiLineString:
		total := 0
		for _, line := range value {
			total += len(line)
		}
		return total
	case orb.Ring:
		return len(value)
	case orb.Polygon:
		total := 0
		for _, ring := range value {
			total += len(ring)
		}
		return total
	case orb.MultiPolygon:
		total := 0
		for _, polygon := range value {
			for _, ring := range polygon {
				total += len(ring)
			}
		}
		return total
	case orb.Collection:
		total := 0
		for _, child := range value {
			total += geometryVertices(child)
		}
		return total
	default:
		return 0
	}
}

// buildIDExpr builds the ID expression for MVT.
func (g *MVTGenerator) buildIDExpr(layer *datasource.LayerInfo) string {
	if layer.IDColumn != "" {
		return fmt.Sprintf("t.%s", quoteIdent(layer.IDColumn))
	}
	return "NULL"
}

// buildPropertyColumns builds the property column expressions for MVT.
func (g *MVTGenerator) buildPropertyColumns(layer *datasource.LayerInfo) string {
	if len(layer.Properties) == 0 {
		return ""
	}

	var cols []string
	for _, prop := range layer.Properties {
		// Skip geometry and ID columns
		if prop.Name == layer.GeometryColumn || prop.Name == layer.IDColumn {
			continue
		}

		// Cast types that MVT doesn't support
		colExpr := g.castPropertyForMVT(prop, layer.PGTypes)
		cols = append(cols, colExpr)
	}

	if len(cols) == 0 {
		return ""
	}
	return ", " + strings.Join(cols, ", ")
}

// castPropertyForMVT returns the column expression with appropriate type casting.
// MVT only supports: STRING, FLOAT, DOUBLE, INT64, UINT64, SINT64, BOOL
func (g *MVTGenerator) castPropertyForMVT(prop datasource.PropertyInfo, pgTypes map[string]string) string {
	colName := quoteIdent(prop.Name)
	pgType := ""
	if pgTypes != nil {
		pgType = strings.ToLower(pgTypes[prop.Name])
	}

	// Types that need casting to text
	needsCastToText := []string{
		"json", "jsonb", "xml", "uuid", "bytea", "date", "time", "timetz",
		"timestamp", "timestamptz", "interval", "inet", "cidr", "macaddr",
		"point", "line", "lseg", "box", "path", "polygon", "circle",
		"tsvector", "tsquery", "bit", "varbit", "money",
	}

	// Array types
	if strings.HasPrefix(pgType, "_") || strings.Contains(pgType, "[]") {
		return fmt.Sprintf("t.%s::text AS %s", colName, colName)
	}

	// Check if type needs casting
	for _, t := range needsCastToText {
		if strings.Contains(pgType, t) {
			return fmt.Sprintf("t.%s::text AS %s", colName, colName)
		}
	}

	// Numeric types that need casting to double
	if strings.Contains(pgType, "numeric") || strings.Contains(pgType, "decimal") {
		return fmt.Sprintf("t.%s::double precision AS %s", colName, colName)
	}

	// Default: use as-is
	return fmt.Sprintf("t.%s", colName)
}

// quoteIdent quotes a SQL identifier.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
