package store

import (
	"encoding/json"
	"time"
)

// ServiceType represents the type of data source service.
type ServiceType string

const (
	ServiceTypePostGIS      ServiceType = "postgis"
	ServiceTypeDuckDB       ServiceType = "duckdb"
	ServiceTypeGeoParquet   ServiceType = "geoparquet"
	ServiceTypeVectorFile   ServiceType = "vectorfile"
	ServiceTypeRasterFile   ServiceType = "rasterfile"
	ServiceTypeRasterMosaic ServiceType = "raster_mosaic"
)

// Workspace represents a logical grouping of services and layers.
type Workspace struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Service represents a data source connection within a workspace.
type Service struct {
	ID             string                `json:"id"`
	WorkspaceID    string                `json:"workspace_id"`
	Name           string                `json:"name"`
	Type           ServiceType           `json:"type"`
	ConnectionInfo json.RawMessage       `json:"connection_info,omitempty"`
	CacheSettings  *ServiceCacheSettings `json:"cache_settings,omitempty"`
	Enabled        bool                  `json:"enabled"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// ServiceCacheSettings allows per-service cache configuration overrides.
type ServiceCacheSettings struct {
	FeaturesEnabled *bool `json:"features_enabled,omitempty"`
	FeaturesTTLSec  *int  `json:"features_ttl_sec,omitempty"`
	TilesEnabled    *bool `json:"tiles_enabled,omitempty"`
	TilesTTLSec     *int  `json:"tiles_ttl_sec,omitempty"`
}

// PostGISConnectionInfo contains PostGIS-specific connection parameters.
type PostGISConnectionInfo struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Database string   `json:"database"`
	User     string   `json:"user"`
	Password string   `json:"password"`
	SSLMode  string   `json:"sslmode,omitempty"`
	Schemas  []string `json:"schemas,omitempty"` // Schemas to discover layers from (default: ["public"])
}

// DuckDBConnectionInfo contains DuckDB-specific connection parameters.
type DuckDBConnectionInfo struct {
	Path            string         `json:"path,omitempty"`
	ManagedImportID string         `json:"managed_import_id,omitempty"`
	SRID            int            `json:"srid,omitempty"`
	LayerSRIDs      map[string]int `json:"layer_srids,omitempty"`
}

// GeoParquetConnectionInfo contains GeoParquet-specific connection parameters.
type GeoParquetConnectionInfo struct {
	Path string `json:"path"`
}

// VectorFileConnectionInfo contains connection parameters for vector file datasources (Shapefile, GeoPackage, etc.).
type VectorFileConnectionInfo struct {
	Path           string            `json:"path"`                      // File path, HTTP URL, or S3 URI
	Layer          string            `json:"layer,omitempty"`           // Layer name (required for multi-layer formats like GeoPackage)
	GeometryColumn string            `json:"geometry_column,omitempty"` // Override auto-detected geometry column
	IDColumn       string            `json:"id_column,omitempty"`       // Override auto-detected ID column
	SRID           int               `json:"srid,omitempty"`            // Override auto-detected SRID (default: 4326)
	OpenOptions    map[string]string `json:"open_options,omitempty"`    // GDAL open options
}

// RasterFileConnectionInfo contains connection parameters for GeoTIFF/COG,
// CF-NetCDF, and GRIB2 services.
type RasterFileConnectionInfo struct {
	Path        string            `json:"path"` // Local path, allowlisted HTTPS URL, or allowlisted S3 URI.
	Driver      string            `json:"driver,omitempty"`
	Variables   []string          `json:"variables,omitempty"`
	OpenOptions map[string]string `json:"open_options,omitempty"`
}

type RasterMosaicGranule struct {
	Path      string   `json:"path"`
	Time      string   `json:"time,omitempty"`
	Elevation *float64 `json:"elevation,omitempty"`
	Priority  int      `json:"priority,omitempty"`
}

type RasterMosaicConnectionInfo struct {
	Name        string                `json:"name,omitempty"`
	Directory   string                `json:"directory,omitempty"`
	Pattern     string                `json:"pattern,omitempty"`
	Granules    []RasterMosaicGranule `json:"granules,omitempty"`
	MaxGranules int                   `json:"max_granules,omitempty"`
}

// Dimension represents a WMS dimension (e.g., time, elevation).
type Dimension struct {
	Name           string `json:"name"`                      // e.g., "time", "elevation"
	Units          string `json:"units"`                     // e.g., "ISO8601" for time
	SourceAxis     string `json:"source_axis,omitempty"`     // Coverage axis label used by WCS and raster readers.
	SourceProperty string `json:"source_property,omitempty"` // Feature attribute containing the dimension start/value.
	EndProperty    string `json:"end_property,omitempty"`    // Optional feature attribute containing an interval end.
	Default        string `json:"default,omitempty"`         // Default value
	MultipleValues bool   `json:"multiple_values,omitempty"` // Allow multiple values
	NearestValue   bool   `json:"nearest_value,omitempty"`   // Support nearest value matching
	Current        bool   `json:"current,omitempty"`         // Support "current" keyword
	Extent         string `json:"extent"`                    // Value extent, e.g., "2000-01-01T00:00:00Z/2000-01-01T00:01:00Z/PT5S"
}

// TileCacheParameterPolicy bounds the cache-key cardinality for portrayal
// parameters. Values outside these exact allowlists are rendered but not
// cached. Empty time/elevation lists therefore cache only the default value.
type TileCacheParameterPolicy struct {
	Styles         []string `json:"styles,omitempty"`
	Times          []string `json:"times,omitempty"`
	Elevations     []string `json:"elevations,omitempty"`
	MetatileFactor int      `json:"metatile_factor,omitempty"`
	GutterPixels   int      `json:"gutter_pixels,omitempty"`
}

// Layer represents a published feature type from a service.
type Layer struct {
	ID            string         `json:"id"`
	ServiceID     string         `json:"service_id"`
	SourceLayer   string         `json:"source_layer"`
	PublicID      string         `json:"public_id"`
	Title         string         `json:"title,omitempty"`
	Description   string         `json:"description,omitempty"`
	Enabled       bool           `json:"enabled"`
	CRSDefault    int            `json:"crs_default"`
	Dimensions    []*Dimension   `json:"dimensions,omitempty"` // WMS dimensions (time, elevation, etc.)
	IsSQLView     bool           `json:"is_sql_view"`
	SQLViewConfig *SQLViewConfig `json:"sql_view_config,omitempty"`
	// Public marks the layer as readable by anyone who can reach the service
	// (including anonymous requests on a public service), bypassing AllowedRoles.
	Public bool `json:"public"`
	// AllowedRoles, when non-empty, restricts read access to these workspace roles
	// (super_admin always allowed). Empty means any role with workspace access.
	AllowedRoles []string `json:"allowed_roles,omitempty"`
	// DefaultStyle and Styles bind workspace SLD styles for portrayal. Styles
	// contains advertised alternates; an empty DefaultStyle uses the built-in.
	DefaultStyle        string                    `json:"default_style,omitempty"`
	Styles              []string                  `json:"styles,omitempty"`
	NativeExtent        *SpatialExtent            `json:"native_extent,omitempty"`
	TileCacheQuotaBytes int64                     `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheParameters *TileCacheParameterPolicy `json:"tile_cache_parameters,omitempty"`
	TileCacheGeneration int64                     `json:"tile_cache_generation"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
}

type LayerGroupMember struct {
	Resource  string   `json:"resource"`
	Style     string   `json:"style,omitempty"`
	Opacity   *float64 `json:"opacity,omitempty"`
	Composite string   `json:"composite,omitempty"`
}

func (m LayerGroupMember) EffectiveOpacity() float64 {
	if m.Opacity == nil {
		return 1
	}
	return *m.Opacity
}

type LayerGroup struct {
	ID                  string             `json:"id"`
	WorkspaceID         string             `json:"workspace_id"`
	PublicID            string             `json:"public_id"`
	Title               string             `json:"title,omitempty"`
	Description         string             `json:"description,omitempty"`
	Enabled             bool               `json:"enabled"`
	Public              bool               `json:"public"`
	AllowedRoles        []string           `json:"allowed_roles,omitempty"`
	Members             []LayerGroupMember `json:"members"`
	DefaultStyle        string             `json:"default_style,omitempty"`
	Styles              []string           `json:"styles,omitempty"`
	NativeExtent        *SpatialExtent     `json:"native_extent,omitempty"`
	TileCacheQuotaBytes int64              `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheGeneration int64              `json:"tile_cache_generation"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

type CreateLayerGroupInput struct {
	WorkspaceID, PublicID, Title, Description string
	Enabled, Public                           bool
	AllowedRoles                              []string
	Members                                   []LayerGroupMember
	DefaultStyle                              string
	Styles                                    []string
	NativeExtent                              *SpatialExtent
	TileCacheQuotaBytes                       int64
}

type UpdateLayerGroupInput struct {
	PublicID, Title, Description *string
	Enabled, Public              *bool
	AllowedRoles                 *[]string
	Members                      *[]LayerGroupMember
	DefaultStyle                 *string
	Styles                       *[]string
	NativeExtent                 **SpatialExtent
	TileCacheQuotaBytes          *int64
}

// Coverage is a published WCS coverage backed by a raster source.
type Coverage struct {
	ID                   string               `json:"id"`
	WorkspaceID          string               `json:"workspace_id"`
	ServiceID            string               `json:"service_id"`
	SourceCoverage       string               `json:"source_coverage"`
	PublicID             string               `json:"public_id"`
	Title                string               `json:"title,omitempty"`
	Description          string               `json:"description,omitempty"`
	Enabled              bool                 `json:"enabled"`
	Public               bool                 `json:"public"`
	AllowedRoles         []string             `json:"allowed_roles,omitempty"`
	RangeFields          []CoverageRangeField `json:"range_fields,omitempty"`
	Dimensions           []*Dimension         `json:"dimensions,omitempty"`
	DefaultStyle         string               `json:"default_style,omitempty"`
	Styles               []string             `json:"styles,omitempty"`
	Resampling           string               `json:"resampling,omitempty"`
	WCS20CoverageSubtype string               `json:"wcs20_coverage_subtype"`
	NativeExtent         *SpatialExtent       `json:"native_extent,omitempty"`
	TileCacheQuotaBytes  int64                `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheGeneration  int64                `json:"tile_cache_generation"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
}

const (
	WCS20CoverageSubtypeRectifiedGrid = "RectifiedGridCoverage"
	WCS20CoverageSubtypeGrid          = "GridCoverage"
)

// SpatialExtent is a persisted native-CRS resource envelope used to constrain
// advertised matrix limits and background cache jobs.
type SpatialExtent struct {
	MinX  float64 `json:"min_x"`
	MinY  float64 `json:"min_y"`
	MaxX  float64 `json:"max_x"`
	MaxY  float64 `json:"max_y"`
	SRID  int     `json:"srid"`
	Stale bool    `json:"stale,omitempty"`
}

// CoverageRangeField is the publish-time override for one source raster band.
type CoverageRangeField struct {
	Band        int      `json:"band"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Definition  string   `json:"definition,omitempty"`
	UOM         string   `json:"uom,omitempty"`
	NilValues   []string `json:"nil_values,omitempty"`
}

