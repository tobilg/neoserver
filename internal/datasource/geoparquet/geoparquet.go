// Package geoparquet provides a GeoParquet DataSource implementation using DuckDB.
package geoparquet

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
	datasource.Register(store.ServiceTypeGeoParquet, NewFromService)
}

// Config holds GeoParquet connection configuration.
type Config struct {
	Path           string `json:"path"`            // Path to parquet file or directory
	GeometryColumn string `json:"geometry_column"` // Override geometry column name
	IDColumn       string `json:"id_column"`       // Override ID column name
	SRID           int    `json:"srid"`            // Override SRID (default: 4326)
}

// DefaultConfig returns default configuration values.
func DefaultConfig() Config {
	return Config{
		GeometryColumn: "geometry",
		SRID:           4326,
	}
}

// DataSource implements datasource.DataSource for GeoParquet files.
type DataSource struct {
	id            string
	db            *sql.DB
	path          string
	tableName     string
	geomCol       string
	idCol         string
	srid          int
	layerCache    map[string]*datasource.LayerInfo
	cacheMu       sync.RWMutex
	sqlViewHelper *duckdbsqlview.Helper
}

// New creates a new GeoParquet DataSource.
func New(id string, cfg Config) (*DataSource, error) {
	// Create in-memory DuckDB for reading parquet
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

	// Derive table name from file path
	tableName := deriveTableName(cfg.Path)

	// Apply defaults
	defaults := DefaultConfig()
	geomCol := cfg.GeometryColumn
	if geomCol == "" {
		geomCol = defaults.GeometryColumn
	}
	srid := cfg.SRID
	if srid == 0 {
		srid = defaults.SRID
	}

	ds := &DataSource{
		id:         id,
		db:         db,
		path:       cfg.Path,
		tableName:  tableName,
		geomCol:    geomCol,
		idCol:      cfg.IDColumn,
		srid:       srid,
		layerCache: make(map[string]*datasource.LayerInfo),
	}
	ds.sqlViewHelper = duckdbsqlview.NewHelper(db, parquetTypeToJSON)

	// Verify the file can be read
	if err := ds.verifyFile(context.Background()); err != nil {
		db.Close()
		return nil, err
	}

	return ds, nil
}

// NewFromService creates a GeoParquet DataSource from a store.Service.
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
	return store.ServiceTypeGeoParquet
}

// ID returns the service ID.
func (ds *DataSource) ID() string {
	return ds.id
}

// verifyFile checks if the parquet file can be read
func (ds *DataSource) verifyFile(ctx context.Context) error {
	query := fmt.Sprintf(`SELECT COUNT(*) FROM read_parquet(%s) LIMIT 1`, quoteLiteral(ds.path))
	var count int
	return ds.db.QueryRowContext(ctx, query).Scan(&count)
}

// DiscoverLayers discovers available layers from GeoParquet.
// For a single parquet file, this returns one layer.
func (ds *DataSource) DiscoverLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	// Get columns from parquet file
	columns, err := ds.getColumns(ctx)
	if err != nil {
		return nil, err
	}

	// Find geometry column
	geomCol := ds.findGeometryColumn(columns)
	if geomCol == "" {
		return nil, fmt.Errorf("no geometry column found in %s", ds.path)
	}

	// Find ID column
	idCol := ds.findIDColumn(columns)

	// Determine geometry type
	geomType := ds.detectGeometryType(ctx, geomCol)

	return []*datasource.DiscoveredLayer{
		{
			Name:           ds.tableName,
			Title:          ds.tableName,
			GeometryColumn: geomCol,
			GeometryType:   geomType,
			SRID:           ds.srid,
			IDColumn:       idCol,
		},
	}, nil
}

