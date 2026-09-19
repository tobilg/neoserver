// Package duckdbsqlview provides shared SQL View support for DuckDB-based datasources.
package duckdbsqlview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/filter"
)

// ForbiddenSQLKeywords contains statement keywords that are not allowed in SQL Views.
// Matched on word boundaries so that identifiers like "created_at" or "updated_at"
// are not false-positives.
var ForbiddenSQLKeywords = []string{
	"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER", "TRUNCATE",
	"GRANT", "REVOKE", "COPY", "VACUUM", "ATTACH", "DETACH", "LOAD", "INSTALL",
	"PRAGMA", "EXPORT", "IMPORT",
}

// ForbiddenSQLFunctions are DuckDB functions that grant filesystem or network access.
// They bypass the datasource path allowlist (e.g. read_csv('/etc/passwd'),
// read_parquet('http://169.254.169.254/...')), so a SQL view referencing them is
// rejected. Filesystem/network access remains available only through configured,
// allowlisted datasources — not through arbitrary user SQL.
var ForbiddenSQLFunctions = []string{
	"read_csv", "read_csv_auto", "read_parquet", "parquet_scan",
	"read_json", "read_json_auto", "read_json_objects", "read_ndjson", "read_ndjson_auto",
	"read_text", "read_blob", "read_xlsx", "glob", "csv_scan", "sniff_csv",
	"parquet_metadata", "parquet_schema", "st_read", "st_readosm", "st_read_meta",
}

var (
	forbiddenKeywordRes  = buildWordRegexes(ForbiddenSQLKeywords)
	forbiddenFunctionRes = buildFuncRegexes(ForbiddenSQLFunctions)
	forbiddenSchemeRe    = regexp.MustCompile(`(?i)\b(?:https?|s3|gs|gcs|azure|az|r2|file)://`)
)