// SQLViewConfig contains configuration for a SQL View layer.
type SQLViewConfig struct {
	SQL            string             `json:"sql"`
	GeometryColumn string             `json:"geometry_column"`
	GeometryType   string             `json:"geometry_type,omitempty"`
	SRID           int                `json:"srid,omitempty"`
	IDColumn       string             `json:"id_column,omitempty"`
	Properties     []*SQLViewProperty `json:"properties,omitempty"`
	ReadOnly       bool               `json:"read_only"` // Always true for SQL views
}

// SQLViewProperty describes a property/column in a SQL view.
type SQLViewProperty struct {
	Name string `json:"name"`
	Type string `json:"type"` // string, number, integer, boolean
}

// Style represents a stored SLD style for WMS rendering.
type Style struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	SLDBody     string    `json:"sld_body"`
	Format      string    `json:"format"` // sld_1.0.0, sld_1.1.0
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// StyleAsset describes a workspace-local ExternalGraphic payload.
type StyleAsset struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UpsertStyleAssetInput struct {
	WorkspaceID string
	Name        string
	ContentType string
	SizeBytes   int64
	SHA256      string
}

// WMSSettings holds per-workspace WMS configuration.
type WMSSettings struct {
	Enabled                bool     `json:"enabled"`
	Public                 bool     `json:"public"`
	MaxWidth               int      `json:"max_width,omitempty"`
	MaxHeight              int      `json:"max_height,omitempty"`
	MaxPixels              int      `json:"max_pixels,omitempty"`
	MaxRenderFeatures      int      `json:"max_render_features,omitempty"`
	MaxRenderVertices      int      `json:"max_render_vertices,omitempty"`
	SimplifyEnabled        *bool    `json:"simplify_enabled,omitempty"`
	SimplifyPixelTolerance float64  `json:"simplify_pixel_tolerance,omitempty"`
	Title                  string   `json:"title,omitempty"`
	Abstract               string   `json:"abstract,omitempty"`
	DefaultStyle           string   `json:"default_style,omitempty"`
	Extensions             []string `json:"extensions,omitempty"`
}