// getColumns returns the columns in the parquet file
func (ds *DataSource) getColumns(ctx context.Context) ([]columnInfo, error) {
	query := fmt.Sprintf(`DESCRIBE SELECT * FROM read_parquet(%s)`, quoteLiteral(ds.path))
	rows, err := ds.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("describe parquet: %w", err)
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
	if ds.geomCol != "" {
		for _, col := range columns {
			if col.Name == ds.geomCol {
				return col.Name
			}
		}
	}

	// Look for common geometry column names
	geomNames := []string{"geometry", "geom", "wkb_geometry", "shape", "the_geom"}
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
	if ds.idCol != "" {
		for _, col := range columns {
			if col.Name == ds.idCol {
				return col.Name
			}
		}
	}

	// Look for common ID column names
	idNames := []string{"id", "fid", "gid", "ogc_fid", "objectid", "feature_id", "__index_level_0__"}
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
func (ds *DataSource) detectGeometryType(ctx context.Context, geomCol string) string {
	query := fmt.Sprintf(
		`SELECT ST_GeometryType(ST_GeomFromWKB(%s)) FROM read_parquet(%s) WHERE %s IS NOT NULL LIMIT 1`,
		quoteIdent(geomCol), quoteLiteral(ds.path), quoteIdent(geomCol),
	)

	var geomType string
	if err := ds.db.QueryRowContext(ctx, query).Scan(&geomType); err != nil {
		return "GEOMETRY"
	}

	return geomType
}

// Query executes a feature query.
func (ds *DataSource) Query(ctx context.Context, layer string, params datasource.QueryParams) ([]json.RawMessage, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}

	query, args, err := ds.buildListSQL(info, params)
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

	query := ds.buildFeatureByIDSQL(info, outputSRID)
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
func (ds *DataSource) QueryWKB(ctx context.Context, layer string, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}

	query, args, err := ds.buildWKBSQL(info, params)
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
			continue // Skip invalid rows
		}

		// Convert properties to map[string]interface{}
		props := make(map[string]interface{})
		if propsMap, ok := propsJSON.(map[string]interface{}); ok {
			props = propsMap
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
	query, args, err := ds.buildWKBSQL(info, params)
	if err != nil {
		return nil, err
	}
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return datasource.NewSQLRenderStream(rows), nil
}

// buildWKBSQL builds a SQL query that returns WKB geometry and properties JSON.
func (ds *DataSource) buildWKBSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	sourceGeom := parquetGeometry(info)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = info.SRID
	}

	geomExpr := sourceGeom
	if info.SRID != outSRID {
		geomExpr = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geomExpr, info.SRID, outSRID)
	}
	if p.SimplifyTolerance > 0 {
		geomExpr = fmt.Sprintf("ST_SimplifyPreserveTopology(%s, %.12g)", geomExpr, p.SimplifyTolerance)
	}

	// Build properties list - exclude geometry column
	var propCols []string
	for _, prop := range info.Properties {
		if prop.Name != info.GeometryColumn && datasource.PropertySelected(prop.Name, p.Properties) {
			propCols = append(propCols, fmt.Sprintf("%s, t.%s", quoteLiteral(prop.Name), quoteIdent(prop.Name)))
		}
	}
	propsExpr := "json_object(" + strings.Join(propCols, ", ") + ")"
	idExpr := "NULL"
	if info.IDColumn != "" {
		idExpr = fmt.Sprintf("CAST(t.%s AS VARCHAR)", quoteIdent(info.IDColumn))
	}

	// Build FROM clause for parquet file
	fromClause := fmt.Sprintf("read_parquet(%s) t", quoteLiteral(ds.path))

	var whereParts []string
	var args []any

	if p.BBox != nil {
		bboxSRID := p.BBoxSRID
		if bboxSRID == 0 {
			bboxSRID = 4326
		}
		var bboxClauses []string
		for _, part := range p.BBox.Parts(bboxSRID) {
			bboxWKT := fmt.Sprintf("POLYGON((%f %f, %f %f, %f %f, %f %f, %f %f))",
				part.MinX, part.MinY,
				part.MaxX, part.MinY,
				part.MaxX, part.MaxY,
				part.MinX, part.MaxY,
				part.MinX, part.MinY)
			if bboxSRID != info.SRID {
				bboxClauses = append(bboxClauses, fmt.Sprintf("ST_Intersects(%s, ST_Transform(ST_GeomFromText('%s'), 'EPSG:%d', 'EPSG:%d', always_xy := true))",
					sourceGeom, bboxWKT, bboxSRID, info.SRID))
			} else {
				bboxClauses = append(bboxClauses, fmt.Sprintf("ST_Intersects(%s, ST_GeomFromText('%s'))",
					sourceGeom, bboxWKT))
			}
		}
		whereParts = append(whereParts, "("+strings.Join(bboxClauses, " OR ")+")")
	}

	argPos := 1
	// Compile the supplied predicate using DuckDB's SQL dialect.
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: parquetGeometry(info)}, &whereParts, &args, &argPos); err != nil {
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
			GeometryExpression: parquetGeometry(info),
			StartParamIndex:    argPos,
			FilterSRID:         filterSRID,
			SourceSRID:         info.SRID,
			AllowedProperties:  allowed,
			GeometryProperty:   info.GeometryColumn,
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

	whereSQL := ""
	if len(whereParts) > 0 {
		whereSQL = "WHERE " + strings.Join(whereParts, " AND ")
	}

	limitSQL := ""
	if p.Limit > 0 {
		limitSQL = fmt.Sprintf("LIMIT %d", p.Limit)
	}
	sql := fmt.Sprintf(`SELECT ST_AsWKB(%s) AS geom, %s AS props, %s AS feature_id FROM %s %s %s`,
		geomExpr, propsExpr, idExpr, fromClause, whereSQL, limitSQL)
	return sql, args, nil
}

