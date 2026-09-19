// Package duckdb provides a DuckDB spatial DataSource implementation.
package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	datasource.Register(store.ServiceTypeDuckDB, NewFromService)
}

// Config holds DuckDB connection configuration.
type Config struct {
	Path            string         `json:"path,omitempty"`
	ManagedImportID string         `json:"managed_import_id,omitempty"`
	ReadOnly        bool           `json:"read_only,omitempty"`
	Extensions      []string       `json:"extensions,omitempty"`
	EncryptionKey   string         `json:"-"`
	SRID            int            `json:"srid,omitempty"`
	LayerSRIDs      map[string]int `json:"layer_srids,omitempty"`
}

type ManagedResolver func(ctx context.Context, workspaceID, serviceID, importID string) (path, encryptionKey string, layerSRIDs map[string]int, err error)

var managedResolver struct {
	sync.RWMutex
	resolve ManagedResolver
}

// ConfigureManagedResolver installs the process-local secret resolver used by
// server-managed imports. Keys never enter service connection_info or logs.
func ConfigureManagedResolver(resolve ManagedResolver) {
	managedResolver.Lock()
	managedResolver.resolve = resolve
	managedResolver.Unlock()
}

// DefaultConfig returns default configuration values.
func DefaultConfig() Config {
	return Config{
		ReadOnly:   true,
		Extensions: []string{"spatial"},
	}
}

// DataSource implements datasource.DataSource for DuckDB with spatial extension.
type DataSource struct {
	id            string
	db            *sql.DB
	layerCache    map[string]*datasource.LayerInfo
	cacheMu       sync.RWMutex
	sqlViewHelper *duckdbsqlview.Helper
	srid          int
	layerSRIDs    map[string]int
}

// New creates a new DuckDB DataSource.
func New(id string, cfg Config) (*DataSource, error) {
	dsn := cfg.Path
	if cfg.EncryptionKey == "" && cfg.ReadOnly {
		dsn += "?access_mode=READ_ONLY"
	}
	if cfg.EncryptionKey != "" {
		dsn = ""
	}
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	maxConns := runtime.GOMAXPROCS(0)
	if cfg.EncryptionKey != "" {
		// ATTACH/USE state is connection-local.
		maxConns = 1
	}
	if maxConns > 4 {
		maxConns = 4
	}
	if maxConns < 1 {
		maxConns = 1
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)

	if cfg.EncryptionKey != "" {
		path := strings.ReplaceAll(cfg.Path, "'", "''")
		key := strings.ReplaceAll(cfg.EncryptionKey, "'", "''")
		readOnly := ""
		if cfg.ReadOnly {
			readOnly = ", READ_ONLY"
		}
		if _, err = db.Exec(fmt.Sprintf("ATTACH '%s' AS managed (ENCRYPTION_KEY '%s'%s); USE managed", path, key, readOnly)); err != nil {
			db.Close()
			return nil, fmt.Errorf("attach managed database: %w", err)
		}
	}

	// Load spatial extension
	for _, ext := range cfg.Extensions {
		if ext != "spatial" {
			db.Close()
			return nil, fmt.Errorf("extension %q is not allowed", ext)
		}
		_, err = db.Exec("LOAD spatial")
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("load extension %s: %w", ext, err)
		}
	}
	if _, err = db.Exec("SET enable_external_access = false"); err != nil {
		db.Close()
		return nil, fmt.Errorf("disable DuckDB external access: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	ds := &DataSource{
		id:         id,
		db:         db,
		layerCache: make(map[string]*datasource.LayerInfo),
		srid:       cfg.SRID,
		layerSRIDs: cfg.LayerSRIDs,
	}
	ds.sqlViewHelper = duckdbsqlview.NewHelper(db, duckdbTypeToJSON)
	return ds, nil
}