func buildWordRegexes(words []string) []*regexp.Regexp {
	res := make([]*regexp.Regexp, len(words))
	for i, w := range words {
		res[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`)
	}
	return res
}

func buildFuncRegexes(fns []string) []*regexp.Regexp {
	res := make([]*regexp.Regexp, len(fns))
	for i, f := range fns {
		// Function name followed by optional whitespace and an opening parenthesis.
		res[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(f) + `\s*\(`)
	}
	return res
}

// TypeMapper is a function that converts a database column type to a JSON type.
type TypeMapper func(colType string) datasource.JSONType

// Helper provides SQL View support for DuckDB-based datasources.
type Helper struct {
	DB         *sql.DB
	TypeMapper TypeMapper
}

// NewHelper creates a new SQL View helper.
func NewHelper(db *sql.DB, typeMapper TypeMapper) *Helper {
	return &Helper{
		DB:         db,
		TypeMapper: typeMapper,
	}
}

func (h *Helper) ValidateSQLViewIdentity(ctx context.Context, config *datasource.SQLViewConfig) error {
	query, err := datasource.SQLViewIdentitySQL(config)
	if err != nil {
		return err
	}
	if err := h.ValidateSQLView(ctx, config.SQL); err != nil {
		return err
	}
	var valid bool
	if err := h.DB.QueryRowContext(ctx, query).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return datasource.ErrSQLViewIdentity
	}
	return nil
}

// ValidateSQLView validates a SQL query for use as a SQL View.
func (h *Helper) ValidateSQLView(ctx context.Context, sql string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	trimmed := strings.TrimSpace(sql)

	// Must start with SELECT.
	if !strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") {
		return errors.New("SQL view must be a SELECT statement")
	}

	// Reject dangerous statement keywords (word-boundary matched).
	for i, re := range forbiddenKeywordRes {
		if re.MatchString(trimmed) {
			return fmt.Errorf("SQL view cannot contain %s", ForbiddenSQLKeywords[i])
		}
	}

	// Reject filesystem/network access functions and URL schemes that would bypass
	// the datasource path allowlist.
	for i, re := range forbiddenFunctionRes {
		if re.MatchString(trimmed) {
			return fmt.Errorf("SQL view cannot call %s()", ForbiddenSQLFunctions[i])
		}
	}
	if forbiddenSchemeRe.MatchString(trimmed) {
		return errors.New("SQL view cannot reference remote URLs")
	}

	// Try to execute with LIMIT 0 to validate syntax
	testSQL := fmt.Sprintf("SELECT * FROM %s AS _sqlview_test LIMIT 0", subquery(sql))
	_, err := h.DB.ExecContext(ctx, testSQL)
	if err != nil {
		return fmt.Errorf("invalid SQL: %w", err)
	}

	return nil
}

// DiscoverSQLViewColumns executes a SQL query with LIMIT 1 to discover column metadata.
func (h *Helper) DiscoverSQLViewColumns(ctx context.Context, sql string) (*datasource.SQLViewDiscovery, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// First validate the SQL
	if err := h.ValidateSQLView(ctx, sql); err != nil {
		return nil, err
	}

	// Execute with LIMIT 1 to get column information and detect geometry
	testSQL := fmt.Sprintf("SELECT * FROM %s AS _sqlview_test LIMIT 1", subquery(sql))
	rows, err := h.DB.QueryContext(ctx, testSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to discover columns: %w", err)
	}
	defer rows.Close()

	// Get column names and types
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	if len(colTypes) == 0 {
		return nil, errors.New("SQL query returns no columns")
	}
	// Column metadata is independent of the rows. Release the connection before
	// SRID detection: managed encrypted databases intentionally use a pool of one.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	discovery := &datasource.SQLViewDiscovery{
		Columns: make([]datasource.PropertyInfo, 0, len(colTypes)),
	}

	// Common ID column names in order of preference
	idColumnPriority := map[string]int{
		"id": 1, "fid": 2, "gid": 3, "ogc_fid": 4, "objectid": 5, "feature_id": 6,
	}
	bestIDPriority := 999
	bestIDColumn := ""

	for i, ct := range colTypes {
		colName := ct.Name()
		colType := ct.DatabaseTypeName()
		colNameLower := strings.ToLower(colName)
		colTypeLower := strings.ToLower(colType)

		// Check if this is a geometry column
		isGeom := strings.Contains(colTypeLower, "geometry") ||
			strings.Contains(colTypeLower, "point") ||
			strings.Contains(colTypeLower, "linestring") ||
			strings.Contains(colTypeLower, "polygon") ||
			strings.Contains(colTypeLower, "multipoint") ||
			strings.Contains(colTypeLower, "multilinestring") ||
			strings.Contains(colTypeLower, "multipolygon") ||
			strings.Contains(colTypeLower, "blob") ||
			strings.Contains(colTypeLower, "wkb") ||
			colNameLower == "geom" || colNameLower == "geometry" || colNameLower == "wkb_geometry"

		if isGeom {
			discovery.GeometryColumn = colName
			discovery.GeometryType = "GEOMETRY"
			// Try to detect SRID
			discovery.SRID, err = h.detectSQLViewSRID(ctx, sql, colName)
			if err != nil {
				return nil, err
			}
		} else {
			discovery.Columns = append(discovery.Columns, datasource.PropertyInfo{
				Name:     colName,
				Type:     colType,
				JSONType: h.TypeMapper(colType),
				Ordinal:  i + 1,
			})

			// Check if this could be an ID column
			if priority, ok := idColumnPriority[colNameLower]; ok && priority < bestIDPriority {
				bestIDPriority = priority
				bestIDColumn = colName
			}
		}
	}

	if discovery.GeometryColumn == "" {
		return nil, errors.New("SQL query must return at least one geometry column")
	}

	discovery.SuggestedIDColumn = bestIDColumn

	return discovery, nil
}

// detectSQLViewSRID tries to detect the SRID of a geometry column from a SQL view.
func (h *Helper) detectSQLViewSRID(ctx context.Context, sql, geomCol string) (int, error) {
	// Try native geometry first
	checkSQL := fmt.Sprintf(`SELECT ST_SRID(%s) FROM %s AS _sqlview_check WHERE %s IS NOT NULL LIMIT 1`,
		QuoteIdent(geomCol), subquery(sql), QuoteIdent(geomCol))

	var srid int
	if err := h.DB.QueryRowContext(ctx, checkSQL).Scan(&srid); err == nil && srid != 0 {
		return srid, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Fall back to WKB conversion
	checkSQL = fmt.Sprintf(`SELECT ST_SRID(ST_GeomFromWKB(%s)) FROM %s AS _sqlview_check WHERE %s IS NOT NULL LIMIT 1`,
		QuoteIdent(geomCol), subquery(sql), QuoteIdent(geomCol))

	if err := h.DB.QueryRowContext(ctx, checkSQL).Scan(&srid); err != nil || srid == 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 4326, nil // Unknown CRS, not a cancelled metadata request.
	}
	return srid, nil
}

// QuerySQLView executes a feature query against a SQL View and returns GeoJSON features.
func (h *Helper) QuerySQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]json.RawMessage, error) {
	query, args, err := h.BuildSQLViewListSQL(config, params)
	if err != nil {
		return nil, err
	}

	rows, err := h.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query sql view: %w", err)
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

// QuerySQLViewWKB executes a SQL View query and returns WKB geometry with properties for rendering.
func (h *Helper) QuerySQLViewWKB(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	query, args, err := h.BuildSQLViewWKBSQL(config, params)
	if err != nil {
		return nil, err
	}

	rows, err := h.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query sql view wkb: %w", err)
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

// CountSQLView returns the number of features matching the SQL View query.
func (h *Helper) CountSQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) (int, error) {
	query, args, err := h.BuildSQLViewCountSQL(config, params)
	if err != nil {
		return 0, err
	}

	var count int
	if err := h.DB.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count sql view: %w", err)
	}

	return count, nil
}

// BuildSQLViewListSQL builds a SQL query to list features from a SQL View.
func (h *Helper) BuildSQLViewListSQL(config *datasource.SQLViewConfig, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	p.SortBy = datasource.StableSort(p.SortBy, config.IDColumn)
	geom := QuoteIdent(config.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = 4326
	}

	sourceSRID := config.SRID
	if sourceSRID == 0 {
		sourceSRID = 4326
	}

	// Build geometry output expression
	geomExpr := fmt.Sprintf("v.%s", geom)
	if sourceSRID != outSRID {
		geomExpr = fmt.Sprintf("ST_Transform(v.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, sourceSRID, outSRID)
	}
	if p.SimplifyTolerance > 0 {
		geomExpr = fmt.Sprintf("ST_SimplifyPreserveTopology(%s, %.12g)", geomExpr, p.SimplifyTolerance)
	}

	// Build ID expression
	var idExpr string
	if config.IDColumn != "" {
		idExpr = fmt.Sprintf("v.%s", QuoteIdent(config.IDColumn))
	} else {
		idExpr = "NULL"
	}

	// Build properties expression
	var propCols []string
	for _, prop := range config.Properties {
		if prop.Name != config.GeometryColumn && prop.Name != config.IDColumn && datasource.PropertySelected(prop.Name, p.Properties) {
			propCols = append(propCols, fmt.Sprintf("'%s', v.%s", strings.ReplaceAll(prop.Name, "'", "''"), QuoteIdent(prop.Name)))
		}
	}
	propExpr := "json_object(" + strings.Join(propCols, ", ") + ")"
	if len(propCols) == 0 {
		propExpr = "'{}'::JSON"
	}

	var whereParts []string
	var args []any
	argPos := 1

	if len(p.FeatureIDs) > 0 {
		if config.IDColumn == "" {
			return "", nil, fmt.Errorf("SQL view requires an id_column for item lookup")
		}
		predicate, idArgs, next := datasource.FeatureIDPredicate(p.FeatureIDs, "v."+QuoteIdent(config.IDColumn), argPos)
		whereParts = append(whereParts, predicate)
		args = append(args, idArgs...)
		argPos = next
	}

	// BBox filter
	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("v."+geom, sourceSRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "v", GeometryExpression: "v." + QuoteIdent(config.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		allowed := make(map[string]struct{})
		for _, prop := range config.Properties {
			allowed[prop.Name] = struct{}{}
		}
		if config.IDColumn != "" {
			allowed[config.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}

		opts := filter.DuckDBOptions{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        sourceSRID,
			AllowedProperties: allowed,
			GeometryProperty:  config.GeometryColumn,
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
			dir := "ASC"
			if s.Desc {
				dir = "DESC"
			}
			orderItems = append(orderItems, fmt.Sprintf("v.%s %s", QuoteIdent(s.Name), dir))
		}
		if len(orderItems) > 0 {
			orderSQL = "ORDER BY " + strings.Join(orderItems, ", ")
		}
	} else if config.IDColumn != "" {
		orderSQL = fmt.Sprintf("ORDER BY v.%s", QuoteIdent(config.IDColumn))
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

	// Wrap user SQL in subquery
	sql := fmt.Sprintf(`SELECT CAST(%s AS VARCHAR) AS feature FROM %s v %s %s %s`, featureExpr, subquery(config.SQL), whereSQL, orderSQL, limitSQL)
	return sql, args, nil
}

// BuildSQLViewWKBSQL builds a SQL query that returns WKB geometry and properties from a SQL View.
func (h *Helper) BuildSQLViewWKBSQL(config *datasource.SQLViewConfig, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	geom := QuoteIdent(config.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = config.SRID
	}
	if outSRID == 0 {
		outSRID = 4326
	}

	sourceSRID := config.SRID
	if sourceSRID == 0 {
		sourceSRID = 4326
	}

	// Build geometry output expression
	geomExpr := fmt.Sprintf("v.%s", geom)
	if sourceSRID != outSRID {
		geomExpr = fmt.Sprintf("ST_Transform(v.%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, sourceSRID, outSRID)
	}

	// Build properties expression
	var propCols []string
	for _, prop := range config.Properties {
		if prop.Name != config.GeometryColumn && prop.Name != config.IDColumn {
			propCols = append(propCols, fmt.Sprintf("'%s', v.%s", prop.Name, QuoteIdent(prop.Name)))
		}
	}
	propsExpr := "json_object(" + strings.Join(propCols, ", ") + ")"
	if len(propCols) == 0 {
		propsExpr = "'{}'::JSON"
	}

	var whereParts []string
	var args []any
	argPos := 1

	if len(p.FeatureIDs) > 0 {
		if config.IDColumn == "" {
			return "", nil, fmt.Errorf("SQL view requires an id_column for item lookup")
		}
		predicate, idArgs, next := datasource.FeatureIDPredicate(p.FeatureIDs, "v."+QuoteIdent(config.IDColumn), argPos)
		whereParts = append(whereParts, predicate)
		args = append(args, idArgs...)
		argPos = next
	}

	// BBox filter
	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("v."+geom, sourceSRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "v", GeometryExpression: "v." + QuoteIdent(config.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		allowed := make(map[string]struct{})
		for _, prop := range config.Properties {
			allowed[prop.Name] = struct{}{}
		}
		if config.IDColumn != "" {
			allowed[config.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}

		opts := filter.DuckDBOptions{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        sourceSRID,
			AllowedProperties: allowed,
			GeometryProperty:  config.GeometryColumn,
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

	limitSQL := ""
	if p.Limit > 0 {
		limitSQL = fmt.Sprintf("LIMIT %d", p.Limit)
	}
	idExpr := "NULL"
	if config.IDColumn != "" {
		idExpr = fmt.Sprintf("CAST(v.%s AS VARCHAR)", QuoteIdent(config.IDColumn))
	}
	sql := fmt.Sprintf(`SELECT ST_AsWKB(%s) AS geom, %s AS props, %s AS feature_id FROM %s v %s %s`,
		geomExpr, propsExpr, idExpr, subquery(config.SQL), whereSQL, limitSQL)
	return sql, args, nil
}

// BuildSQLViewCountSQL builds a SQL query to count features from a SQL View.
func (h *Helper) BuildSQLViewCountSQL(config *datasource.SQLViewConfig, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	geom := QuoteIdent(config.GeometryColumn)

	sourceSRID := config.SRID
	if sourceSRID == 0 {
		sourceSRID = 4326
	}

	var whereParts []string
	var args []any
	argPos := 1

	if len(p.FeatureIDs) > 0 {
		if config.IDColumn == "" {
			return "", nil, fmt.Errorf("SQL view requires an id_column for item lookup")
		}
		predicate, idArgs, next := datasource.FeatureIDPredicate(p.FeatureIDs, "v."+QuoteIdent(config.IDColumn), argPos)
		whereParts = append(whereParts, predicate)
		args = append(args, idArgs...)
		argPos = next
	}

	// BBox filter
	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("v."+geom, sourceSRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLDuckDB, TableAlias: "v", GeometryExpression: "v." + QuoteIdent(config.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && p.Filter != "" {
		allowed := make(map[string]struct{})
		for _, prop := range config.Properties {
			allowed[prop.Name] = struct{}{}
		}
		if config.IDColumn != "" {
			allowed[config.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}

		opts := filter.DuckDBOptions{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        sourceSRID,
			AllowedProperties: allowed,
			GeometryProperty:  config.GeometryColumn,
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

	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s v %s`, subquery(config.SQL), whereSQL)
	return sql, args, nil
}

// QuoteIdent quotes an identifier for use in SQL.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func buildBBoxPredicate(geomExpr string, sourceSRID, bboxSRID, argPos int, bbox *datasource.BBox) (string, []any, int) {
	if bboxSRID == 0 {
		bboxSRID = 4326
	}
	if sourceSRID == 0 {
		sourceSRID = 4326
	}
	var clauses []string
	var args []any
	for _, part := range bbox.Parts(bboxSRID) {
		envelope := fmt.Sprintf("ST_MakeEnvelope($%d, $%d, $%d, $%d)", argPos, argPos+1, argPos+2, argPos+3)
		if sourceSRID != bboxSRID {
			envelope = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", envelope, bboxSRID, sourceSRID)
		}
		clauses = append(clauses, fmt.Sprintf("ST_Intersects(%s, %s)", geomExpr, envelope))
		args = append(args, part.MinX, part.MinY, part.MaxX, part.MaxY)
		argPos += 4
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args, argPos
}

// DefaultTypeMapper is a default implementation of TypeMapper for DuckDB types.
func DefaultTypeMapper(colType string) datasource.JSONType {
	colTypeLower := strings.ToLower(colType)
	switch {
	case strings.Contains(colTypeLower, "bool"):
		return datasource.JSONTypeBoolean
	// Check array types before int (since INTEGER[] contains "int")
	case strings.Contains(colTypeLower, "[]") || strings.Contains(colTypeLower, "list"):
		return datasource.JSONTypeArray
	case strings.Contains(colTypeLower, "int") || strings.Contains(colTypeLower, "bigint"):
		return datasource.JSONTypeInteger
	case strings.Contains(colTypeLower, "float") || strings.Contains(colTypeLower, "double") || strings.Contains(colTypeLower, "decimal") || strings.Contains(colTypeLower, "numeric"):
		return datasource.JSONTypeNumber
	case strings.Contains(colTypeLower, "json"):
		return datasource.JSONTypeObject
	case strings.Contains(colTypeLower, "struct") || strings.Contains(colTypeLower, "map"):
		return datasource.JSONTypeObject
	default:
		return datasource.JSONTypeString
	}
}

// subquery embeds a user SQL view as a derived table. The query sits on its
// own lines so a trailing line comment cannot swallow the closing parenthesis.
func subquery(sql string) string {
	return "(\n" + sql + "\n)"
}
