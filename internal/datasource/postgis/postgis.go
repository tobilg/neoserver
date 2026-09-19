// Package postgis provides a PostGIS DataSource implementation.
package postgis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/filter"
	"github.com/tobilg/neoserver/internal/store"
)

func init() {
	datasource.Register(store.ServiceTypePostGIS, NewFromService)
}

// Config holds PostGIS connection configuration.
type Config struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	Database        string        `json:"database"`
	User            string        `json:"user"`
	Password        string        `json:"password"`
	SSLMode         string        `json:"sslmode,omitempty"`
	Schemas         []string      `json:"schemas,omitempty"`
	MaxOpenConns    int           `json:"max_open_conns,omitempty"`
	MaxIdleConns    int           `json:"max_idle_conns,omitempty"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime,omitempty"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time,omitempty"`
}

// DefaultConfig returns default configuration values.
func DefaultConfig() Config {
	return Config{
		Port:            5432,
		SSLMode:         "prefer",
		Schemas:         []string{"public"},
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 10 * time.Minute,
	}
}

// DataSource implements datasource.DataSource for PostGIS.
type DataSource struct {
	id          string
	pool        *pgxpool.Pool
	sqlViewPool *pgxpool.Pool
	schemas     []string
	layerCache  map[string]*datasource.LayerInfo
	cacheMu     sync.RWMutex
}

// New creates a new PostGIS DataSource.
func New(id string, cfg Config) (*DataSource, error) {
	poolConfig, err := connectionConfig(cfg)
	if err != nil {
		return nil, err
	}

	poolConfig.MaxConns = int32(cfg.MaxOpenConns)
	poolConfig.MinConns = int32(cfg.MaxIdleConns)
	poolConfig.MaxConnLifetime = cfg.ConnMaxLifetime
	poolConfig.MaxConnIdleTime = cfg.ConnMaxIdleTime

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	readConfig := poolConfig.Copy()
	readConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	readConfig.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	readConfig.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	readConfig.ConnConfig.RuntimeParams["search_path"] = "pg_catalog,public"
	readPool, err := pgxpool.NewWithConfig(ctx, readConfig)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create SQL view pool: %w", err)
	}
	schemas := cfg.Schemas
	if len(schemas) == 0 {
		schemas = []string{"public"}
	}

	return &DataSource{
		id:          id,
		pool:        pool,
		sqlViewPool: readPool,
		schemas:     schemas,
		layerCache:  make(map[string]*datasource.LayerInfo),
	}, nil
}

func connectionConfig(cfg Config) (*pgxpool.Config, error) {
	switch cfg.SSLMode {
	case "", "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return nil, errors.New("invalid PostGIS TLS mode")
	}
	endpoint := url.URL{Scheme: "postgres", User: url.UserPassword(cfg.User, cfg.Password),
		Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Path: "/" + cfg.Database,
		RawQuery: url.Values{"sslmode": []string{cfg.SSLMode}}.Encode()}
	parsed, err := pgxpool.ParseConfig(endpoint.String())
	if err != nil {
		return nil, errors.New("invalid PostGIS connection configuration; check host, port and TLS mode")
	}
	return parsed, nil
}

// NewFromService creates a PostGIS DataSource from a store.Service.
func NewFromService(svc *store.Service) (datasource.DataSource, error) {
	var cfg Config
	if err := json.Unmarshal(svc.ConnectionInfo, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if cfg.Port == 0 {
		cfg.Port = defaults.Port
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = defaults.SSLMode
	}
	if len(cfg.Schemas) == 0 {
		cfg.Schemas = defaults.Schemas
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = defaults.MaxOpenConns
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = defaults.MaxIdleConns
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = defaults.ConnMaxLifetime
	}
	if cfg.ConnMaxIdleTime == 0 {
		cfg.ConnMaxIdleTime = defaults.ConnMaxIdleTime
	}

	return New(svc.ID, cfg)
}

// Type returns the data source type.
func (ds *DataSource) Type() store.ServiceType {
	return store.ServiceTypePostGIS
}

// ID returns the service ID.
func (ds *DataSource) ID() string {
	return ds.id
}

// Pool returns the underlying connection pool (for backward compatibility).
func (ds *DataSource) Pool() *pgxpool.Pool {
	return ds.pool
}

// DiscoverLayers discovers available layers from PostGIS.
func (ds *DataSource) DiscoverLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	rows, err := ds.pool.Query(ctx, sqlDiscoverLayers, ds.schemas)
	if err != nil {
		return nil, fmt.Errorf("discover layers: %w", err)
	}
	defer rows.Close()

	var layers []*datasource.DiscoveredLayer
	seen := make(map[string]bool)

	for rows.Next() {
		var (
			sourceID    string
			schema      string
			table       string
			description string
			geomCol     string
			srid        int
			geomType    string
			idCol       string
		)

		if err := rows.Scan(&sourceID, &schema, &table, &description, &geomCol, &srid, &geomType, &idCol); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		// Skip duplicates (same table with multiple geometry columns - take first)
		if seen[sourceID] {
			continue
		}
		seen[sourceID] = true

		layers = append(layers, &datasource.DiscoveredLayer{
			Name:           sourceID,
			Schema:         schema,
			Title:          table,
			Description:    description,
			GeometryColumn: geomCol,
			GeometryType:   geomType,
			SRID:           srid,
			IDColumn:       idCol,
		})
	}

	return layers, rows.Err()
}

// Query executes a feature query.
func (ds *DataSource) Query(ctx context.Context, layer string, params datasource.QueryParams) ([]json.RawMessage, error) {
	stream, err := ds.QueryStream(ctx, layer, params)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var out []json.RawMessage
	for stream.Next() {
		out = append(out, append(json.RawMessage(nil), stream.Feature()...))
	}
	return out, stream.Err()
}

type featureStream struct {
	rows    pgx.Rows
	current json.RawMessage
	err     error
}

func (s *featureStream) Next() bool {
	if !s.rows.Next() {
		return false
	}
	var b []byte
	if err := s.rows.Scan(&b); err != nil {
		s.err = err
		return false
	}
	s.current = b
	return true
}
func (s *featureStream) Feature() json.RawMessage { return s.current }
func (s *featureStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}
func (s *featureStream) Close() error { s.rows.Close(); return nil }