var validWMSExtensions = map[string]struct{}{
	"dynamic-raster": {}, "advanced-labels": {}, "rendering-transformations": {}, "compositing": {}, "z-order": {}, "dynamic-style": {}, "remote-graphics": {},
}

func ValidWMSExtension(value string) bool {
	_, ok := validWMSExtensions[value]
	return ok
}

// WFSSettings holds per-workspace WFS configuration.
type WFSSettings struct {
	Enabled        bool   `json:"enabled"`
	Public         bool   `json:"public"`
	MaxFeatures    int    `json:"max_features,omitempty"`
	DefaultCount   int    `json:"default_count,omitempty"`
	MaxOffset      int    `json:"max_offset,omitempty"`
	CountTimeoutMS int    `json:"count_timeout_ms,omitempty"`
	Title          string `json:"title,omitempty"`
	Abstract       string `json:"abstract,omitempty"`
}

// OGCAPISettings holds per-workspace OGC API Features configuration.
type OGCAPISettings struct {
	Enabled      bool   `json:"enabled"`
	Public       bool   `json:"public"`
	LimitDefault int    `json:"limit_default,omitempty"`
	LimitMax     int    `json:"limit_max,omitempty"`
	MaxOffset    int    `json:"max_offset,omitempty"`
	Title        string `json:"title,omitempty"`
	Abstract     string `json:"abstract,omitempty"`
}