// NewFromService creates a DuckDB DataSource from a store.Service.
func NewFromService(svc *store.Service) (datasource.DataSource, error) {
	var cfg Config
	if err := json.Unmarshal(svc.ConnectionInfo, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if len(cfg.Extensions) == 0 {
		cfg.Extensions = defaults.Extensions
	}

	if cfg.ManagedImportID != "" {
		managedResolver.RLock()
		resolve := managedResolver.resolve
		managedResolver.RUnlock()
		if resolve == nil {
			return nil, errors.New("managed DuckDB resolver is unavailable")
		}
		if svc.WorkspaceID == "" || svc.ID == "" {
			return nil, errors.New("managed datasource ownership is required")
		}
		path, key, srids, err := resolve(context.Background(), svc.WorkspaceID, svc.ID, cfg.ManagedImportID)
		if err != nil {
			return nil, fmt.Errorf("resolve managed DuckDB: %w", err)
		}
		cfg.Path, cfg.EncryptionKey, cfg.ReadOnly = path, key, true
		cfg.SRID, cfg.LayerSRIDs = 0, srids
	}
	if cfg.Path == "" {
		return nil, errors.New("path is required")
	}

	// Enforce the datasource allowlist for operator-supplied database files.
	if cfg.Path != "" && cfg.Path != ":memory:" {
		if cfg.ManagedImportID == "" {
			resolved, err := pathpolicy.Resolve(context.Background(), cfg.Path)
			if err != nil {
				return nil, err
			}
			cfg.Path = resolved
		}
	}

	return New(svc.ID, cfg)
}

// Type returns the data source type.
func (ds *DataSource) Type() store.ServiceType {
	return store.ServiceTypeDuckDB
}

// ID returns the service ID.
func (ds *DataSource) ID() string {
	return ds.id
}

// DiscoverLayers discovers available layers from DuckDB.
func (ds *DataSource) DiscoverLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	// Query for tables with geometry columns using DuckDB's spatial extension
	query := `
		SELECT
			table_name,
			column_name,
			COALESCE(geometry_type, 'GEOMETRY') as geometry_type,
			COALESCE(srid, 0) as srid
		FROM st_geometry_columns()
	`

	rows, err := ds.db.QueryContext(ctx, query)
	if err != nil {
		// If st_geometry_columns() fails, try alternative discovery
		return ds.discoverLayersAlternative(ctx)
	}
	defer rows.Close()

	// Collect geometry tables
	type geomTable struct {
		tableName string
		geomCol   string
		geomType  string
		srid      int
	}
	var geomTables []geomTable
	seen := make(map[string]bool)

	for rows.Next() {
		var tableName, geomCol, geomType string
		var srid int

		if err := rows.Scan(&tableName, &geomCol, &geomType, &srid); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		// Skip duplicates
		if seen[tableName] {
			continue
		}
		seen[tableName] = true

		geomTables = append(geomTables, geomTable{tableName, geomCol, geomType, srid})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(geomTables) == 0 {
		return nil, nil
	}

	// Extract table names for batch query
	tableNames := make([]string, len(geomTables))
	for i, gt := range geomTables {
		tableNames[i] = gt.tableName
	}

	// Batch query for ID columns for all tables at once
	idCols := ds.findIDColumnsForTables(ctx, tableNames)

	// Build result
	var layers []*datasource.DiscoveredLayer
	for _, gt := range geomTables {
		gt.srid = ds.getSRID(ctx, gt.tableName, gt.geomCol)
		layers = append(layers, &datasource.DiscoveredLayer{
			Name:           gt.tableName,
			Title:          gt.tableName,
			GeometryColumn: gt.geomCol,
			GeometryType:   gt.geomType,
			SRID:           gt.srid,
			IDColumn:       idCols[gt.tableName],
		})
	}

	return layers, nil
}

// discoverLayersAlternative tries to discover layers by scanning table columns
// Uses batched queries to avoid N+1 query pattern
func (ds *DataSource) discoverLayersAlternative(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	// Get all columns for all tables in a single query
	query := `
		SELECT t.table_name, c.column_name, c.data_type
		FROM information_schema.tables t
		JOIN information_schema.columns c ON t.table_name = c.table_name AND t.table_schema = c.table_schema
		WHERE t.table_schema = 'main' AND t.table_type = 'BASE TABLE'
		ORDER BY t.table_name, c.ordinal_position
	`

	rows, err := ds.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()

	// Group columns by table
	type columnInfo struct {
		name     string
		dataType string
	}
	tableColumns := make(map[string][]columnInfo)

	for rows.Next() {
		var tableName, colName, colType string
		if err := rows.Scan(&tableName, &colName, &colType); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}
		tableColumns[tableName] = append(tableColumns[tableName], columnInfo{name: colName, dataType: colType})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Process tables to find geometry and ID columns
	var layers []*datasource.DiscoveredLayer

	for tableName, columns := range tableColumns {
		// Find geometry column
		geomCol := ""
		for _, col := range columns {
			colTypeLower := strings.ToLower(col.dataType)
			if strings.Contains(colTypeLower, "geometry") ||
				strings.Contains(colTypeLower, "point") ||
				strings.Contains(colTypeLower, "linestring") ||
				strings.Contains(colTypeLower, "polygon") ||
				strings.Contains(colTypeLower, "multipoint") ||
				strings.Contains(colTypeLower, "multilinestring") ||
				strings.Contains(colTypeLower, "multipolygon") ||
				col.name == "geom" || col.name == "geometry" || col.name == "wkb_geometry" {
				geomCol = col.name
				break
			}
		}

		if geomCol == "" {
			continue // No geometry column, skip this table
		}

		// Find ID column (in memory, no query needed)
		idCol := ""
		idPriority := map[string]int{
			"id": 1, "fid": 2, "gid": 3, "ogc_fid": 4, "objectid": 5, "feature_id": 6,
		}
		bestPriority := 999
		for _, col := range columns {
			colLower := strings.ToLower(col.name)
			if priority, ok := idPriority[colLower]; ok && priority < bestPriority {
				idCol = col.name
				bestPriority = priority
			}
		}

		// Prefer the declared single-column primary key over naming heuristics.
		if primary := ds.primaryKey(ctx, tableName); primary != "" {
			idCol = primary
		}
		// Native CRS is metadata, never inferred from coordinate values.
		srid := ds.getSRID(ctx, tableName, geomCol)

		layers = append(layers, &datasource.DiscoveredLayer{
			Name:           tableName,
			Title:          tableName,
			GeometryColumn: geomCol,
			GeometryType:   "GEOMETRY",
			SRID:           srid,
			IDColumn:       idCol,
		})
	}

	return layers, nil
}

// findGeometryColumn looks for a geometry column in a table
func (ds *DataSource) findGeometryColumn(ctx context.Context, tableName string) (string, string, int) {
	query := fmt.Sprintf(`
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_name = %s AND table_schema = 'main'
	`, quoteLiteral(tableName))

	rows, err := ds.db.QueryContext(ctx, query)
	if err != nil {
		return "", "", 0
	}
	var geometryColumn string
	for rows.Next() {
		var colName, colType string
		if err := rows.Scan(&colName, &colType); err != nil {
			continue
		}

		// Check if column type indicates geometry
		colTypeLower := strings.ToLower(colType)
		if strings.Contains(colTypeLower, "geometry") ||
			strings.Contains(colTypeLower, "point") ||
			strings.Contains(colTypeLower, "linestring") ||
			strings.Contains(colTypeLower, "polygon") ||
			strings.Contains(colTypeLower, "multipoint") ||
			strings.Contains(colTypeLower, "multilinestring") ||
			strings.Contains(colTypeLower, "multipolygon") ||
			colName == "geom" || colName == "geometry" || colName == "wkb_geometry" {
			geometryColumn = colName
			break
		}
	}
	// Encrypted managed databases use one connection because ATTACH/USE state is
	// connection-local. Release the metadata rows before issuing the SRID query
	// or database/sql will wait forever for a second connection.
	_ = rows.Close()
	if geometryColumn == "" {
		return "", "", 0
	}
	return geometryColumn, "GEOMETRY", ds.getSRID(ctx, tableName, geometryColumn)
}

// getSRID tries to determine the SRID of a geometry column
func (ds *DataSource) getSRID(ctx context.Context, tableName, geomCol string) int {
	var persisted int
	if err := ds.db.QueryRowContext(ctx, `SELECT srid FROM __neoserver.layers WHERE name = ?`, tableName).Scan(&persisted); err == nil && persisted > 0 {
		return persisted
	}
	if srid := ds.layerSRIDs[tableName]; srid > 0 {
		return srid
	}
	if ds.srid > 0 {
		return ds.srid
	}
	// ST_CRS reads the logical geometry type, even for an empty table.
	query := fmt.Sprintf(`SELECT ST_CRS((SELECT %s FROM %s LIMIT 1))`, quoteIdent(geomCol), quoteIdent(tableName))
	var crs sql.NullString
	if err := ds.db.QueryRowContext(ctx, query).Scan(&crs); err != nil {
		return 0
	}
	if strings.EqualFold(crs.String, "OGC:CRS84") {
		return 4326
	}
	if strings.HasPrefix(strings.ToUpper(crs.String), "EPSG:") {
		srid, _ := strconv.Atoi(crs.String[5:])
		return srid
	}
	return 0
}

func (ds *DataSource) primaryKey(ctx context.Context, tableName string) string {
	var column string
	err := ds.db.QueryRowContext(ctx, `SELECT constraint_column_names[1] FROM duckdb_constraints() WHERE database_name = current_database() AND schema_name = 'main' AND table_name = ? AND constraint_type = 'PRIMARY KEY' AND len(constraint_column_names) = 1`, tableName).Scan(&column)
	if err != nil {
		return ""
	}
	return column
}

// findIDColumn looks for a primary key or ID column
func (ds *DataSource) findIDColumn(ctx context.Context, tableName string) string {
	if column := ds.primaryKey(ctx, tableName); column != "" {
		return column
	}
	// Look for common ID column names
	query := fmt.Sprintf(`
		SELECT column_name
		FROM information_schema.columns
		WHERE table_name = %s AND table_schema = 'main'
		AND lower(column_name) IN ('id', 'fid', 'gid', 'ogc_fid', 'objectid', 'feature_id')
		ORDER BY CASE lower(column_name)
			WHEN 'id' THEN 1
			WHEN 'fid' THEN 2
			WHEN 'gid' THEN 3
			WHEN 'ogc_fid' THEN 4
			WHEN 'objectid' THEN 5
			WHEN 'feature_id' THEN 6
		END
		LIMIT 1
	`, quoteLiteral(tableName))

	var idCol string
	if err := ds.db.QueryRowContext(ctx, query).Scan(&idCol); err != nil {
		return ""
	}
	return idCol
}

// findIDColumnsForTables finds ID columns for multiple tables in a single query
func (ds *DataSource) findIDColumnsForTables(ctx context.Context, tableNames []string) map[string]string {
	result := make(map[string]string)

	if len(tableNames) == 0 {
		return result
	}

	// Build a single query to get all ID columns for all tables
	query := `
		SELECT table_name, column_name,
			CASE lower(column_name)
				WHEN 'id' THEN 1
				WHEN 'fid' THEN 2
				WHEN 'gid' THEN 3
				WHEN 'ogc_fid' THEN 4
				WHEN 'objectid' THEN 5
				WHEN 'feature_id' THEN 6
			END as priority
		FROM information_schema.columns
		WHERE table_schema = 'main'
		AND lower(column_name) IN ('id', 'fid', 'gid', 'ogc_fid', 'objectid', 'feature_id')
		ORDER BY table_name, priority
	`

	rows, err := ds.db.QueryContext(ctx, query)
	if err != nil {
		return result
	}
	defer rows.Close()

	// Track best ID column per table (first one wins due to ORDER BY priority)
	for rows.Next() {
		var tableName, colName string
		var priority int
		if err := rows.Scan(&tableName, &colName, &priority); err != nil {
			continue
		}
		// Only set if not already set (first one has highest priority)
		if _, exists := result[tableName]; !exists {
			result[tableName] = colName
		}
	}
	_ = rows.Close()
	for _, table := range tableNames {
		if column := ds.primaryKey(ctx, table); column != "" {
			result[table] = column
		}
	}

	return result
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
	table := quoteIdent(info.Name)
	geom := quoteIdent(info.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = info.SRID
	}

	geomExpr := "t." + geom
	if info.SRID != outSRID {
		geomExpr = fmt.Sprintf("ST_Transform(t.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
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

	var whereParts []string
	var args []any
	argPos := 1

	if p.BBox != nil {
		bboxSRID := p.BBoxSRID
		if bboxSRID == 0 {
			bboxSRID = 4326
		}
		var bboxClauses []string
		for _, part := range p.BBox.Parts(bboxSRID) {
			// DuckDB spatial uses ST_Intersects with ST_GeomFromText.
			bboxWKT := fmt.Sprintf("POLYGON((%f %f, %f %f, %f %f, %f %f, %f %f))",
				part.MinX, part.MinY,
				part.MaxX, part.MinY,
				part.MaxX, part.MaxY,
				part.MinX, part.MaxY,
				part.MinX, part.MinY)
			if bboxSRID != info.SRID {
				bboxClauses = append(bboxClauses, fmt.Sprintf("ST_Intersects(t.%s, ST_Transform(ST_GeomFromText('%s'), 'EPSG:%d', 'EPSG:%d', always_xy := true))",
					geom, bboxWKT, bboxSRID, info.SRID))
			} else {
				bboxClauses = append(bboxClauses, fmt.Sprintf("ST_Intersects(t.%s, ST_GeomFromText('%s'))",
					geom, bboxWKT))
			}
		}
		whereParts = append(whereParts, "("+strings.Join(bboxClauses, " OR ")+")")
	}

	// Use pre-compiled filter if provided (e.g., from FES XML)
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: "t." + quoteIdent(info.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && strings.TrimSpace(p.Filter) != "" {
		// Compile CQL2 filter
		allowed := make(map[string]struct{})
		for _, pr := range info.Properties {
			allowed[pr.Name] = struct{}{}
		}
		if info.IDColumn != "" {
			allowed[info.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}

		filterSQL, fargs, _, err := filter.CompileForDuckDB(p.Filter, filter.DuckDBOptions{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        info.SRID,
			AllowedProperties: allowed,
			GeometryProperty:  info.GeometryColumn,
		})
		if err != nil {
			return "", nil, err
		}
		if filterSQL != "" {
			whereParts = append(whereParts, filterSQL)
			args = append(args, fargs...)
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
	sql := fmt.Sprintf(`SELECT ST_AsWKB(%s) AS geom, %s AS props, %s AS feature_id FROM %s t %s %s`,
		geomExpr, propsExpr, idExpr, table, whereSQL, limitSQL)
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
	// Check cache first (read lock)
	ds.cacheMu.RLock()
	if info, ok := ds.layerCache[layer]; ok {
		ds.cacheMu.RUnlock()
		return info, nil
	}
	ds.cacheMu.RUnlock()

	// Query layer info
	info, err := ds.queryLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}

	// Cache the result (write lock with double-check)
	ds.cacheMu.Lock()
	if existing, ok := ds.layerCache[layer]; ok {
		// Another goroutine cached it while we were querying
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
	query := fmt.Sprintf(`SELECT ST_AsText(ST_Extent_Agg(%s)) FROM %s WHERE %s IS NOT NULL`, quoteIdent(info.GeometryColumn), quoteIdent(info.Name), quoteIdent(info.GeometryColumn))
	var wkt string
	if err := ds.db.QueryRowContext(ctx, query).Scan(&wkt); err != nil {
		return nil, fmt.Errorf("layer extent: %w", err)
	}
	return datasource.ParseExtentWKT(wkt, info.SRID)
}

// Health checks the data source connection.
func (ds *DataSource) Health(ctx context.Context) error {
	return ds.db.PingContext(ctx)
}

// Close releases resources.
func (ds *DataSource) Close() error {
	return ds.db.Close()
}

// queryLayerInfo queries the database for layer metadata.
func (ds *DataSource) queryLayerInfo(ctx context.Context, tableName string) (*datasource.LayerInfo, error) {
	// Check if table exists
	var exists int
	checkQuery := fmt.Sprintf(`SELECT 1 FROM information_schema.tables WHERE table_name = %s AND table_schema = 'main'`, quoteLiteral(tableName))
	if err := ds.db.QueryRowContext(ctx, checkQuery).Scan(&exists); err != nil {
		return nil, datasource.LayerNotFoundError{Layer: tableName}
	}

	// Get geometry column info
	geomCol, geomType, srid := ds.findGeometryColumn(ctx, tableName)
	if geomCol == "" {
		return nil, fmt.Errorf("table %s has no geometry column", tableName)
	}
	if srid <= 0 {
		return nil, fmt.Errorf("native CRS of table %s is unknown; set connection_info.srid or connection_info.layer_srids", tableName)
	}

	// Get ID column
	idCol := ds.findIDColumn(ctx, tableName)

	// Get properties
	properties, pgTypes := ds.getProperties(ctx, tableName, geomCol)

	return &datasource.LayerInfo{
		Name:           tableName,
		Title:          tableName,
		GeometryColumn: geomCol,
		GeometryType:   geomType,
		SRID:           srid,
		IDColumn:       idCol,
		Properties:     properties,
		PGTypes:        pgTypes,
	}, nil
}

// getProperties returns the non-geometry columns of a table
func (ds *DataSource) getProperties(ctx context.Context, tableName, geomCol string) ([]datasource.PropertyInfo, map[string]string) {
	query := fmt.Sprintf(`
		SELECT column_name, data_type, ordinal_position
		FROM information_schema.columns
		WHERE table_name = %s AND table_schema = 'main'
		AND column_name != %s
		ORDER BY ordinal_position
	`, quoteLiteral(tableName), quoteLiteral(geomCol))

	rows, err := ds.db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var props []datasource.PropertyInfo
	pgTypes := make(map[string]string)

	for rows.Next() {
		var colName, colType string
		var ordinal int
		if err := rows.Scan(&colName, &colType, &ordinal); err != nil {
			continue
		}

		pgTypes[colName] = colType

		props = append(props, datasource.PropertyInfo{
			Name:     colName,
			Type:     colType,
			JSONType: duckdbTypeToJSON(colType),
			Ordinal:  ordinal,
		})
	}

	return props, pgTypes
}

func (ds *DataSource) buildListSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	p.SortBy = datasource.StableSort(p.SortBy, info.IDColumn)
	table := quoteIdent(info.Name)
	geom := quoteIdent(info.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = 4326
	}

	// Build geometry expression with transform if needed
	geomExpr := fmt.Sprintf("t.%s", geom)
	if info.SRID != outSRID && info.SRID != 0 {
		geomExpr = fmt.Sprintf("ST_Transform(t.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
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
		bboxSRID := p.BBoxSRID
		if bboxSRID == 0 {
			bboxSRID = 4326
		}

		var bboxClauses []string
		for _, part := range p.BBox.Parts(bboxSRID) {
			var bboxGeom string
			if info.SRID == bboxSRID || info.SRID == 0 {
				bboxGeom = fmt.Sprintf("ST_MakeEnvelope($%d, $%d, $%d, $%d)", argPos, argPos+1, argPos+2, argPos+3)
			} else {
				bboxGeom = fmt.Sprintf("ST_Transform(ST_MakeEnvelope($%d, $%d, $%d, $%d), 'EPSG:%d', 'EPSG:%d', always_xy := true)",
					argPos, argPos+1, argPos+2, argPos+3, bboxSRID, info.SRID)
			}
			bboxClauses = append(bboxClauses, fmt.Sprintf("ST_Intersects(t.%s, %s)", geom, bboxGeom))
			args = append(args, part.MinX, part.MinY, part.MaxX, part.MaxY)
			argPos += 4
		}
		whereParts = append(whereParts, "("+strings.Join(bboxClauses, " OR ")+")")
	}

	// Use pre-compiled filter if provided (e.g., from FES XML)
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: "t." + quoteIdent(info.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		// Compile CQL2 filter
		// Build allowed properties set
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

	sql := fmt.Sprintf(`SELECT CAST(%s AS VARCHAR) AS feature FROM %s t %s %s %s`, featureExpr, table, whereSQL, orderSQL, limitSQL)
	return sql, args, nil
}

func (ds *DataSource) buildFeatureByIDSQL(info *datasource.LayerInfo, outSRID int) string {
	table := quoteIdent(info.Name)
	geom := quoteIdent(info.GeometryColumn)
	idCol := quoteIdent(info.IDColumn)

	if outSRID == 0 {
		outSRID = 4326
	}

	geomExpr := fmt.Sprintf("t.%s", geom)
	if info.SRID != outSRID && info.SRID != 0 {
		geomExpr = fmt.Sprintf("ST_Transform(t.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, info.SRID, outSRID)
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

	return fmt.Sprintf(`SELECT CAST(%s AS VARCHAR) AS feature FROM %s t WHERE t.%s = $1 LIMIT 1`, featureExpr, table, idCol)
}

func (ds *DataSource) buildCountSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	table := quoteIdent(info.Name)
	geom := quoteIdent(info.GeometryColumn)

	var whereParts []string
	var args []any
	argPos := 1

	// BBox filter
	if p.BBox != nil {
		bboxSRID := p.BBoxSRID
		if bboxSRID == 0 {
			bboxSRID = 4326
		}

		var bboxClauses []string
		for _, part := range p.BBox.Parts(bboxSRID) {
			var bboxGeom string
			if info.SRID == bboxSRID || info.SRID == 0 {
				bboxGeom = fmt.Sprintf("ST_MakeEnvelope($%d, $%d, $%d, $%d)", argPos, argPos+1, argPos+2, argPos+3)
			} else {
				bboxGeom = fmt.Sprintf("ST_Transform(ST_MakeEnvelope($%d, $%d, $%d, $%d), 'EPSG:%d', 'EPSG:%d', always_xy := true)",
					argPos, argPos+1, argPos+2, argPos+3, bboxSRID, info.SRID)
			}
			bboxClauses = append(bboxClauses, fmt.Sprintf("ST_Intersects(t.%s, %s)", geom, bboxGeom))
			args = append(args, part.MinX, part.MinY, part.MaxX, part.MaxY)
			argPos += 4
		}
		whereParts = append(whereParts, "("+strings.Join(bboxClauses, " OR ")+")")
	}

	// Use pre-compiled filter if provided (e.g., from FES XML)
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "t", GeometryExpression: "t." + quoteIdent(info.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		// Compile CQL2 filter
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

	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s t %s`, table, whereSQL)
	return sql, args, nil
}

// Helper functions

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

func duckdbTypeToJSON(colType string) datasource.JSONType {
	colTypeLower := strings.ToLower(colType)
	switch {
	case strings.Contains(colTypeLower, "bool"):
		return datasource.JSONTypeBoolean
	case strings.Contains(colTypeLower, "int") || strings.Contains(colTypeLower, "bigint"):
		return datasource.JSONTypeInteger
	case strings.Contains(colTypeLower, "float") || strings.Contains(colTypeLower, "double") || strings.Contains(colTypeLower, "decimal") || strings.Contains(colTypeLower, "numeric"):
		return datasource.JSONTypeNumber
	case strings.Contains(colTypeLower, "json"):
		return datasource.JSONTypeObject
	case strings.Contains(colTypeLower, "[]") || strings.Contains(colTypeLower, "list"):
		return datasource.JSONTypeArray
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

// SQL View support - delegates to shared helper

// ValidateSQLView validates a SQL query for use as a SQL View.
func (ds *DataSource) ValidateSQLView(ctx context.Context, sql string) error {
	return ds.sqlViewHelper.ValidateSQLView(ctx, sql)
}

// DiscoverSQLViewColumns executes a SQL query with LIMIT 0 to discover column metadata.
func (ds *DataSource) DiscoverSQLViewColumns(ctx context.Context, sql string) (*datasource.SQLViewDiscovery, error) {
	return ds.sqlViewHelper.DiscoverSQLViewColumns(ctx, sql)
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