func (ds *DataSource) QueryStream(ctx context.Context, layer string, params datasource.QueryParams) (datasource.FeatureStream, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}

	sql, args, err := ds.buildListSQL(info, params)
	if err != nil {
		return nil, err
	}

	rows, err := ds.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return &featureStream{rows: rows}, nil
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

	sql := ds.buildFeatureByIDSQL(info, outputSRID)
	arg, err := parseFeatureID(info.PGTypes[info.IDColumn], featureID)
	if err != nil {
		return nil, false, err
	}

	var b []byte
	err = ds.pool.QueryRow(ctx, sql, arg).Scan(&b)
	if err == nil {
		return json.RawMessage(b), true, nil
	}
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("query row: %w", err)
}

// QueryWKB executes a feature query and returns WKB geometry with properties for rendering.
func (ds *DataSource) QueryWKB(ctx context.Context, layer string, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	stream, err := ds.QueryWKBStream(ctx, layer, params)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var out []datasource.RenderFeature
	for stream.Next() {
		out = append(out, stream.Feature())
	}
	return out, stream.Err()
}

type renderStream struct {
	rows    pgx.Rows
	current datasource.RenderFeature
	err     error
}

func (s *renderStream) Next() bool {
	if !s.rows.Next() {
		return false
	}
	var geom []byte
	var props map[string]interface{}
	var id any
	if err := s.rows.Scan(&geom, &props, &id); err != nil {
		s.err = err
		return false
	}
	s.current = datasource.RenderFeature{ID: datasource.StableRenderFeatureID(id, geom, props), Geometry: geom, Properties: props}
	return true
}
func (s *renderStream) Feature() datasource.RenderFeature { return s.current }
func (s *renderStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}
func (s *renderStream) Close() error { s.rows.Close(); return nil }

func (ds *DataSource) QueryWKBStream(ctx context.Context, layer string, params datasource.QueryParams) (datasource.RenderFeatureStream, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return nil, err
	}

	sql, args, err := ds.buildWKBSQL(info, params)
	if err != nil {
		return nil, err
	}

	rows, err := ds.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return &renderStream{rows: rows}, nil
}

// buildWKBSQL builds a SQL query that returns WKB geometry and properties JSON.
func (ds *DataSource) buildWKBSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:]) // Remove schema prefix
	geom := quoteIdent(info.GeometryColumn)

	geomExpr := fmt.Sprintf("(%s.%s)::geometry", "t", geom)
	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = info.SRID
	}
	geomOut := geomExpr
	if info.SRID != outSRID {
		geomOut = fmt.Sprintf("ST_Transform(%s, %d)", geomExpr, outSRID)
	}
	if p.SimplifyTolerance > 0 {
		geomOut = fmt.Sprintf("ST_SimplifyPreserveTopology(%s, %.12g)", geomOut, p.SimplifyTolerance)
	}

	propExpr := buildProjectedProperties(info, p.Properties, "t")
	idExpr := "NULL::text"
	if info.IDColumn != "" {
		idExpr = fmt.Sprintf("t.%s::text", quoteIdent(info.IDColumn))
	}

	var whereParts []string
	var args []any
	argPos := 1

	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("t."+geom, info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// Use pre-compiled filter if provided (e.g., from FES XML)
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLPostGIS, TableAlias: "t", GeometryExpression: "t." + quoteIdent(info.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
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

		filterSQL, fargs, _, err := filter.Compile(p.Filter, filter.Options{
			StartParamIndex:   argPos,
			FilterSRID:        nonZero(p.FilterSRID, 4326),
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
	sql := fmt.Sprintf(`SELECT ST_AsBinary(%s) AS geom, %s AS props, %s AS feature_id FROM %s.%s t %s %s`,
		geomOut, propExpr, idExpr, schema, table, whereSQL, limitSQL)
	return sql, args, nil
}

// Count returns the number of features matching the query.
func (ds *DataSource) Count(ctx context.Context, layer string, params datasource.QueryParams) (int, error) {
	info, err := ds.GetLayerInfo(ctx, layer)
	if err != nil {
		return 0, err
	}

	sql, args, err := ds.buildCountSQL(info, params)
	if err != nil {
		return 0, err
	}

	var count int
	if err := ds.pool.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}

	return count, nil
}

// GetLayerInfo returns metadata about a specific layer.
func (ds *DataSource) GetLayerInfo(ctx context.Context, layer string) (*datasource.LayerInfo, error) {
	return ds.getLayerInfo(ctx, ds.pool, layer)
}

func (ds *DataSource) getLayerInfo(ctx context.Context, exec writeExecutor, layer string) (*datasource.LayerInfo, error) {
	// Check cache first (read lock)
	ds.cacheMu.RLock()
	if info, ok := ds.layerCache[layer]; ok {
		ds.cacheMu.RUnlock()
		return info, nil
	}
	ds.cacheMu.RUnlock()

	// Parse layer name (schema.table or just table)
	schema, table := parseLayerName(layer)
	if schema == "" {
		// Default to first configured schema
		if len(ds.schemas) > 0 {
			schema = ds.schemas[0]
		} else {
			schema = "public"
		}
	}

	// Query layer info
	info, err := ds.queryLayerInfoUsing(ctx, exec, schema, table)
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
	schema, table := parseLayerName(layer)
	if schema == "" {
		schema = info.Schema
	}
	var extent datasource.Extent
	extent.SRID = info.SRID
	query := fmt.Sprintf(`SELECT ST_XMin(e), ST_YMin(e), ST_XMax(e), ST_YMax(e)
		FROM (SELECT ST_Extent(%s) AS e FROM %s.%s) bounds`, quoteIdent(info.GeometryColumn), quoteIdent(schema), quoteIdent(table))
	if err := ds.pool.QueryRow(ctx, query).Scan(&extent.MinX, &extent.MinY, &extent.MaxX, &extent.MaxY); err != nil {
		return nil, fmt.Errorf("layer extent: %w", err)
	}
	return &extent, nil
}

// Health checks the data source connection.
func (ds *DataSource) Health(ctx context.Context) error {
	return ds.pool.Ping(ctx)
}

// Close releases resources.
func (ds *DataSource) Close() error {
	ds.pool.Close()
	if ds.sqlViewPool != nil {
		ds.sqlViewPool.Close()
	}
	return nil
}

// ClearCache clears the layer info cache.
func (ds *DataSource) ClearCache() {
	ds.cacheMu.Lock()
	ds.layerCache = make(map[string]*datasource.LayerInfo)
	ds.cacheMu.Unlock()
}

// queryLayerInfo queries the database for layer metadata.
func (ds *DataSource) queryLayerInfo(ctx context.Context, schema, table string) (*datasource.LayerInfo, error) {
	return ds.queryLayerInfoUsing(ctx, ds.pool, schema, table)
}

func (ds *DataSource) queryLayerInfoUsing(ctx context.Context, exec writeExecutor, schema, table string) (*datasource.LayerInfo, error) {
	row := exec.QueryRow(ctx, sqlLayerInfo, schema, table)

	var (
		geomCol     string
		srid        int
		geomType    string
		idCol       string
		description string
		props       [][]string
	)

	err := row.Scan(&geomCol, &srid, &geomType, &idCol, &description, &props)
	if err == pgx.ErrNoRows {
		return nil, datasource.LayerNotFoundError{Layer: schema + "." + table}
	}
	if err != nil {
		return nil, fmt.Errorf("query layer info: %w", err)
	}

	properties, pgTypes := parseProps(props)

	return &datasource.LayerInfo{
		Name:           schema + "." + table,
		Schema:         schema,
		Title:          table,
		Description:    description,
		GeometryColumn: geomCol,
		GeometryType:   geomType,
		SRID:           srid,
		IDColumn:       idCol,
		Properties:     properties,
		PGTypes:        pgTypes,
	}, nil
}

func (ds *DataSource) buildListSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	p.SortBy = datasource.StableSort(p.SortBy, info.IDColumn)
	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:]) // Remove schema prefix
	geom := quoteIdent(info.GeometryColumn)

	geomExpr := fmt.Sprintf("(%s.%s)::geometry", "t", geom)
	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = 4326
	}
	geomOut := geomExpr
	if info.SRID != outSRID {
		geomOut = fmt.Sprintf("ST_Transform(%s, %d)", geomExpr, outSRID)
	}

	var idExpr string
	if info.IDColumn != "" {
		idCol := quoteIdent(info.IDColumn)
		idExpr = fmt.Sprintf("%s.%s", "t", idCol)
	} else {
		idExpr = "NULL"
	}
	propExpr := buildProjectedProperties(info, p.Properties, "t")

	var whereParts []string
	var args []any
	argPos := 1

	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("t."+geom, info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// Use pre-compiled filter if provided (e.g., from FES XML)
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLPostGIS, TableAlias: "t", GeometryExpression: "t." + quoteIdent(info.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
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

		filterSQL, fargs, next, err := filter.Compile(p.Filter, filter.Options{
			StartParamIndex:   argPos,
			FilterSRID:        nonZero(p.FilterSRID, 4326),
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
			argPos = next
		}
	}

	orderSQL := ""
	if len(p.SortBy) > 0 {
		var orderItems []string
		for _, s := range p.SortBy {
			if s.Name == "" {
				continue
			}
			if s.Name != info.IDColumn {
				if _, ok := info.PGTypes[s.Name]; !ok {
					continue
				}
			}
			dir := "ASC"
			if s.Desc {
				dir = "DESC"
			}
			orderItems = append(orderItems, fmt.Sprintf("%s.%s %s", "t", quoteIdent(s.Name), dir))
		}
		if len(orderItems) > 0 {
			orderSQL = "ORDER BY " + strings.Join(orderItems, ",")
		}
	} else if info.IDColumn != "" {
		orderSQL = fmt.Sprintf("ORDER BY %s.%s", "t", quoteIdent(info.IDColumn))
	}

	limitSQL := fmt.Sprintf("LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, p.Limit, p.Offset)

	featureExpr := fmt.Sprintf(
		`jsonb_build_object('type','Feature','id',%s,'geometry',ST_AsGeoJSON(%s)::jsonb,'properties',%s)`,
		idExpr,
		geomOut,
		propExpr,
	)

	whereSQL := ""
	if len(whereParts) > 0 {
		whereSQL = "WHERE " + strings.Join(whereParts, " AND ")
	}

	sql := fmt.Sprintf(`SELECT %s AS feature FROM %s.%s t %s %s %s`, featureExpr, schema, table, whereSQL, orderSQL, limitSQL)
	return sql, args, nil
}

func (ds *DataSource) buildFeatureByIDSQL(info *datasource.LayerInfo, outSRID int) string {
	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:])
	geom := quoteIdent(info.GeometryColumn)
	idCol := quoteIdent(info.IDColumn)

	geomExpr := fmt.Sprintf("(%s.%s)::geometry", "t", geom)
	geomOut := geomExpr
	if info.SRID != outSRID {
		geomOut = fmt.Sprintf("ST_Transform(%s, %d)", geomExpr, outSRID)
	}

	propExpr := fmt.Sprintf("(to_jsonb(t) - %s - %s)", quoteLiteral(info.GeometryColumn), quoteLiteral(info.IDColumn))
	featureExpr := fmt.Sprintf(
		`jsonb_build_object('type','Feature','id',t.%s,'geometry',ST_AsGeoJSON(%s)::jsonb,'properties',%s)`,
		idCol,
		geomOut,
		propExpr,
	)

	return fmt.Sprintf(`SELECT %s AS feature FROM %s.%s t WHERE t.%s = $1 LIMIT 1`, featureExpr, schema, table, idCol)
}

