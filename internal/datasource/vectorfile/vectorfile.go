// Package vectorfile provides a DataSource implementation for vector file formats
// (Shapefile, GeoPackage, GeoJSON, etc.) using DuckDB's spatial extension.
package vectorfile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/duckdbsqlview"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/filter"
	"github.com/tobilg/neoserver/internal/store"
)

func init() {
	datasource.Register(store.ServiceTypeVectorFile, NewFromService)
}

// Config holds vector file connection configuration.
type Config struct {
	Path           string            `json:"path"`                      // File path, HTTP URL, or S3 URI
	Layer          string            `json:"layer,omitempty"`           // Layer name (for multi-layer formats)
	GeometryColumn string            `json:"geometry_column,omitempty"` // Override geometry column name
	IDColumn       string            `json:"id_column,omitempty"`       // Override ID column name
	SRID           int               `json:"srid,omitempty"`            // Override SRID (default: 4326)
	OpenOptions    map[string]string `json:"open_options,omitempty"`    // GDAL open options
}

// DefaultConfig returns default configuration values.
func DefaultConfig() Config {
	return Config{
		GeometryColumn: "wkb_geometry", // Default from ST_Read
		SRID:           4326,
	}
}

// DataSource implements datasource.DataSource for vector files.
type DataSource struct {
	id            string
	db            *sql.DB
	cfg           Config
	format        string
	layerCache    map[string]*datasource.LayerInfo
	cacheMu       sync.RWMutex
	sqlViewHelper *duckdbsqlview.Helper
}

// New creates a new vector file DataSource.
func New(id string, cfg Config) (*DataSource, error) {
	if _, err := readOptions(cfg.OpenOptions); err != nil {
		return nil, err
	}
	// Create in-memory DuckDB for reading vector files
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	maxConns := runtime.GOMAXPROCS(0)
	if maxConns > 4 {
		maxConns = 4
	}
	if maxConns < 1 {
		maxConns = 1
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)

	// Load spatial extension
	_, err = db.Exec("INSTALL spatial; LOAD spatial;")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("load spatial extension: %w", err)
	}

	// Load httpfs extension for remote files
	if strings.HasPrefix(cfg.Path, "s3://") || strings.HasPrefix(cfg.Path, "http://") || strings.HasPrefix(cfg.Path, "https://") {
		_, err = db.Exec("INSTALL httpfs; LOAD httpfs;")
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("load httpfs extension: %w", err)
		}
	}

	// Apply defaults
	defaults := DefaultConfig()
	if cfg.GeometryColumn == "" {
		cfg.GeometryColumn = defaults.GeometryColumn
	}
	if cfg.SRID == 0 {
		cfg.SRID = defaults.SRID
	}

	// Detect format from file extension
	format := detectFormat(cfg.Path)

	ds := &DataSource{
		id:         id,
		db:         db,
		cfg:        cfg,
		format:     format,
		layerCache: make(map[string]*datasource.LayerInfo),
	}
	ds.sqlViewHelper = duckdbsqlview.NewHelper(db, columnTypeToJSON)

	// Verify the file can be read
	if err := ds.verifyFile(context.Background()); err != nil {
		db.Close()
		return nil, err
	}

	return ds, nil
}