// WCSSettings holds per-workspace WCS configuration. Zero limits inherit the
// corresponding process-wide defaults.
type WCSSettings struct {
	Enabled              bool     `json:"enabled"`
	Public               bool     `json:"public"`
	Title                string   `json:"title,omitempty"`
	Abstract             string   `json:"abstract,omitempty"`
	MaxCells             int64    `json:"max_cells,omitempty"`
	MaxOutputBytes       int64    `json:"max_output_bytes,omitempty"`
	ProcessingTimeoutMS  int      `json:"processing_timeout_ms,omitempty"`
	Extensions           []string `json:"extensions,omitempty"`
	AllowedSubsettingCRS []string `json:"allowed_subsetting_crs,omitempty"`
	AllowedOutputCRS     []string `json:"allowed_output_crs,omitempty"`
	InterpolationMethods []string `json:"interpolation_methods,omitempty"`
	OutputFormats        []string `json:"output_formats,omitempty"`
	MaxDimensions        int      `json:"max_dimensions,omitempty"`
	MaxAxisValues        int64    `json:"max_axis_values,omitempty"`
	MaxSourceGranules    int      `json:"max_source_granules,omitempty"`
	MaxTemporaryBytes    int64    `json:"max_temporary_bytes,omitempty"`
}

// OGCTilesAPIVectorSettings configures vector tile (MVT) support.
type OGCTilesAPIVectorSettings struct {
	Enabled bool     `json:"enabled"`
	Formats []string `json:"formats,omitempty"` // Default: ["application/vnd.mapbox-vector-tile"]
}

// OGCTilesAPIMapSettings configures map tile (raster) support.
type OGCTilesAPIMapSettings struct {
	Enabled bool     `json:"enabled"`
	Formats []string `json:"formats,omitempty"` // Default: ["image/png", "image/jpeg", "image/webp"]
}

// OGCTilesAPIInnerSettings holds the detailed tile configuration.
type OGCTilesAPIInnerSettings struct {
	DatasetMapLayerGroupID    string                    `json:"dataset_map_layer_group_id,omitempty"`
	TileMatrixSets            []string                  `json:"tile_matrix_sets,omitempty"` // Default: ["WebMercatorQuad"]
	VectorTiles               OGCTilesAPIVectorSettings `json:"vector_tiles"`
	MapTiles                  OGCTilesAPIMapSettings    `json:"map_tiles"`
	CacheEnabled              bool                      `json:"cache_enabled"`
	MaxFeatures               int                       `json:"max_features,omitempty"`
	MaxVertices               int                       `json:"max_vertices,omitempty"`
	MaxTileBytes              int                       `json:"max_tile_bytes,omitempty"`
	PersistentCacheQuotaBytes int64                     `json:"persistent_cache_quota_bytes,omitempty"`
}