func (ds *DataSource) buildCountSQL(info *datasource.LayerInfo, p datasource.QueryParams) (string, []any, error) {
	p = p.WithDateTimeFilter()
	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:])
	geom := quoteIdent(info.GeometryColumn)

	var whereParts []string
	var args []any
	argPos := 1

	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("t."+geom, info.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// Use pre-compiled filter if provided (e.g., from FES XML)
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLPostGIS, TableAlias: "t", GeometryExpression: "t." + quoteIdent(info.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
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

		filterSQL, fargs, _, err := filter.Compile(p.Filter, filter.Options{
			StartParamIndex:   argPos,
			FilterSRID:        nonZero(p.FilterSRID, 4326),
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

	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s t %s`, schema, table, whereSQL)
	return sql, args, nil
}

// SQL queries
const sqlDiscoverLayers = `
SELECT
	Format('%s.%s', n.nspname, c.relname) AS id,
	n.nspname AS schema,
	c.relname AS table,
	coalesce(d.description, '') AS description,
	a.attname AS geometry_column,
	postgis_typmod_srid(a.atttypmod) AS srid,
	postgis_typmod_type(a.atttypmod) AS geometry_type,
	coalesce(ia.attname, '') AS id_column
FROM pg_class c
JOIN pg_namespace n ON (c.relnamespace = n.oid)
JOIN pg_attribute a ON (a.attrelid = c.oid)
JOIN pg_type t ON (a.atttypid = t.oid)
LEFT JOIN pg_description d ON (c.oid = d.objoid AND d.objsubid = 0)
LEFT JOIN pg_index i ON (c.oid = i.indrelid AND i.indisprimary AND i.indnatts = 1)
LEFT JOIN pg_attribute ia ON (ia.attrelid = i.indexrelid)
LEFT JOIN pg_type it ON (ia.atttypid = it.oid AND it.typname in ('int2', 'int4', 'int8'))
WHERE c.relkind IN ('r', 'v', 'm', 'p', 'f')
AND n.nspname = ANY($1)
AND t.typname IN ('geometry', 'geography')
AND has_table_privilege(c.oid, 'select')
AND postgis_typmod_srid(a.atttypmod) > 0
ORDER BY id, a.attnum
`

const sqlLayerInfo = `
SELECT
	a.attname AS geometry_column,
	postgis_typmod_srid(a.atttypmod) AS srid,
	postgis_typmod_type(a.atttypmod) AS geometry_type,
	coalesce(ia.attname, '') AS id_column,
	coalesce(d.description, '') AS description,
	(
		SELECT array_agg(ARRAY[sa.attname, st.typname, coalesce(da.description,''), sa.attnum::text]::text[] ORDER BY sa.attnum)
		FROM pg_attribute sa
		JOIN pg_type st ON sa.atttypid = st.oid
		LEFT JOIN pg_description da ON (c.oid = da.objoid and sa.attnum = da.objsubid)
		WHERE sa.attrelid = c.oid
		AND sa.attnum > 0
		AND NOT sa.attisdropped
		AND st.typname NOT IN ('geometry', 'geography')
	) AS props
FROM pg_class c
JOIN pg_namespace n ON (c.relnamespace = n.oid)
JOIN pg_attribute a ON (a.attrelid = c.oid)
JOIN pg_type t ON (a.atttypid = t.oid)
LEFT JOIN pg_description d ON (c.oid = d.objoid AND d.objsubid = 0)
LEFT JOIN pg_index i ON (c.oid = i.indrelid AND i.indisprimary AND i.indnatts = 1)
LEFT JOIN pg_attribute ia ON (ia.attrelid = i.indexrelid)
LEFT JOIN pg_type it ON (ia.atttypid = it.oid AND it.typname in ('int2', 'int4', 'int8'))
WHERE n.nspname = $1
AND c.relname = $2
AND t.typname IN ('geometry', 'geography')
AND has_table_privilege(c.oid, 'select')
LIMIT 1
`

// Helper functions

func parseLayerName(layer string) (schema, table string) {
	parts := strings.SplitN(layer, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

func parseFeatureID(pgType, v string) (any, error) {
	switch pgType {
	case "int2", "int4", "int8":
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, datasource.InvalidFeatureIDError{Value: v}
		}
		return i, nil
	default:
		return v, nil
	}
}

func parseProps(props [][]string) ([]datasource.PropertyInfo, map[string]string) {
	var out []datasource.PropertyInfo
	pgTypes := make(map[string]string)

	for _, p := range props {
		if len(p) < 4 {
			continue
		}
		name := p[0]
		pgType := p[1]
		desc := p[2]
		ordinal := 0
		if len(p) > 3 {
			ordinal, _ = strconv.Atoi(p[3])
		}

		pgTypes[name] = pgType

		out = append(out, datasource.PropertyInfo{
			Name:        name,
			Type:        pgType,
			JSONType:    pgTypeToJSON(pgType),
			Description: desc,
			Ordinal:     ordinal,
		})
	}

	return out, pgTypes
}

func pgTypeToJSON(pgType string) datasource.JSONType {
	switch pgType {
	case "bool":
		return datasource.JSONTypeBoolean
	case "int2", "int4", "int8", "oid":
		return datasource.JSONTypeInteger
	case "float4", "float8", "numeric":
		return datasource.JSONTypeNumber
	case "json", "jsonb":
		return datasource.JSONTypeObject
	case "_text", "_int4", "_int8", "_float8":
		return datasource.JSONTypeArray
	default:
		return datasource.JSONTypeString
	}
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func buildBBoxPredicate(geomExpr string, sourceSRID, bboxSRID, argPos int, bbox *datasource.BBox) (string, []any, int) {
	bboxSRID = nonZero(bboxSRID, 4326)
	sourceSRID = nonZero(sourceSRID, 4326)
	parts := bbox.Parts(bboxSRID)
	clauses := make([]string, 0, len(parts))
	args := make([]any, 0, len(parts)*4)
	for _, part := range parts {
		envelope := fmt.Sprintf("ST_MakeEnvelope($%d,$%d,$%d,$%d,%d)", argPos, argPos+1, argPos+2, argPos+3, bboxSRID)
		if sourceSRID != bboxSRID {
			envelope = fmt.Sprintf("ST_Transform(%s, %d)", envelope, sourceSRID)
		}
		clauses = append(clauses, fmt.Sprintf("ST_Intersects(%s, %s)", geomExpr, envelope))
		args = append(args, part.MinX, part.MinY, part.MaxX, part.MaxY)
		argPos += 4
	}
	predicate := clauses[0]
	if len(clauses) > 1 {
		predicate = "(" + strings.Join(clauses, " OR ") + ")"
	}
	return predicate, args, argPos
}

func buildProjectedProperties(info *datasource.LayerInfo, requested []string, alias string) string {
	if len(requested) == 0 {
		if info.IDColumn != "" {
			return fmt.Sprintf("(to_jsonb(%s) - %s - %s)", alias, quoteLiteral(info.GeometryColumn), quoteLiteral(info.IDColumn))
		}
		return fmt.Sprintf("(to_jsonb(%s) - %s)", alias, quoteLiteral(info.GeometryColumn))
	}
	parts := make([]string, 0, len(requested)*2)
	for _, prop := range info.Properties {
		if prop.Name == info.IDColumn || !datasource.PropertySelected(prop.Name, requested) {
			continue
		}
		parts = append(parts, quoteLiteral(prop.Name), alias+"."+quoteIdent(prop.Name))
	}
	if len(parts) == 0 {
		return "'{}'::jsonb"
	}
	return "jsonb_build_object(" + strings.Join(parts, ",") + ")"
}

func nonZero(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// WritableDataSource implementation

type writeExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type transactionWriter struct {
	ds *DataSource
	tx pgx.Tx
}

func (ds *DataSource) AtomicWrite(ctx context.Context, fn func(datasource.FeatureWriter) error) error {
	tx, err := ds.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := fn(&transactionWriter{ds: ds, tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// Insert inserts new features into the layer.
func (ds *DataSource) Insert(ctx context.Context, layer string, features []datasource.FeatureData) ([]string, error) {
	return ds.insert(ctx, ds.pool, layer, features)
}

func (ds *DataSource) insert(ctx context.Context, exec writeExecutor, layer string, features []datasource.FeatureData) ([]string, error) {
	info, err := ds.getLayerInfo(ctx, exec, layer)
	if err != nil {
		return nil, err
	}

	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:])

	var ids []string

	for _, feature := range features {
		// Build column list and values
		var columns []string
		var placeholders []string
		var args []any
		argPos := 1

		// Add properties
		for propName, propValue := range feature.Properties {
			// Check if property exists in layer
			if _, ok := info.PGTypes[propName]; !ok {
				continue // Skip unknown properties
			}
			columns = append(columns, quoteIdent(propName))
			placeholders = append(placeholders, fmt.Sprintf("$%d", argPos))
			args = append(args, propValue)
			argPos++
		}

		// Add geometry if provided
		if feature.Geometry != "" {
			columns = append(columns, quoteIdent(info.GeometryColumn))
			// Parse GML geometry and convert to PostGIS
			placeholders = append(placeholders, geometryExpression(argPos, feature.GeometrySRID, info.SRID))
			args = append(args, feature.Geometry)
			argPos++
		}

		if len(columns) == 0 {
			continue // Nothing to insert
		}

		// Build INSERT statement with RETURNING clause
		var sql string
		if info.IDColumn != "" {
			sql = fmt.Sprintf(
				"INSERT INTO %s.%s (%s) VALUES (%s) RETURNING %s::text",
				schema, table,
				strings.Join(columns, ", "),
				strings.Join(placeholders, ", "),
				quoteIdent(info.IDColumn),
			)
		} else {
			sql = fmt.Sprintf(
				"INSERT INTO %s.%s (%s) VALUES (%s)",
				schema, table,
				strings.Join(columns, ", "),
				strings.Join(placeholders, ", "),
			)
		}

		if info.IDColumn != "" {
			var id string
			if err := exec.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
				return nil, fmt.Errorf("insert: %w", err)
			}
			ids = append(ids, id)
		} else {
			if _, err := exec.Exec(ctx, sql, args...); err != nil {
				return nil, fmt.Errorf("insert: %w", err)
			}
			// Use provided GML ID or generate a placeholder
			if feature.ID != "" {
				ids = append(ids, feature.ID)
			} else {
				ids = append(ids, "new")
			}
		}
	}

	return ids, nil
}

// Update updates features matching the filter.
func (ds *DataSource) Update(ctx context.Context, layer string, properties map[string]interface{}, filter string, args []interface{}) (int, error) {
	ids, err := ds.update(ctx, ds.pool, layer, properties, filter, args)
	return len(ids), err
}

func (ds *DataSource) update(ctx context.Context, exec writeExecutor, layer string, properties map[string]interface{}, filter string, args []interface{}) ([]string, error) {
	info, err := ds.getLayerInfo(ctx, exec, layer)
	if err != nil {
		return nil, err
	}

	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:])

	// Build SET clause
	var setClauses []string
	var updateArgs []any
	argPos := 1

	for propName, propValue := range properties {
		if propName == info.IDColumn {
			return nil, fmt.Errorf("feature identifiers are immutable")
		}
		if propName == info.GeometryColumn {
			if propValue == nil {
				setClauses = append(setClauses, quoteIdent(propName)+" = NULL")
				continue
			}
			geometry, ok := propValue.(datasource.GeometryValue)
			if !ok {
				return nil, fmt.Errorf("geometry requires validated GML and an input CRS")
			}
			setClauses = append(setClauses, fmt.Sprintf("%s = %s", quoteIdent(propName), geometryExpression(argPos, geometry.SRID, info.SRID)))
			updateArgs = append(updateArgs, geometry.GML)
			argPos++
			continue
		}
		// Check if property exists in layer
		if _, ok := info.PGTypes[propName]; !ok {
			continue // Skip unknown properties
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", quoteIdent(propName), argPos))
		updateArgs = append(updateArgs, propValue)
		argPos++
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("no valid properties to update")
	}

	// Build WHERE clause
	whereSQL := ""
	if filter != "" {
		// Adjust parameter numbers in the filter to account for SET parameters
		adjustedFilter := adjustFilterParams(filter, argPos-1)
		whereSQL = "WHERE " + adjustedFilter
		updateArgs = append(updateArgs, args...)
	}

	sql := fmt.Sprintf(
		"UPDATE %s.%s t SET %s %s",
		schema, table,
		strings.Join(setClauses, ", "),
		whereSQL,
	)

	return mutationIDs(ctx, exec, info, sql, updateArgs)
}

// Delete deletes features matching the filter.
func (ds *DataSource) Delete(ctx context.Context, layer string, filter string, args []interface{}) (int, error) {
	ids, err := ds.delete(ctx, ds.pool, layer, filter, args)
	return len(ids), err
}

func (ds *DataSource) delete(ctx context.Context, exec writeExecutor, layer string, filter string, args []interface{}) ([]string, error) {
	info, err := ds.getLayerInfo(ctx, exec, layer)
	if err != nil {
		return nil, err
	}

	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:])

	// Build WHERE clause
	whereSQL := ""
	if filter != "" {
		whereSQL = "WHERE " + filter
	}

	sql := fmt.Sprintf("DELETE FROM %s.%s t %s", schema, table, whereSQL)

	return mutationIDs(ctx, exec, info, sql, args)
}

// Replace replaces features matching the filter with new feature data.
func (ds *DataSource) Replace(ctx context.Context, layer string, feature datasource.FeatureData, filter string, args []interface{}) ([]string, error) {
	return ds.replace(ctx, ds.pool, layer, feature, filter, args)
}

func (ds *DataSource) replace(ctx context.Context, exec writeExecutor, layer string, feature datasource.FeatureData, filter string, args []interface{}) ([]string, error) {
	info, err := ds.getLayerInfo(ctx, exec, layer)
	if err != nil {
		return nil, err
	}

	schema := quoteIdent(info.Schema)
	table := quoteIdent(info.Name[len(info.Schema)+1:])

	// Build SET clause from feature properties
	var setClauses []string
	var updateArgs []any
	argPos := 1

	for propName, propValue := range feature.Properties {
		if propName == info.IDColumn {
			continue
		} // Replacement retains the feature identity.
		// Check if property exists in layer
		if _, ok := info.PGTypes[propName]; !ok {
			continue // Skip unknown properties
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", quoteIdent(propName), argPos))
		updateArgs = append(updateArgs, propValue)
		argPos++
	}

	// Add geometry if provided
	if feature.Geometry != "" {
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", quoteIdent(info.GeometryColumn), geometryExpression(argPos, feature.GeometrySRID, info.SRID)))
		updateArgs = append(updateArgs, feature.Geometry)
		argPos++
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("no valid properties to replace")
	}

	// Build WHERE clause
	whereSQL := ""
	if filter != "" {
		adjustedFilter := adjustFilterParams(filter, argPos-1)
		whereSQL = "WHERE " + adjustedFilter
		updateArgs = append(updateArgs, args...)
	}

	// Build UPDATE statement with RETURNING clause
	var sql string
	var ids []string

	if info.IDColumn != "" {
		sql = fmt.Sprintf(
			"UPDATE %s.%s t SET %s %s RETURNING %s::text",
			schema, table,
			strings.Join(setClauses, ", "),
			whereSQL,
			quoteIdent(info.IDColumn),
		)

		rows, err := exec.Query(ctx, sql, updateArgs...)
		if err != nil {
			return nil, fmt.Errorf("replace: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, fmt.Errorf("scan: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("rows: %w", err)
		}
	} else {
		sql = fmt.Sprintf(
			"UPDATE %s.%s t SET %s %s",
			schema, table,
			strings.Join(setClauses, ", "),
			whereSQL,
		)

		result, err := exec.Exec(ctx, sql, updateArgs...)
		if err != nil {
			return nil, fmt.Errorf("replace: %w", err)
		}

		// Return placeholder IDs based on rows affected
		for i := int64(0); i < result.RowsAffected(); i++ {
			ids = append(ids, fmt.Sprintf("replaced-%d", i))
		}
	}

	return ids, nil
}

func (w *transactionWriter) Insert(ctx context.Context, layer string, features []datasource.FeatureData) ([]string, error) {
	return w.ds.insert(ctx, w.tx, layer, features)
}

func (w *transactionWriter) Update(ctx context.Context, layer string, properties map[string]interface{}, filter string, args []interface{}) (int, error) {
	ids, err := w.ds.update(ctx, w.tx, layer, properties, filter, args)
	return len(ids), err
}

func (w *transactionWriter) Delete(ctx context.Context, layer, filter string, args []interface{}) (int, error) {
	ids, err := w.ds.delete(ctx, w.tx, layer, filter, args)
	return len(ids), err
}

func (w *transactionWriter) Replace(ctx context.Context, layer string, feature datasource.FeatureData, filter string, args []interface{}) ([]string, error) {
	return w.ds.replace(ctx, w.tx, layer, feature, filter, args)
}

// paramPattern matches PostgreSQL parameter placeholders ($1, $2, etc.)
var paramPattern = regexp.MustCompile(`\$(\d+)`)

// adjustFilterParams adjusts parameter numbers in a SQL filter string.
// This is needed when the filter is appended after other parameters.
func adjustFilterParams(filter string, offset int) string {
	if offset == 0 || filter == "" {
		return filter
	}

	return paramPattern.ReplaceAllStringFunc(filter, func(match string) string {
		// Parse the number after $
		numStr := match[1:]
		num, err := strconv.Atoi(numStr)
		if err != nil {
			return match // Keep original if parsing fails
		}
		return fmt.Sprintf("$%d", num+offset)
	})
}

// SQL View support

// ValidateSQLView checks parsed structure before executing on the dedicated
// read-only pool. Read-only database privileges remain required defense in depth.
func (ds *DataSource) ValidateSQLView(ctx context.Context, sql string) error {
	if err := validateSQLStructure(sql); err != nil {
		return err
	}
	if ds.sqlViewPool == nil {
		return fmt.Errorf("SQL view pool is unavailable")
	}
	_, err := ds.sqlViewPool.Exec(ctx, fmt.Sprintf("SELECT * FROM (%s) AS _sqlview_test LIMIT 0", strings.TrimSuffix(strings.TrimSpace(sql), ";")))
	if err != nil {
		return fmt.Errorf("invalid SQL: %w", err)
	}
	return nil
}

// DiscoverSQLViewColumns executes a SQL query with LIMIT 0 to discover column metadata.
func (ds *DataSource) DiscoverSQLViewColumns(ctx context.Context, sql string) (*datasource.SQLViewDiscovery, error) {
	// First validate the SQL
	if err := ds.ValidateSQLView(ctx, sql); err != nil {
		return nil, err
	}

	// Execute with LIMIT 0 to get column information
	testSQL := fmt.Sprintf("SELECT * FROM (%s) AS _sqlview_test LIMIT 0", sql)
	rows, err := ds.sqlViewPool.Query(ctx, testSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to discover columns: %w", err)
	}
	defer rows.Close()

	// Copy metadata before releasing the result set. The checks below query
	// the same pool, so retaining rows would deadlock a one-connection pool.
	fieldDescs := append([]pgconn.FieldDescription(nil), rows.FieldDescriptions()...)
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to discover columns: %w", err)
	}
	if len(fieldDescs) == 0 {
		return nil, fmt.Errorf("SQL query returns no columns")
	}

	discovery := &datasource.SQLViewDiscovery{
		Columns: make([]datasource.PropertyInfo, 0, len(fieldDescs)),
	}

	// Common ID column names in order of preference
	idColumnPriority := map[string]int{
		"id": 1, "fid": 2, "gid": 3, "ogc_fid": 4, "objectid": 5, "feature_id": 6,
	}
	bestIDPriority := 999
	bestIDColumn := ""

	for i, fd := range fieldDescs {
		colName := string(fd.Name)
		colNameLower := strings.ToLower(colName)

		// Check if this is a geometry column by querying the type
		isGeom, geomType, srid := ds.checkGeometryColumn(ctx, sql, colName)

		if isGeom {
			discovery.GeometryColumn = colName
			discovery.GeometryType = geomType
			discovery.SRID = srid
		} else {
			// Get PostgreSQL type name
			pgType := ds.getTypeName(ctx, fd.DataTypeOID)
			jsonType := pgTypeToJSON(pgType)

			discovery.Columns = append(discovery.Columns, datasource.PropertyInfo{
				Name:     colName,
				Type:     pgType,
				JSONType: jsonType,
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
		return nil, fmt.Errorf("SQL query must return at least one geometry column")
	}

	discovery.SuggestedIDColumn = bestIDColumn

	return discovery, nil
}

// checkGeometryColumn checks if a column is a geometry column and returns its type and SRID.
func (ds *DataSource) checkGeometryColumn(ctx context.Context, sql, colName string) (bool, string, int) {
	// Try to get geometry type and SRID using PostGIS functions
	checkSQL := fmt.Sprintf(`
		SELECT
			GeometryType(%s) AS geom_type,
			ST_SRID(%s) AS srid
		FROM (%s) AS _sqlview_check
		WHERE %s IS NOT NULL
		LIMIT 1
	`, quoteIdent(colName), quoteIdent(colName), sql, quoteIdent(colName))

	var geomType string
	var srid int
	err := ds.sqlViewPool.QueryRow(ctx, checkSQL).Scan(&geomType, &srid)
	if err != nil {
		return false, "", 0
	}

	if geomType != "" {
		if srid == 0 {
			srid = 4326 // Default to WGS84
		}
		return true, geomType, srid
	}

	return false, "", 0
}

// getTypeName returns the PostgreSQL type name for an OID.
func (ds *DataSource) getTypeName(ctx context.Context, oid uint32) string {
	var typeName string
	err := ds.sqlViewPool.QueryRow(ctx, "SELECT typname FROM pg_type WHERE oid = $1", oid).Scan(&typeName)
	if err != nil {
		return "unknown"
	}
	return typeName
}

// ValidateSQLViewIdentity verifies that every published row has a unique, non-null ID.
func (ds *DataSource) ValidateSQLViewIdentity(ctx context.Context, config *datasource.SQLViewConfig) error {
	query, err := datasource.SQLViewIdentitySQL(config)
	if err != nil {
		return err
	}
	if err := ds.ValidateSQLView(ctx, config.SQL); err != nil {
		return err
	}
	var valid bool
	if err := ds.sqlViewPool.QueryRow(ctx, query).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return datasource.ErrSQLViewIdentity
	}
	return nil
}

// QuerySQLView executes a feature query against a SQL View and returns GeoJSON features.
func (ds *DataSource) QuerySQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]json.RawMessage, error) {
	sql, args, err := ds.buildSQLViewListSQL(config, params)
	if err != nil {
		return nil, err
	}

	rows, err := ds.sqlViewPool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query sql view: %w", err)
	}
	defer rows.Close()

	var out []json.RawMessage
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, json.RawMessage(b))
	}

	return out, rows.Err()
}

// QuerySQLViewWKB executes a SQL View query and returns WKB geometry with properties for rendering.
func (ds *DataSource) QuerySQLViewWKB(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	sql, args, err := ds.buildSQLViewWKBSQL(config, params)
	if err != nil {
		return nil, err
	}

	rows, err := ds.sqlViewPool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query sql view wkb: %w", err)
	}
	defer rows.Close()

	var out []datasource.RenderFeature
	for rows.Next() {
		var geomBytes []byte
		var propsJSON map[string]interface{}
		var id any

		if err := rows.Scan(&geomBytes, &propsJSON, &id); err != nil {
			continue // Skip invalid rows
		}

		out = append(out, datasource.RenderFeature{
			ID:         datasource.StableRenderFeatureID(id, geomBytes, propsJSON),
			Geometry:   geomBytes,
			Properties: propsJSON,
		})
	}

	return out, rows.Err()
}

// CountSQLView returns the number of features matching the SQL View query.
func (ds *DataSource) CountSQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) (int, error) {
	sql, args, err := ds.buildSQLViewCountSQL(config, params)
	if err != nil {
		return 0, err
	}

	var count int
	if err := ds.sqlViewPool.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count sql view: %w", err)
	}

	return count, nil
}

// buildSQLViewListSQL builds a SQL query to list features from a SQL View.
func (ds *DataSource) buildSQLViewListSQL(config *datasource.SQLViewConfig, p datasource.QueryParams) (string, []any, error) {
	if config == nil {
		return "", nil, fmt.Errorf("SQL view configuration required")
	}
	if err := validateSQLStructure(config.SQL); err != nil {
		return "", nil, err
	}
	p = p.WithDateTimeFilter()
	p.SortBy = datasource.StableSort(p.SortBy, config.IDColumn)
	geom := quoteIdent(config.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = 4326
	}

	// Build geometry output expression
	geomExpr := fmt.Sprintf("v.%s", geom)
	if config.SRID != 0 && config.SRID != outSRID {
		geomExpr = fmt.Sprintf("ST_Transform(v.%s, %d)", geom, outSRID)
	}
	if p.SimplifyTolerance > 0 {
		geomExpr = fmt.Sprintf("ST_SimplifyPreserveTopology(%s, %.12g)", geomExpr, p.SimplifyTolerance)
	}

	// Build ID expression
	var idExpr string
	if config.IDColumn != "" {
		idExpr = fmt.Sprintf("v.%s", quoteIdent(config.IDColumn))
	} else {
		idExpr = "NULL"
	}

	// Build properties expression - exclude geometry and ID columns
	var propParts []string
	for _, prop := range config.Properties {
		if prop.Name != config.GeometryColumn && prop.Name != config.IDColumn && datasource.PropertySelected(prop.Name, p.Properties) {
			propParts = append(propParts, fmt.Sprintf("%s, v.%s", quoteLiteral(prop.Name), quoteIdent(prop.Name)))
		}
	}
	propExpr := "'{}'::jsonb"
	if len(propParts) > 0 {
		propExpr = fmt.Sprintf("jsonb_build_object(%s)", strings.Join(propParts, ", "))
	}

	var whereParts []string
	var args []any
	argPos := 1

	if len(p.FeatureIDs) > 0 {
		if config.IDColumn == "" {
			return "", nil, fmt.Errorf("SQL view requires an id_column for item lookup")
		}
		predicate, idArgs, next := datasource.FeatureIDPredicate(p.FeatureIDs, "v."+quoteIdent(config.IDColumn), argPos)
		whereParts = append(whereParts, predicate)
		args = append(args, idArgs...)
		argPos = next
	}

	// BBox filter
	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("v."+geom, config.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLPostGIS, TableAlias: "v", GeometryExpression: "v." + quoteIdent(config.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && strings.TrimSpace(p.Filter) != "" {
		allowed := make(map[string]struct{})
		for _, pr := range config.Properties {
			allowed[pr.Name] = struct{}{}
		}
		if config.IDColumn != "" {
			allowed[config.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}
		sourceSRID := config.SRID
		if sourceSRID == 0 {
			sourceSRID = 4326
		}

		filterSQL, fargs, next, err := filter.Compile(p.Filter, filter.Options{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        sourceSRID,
			AllowedProperties: allowed,
			GeometryProperty:  config.GeometryColumn,
		})
		if err != nil {
			return "", nil, err
		}
		if filterSQL != "" {
			whereParts = append(whereParts, filterSQL)
			args = append(args, fargs...)
			argPos = next
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
			orderItems = append(orderItems, fmt.Sprintf("v.%s %s", quoteIdent(s.Name), dir))
		}
		if len(orderItems) > 0 {
			orderSQL = "ORDER BY " + strings.Join(orderItems, ",")
		}
	} else if config.IDColumn != "" {
		orderSQL = fmt.Sprintf("ORDER BY v.%s", quoteIdent(config.IDColumn))
	}

	// Build LIMIT/OFFSET
	limitSQL := fmt.Sprintf("LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, p.Limit, p.Offset)

	// Build feature JSON expression
	featureExpr := fmt.Sprintf(
		`jsonb_build_object('type','Feature','id',%s,'geometry',ST_AsGeoJSON(%s)::jsonb,'properties',%s)`,
		idExpr,
		geomExpr,
		propExpr,
	)

	whereSQL := ""
	if len(whereParts) > 0 {
		whereSQL = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// Wrap user SQL in subquery
	sql := fmt.Sprintf(`SELECT %s AS feature FROM (%s) v %s %s %s`, featureExpr, config.SQL, whereSQL, orderSQL, limitSQL)
	return sql, args, nil
}

// buildSQLViewWKBSQL builds a SQL query that returns WKB geometry and properties from a SQL View.
func (ds *DataSource) buildSQLViewWKBSQL(config *datasource.SQLViewConfig, p datasource.QueryParams) (string, []any, error) {
	if config == nil {
		return "", nil, fmt.Errorf("SQL view configuration required")
	}
	if err := validateSQLStructure(config.SQL); err != nil {
		return "", nil, err
	}
	p = p.WithDateTimeFilter()
	geom := quoteIdent(config.GeometryColumn)

	outSRID := p.OutputSRID
	if outSRID == 0 {
		outSRID = config.SRID
	}
	if outSRID == 0 {
		outSRID = 4326
	}

	// Build geometry output expression
	geomExpr := fmt.Sprintf("v.%s", geom)
	if config.SRID != 0 && config.SRID != outSRID {
		geomExpr = fmt.Sprintf("ST_Transform(v.%s, %d)", geom, outSRID)
	}

	// Build properties expression - exclude geometry column
	var propParts []string
	for _, prop := range config.Properties {
		if prop.Name != config.GeometryColumn && prop.Name != config.IDColumn {
			propParts = append(propParts, fmt.Sprintf("%s, v.%s", quoteLiteral(prop.Name), quoteIdent(prop.Name)))
		}
	}
	propExpr := "'{}'::jsonb"
	if len(propParts) > 0 {
		propExpr = fmt.Sprintf("jsonb_build_object(%s)", strings.Join(propParts, ", "))
	}

	var whereParts []string
	var args []any
	argPos := 1

	if len(p.FeatureIDs) > 0 {
		if config.IDColumn == "" {
			return "", nil, fmt.Errorf("SQL view requires an id_column for item lookup")
		}
		predicate, idArgs, next := datasource.FeatureIDPredicate(p.FeatureIDs, "v."+quoteIdent(config.IDColumn), argPos)
		whereParts = append(whereParts, predicate)
		args = append(args, idArgs...)
		argPos = next
	}

	// BBox filter
	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("v."+geom, config.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLPostGIS, TableAlias: "v", GeometryExpression: "v." + quoteIdent(config.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && strings.TrimSpace(p.Filter) != "" {
		allowed := make(map[string]struct{})
		for _, pr := range config.Properties {
			allowed[pr.Name] = struct{}{}
		}
		if config.IDColumn != "" {
			allowed[config.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}
		sourceSRID := config.SRID
		if sourceSRID == 0 {
			sourceSRID = 4326
		}

		filterSQL, fargs, _, err := filter.Compile(p.Filter, filter.Options{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        sourceSRID,
			AllowedProperties: allowed,
			GeometryProperty:  config.GeometryColumn,
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
	idExpr := "NULL::text"
	if config.IDColumn != "" {
		idExpr = fmt.Sprintf("v.%s::text", quoteIdent(config.IDColumn))
	}
	sql := fmt.Sprintf(`SELECT ST_AsBinary(%s) AS geom, %s AS props, %s AS feature_id FROM (%s) v %s %s`,
		geomExpr, propExpr, idExpr, config.SQL, whereSQL, limitSQL)
	return sql, args, nil
}

// buildSQLViewCountSQL builds a SQL query to count features from a SQL View.
func (ds *DataSource) buildSQLViewCountSQL(config *datasource.SQLViewConfig, p datasource.QueryParams) (string, []any, error) {
	if config == nil {
		return "", nil, fmt.Errorf("SQL view configuration required")
	}
	if err := validateSQLStructure(config.SQL); err != nil {
		return "", nil, err
	}
	p = p.WithDateTimeFilter()
	geom := quoteIdent(config.GeometryColumn)

	var whereParts []string
	var args []any
	argPos := 1

	if len(p.FeatureIDs) > 0 {
		if config.IDColumn == "" {
			return "", nil, fmt.Errorf("SQL view requires an id_column for item lookup")
		}
		predicate, idArgs, next := datasource.FeatureIDPredicate(p.FeatureIDs, "v."+quoteIdent(config.IDColumn), argPos)
		whereParts = append(whereParts, predicate)
		args = append(args, idArgs...)
		argPos = next
	}

	// BBox filter
	if p.BBox != nil {
		predicate, bboxArgs, nextArg := buildBBoxPredicate("v."+geom, config.SRID, p.BBoxSRID, argPos, p.BBox)
		whereParts = append(whereParts, predicate)
		args = append(args, bboxArgs...)
		argPos = nextArg
	}

	// CQL2 filter
	if err := datasource.AppendQueryPredicate(p, datasource.PredicateOptions{Dialect: datasource.SQLPostGIS, TableAlias: "v", GeometryExpression: "v." + quoteIdent(config.GeometryColumn)}, &whereParts, &args, &argPos); err != nil {
		return "", nil, err
	}
	if !p.HasCompiledPredicate() && strings.TrimSpace(p.Filter) != "" {
		allowed := make(map[string]struct{})
		for _, pr := range config.Properties {
			allowed[pr.Name] = struct{}{}
		}
		if config.IDColumn != "" {
			allowed[config.IDColumn] = struct{}{}
		}

		filterSRID := p.FilterSRID
		if filterSRID == 0 {
			filterSRID = 4326
		}
		sourceSRID := config.SRID
		if sourceSRID == 0 {
			sourceSRID = 4326
		}

		filterSQL, fargs, _, err := filter.Compile(p.Filter, filter.Options{
			StartParamIndex:   argPos,
			FilterSRID:        filterSRID,
			SourceSRID:        sourceSRID,
			AllowedProperties: allowed,
			GeometryProperty:  config.GeometryColumn,
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

	sql := fmt.Sprintf(`SELECT COUNT(*) FROM (%s) v %s`, config.SQL, whereSQL)
	return sql, args, nil
}