// NewFromService creates a vector file DataSource from a store.Service.
func NewFromService(svc *store.Service) (datasource.DataSource, error) {
	var cfg Config
	if err := json.Unmarshal(svc.ConnectionInfo, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Enforce the datasource allowlist (deny-by-default globs + SSRF host blocks).
	resolved, err := pathpolicy.Resolve(context.Background(), cfg.Path)
	if err != nil {
		return nil, err
	}
	cfg.Path = resolved

	return New(svc.ID, cfg)
}

// Type returns the data source type.
func (ds *DataSource) Type() store.ServiceType {
	return store.ServiceTypeVectorFile
}

// ID returns the service ID.
func (ds *DataSource) ID() string {
	return ds.id
}

// verifyFile checks if the vector file can be read
func (ds *DataSource) verifyFile(ctx context.Context) error {
	readExpr := ds.buildReadExpression("")
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s LIMIT 1`, readExpr)
	var count int
	return ds.db.QueryRowContext(ctx, query).Scan(&count)
}

// buildReadExpression builds the ST_Read expression for querying
func (ds *DataSource) buildReadExpression(layer string) string {
	// Escape single quotes in path
	path := strings.ReplaceAll(ds.cfg.Path, "'", "''")

	// Build open options string if present
	openOpts, _ := readOptions(ds.cfg.OpenOptions) // validated before opening
	openOpts += ReadPolicySQL

	// Determine which layer to use
	layerName := ds.cfg.Layer
	if layerName == "" && layer != "" && ds.isMultiLayerFormat() {
		layerName = layer
	}

	if layerName != "" {
		// Escape single quotes in layer name
		layerName = strings.ReplaceAll(layerName, "'", "''")
		return fmt.Sprintf("ST_Read('%s', layer='%s'%s)", path, layerName, openOpts)
	}

	return fmt.Sprintf("ST_Read('%s'%s)", path, openOpts)
}

func validOpenOptionName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// Query executes a feature query.
func (ds *DataSource) Query(ctx context.Context, layer string, params datasource.QueryParams) ([]json.RawMessage, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}

	query, args, err := ds.buildListSQL(info, layer, params)
	if err != nil {
		return nil, err
	}

	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []json.RawMessage
	for rows.Next() {
		// SQL casts the JSON result to VARCHAR so the driver cannot round
		// integer IDs/properties through its map[string]any JSON decoder.
		var feature string
		if err := rows.Scan(&feature); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, json.RawMessage(feature))
	}

	return out, rows.Err()
}

// QueryByID retrieves a single feature by ID.
func (ds *DataSource) QueryByID(ctx context.Context, layer, featureID string, outputSRID int) (json.RawMessage, bool, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, false, err
	}

	if info.IDColumn == "" {
		return nil, false, fmt.Errorf("layer %s has no ID column", layer)
	}

	query := ds.buildFeatureByIDSQL(info, layer, outputSRID)
	arg, err := parseFeatureID(info.PGTypes[info.IDColumn], featureID)
	if err != nil {
		return nil, false, err
	}

	var feature string
	err = ds.db.QueryRowContext(ctx, query, arg).Scan(&feature)
	if err == nil {
		return json.RawMessage(feature), true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("query row: %w", err)
}

// QueryWKB executes a feature query and returns WKB geometry with properties for rendering.
// This is used by WMS GetMap to get geometry in a format suitable for rendering.
func (ds *DataSource) QueryWKB(ctx context.Context, layer string, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}

	query, args, err := ds.buildWKBSQL(info, layer, params)
	if err != nil {
		return nil, err
	}

	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []datasource.RenderFeature
	for rows.Next() {
		var geomBytes []byte
		var propsJSON interface{}
		var id any
		if err := rows.Scan(&geomBytes, &propsJSON, &id); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		// Convert properties from DuckDB JSON (map[string]interface{}) to our format
		props := make(map[string]interface{})
		if propsJSON != nil {
			if m, ok := propsJSON.(map[string]interface{}); ok {
				props = m
			}
		}

		out = append(out, datasource.RenderFeature{
			ID:         datasource.StableRenderFeatureID(id, geomBytes, props),
			Geometry:   geomBytes,
			Properties: props,
		})
	}

	return out, rows.Err()
}

func (ds *DataSource) QueryWKBStream(ctx context.Context, layer string, params datasource.QueryParams) (datasource.RenderFeatureStream, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}
	query, args, err := ds.buildWKBSQL(info, layer, params)
	if err != nil {
		return nil, err
	}
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return datasource.NewSQLRenderStream(rows), nil
}

// buildWKBSQL builds SQL returning: SELECT ST_AsWKB(geom) AS geom, props AS props FROM ...
func (ds *DataSource) buildWKBSQL(info *datasource.LayerInfo, layer string, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	geom := quoteIdent(info.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = 4326
	}

	// Check if geometry is native GEOMETRY type or needs WKB conversion
	geomColType := info.PGTypes[info.GeometryColumn]
	isNative := isNativeGeometry(geomColType)

	// Build geometry expression - handle both native GEOMETRY and WKB/BLOB
	var geomExpr string
	if isNative {
		geomExpr = fmt.Sprintf("t.%s", geom)
		if info.SRID != outSRID && info.SRID != 0 {
			geomExpr = fmt.Sprintf("ST_Transform(t.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
		}
	} else {
		geomExpr = fmt.Sprintf("ST_GeomFromWKB(t.%s)", geom)
		if info.SRID != outSRID && info.SRID != 0 {
			geomExpr = fmt.Sprintf("ST_Transform(ST_GeomFromWKB(t.%s), 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
		}
	}
	if p.SimplifyTolerance > 0 {
		geomExpr = fmt.Sprintf("ST_SimplifyPreserveTopology(%s, %.12g)", geomExpr, p.SimplifyTolerance)
	}

	// Build properties expression
	propCols := make([]string, 0, len(info.Properties))
	for _, prop := range info.Properties {
		if datasource.PropertySelected(prop.Name, p.Properties) {
			propCols = append(propCols, fmt.Sprintf("%s, t.%s", quoteLiteral(prop.Name), quoteIdent(prop.Name)))
		}
	}
	propExpr := "json_object(" + strings.Join(propCols, ", ") + ")"
	if len(propCols) == 0 {
		propExpr = "'{}'::JSON"
	}
	idExpr := "NULL"
	if info.IDColumn != "" {
		idExpr = fmt.Sprintf("CAST(t.%s AS VARCHAR)", quoteIdent(info.IDColumn))
	}

	var whereParts []string
	var args []any
	argPos := 1

	// BBox filter
	if p.BBox != nil {
		// Use native geometry or WKB conversion based on column type
		var geomForFilter string
		if isNative {
			geomForFilter = fmt.Sprintf("t.%s", geom)
		} else {
			geomForFilter = fmt.Sprintf("ST_GeomFromWKB(t.%s)", geom)
		}
		predicate, bboxArgs, nextArg := buildBBoxPredicate(geomForFilter, info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: filterGeometry(info)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		allowed := make(map[string]struct{})
		for _, prop := range info.Properties {
			allowed[prop.Name] = struct{}{}
		}
		if info.IDColumn != "" {
			allowed[info.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}

		opts := filter.DuckDBOptions{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        info.SRID,
			AllowedProperties: allowed,
			GeometryProperty:  info.GeometryColumn,
		}

		filterSQL, filterArgs, _, err := filter.CompileForDuckDB(p.Filter, opts)
		if err != nil {
			return "", nil, fmt.Errorf("compile filter: %w", err)
		}
		if filterSQL != "" {
			whereParts = append(whereParts, filterSQL)
			args = append(args, filterArgs...)
		}
	}

	whereSQL := ""
	if len(whereParts) > 0 {
		whereSQL = "WHERE " + strings.Join(whereParts, " AND ")
	}

	readExpr := ds.buildReadExpression(layer)
	limitSQL := ""
	if p.Limit > 0 {
		limitSQL = fmt.Sprintf("LIMIT %d", p.Limit)
	}
	sql := fmt.Sprintf(`SELECT ST_AsWKB(%s) AS geom, %s AS props, %s AS feature_id FROM %s t %s %s`, geomExpr, propExpr, idExpr, readExpr, whereSQL, limitSQL)
	return sql, args, nil
}

// Count returns the number of features matching the query.
func (ds *DataSource) Count(ctx context.Context, layer string, params datasource.QueryParams) (int, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return 0, err
	}

	query, args, err := ds.buildCountSQL(info, layer, params)
	if err != nil {
		return 0, err
	}

	var count int
	if err := ds.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}

	return count, nil
}

// GetLayerInfo returns metadata about a specific layer.
func (ds *DataSource) GetLayerInfo(ctx context.Context, layer string) (*datasource.LayerInfo, error) {
	// Check cache first
	ds.cacheMu.RLock()
	info, ok := ds.layerCache[layer]
	ds.cacheMu.RUnlock()
	if ok {
		return info, nil
	}

	// Get columns for the layer
	columns, err := ds.getColumns(ctx, layer)
	if err != nil {
		return nil, err
	}

	// Find geometry column
	geomCol := ds.findGeometryColumn(columns)
	if geomCol == "" {
		return nil, fmt.Errorf("no geometry column found in layer %s", layer)
	}

	// Find ID column
	idCol := ds.findIDColumn(columns)

	// Build properties list
	properties, pgTypes := ds.buildProperties(columns, geomCol)

	// Detect geometry type and SRID
	geomType := ds.detectGeometryType(ctx, layer, geomCol)
	srid := ds.detectSRID(ctx, layer, geomCol)
	if srid == 0 {
		srid = ds.cfg.SRID
	}

	info = &datasource.LayerInfo{
		Name:           layer,
		Title:          layer,
		GeometryColumn: geomCol,
		GeometryType:   geomType,
		SRID:           srid,
		IDColumn:       idCol,
		Properties:     properties,
		PGTypes:        pgTypes,
	}

	// Cache the result
	ds.cacheMu.Lock()
	if existing, ok := ds.layerCache[layer]; ok {
		ds.cacheMu.Unlock()
		return existing, nil
	}
	ds.layerCache[layer] = info
	ds.cacheMu.Unlock()

	return info, nil
}

func (ds *DataSource) GetLayerExtent(ctx context.Context, layer string) (*datasource.Extent, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}
	geom, readExpression := quoteIdent(info.GeometryColumn), ds.buildReadExpression(layer)
	queries := []string{
		fmt.Sprintf(`SELECT ST_AsText(ST_Extent_Agg(%s)) FROM %s WHERE %s IS NOT NULL`, geom, readExpression, geom),
		fmt.Sprintf(`SELECT ST_AsText(ST_Extent_Agg(ST_GeomFromWKB(%s))) FROM %s WHERE %s IS NOT NULL`, geom, readExpression, geom),
	}
	var lastErr error
	for _, query := range queries {
		var wkt string
		if err := ds.db.QueryRowContext(ctx, query).Scan(&wkt); err == nil {
			return datasource.ParseExtentWKT(wkt, info.SRID)
		} else {
			lastErr = err
		}
	}
	return nil, fmt.Errorf("layer extent: %w", lastErr)
}

// Health checks the data source connection.
func (ds *DataSource) Health(ctx context.Context) error {
	return ds.verifyFile(ctx)
}

// Close releases resources.
func (ds *DataSource) Close() error {
	return ds.db.Close()
}

// getColumns returns the columns for a layer
func (ds *DataSource) getColumns(ctx context.Context, layer string) ([]columnInfo, error) {
	readExpr := ds.buildReadExpression(layer)
	query := fmt.Sprintf(`DESCRIBE SELECT * FROM %s`, readExpr)
	rows, err := ds.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("describe layer: %w", err)
	}
	defer rows.Close()

	var columns []columnInfo
	for rows.Next() {
		var name, colType string
		var null, key, defaultVal, extra sql.NullString
		if err := rows.Scan(&name, &colType, &null, &key, &defaultVal, &extra); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}
		columns = append(columns, columnInfo{Name: name, Type: colType})
	}

	return columns, rows.Err()
}

type columnInfo struct {
	Name string
	Type string
}

// findGeometryColumn looks for a geometry column
func (ds *DataSource) findGeometryColumn(columns []columnInfo) string {
	// Check for configured geometry column
	if ds.cfg.GeometryColumn != "" {
		for _, col := range columns {
			if strings.EqualFold(col.Name, ds.cfg.GeometryColumn) {
				return col.Name
			}
		}
	}

	// Look for common geometry column names (wkb_geometry is default from ST_Read)
	geomNames := []string{"wkb_geometry", "geometry", "geom", "shape", "the_geom"}
	for _, gn := range geomNames {
		for _, col := range columns {
			if strings.EqualFold(col.Name, gn) {
				return col.Name
			}
		}
	}

	// Look for columns with geometry-like types
	for _, col := range columns {
		typeLower := strings.ToLower(col.Type)
		if strings.Contains(typeLower, "geometry") ||
			strings.Contains(typeLower, "blob") ||
			strings.Contains(typeLower, "wkb") {
			return col.Name
		}
	}

	return ""
}

// findIDColumn looks for an ID column
func (ds *DataSource) findIDColumn(columns []columnInfo) string {
	// Check for configured ID column
	if ds.cfg.IDColumn != "" {
		for _, col := range columns {
			if strings.EqualFold(col.Name, ds.cfg.IDColumn) {
				return col.Name
			}
		}
	}

	// Look for common ID column names
	idNames := []string{"id", "fid", "gid", "ogc_fid", "objectid", "feature_id"}
	for _, idName := range idNames {
		for _, col := range columns {
			if strings.EqualFold(col.Name, idName) {
				return col.Name
			}
		}
	}

	return ""
}

// detectGeometryType tries to determine the geometry type
func (ds *DataSource) detectGeometryType(ctx context.Context, layer, geomCol string) string {
	readExpr := ds.buildReadExpression(layer)
	quotedGeom := quoteIdent(geomCol)

	// Try native geometry first
	query := fmt.Sprintf(
		`SELECT ST_GeometryType(%s) FROM %s WHERE %s IS NOT NULL LIMIT 1`,
		quotedGeom, readExpr, quotedGeom,
	)

	var geomType string
	if err := ds.db.QueryRowContext(ctx, query).Scan(&geomType); err == nil {
		return geomType
	}

	// Fall back to WKB conversion
	query = fmt.Sprintf(
		`SELECT ST_GeometryType(ST_GeomFromWKB(%s)) FROM %s WHERE %s IS NOT NULL LIMIT 1`,
		quotedGeom, readExpr, quotedGeom,
	)

	if err := ds.db.QueryRowContext(ctx, query).Scan(&geomType); err != nil {
		return "GEOMETRY"
	}

	return geomType
}

// detectSRID tries to determine the SRID from the data
func (ds *DataSource) detectSRID(ctx context.Context, layer, geomCol string) int {
	readExpr := ds.buildReadExpression(layer)
	quotedGeom := quoteIdent(geomCol)

	// Try native geometry first
	query := fmt.Sprintf(
		`SELECT ST_SRID(%s) FROM %s WHERE %s IS NOT NULL LIMIT 1`,
		quotedGeom, readExpr, quotedGeom,
	)

	var srid int
	if err := ds.db.QueryRowContext(ctx, query).Scan(&srid); err == nil {
		return srid
	}

	// Fall back to WKB conversion
	query = fmt.Sprintf(
		`SELECT ST_SRID(ST_GeomFromWKB(%s)) FROM %s WHERE %s IS NOT NULL LIMIT 1`,
		quotedGeom, readExpr, quotedGeom,
	)

	if err := ds.db.QueryRowContext(ctx, query).Scan(&srid); err != nil {
		return 0
	}

	return srid
}

// buildProperties creates the properties list from columns
func (ds *DataSource) buildProperties(columns []columnInfo, geomCol string) ([]datasource.PropertyInfo, map[string]string) {
	var props []datasource.PropertyInfo
	pgTypes := make(map[string]string)

	for i, col := range columns {
		if strings.EqualFold(col.Name, geomCol) {
			// Store geometry column type for later use
			pgTypes[col.Name] = col.Type
			continue
		}

		pgTypes[col.Name] = col.Type

		props = append(props, datasource.PropertyInfo{
			Name:     col.Name,
			Type:     col.Type,
			JSONType: columnTypeToJSON(col.Type),
			Ordinal:  i,
		})
	}

	return props, pgTypes
}

// isNativeGeometry checks if the geometry column type is native GEOMETRY (not WKB/BLOB)
func isNativeGeometry(colType string) bool {
	typeLower := strings.ToLower(colType)
	// If type is GEOMETRY, it's native and doesn't need WKB conversion
	// BLOB and WKB_BLOB types need conversion
	return strings.HasPrefix(typeLower, "geometry") && !strings.Contains(typeLower, "blob")
}

func (ds *DataSource) buildListSQL(info *datasource.LayerInfo, layer string, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	p.SortBy = datasource.StableSort(p.SortBy, info.IDColumn)
	geom := quoteIdent(info.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = 4326
	}

	// Check if geometry is native GEOMETRY type or needs WKB conversion
	geomColType := info.PGTypes[info.GeometryColumn]
	isNative := isNativeGeometry(geomColType)

	// Build geometry expression - handle both native GEOMETRY and WKB/BLOB
	var geomExpr string
	if isNative {
		geomExpr = fmt.Sprintf("t.%s", geom)
		if info.SRID != outSRID && info.SRID != 0 {
			geomExpr = fmt.Sprintf("ST_Transform(t.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
		}
	} else {
		geomExpr = fmt.Sprintf("ST_GeomFromWKB(t.%s)", geom)
		if info.SRID != outSRID && info.SRID != 0 {
			geomExpr = fmt.Sprintf("ST_Transform(ST_GeomFromWKB(t.%s), 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
		}
	}

	// Build ID expression
	var idExpr string
	if info.IDColumn != "" {
		idExpr = fmt.Sprintf("t.%s", quoteIdent(info.IDColumn))
	} else {
		idExpr = "NULL"
	}

	// Build properties expression
	propCols := make([]string, 0, len(info.Properties))
	for _, prop := range info.Properties {
		if prop.Name != info.IDColumn && datasource.PropertySelected(prop.Name, p.Properties) {
			propCols = append(propCols, fmt.Sprintf("%s, t.%s", quoteLiteral(prop.Name), quoteIdent(prop.Name)))
		}
	}
	propExpr := "json_object(" + strings.Join(propCols, ", ") + ")"
	if len(propCols) == 0 {
		propExpr = "'{}'::JSON"
	}

	var whereParts []string
	var args []any
	argPos := 1

	// BBox filter
	if p.BBox != nil {
		// Use native geometry or WKB conversion based on column type
		var geomForFilter string
		if isNative {
			geomForFilter = fmt.Sprintf("t.%s", geom)
		} else {
			geomForFilter = fmt.Sprintf("ST_GeomFromWKB(t.%s)", geom)
		}
		predicate, bboxArgs, nextArg := buildBBoxPredicate(geomForFilter, info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: filterGeometry(info)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		allowed := make(map[string]struct{})
		for _, prop := range info.Properties {
			allowed[prop.Name] = struct{}{}
		}
		if info.IDColumn != "" {
			allowed[info.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}

		opts := filter.DuckDBOptions{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        info.SRID,
			AllowedProperties: allowed,
			GeometryProperty:  info.GeometryColumn,
		}

		filterSQL, filterArgs, nextIdx, err := filter.CompileForDuckDB(p.Filter, opts)
		if err != nil {
			return "", nil, fmt.Errorf("compile filter: %w", err)
		}
		if filterSQL != "" {
			whereParts = append(whereParts, filterSQL)
			args = append(args, filterArgs...)
			argPos = nextIdx
		}
	}

	// Build ORDER BY
	orderSQL := ""
	if len(p.SortBy) > 0 {
		var orderItems []string
		for _, s := range p.SortBy {
			if s.Name == "" {
				continue
			}
			if _, ok := info.PGTypes[s.Name]; !ok && s.Name != info.IDColumn {
				continue
			}
			dir := "ASC"
			if s.Desc {
				dir = "DESC"
			}
			orderItems = append(orderItems, fmt.Sprintf("t.%s %s", quoteIdent(s.Name), dir))
		}
		if len(orderItems) > 0 {
			orderSQL = "ORDER BY " + strings.Join(orderItems, ", ")
		}
	} else if info.IDColumn != "" {
		orderSQL = fmt.Sprintf("ORDER BY t.%s", quoteIdent(info.IDColumn))
	}

	// Build LIMIT/OFFSET
	limitSQL := fmt.Sprintf("LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, p.Limit, p.Offset)

	// Build feature JSON
	featureExpr := datasource.DuckDBFeatureJSONExpression(idExpr, geomExpr, propExpr)

	whereSQL := ""
	if len(whereParts) > 0 {
		whereSQL = "WHERE " + strings.Join(whereParts, " AND ")
	}

	readExpr := ds.buildReadExpression(layer)
	sql := fmt.Sprintf(`SELECT CAST(%s AS VARCHAR) AS feature FROM %s t %s %s %s`, featureExpr, readExpr, whereSQL, orderSQL, limitSQL)
	return sql, args, nil
}

func (ds *DataSource) buildFeatureByIDSQL(info *datasource.LayerInfo, layer string, outSRID int) string {
	geom := quoteIdent(info.GeometryColumn)
	idCol := quoteIdent(info.IDColumn)

	if outSRID == 0 {
		outSRID = 4326
	}

	// Check if geometry is native GEOMETRY type or needs WKB conversion
	geomColType := info.PGTypes[info.GeometryColumn]
	isNative := isNativeGeometry(geomColType)

	// Build geometry expression - handle both native GEOMETRY and WKB/BLOB
	var geomExpr string
	if isNative {
		geomExpr = fmt.Sprintf("t.%s", geom)
		if info.SRID != outSRID && info.SRID != 0 {
			geomExpr = fmt.Sprintf("ST_Transform(t.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
		}
	} else {
		geomExpr = fmt.Sprintf("ST_GeomFromWKB(t.%s)", geom)
		if info.SRID != outSRID && info.SRID != 0 {
			geomExpr = fmt.Sprintf("ST_Transform(ST_GeomFromWKB(t.%s), 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
		}
	}

	// Build properties expression
	propCols := make([]string, 0, len(info.Properties))
	for _, prop := range info.Properties {
		if prop.Name != info.IDColumn {
			propCols = append(propCols, fmt.Sprintf("%s, t.%s", quoteLiteral(prop.Name), quoteIdent(prop.Name)))
		}
	}
	propExpr := "json_object(" + strings.Join(propCols, ", ") + ")"
	if len(propCols) == 0 {
		propExpr = "'{}'::JSON"
	}

	featureExpr := datasource.DuckDBFeatureJSONExpression("t."+idCol, geomExpr, propExpr)

	readExpr := ds.buildReadExpression(layer)
	return fmt.Sprintf(`SELECT CAST(%s AS VARCHAR) AS feature FROM %s t WHERE t.%s = $1 LIMIT 1`, featureExpr, readExpr, idCol)
}

func (ds *DataSource) buildCountSQL(info *datasource.LayerInfo, layer string, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	geom := quoteIdent(info.GeometryColumn)

	// Check if geometry is native GEOMETRY type or needs WKB conversion
	geomColType := info.PGTypes[info.GeometryColumn]
	isNative := isNativeGeometry(geomColType)

	var whereParts []string
	var args []any
	argPos := 1

	// BBox filter
	if p.BBox != nil {
		// Use native geometry or WKB conversion based on column type
		var geomForFilter string
		if isNative {
			geomForFilter = fmt.Sprintf("t.%s", geom)
		} else {
			geomForFilter = fmt.Sprintf("ST_GeomFromWKB(t.%s)", geom)
		}
		predicate, bboxArgs, nextArg := buildBBoxPredicate(geomForFilter, info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: filterGeometry(info)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		allowed := make(map[string]struct{})
		for _, prop := range info.Properties {
			allowed[prop.Name] = struct{}{}
		}
		if info.IDColumn != "" {
			allowed[info.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}

		opts := filter.DuckDBOptions{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        info.SRID,
			AllowedProperties: allowed,
			GeometryProperty:  info.GeometryColumn,
		}

		filterSQL, filterArgs, _, err := filter.CompileForDuckDB(p.Filter, opts)
		if err != nil {
			return "", nil, fmt.Errorf("compile filter: %w", err)
		}
		if filterSQL != "" {
			whereParts = append(whereParts, filterSQL)
			args = append(args, filterArgs...)
		}
	}

	whereSQL := ""
	if len(whereParts) > 0 {
		whereSQL = "WHERE " + strings.Join(whereParts, " AND ")
	}

	readExpr := ds.buildReadExpression(layer)
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s t %s`, readExpr, whereSQL)
	return sql, args, nil
}