// OGCTilesAPISettings holds per-workspace OGC API Tiles configuration.
type OGCTilesAPISettings struct {
	Enabled  bool                     `json:"enabled"`
	Public   bool                     `json:"public"`
	Title    string                   `json:"title,omitempty"`
	Abstract string                   `json:"abstract,omitempty"`
	Versions []string                 `json:"versions,omitempty"` // Default: ["1.0.0"]
	Settings OGCTilesAPIInnerSettings `json:"settings"`
}

// WMTSSettings holds independently activatable WMTS protocol metadata. Tile
// formats and matrix sets deliberately reuse OGCTilesAPIInnerSettings.
type WMTSSettings struct {
	Enabled                 bool   `json:"enabled"`
	Public                  bool   `json:"public"`
	Title                   string `json:"title,omitempty"`
	Abstract                string `json:"abstract,omitempty"`
	FeatureInfoEnabled      bool   `json:"feature_info_enabled"`
	VectorTilesEnabled      bool   `json:"vector_tiles_enabled,omitempty"`
	TileMatrixLimitsEnabled bool   `json:"tile_matrix_limits_enabled"`
	ProviderName            string `json:"provider_name,omitempty"`
	ProviderSite            string `json:"provider_site,omitempty"`
	ContactName             string `json:"contact_name,omitempty"`
	ContactPosition         string `json:"contact_position,omitempty"`
	ContactEmail            string `json:"contact_email,omitempty"`
}

func DefaultWMTSSettings() WMTSSettings {
	return WMTSSettings{FeatureInfoEnabled: true, ProviderName: "neoserver"}
}

// DefaultOGCTilesAPISettings returns the default OGC Tiles API settings.
func DefaultOGCTilesAPISettings() OGCTilesAPISettings {
	return OGCTilesAPISettings{
		Enabled:  false,
		Public:   false,
		Versions: []string{"1.0.0"},
		Settings: OGCTilesAPIInnerSettings{
			TileMatrixSets: []string{"WebMercatorQuad", "WorldCRS84Quad"},
			VectorTiles: OGCTilesAPIVectorSettings{
				Enabled: true,
				Formats: []string{"application/vnd.mapbox-vector-tile"},
			},
			MapTiles: OGCTilesAPIMapSettings{
				Enabled: true,
				Formats: []string{"image/png", "image/jpeg", "image/webp"},
			},
			CacheEnabled: true,
		},
	}
}

// WorkspaceSettings aggregates all OGC service settings for a workspace.
type WorkspaceSettings struct {
	WMS         WMSSettings         `json:"wms"`
	WFS         WFSSettings         `json:"wfs"`
	OGCAPI      OGCAPISettings      `json:"ogc_api"`
	OGCTilesAPI OGCTilesAPISettings `json:"ogc_tiles_api"`
	WCS         WCSSettings         `json:"wcs"`
	WMTS        WMTSSettings        `json:"wmts"`
}

// Role represents a permission role (built-in or custom).
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
}