// Count returns the number of features matching the query.
func (ds *DataSource) Count(ctx context.Context, layer string, params datasource.QueryParams) (int, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return 0, err
	}

	query, args, err := ds.buildCountSQL(info, params)
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

	// Verify layer name matches our table
	if layer != ds.tableName {
		return nil, datasource.LayerNotFoundError{Layer: layer}
	}

	// Get columns
	columns, err := ds.getColumns(ctx)
	if err != nil {
		return nil, err
	}

	// Find geometry column
	geomCol := ds.findGeometryColumn(columns)
	if geomCol == "" {
		return nil, fmt.Errorf("no geometry column found")
	}

	// Find ID column
	idCol := ds.findIDColumn(columns)

	// Build properties list
	properties, pgTypes := ds.buildProperties(columns, geomCol)
	for _, column := range columns {
		if column.Name == geomCol {
			pgTypes[geomCol] = column.Type
		}
	}

	info = &datasource.LayerInfo{
		Name:           ds.tableName,
		Title:          ds.tableName,
		GeometryColumn: geomCol,
		GeometryType:   ds.detectGeometryType(ctx, geomCol),
		SRID:           ds.srid,
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
	geom := quoteIdent(info.GeometryColumn)
	queries := []string{
		fmt.Sprintf(`SELECT ST_AsText(ST_Extent_Agg(%s)) FROM read_parquet(%s) WHERE %s IS NOT NULL`, geom, quoteLiteral(ds.path), geom),
		fmt.Sprintf(`SELECT ST_AsText(ST_Extent_Agg(ST_GeomFromWKB(%s))) FROM read_parquet(%s) WHERE %s IS NOT NULL`, geom, quoteLiteral(ds.path), geom),
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

// buildProperties creates the properties list from columns
func (ds *DataSource) buildProperties(columns []columnInfo, geomCol string) ([]datasource.PropertyInfo, map[string]string) {
	var props []datasource.PropertyInfo
	pgTypes := make(map[string]string)

	for i, col := range columns {
		if col.Name == geomCol {
			continue
		}

		pgTypes[col.Name] = col.Type

		props = append(props, datasource.PropertyInfo{
			Name:     col.Name,
			Type:     col.Type,
			JSONType: parquetTypeToJSON(col.Type),
			Ordinal:  i,
		})
	}

	return props, pgTypes
}

// Health checks the data source connection.
func (ds *DataSource) Health(ctx context.Context) error {
	return ds.verifyFile(ctx)
}

// Close releases resources.
func (ds *DataSource) Close() error {
	return ds.db.Close()
}

func (ds *DataSource) buildListSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	p.SortBy = datasource.StableSort(p.SortBy, info.IDColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = 4326
	}

	// Build geometry expression - convert from WKB and transform if needed
	geomExpr := parquetGeometry(info)
	if info.SRID != outSRID && info.SRID != 0 {
		geomExpr = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geomExpr, info.SRID, outSRID)
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
		predicate, bboxArgs, nextArg := buildBBoxPredicate(parquetGeometry(info), info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// Compile the supplied predicate using DuckDB's SQL dialect.
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: parquetGeometry(info)}, &whereParts, &args, &argPos); err != nil {
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
			GeometryExpression: parquetGeometry(info),
			StartParamIndex:    argPos,
			FilterSRID:         filterSRID,
			SourceSRID:         info.SRID,
			AllowedProperties:  allowed,
			GeometryProperty:   info.GeometryColumn,
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

	parquetRead := fmt.Sprintf("read_parquet(%s)", quoteLiteral(ds.path))
	sql := fmt.Sprintf(`SELECT CAST(%s AS VARCHAR) AS feature FROM %s t %s %s %s`, featureExpr, parquetRead, whereSQL, orderSQL, limitSQL)
	return sql, args, nil
}

func (ds *DataSource) buildFeatureByIDSQL(info *datasource.LayerInfo, outSRID int) string {
	idCol := quoteIdent(info.IDColumn)

	if outSRID == 0 {
		outSRID = 4326
	}

	geomExpr := parquetGeometry(info)
	if info.SRID != outSRID && info.SRID != 0 {
		geomExpr = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geomExpr, info.SRID, outSRID)
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

	parquetRead := fmt.Sprintf("read_parquet(%s)", quoteLiteral(ds.path))
	return fmt.Sprintf(`SELECT CAST(%s AS VARCHAR) AS feature FROM %s t WHERE t.%s = $1 LIMIT 1`, featureExpr, parquetRead, idCol)
}

func (ds *DataSource) buildCountSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()

	var whereParts []string
	var args []any
	argPos := 1

	// BBox filter
	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate(parquetGeometry(info), info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// Compile the supplied predicate using DuckDB's SQL dialect.
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: parquetGeometry(info)}, &whereParts, &args, &argPos); err != nil {
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
			GeometryExpression: parquetGeometry(info),
			StartParamIndex:    argPos,
			FilterSRID:         filterSRID,
			SourceSRID:         info.SRID,
			AllowedProperties:  allowed,
			GeometryProperty:   info.GeometryColumn,
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

	parquetRead := fmt.Sprintf("read_parquet(%s)", quoteLiteral(ds.path))
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s t %s`, parquetRead, whereSQL)
	return sql, args, nil
}

// Helper functions

func parquetGeometry(info *datasource.LayerInfo) string {
	expression := "t." + quoteIdent(info.GeometryColumn)
	if strings.HasPrefix(strings.ToUpper(info.PGTypes[info.GeometryColumn]), "GEOMETRY") {
		return expression
	}
	return "ST_GeomFromWKB(" + expression + ")"
}

func deriveTableName(path string) string {
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

func parquetTypeToJSON(colType string) datasource.JSONType {
	colTypeLower := strings.ToLower(colType)
	switch {
	// Check complex types first (they may contain primitive type names as parameters)
	case strings.Contains(colTypeLower, "struct") || strings.Contains(colTypeLower, "map"):
		return datasource.JSONTypeObject
	case strings.Contains(colTypeLower, "list") || strings.Contains(colTypeLower, "[]"):
		return datasource.JSONTypeArray
	// Then check primitive types
	case strings.Contains(colTypeLower, "bool"):
		return datasource.JSONTypeBoolean
	case strings.Contains(colTypeLower, "int") || strings.Contains(colTypeLower, "bigint"):
		return datasource.JSONTypeInteger
	case strings.Contains(colTypeLower, "float") || strings.Contains(colTypeLower, "double") || strings.Contains(colTypeLower, "decimal"):
		return datasource.JSONTypeNumber
	default:
		return datasource.JSONTypeString
	}
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// quoteLiteral escapes a string for use as a SQL string literal.
// It escapes single quotes by doubling them.
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
	return errors.New("SQL views are not supported for GeoParquet data sources")
}

// DiscoverSQLViewColumns executes a SQL query with LIMIT 0 to discover column metadata.
func (ds *DataSource) DiscoverSQLViewColumns(ctx context.Context, sql string) (*datasource.SQLViewDiscovery, error) {
	return nil, errors.New("SQL views are not supported for GeoParquet data sources")
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