// Helper functions

// detectFormat detects the file format from the path extension
func detectFormat(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".shp":
		return "shapefile"
	case ".gpkg":
		return "geopackage"
	case ".geojson", ".json":
		return "geojson"
	case ".fgb":
		return "flatgeobuf"
	case ".kml":
		return "kml"
	case ".gml":
		return "gml"
	default:
		return "unknown"
	}
}

// isMultiLayerFormat returns true if the format supports multiple layers
func (ds *DataSource) isMultiLayerFormat() bool {
	return ds.format == "geopackage" || ds.format == "gml"
}

func parseFeatureID(colType, v string) (any, error) {
	colTypeLower := strings.ToLower(colType)
	if strings.Contains(colTypeLower, "int") || strings.Contains(colTypeLower, "bigint") {
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, datasource.InvalidFeatureIDError{Value: v}
		}
		return i, nil
	}
	return v, nil
}

func columnTypeToJSON(colType string) datasource.JSONType {
	colTypeLower := strings.ToLower(colType)
	switch {
	case strings.Contains(colTypeLower, "bool"):
		return datasource.JSONTypeBoolean
	case strings.Contains(colTypeLower, "int") || strings.Contains(colTypeLower, "bigint"):
		return datasource.JSONTypeInteger
	case strings.Contains(colTypeLower, "float") || strings.Contains(colTypeLower, "double") || strings.Contains(colTypeLower, "decimal") || strings.Contains(colTypeLower, "numeric"):
		return datasource.JSONTypeNumber
	case strings.Contains(colTypeLower, "struct") || strings.Contains(colTypeLower, "map"):
		return datasource.JSONTypeObject
	case strings.Contains(colTypeLower, "list") || strings.Contains(colTypeLower, "[]"):
		return datasource.JSONTypeArray
	default:
		return datasource.JSONTypeString
	}
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// quoteLiteral escapes a string for use as a SQL string literal.
func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func buildBBoxPredicate(geomExpr string, sourceSRID, bboxSRID, argPos int, bbox *datasource.BBox) (string, []any, int) {
	if bboxSRID == 0 {
		bboxSRID = 4326
	}
	var clauses []string
	var args []any
	for _, part := range bbox.Parts(bboxSRID) {
		envelope := fmt.Sprintf("ST_MakeEnvelope($%d, $%d, $%d, $%d)", argPos, argPos+1, argPos+2, argPos+3)
		if sourceSRID != 0 && sourceSRID != bboxSRID {
			envelope = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", envelope, bboxSRID, sourceSRID)
		}
		clauses = append(clauses, fmt.Sprintf("ST_Intersects(%s, %s)", geomExpr, envelope))
		args = append(args, part.MinX, part.MinY, part.MaxX, part.MaxY)
		argPos += 4
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args, argPos
}

// SQL View support - delegates to shared helper

// ValidateSQLView validates a SQL query for use as a SQL View.
func (ds *DataSource) ValidateSQLView(ctx context.Context, sql string) error {
	return errors.New("SQL views are not supported for vector-file data sources")
}

// DiscoverSQLViewColumns executes a SQL query with LIMIT 0 to discover column metadata.
func (ds *DataSource) DiscoverSQLViewColumns(ctx context.Context, sql string) (*datasource.SQLViewDiscovery, error) {
	return nil, errors.New("SQL views are not supported for vector-file data sources")
}

// ValidateSQLViewIdentity verifies that every published row has a unique, non-null ID.
func (ds *DataSource) ValidateSQLViewIdentity(ctx context.Context, config *datasource.SQLViewConfig) error {
	return ds.sqlViewHelper.ValidateSQLViewIdentity(ctx, config)
}

// QuerySQLView executes a feature query against a SQL View and returns GeoJSON features.
func (ds *DataSource) QuerySQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]json.RawMessage, error) {
	return ds.sqlViewHelper.QuerySQLView(ctx, config, params)
}

// QuerySQLViewWKB executes a SQL View query and returns WKB geometry with properties for rendering.
func (ds *DataSource) QuerySQLViewWKB(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	return ds.sqlViewHelper.QuerySQLViewWKB(ctx, config, params)
}

// CountSQLView returns the number of features matching the SQL View query.
func (ds *DataSource) CountSQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) (int, error) {
	return ds.sqlViewHelper.CountSQLView(ctx, config, params)
}

func filterGeometry(info *datasource.LayerInfo) string {
	column := "t." + quoteIdent(info.GeometryColumn)
	if !isNativeGeometry(info.PGTypes[info.GeometryColumn]) {
		return "ST_GeomFromWKB(" + column + ")"
	}
	return column
}