// APIKey represents an API key for authentication.
type APIKey struct {
	ID          string     `json:"id"`
	KeyHash     string     `json:"-"` // Never expose in JSON
	KeyPrefix   string     `json:"key_prefix"`
	OwnerName   string     `json:"owner_name"`
	OwnerEmail  string     `json:"owner_email,omitempty"`
	WorkspaceID *string    `json:"workspace_id,omitempty"` // nil for global keys
	RoleID      string     `json:"role_id"`
	Name        string     `json:"name"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Revoked     bool       `json:"revoked"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ClaimRoleMapping maps OIDC/JWT claims to workspace roles.
type ClaimRoleMapping struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"` // "*" for global mappings
	ClaimName   string    `json:"claim_name"`
	ClaimValue  string    `json:"claim_value"`
	RoleID      string    `json:"role_id"`
	Priority    int       `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
}

// SigningKey represents an internal JWT signing key.
type SigningKey struct {
	ID         string    `json:"id"`
	PrivateKey []byte    `json:"-"` // Never expose in JSON
	PublicKey  []byte    `json:"public_key,omitempty"`
	Algorithm  string    `json:"algorithm"`
	CreatedAt  time.Time `json:"created_at"`
	IsActive   bool      `json:"is_active"`
}

// CasbinRule represents a policy rule for Casbin.
type CasbinRule struct {
	ID    int    `json:"id"`
	PType string `json:"ptype"`
	V0    string `json:"v0,omitempty"`
	V1    string `json:"v1,omitempty"`
	V2    string `json:"v2,omitempty"`
	V3    string `json:"v3,omitempty"`
	V4    string `json:"v4,omitempty"`
	V5    string `json:"v5,omitempty"`
}

// CreateWorkspaceInput contains the fields for creating a new workspace.
type CreateWorkspaceInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// UpdateWorkspaceInput contains the fields for updating a workspace.
type UpdateWorkspaceInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// CreateServiceInput contains the fields for creating a new service.
type CreateServiceInput struct {
	WorkspaceID    string                `json:"workspace_id"`
	Name           string                `json:"name"`
	Type           ServiceType           `json:"type"`
	ConnectionInfo json.RawMessage       `json:"connection_info"`
	CacheSettings  *ServiceCacheSettings `json:"cache_settings,omitempty"`
	Enabled        bool                  `json:"enabled"`
}

// UpdateServiceInput contains the fields for updating a service.
type UpdateServiceInput struct {
	Name           *string               `json:"name,omitempty"`
	ConnectionInfo *json.RawMessage      `json:"connection_info,omitempty"`
	CacheSettings  *ServiceCacheSettings `json:"cache_settings,omitempty"`
	Enabled        *bool                 `json:"enabled,omitempty"`
}

// CreateLayerInput contains the fields for creating a new layer.
type CreateLayerInput struct {
	ServiceID           string                    `json:"service_id"`
	SourceLayer         string                    `json:"source_layer"`
	PublicID            string                    `json:"public_id"`
	Title               string                    `json:"title,omitempty"`
	Description         string                    `json:"description,omitempty"`
	Enabled             bool                      `json:"enabled"`
	CRSDefault          int                       `json:"crs_default,omitempty"`
	Dimensions          []*Dimension              `json:"dimensions,omitempty"`
	IsSQLView           bool                      `json:"is_sql_view"`
	SQLViewConfig       *SQLViewConfig            `json:"sql_view_config,omitempty"`
	Public              bool                      `json:"public"`
	AllowedRoles        []string                  `json:"allowed_roles,omitempty"`
	DefaultStyle        string                    `json:"default_style,omitempty"`
	Styles              []string                  `json:"styles,omitempty"`
	NativeExtent        *SpatialExtent            `json:"native_extent,omitempty"`
	TileCacheQuotaBytes int64                     `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheParameters *TileCacheParameterPolicy `json:"tile_cache_parameters,omitempty"`
}

// CreateCoverageInput contains immutable source identity and publish metadata.
type CreateCoverageInput struct {
	WorkspaceID          string               `json:"workspace_id,omitempty"`
	ServiceID            string               `json:"service_id"`
	SourceCoverage       string               `json:"source_coverage"`
	PublicID             string               `json:"public_id"`
	Title                string               `json:"title,omitempty"`
	Description          string               `json:"description,omitempty"`
	Enabled              bool                 `json:"enabled"`
	Public               bool                 `json:"public"`
	AllowedRoles         []string             `json:"allowed_roles,omitempty"`
	RangeFields          []CoverageRangeField `json:"range_fields,omitempty"`
	Dimensions           []*Dimension         `json:"dimensions,omitempty"`
	DefaultStyle         string               `json:"default_style,omitempty"`
	Styles               []string             `json:"styles,omitempty"`
	Resampling           string               `json:"resampling,omitempty"`
	WCS20CoverageSubtype string               `json:"wcs20_coverage_subtype,omitempty"`
	NativeExtent         *SpatialExtent       `json:"native_extent,omitempty"`
	TileCacheQuotaBytes  int64                `json:"tile_cache_quota_bytes,omitempty"`
}

// UpdateCoverageInput contains mutable coverage publication metadata.
type UpdateCoverageInput struct {
	PublicID             *string              `json:"public_id,omitempty"`
	Title                *string              `json:"title,omitempty"`
	Description          *string              `json:"description,omitempty"`
	Enabled              *bool                `json:"enabled,omitempty"`
	Public               *bool                `json:"public,omitempty"`
	AllowedRoles         []string             `json:"allowed_roles,omitempty"`
	RangeFields          []CoverageRangeField `json:"range_fields,omitempty"`
	Dimensions           []*Dimension         `json:"dimensions,omitempty"`
	DefaultStyle         *string              `json:"default_style,omitempty"`
	Styles               []string             `json:"styles,omitempty"`
	Resampling           *string              `json:"resampling,omitempty"`
	WCS20CoverageSubtype *string              `json:"wcs20_coverage_subtype,omitempty"`
	NativeExtent         *SpatialExtent       `json:"native_extent,omitempty"`
	TileCacheQuotaBytes  *int64               `json:"tile_cache_quota_bytes,omitempty"`
	MarkExtentStale      *bool                `json:"-"`
}

// UpdateLayerInput contains the fields for updating a layer.
type UpdateLayerInput struct {
	PublicID            *string                   `json:"public_id,omitempty"`
	Title               *string                   `json:"title,omitempty"`
	Description         *string                   `json:"description,omitempty"`
	Enabled             *bool                     `json:"enabled,omitempty"`
	CRSDefault          *int                      `json:"crs_default,omitempty"`
	Dimensions          []*Dimension              `json:"dimensions,omitempty"`
	SQLViewConfig       *SQLViewConfig            `json:"sql_view_config,omitempty"`
	Public              *bool                     `json:"public,omitempty"`
	AllowedRoles        []string                  `json:"allowed_roles,omitempty"`
	DefaultStyle        *string                   `json:"default_style,omitempty"`
	Styles              []string                  `json:"styles,omitempty"`
	NativeExtent        *SpatialExtent            `json:"native_extent,omitempty"`
	TileCacheQuotaBytes *int64                    `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheParameters *TileCacheParameterPolicy `json:"tile_cache_parameters,omitempty"`
	MarkExtentStale     *bool                     `json:"-"`
}

// CreateAPIKeyInput contains the fields for creating an API key.
type CreateAPIKeyInput struct {
	OwnerName   string     `json:"owner_name"`
	OwnerEmail  string     `json:"owner_email,omitempty"`
	WorkspaceID *string    `json:"workspace_id,omitempty"`
	RoleID      string     `json:"role_id"`
	Name        string     `json:"name"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// CreateAPIKeyOutput contains the response after creating an API key.
type CreateAPIKeyOutput struct {
	APIKey
	Key string `json:"key"` // Only returned on creation
}

// CreateClaimMappingInput contains the fields for creating a claim mapping.
type CreateClaimMappingInput struct {
	WorkspaceID string `json:"workspace_id"` // "*" for global
	ClaimName   string `json:"claim_name"`
	ClaimValue  string `json:"claim_value"`
	RoleID      string `json:"role_id"`
	Priority    int    `json:"priority,omitempty"`
}

// CreateRoleInput contains the fields for creating a custom role.
type CreateRoleInput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CreateStyleInput contains the fields for creating a new style.
type CreateStyleInput struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	SLDBody     string `json:"sld_body"`
	Format      string `json:"format,omitempty"` // Defaults to sld_1.1.0
}

// UpdateStyleInput contains the fields for updating a style.
type UpdateStyleInput struct {
	Name        *string `json:"name,omitempty"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	SLDBody     *string `json:"sld_body,omitempty"`
	Format      *string `json:"format,omitempty"`
}

// WFSStoredQuery represents a WFS stored query definition.
type WFSStoredQuery struct {
	ID              string                    `json:"id"`
	WorkspaceID     string                    `json:"workspace_id"`
	QueryID         string                    `json:"query_id"` // The WFS stored query ID (like "urn:my-query")
	Title           string                    `json:"title,omitempty"`
	Abstract        string                    `json:"abstract,omitempty"`
	Parameters      []WFSStoredQueryParameter `json:"parameters,omitempty"`
	QueryExpression string                    `json:"query_expression"`
	Language        string                    `json:"language,omitempty"`
	ReturnTypes     []string                  `json:"return_types,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
}

// WFSStoredQueryParameter represents a parameter in a WFS stored query.
type WFSStoredQueryParameter struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// CreateWFSStoredQueryInput contains the fields for creating a WFS stored query.
type CreateWFSStoredQueryInput struct {
	WorkspaceID     string                    `json:"workspace_id"`
	QueryID         string                    `json:"query_id"`
	Title           string                    `json:"title,omitempty"`
	Abstract        string                    `json:"abstract,omitempty"`
	Parameters      []WFSStoredQueryParameter `json:"parameters,omitempty"`
	QueryExpression string                    `json:"query_expression"`
	Language        string                    `json:"language,omitempty"`
	ReturnTypes     []string                  `json:"return_types,omitempty"`
}
