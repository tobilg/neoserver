package conf

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server          Server
	Store           Store
	Database        Database
	Datasource      Datasource
	Paging          Paging
	Metadata        Metadata
	Website         Website
	WMS             WMS
	WFS             WFS
	WCS             WCS
	MosaicCatalog   MosaicCatalog
	Importer        Importer
	Tiles           Tiles
	WMTS            WMTS
	Auth            Auth
	Audit           Audit
	Cache           Cache
	PersistentCache PersistentCache
	Observability   Observability
	Collections     map[string]CollectionOverride
}

type Observability struct {
	OTel  OTel
	Pprof Pprof
}
type OTel struct {
	Enabled            bool
	ServiceName        string
	ShutdownTimeoutSec int
}
type Pprof struct{ Enabled bool }

// Store configuration for the encrypted DuckDB backing store.
type Store struct {
	Path          string // Path to the DuckDB database file
	EncryptionKey string // Encryption key (32 bytes hex-encoded or raw)
}

type Server struct {
	HttpHost          string
	HttpPort          int
	UrlBase           string
	BasePath          string
	CORSOrigins       string
	Debug             bool
	ReadTimeoutSec    int
	WriteTimeoutSec   int
	DisableUI         bool
	AdminUI           bool
	Devel             bool
	MaxBodyBytes      int64    // Maximum request body size in bytes (0 = use default)
	TrustedProxyCIDRs []string // Proxies permitted to supply forwarding headers
}

type Database struct {
	DatabaseURL     string
	Schemas         []string
	TableIncludes   []string
	TableExcludes   []string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Datasource configuration for file-based data sources.
type Datasource struct {
	// AllowedPaths is a deny-by-default glob allowlist (doublestar syntax) of local
	// paths and remote URLs that file datasources may read. Example entries:
	// "./data/**", "/srv/geo/**", "s3://my-bucket/**", "https://host/data/**".
	AllowedPaths      []string
	RemoteCachePath   string
	RemoteMaxBytes    int64
	RemoteTimeoutSec  int
	MosaicMaxGranules int
}

type Paging struct {
	LimitDefault int
	LimitMax     int
	MaxOffset    int
	// CountTimeoutMS bounds the exact count used for OGC API Features
	// numberMatched. A timed-out count is omitted without failing the page.
	CountTimeoutMS int
}

type Metadata struct {
	Title       string
	Description string
}

type Website struct {
	BasemapURL string `mapstructure:"BasemapUrl"`
}

// WMS configuration for the OGC WMS 1.3.0 service.
type WMS struct {
	Enabled                       bool
	MaxWidth                      int
	MaxHeight                     int
	MaxPixels                     int
	MaxRenderFeatures             int
	MaxRenderVertices             int
	MaxRasterMemoryBytes          int64
	MaxConcurrentRenders          int
	RenderQueueTimeoutMS          int
	SimplifyEnabled               bool
	SimplifyPixelTolerance        float64
	DefaultStyle                  string
	SLDPath                       string
	Styles                        map[string]WMSStyle
	Extensions                    []string
	MaxEnvVariables               int
	MaxEnvValueBytes              int
	MaxContourLevels              int
	MaxGroupDepth                 int
	MaxMosaicGranulesPerRender    int
	MaxStyleBodyBytes             int64
	MaxStyleAssetBytes            int64
	StyleAssetPath                string
	ExternalGraphicCachePath      string
	ExternalGraphicAllowedOrigins []string
	ExternalGraphicTimeoutMS      int
	MaxExternalGraphicDimension   int
	MaxExternalGraphicsPerRender  int
	MaxProcessFeatures            int
	MaxProcessCells               int
	MaxProcessMemoryBytes         int64
	ProcessTimeoutMS              int
	FontPaths                     []string
	MaxOutputBytes                int64
}

// WMSStyle defines default styling for layers.
type WMSStyle struct {
	FillColor   string
	FillOpacity float64
	StrokeColor string
	StrokeWidth float64
	PointRadius float64
}

// WFS configuration for the OGC WFS 2.0 service.
type WFS struct {
	Enabled                 bool
	AllowAnonymousMutations bool
	MaxFeatures             int
	DefaultCount            int
	DefaultSRS              string
	AppNamespace            string
	AppNamespacePrefix      string
	MaxOffset               int
	CountTimeoutMS          int
	MaxLockExpirySec        int
	MaxLocksPerWorkspace    int
	MaxLocksPerPrincipal    int
	MaxFeaturesPerLock      int
	LockCleanupIntervalSec  int
	MaxVersionedFeatures    int
	MaxVersionsPerFeature   int
	MaxOutputBytes          int64
	MaxTemporaryBytes       int64
	TemporaryDirectory      string
	MaxConcurrentExports    int
	ExportQueueTimeoutMS    int
	ExportTimeoutMS         int
}

// WCS configuration for the OGC WCS 2.1/2.0 GET/KVP service.
type WCS struct {
	Enabled               bool
	MaxCells              int64
	MaxOutputBytes        int64
	MaxDimensions         int
	MaxAxisValues         int64
	MaxSourceGranules     int
	MaxTemporaryBytes     int64
	TemporaryDirectory    string
	MaxConcurrentRequests int
	QueueTimeoutMS        int
	ProcessingTimeoutMS   int
}

// MosaicCatalog configures the operational granule index used by managed
// raster mosaics. The database is separate from the catalog but reuses the
// catalog encryption key.
type MosaicCatalog struct {
	Enabled            bool
	DatabasePath       string
	WorkerCount        int
	MaxConcurrentJobs  int
	MaxGranulesPerJob  int64
	BatchSize          int
	MaxRetries         int
	ShutdownTimeoutSec int
}

// Importer configures durable, managed vector import jobs. Imported data is
// materialized into one encrypted DuckDB database per successful import.
type Importer struct {
	Enabled                bool
	Root                   string
	TemporaryDirectory     string
	WorkerCount            int
	MaxConcurrentJobs      int
	MaxUploadBytes         int64
	MaxRetainedSourceBytes int64
	UploadTimeoutSec       int
	UploadIdleTimeoutSec   int
	MaxSourceBytes         int64
	MaxExpandedBytes       int64
	MaxArchiveFiles        int
	MaxLayers              int
	MaxFeatures            int64
	PreviewFeatures        int
	TransformTimeoutSec    int
	MaxRetries             int
	ShutdownTimeoutSec     int
}

// Tiles configuration for the OGC API - Tiles service.
type Tiles struct {
	Enabled                  bool
	MaxZoom                  int // Default: 22
	MinZoom                  int // Default: 0
	TileSize                 int // Default: 4096 (MVT extent)
	MaxFeatures              int
	MaxVertices              int
	MaxTileBytes             int
	MaxConcurrentRenders     int
	RenderQueueTimeoutMS     int
	StatementTimeoutMS       int
	MaxMetatileFactor        int
	MaxGutterPixels          int
	MaxParameterCombinations int
}

// WMTS configures the process-wide WMTS 1.0 route and hard FeatureInfo limit.
type WMTS struct {
	Enabled               bool
	MaxFeatureInfoResults int
}

// PersistentCache configures the optional durable L2 tile cache.
type PersistentCache struct {
	Enabled                bool
	Backend                string
	DatabasePath           string
	MaxBytes               int64
	AccessFlushIntervalSec int
	MaintenanceIntervalSec int
	Filesystem             PersistentCacheFilesystem
	S3                     PersistentCacheS3
	Jobs                   PersistentCacheJobs
}

type PersistentCacheFilesystem struct {
	Root string
}

type PersistentCacheS3 struct {
	Bucket               string
	Region               string
	Prefix               string
	Endpoint             string
	UsePathStyle         bool
	ServerSideEncryption string
	KMSKeyID             string `mapstructure:"KMSKeyID"`
	// Single-active-node guard: an ownership marker object in the prefix
	// refuses startup when another live server already owns it.
	OwnershipEnabled          bool
	OwnershipCheckIntervalSec int
}

type PersistentCacheJobs struct {
	WorkerCount          int
	MaxConcurrentJobs    int
	MaxConcurrentRenders int
	MaxTilesPerJob       int64
	MaxRetries           int
	ShutdownTimeoutSec   int
}

// Auth configuration for authentication.
type Auth struct {
	Enabled      bool
	Method       string // "none", "apikey", "basic", "oidc"
	ApiKey       string
	RequireHTTPS bool
	Users        map[string]string // username -> password for basic auth
	DefaultRole  string            // role granted to static-apikey / basic-auth principals (default "viewer")
	// AllowAPIKeyInQuery permits API keys via the ?apikey= query parameter. Disabled
	// by default because query strings leak into access logs, proxies, and browser
	// history. Enable only for OGC clients that can authenticate solely via the URL.
	AllowAPIKeyInQuery bool
	Session            SessionConfig
	OIDC               OIDCConfig
}

type SessionConfig struct {
	TTLSec             int
	IdleTimeoutSec     int
	CleanupIntervalSec int
}

// Audit configures the durable security/change-event audit database.
type Audit struct {
	Enabled            bool
	DatabasePath       string
	RetentionDays      int
	RecordReads        bool
	MaxFieldBytes      int
	CleanupIntervalSec int
	ShutdownTimeoutSec int
	WriteTimeoutSec    int
	RetryIntervalSec   int
	RetryBatchSize     int
}

// OIDCConfig holds OpenID Connect configuration.
type OIDCConfig struct {
	IssuerURL           string
	ClientID            string
	RequiredScopes      []string
	SkipIssuerCheck     bool
	BrowserLoginEnabled bool
	BrowserScopes       []string
	GroupClaims         []string
}

// Cache configuration for in-memory caching.
type Cache struct {
	Enabled               bool
	Profile               string
	MaxMemoryMB           int
	CounterRatio          int
	BufferItems           int
	MetricsEnabled        bool
	TTLTickSec            int
	FillCoalescingEnabled bool
	FillTimeoutSec        int
	Capabilities          CacheCapabilities
	Collections           CacheCollections
	Features              CacheFeatures
	Tiles                 CacheTiles
	Counts                CacheCounts
}

type CacheCounts struct {
	Enabled     bool
	TTLSec      int
	MaxMemoryMB int
}

// CacheCapabilities configures GetCapabilities caching (WMS/WFS).
type CacheCapabilities struct {
	Enabled      bool
	TTLSec       int // TTL in seconds
	MaxMemoryMB  int
	MaxEntrySize int
}

// CacheCollections configures OGC collections caching.
type CacheCollections struct {
	Enabled      bool
	TTLSec       int
	MaxMemoryMB  int
	MaxEntrySize int
}

// CacheFeatures configures feature query result caching.
type CacheFeatures struct {
	Enabled      bool
	TTLSec       int
	MaxEntries   int
	MaxEntrySize int // Maximum size of a single entry in bytes
	MaxMemoryMB  int
}

// CacheTiles configures WMS tile caching.
type CacheTiles struct {
	Enabled      bool
	TTLSec       int
	MaxMemoryMB  int
	MaxEntrySize int
}

// CollectionOverride allows customizing auto-discovered collections.
// Keyed by discovered id (default: "schema.table").
type CollectionOverride struct {
	CollectionID      string `mapstructure:"CollectionId"`
	Title             string
	Description       string
	Hidden            bool
	IDColumn          string
	GeometryColumn    string
	PropertiesInclude []string
	PropertiesExclude []string
	CRSDefault        int   `mapstructure:"CrsDefault"`
	CRSAllowed        []int `mapstructure:"CrsAllowed"`
}

func setDefaults() {
	viper.SetDefault("Server.HttpHost", "127.0.0.1")
	viper.SetDefault("Server.HttpPort", 9000)
	viper.SetDefault("Server.UrlBase", "")
	viper.SetDefault("Server.BasePath", "")
	viper.SetDefault("Server.CORSOrigins", "*")
	viper.SetDefault("Server.Debug", false)
	viper.SetDefault("Server.ReadTimeoutSec", 5)
	viper.SetDefault("Server.WriteTimeoutSec", 30)
	viper.SetDefault("Server.DisableUI", false)
	viper.SetDefault("Server.AdminUI", true)
	viper.SetDefault("Server.MaxBodyBytes", 10<<20) // 10 MB
	viper.SetDefault("Server.TrustedProxyCIDRs", []string{})

	// Store defaults (backing store for workspace configuration)
	viper.SetDefault("Store.Path", "./data/neoserver.db")
	viper.SetDefault("Store.EncryptionKey", "")

	viper.SetDefault("Database.DatabaseURL", "")
	viper.SetDefault("Database.Schemas", []string{"public"})
	viper.SetDefault("Database.TableIncludes", []string{})
	viper.SetDefault("Database.TableExcludes", []string{})
	// Datasource file/URL allowlist (deny-by-default globs). Local data dir allowed by default.
	viper.SetDefault("Datasource.AllowedPaths", []string{"./data/**", "data/**"})
	viper.SetDefault("Datasource.RemoteCachePath", "./data/remote-cache")
	viper.SetDefault("Datasource.RemoteMaxBytes", int64(1<<30))
	viper.SetDefault("Datasource.RemoteTimeoutSec", 60)
	viper.SetDefault("Datasource.MosaicMaxGranules", 100000)

	viper.SetDefault("Database.MaxOpenConns", 25)
	viper.SetDefault("Database.MaxIdleConns", 5)
	viper.SetDefault("Database.ConnMaxLifetimeSec", 3600)
	viper.SetDefault("Database.ConnMaxIdleTimeSec", 600)

	viper.SetDefault("Paging.LimitDefault", 10)
	viper.SetDefault("Paging.LimitMax", 1000)
	viper.SetDefault("Paging.MaxOffset", 0)
	viper.SetDefault("Paging.CountTimeoutMS", 5000)

	viper.SetDefault("Metadata.Title", App.Name)
	viper.SetDefault("Metadata.Description", "PostGIS OGC API - Features server")

	viper.SetDefault("Website.BasemapUrl", "")

	// WMS defaults
	viper.SetDefault("WMS.Enabled", false)
	viper.SetDefault("WMS.MaxWidth", 4096)
	viper.SetDefault("WMS.MaxHeight", 4096)
	viper.SetDefault("WMS.MaxPixels", 4096*4096)
	viper.SetDefault("WMS.MaxRenderFeatures", 50000)
	viper.SetDefault("WMS.MaxRenderVertices", 5000000)
	viper.SetDefault("WMS.MaxRasterMemoryBytes", int64(256<<20))
	viper.SetDefault("WMS.MaxConcurrentRenders", 0)
	viper.SetDefault("WMS.RenderQueueTimeoutMS", 2000)
	viper.SetDefault("WMS.SimplifyEnabled", true)
	viper.SetDefault("WMS.SimplifyPixelTolerance", 0.5)
	viper.SetDefault("WMS.DefaultStyle", "default")
	viper.SetDefault("WMS.SLDPath", "./styles")
	viper.SetDefault("WMS.Extensions", []string{})
	viper.SetDefault("WMS.MaxEnvVariables", 32)
	viper.SetDefault("WMS.MaxEnvValueBytes", 256)
	viper.SetDefault("WMS.MaxContourLevels", 32)
	viper.SetDefault("WMS.MaxGroupDepth", 8)
	viper.SetDefault("WMS.MaxMosaicGranulesPerRender", 256)
	viper.SetDefault("WMS.MaxStyleBodyBytes", int64(1<<20))
	viper.SetDefault("WMS.MaxStyleAssetBytes", int64(5<<20))
	viper.SetDefault("WMS.StyleAssetPath", "./data/style-assets")
	viper.SetDefault("WMS.ExternalGraphicCachePath", "./data/external-graphic-cache")
	viper.SetDefault("WMS.ExternalGraphicAllowedOrigins", []string{})
	viper.SetDefault("WMS.ExternalGraphicTimeoutMS", 5000)
	viper.SetDefault("WMS.MaxExternalGraphicDimension", 4096)
	viper.SetDefault("WMS.MaxExternalGraphicsPerRender", 64)
	viper.SetDefault("WMS.MaxProcessFeatures", 100000)
	viper.SetDefault("WMS.MaxProcessCells", 4096*4096)
	viper.SetDefault("WMS.MaxProcessMemoryBytes", int64(256<<20))
	viper.SetDefault("WMS.ProcessTimeoutMS", 10000)
	viper.SetDefault("WMS.FontPaths", []string{})
	viper.SetDefault("WMS.MaxOutputBytes", int64(256<<20))
	viper.SetDefault("WMS.Styles", map[string]any{
		"default": map[string]any{
			"FillColor":   "#3388ff",
			"FillOpacity": 0.5,
			"StrokeColor": "#3388ff",
			"StrokeWidth": 2.0,
			"PointRadius": 5.0,
		},
	})

	// WFS defaults
	viper.SetDefault("WFS.Enabled", false)
	viper.SetDefault("WFS.AllowAnonymousMutations", false)
	viper.SetDefault("WFS.MaxFeatures", 10000)
	viper.SetDefault("WFS.DefaultCount", 100)
	viper.SetDefault("WFS.DefaultSRS", "urn:ogc:def:crs:EPSG::4326")
	viper.SetDefault("WFS.AppNamespace", "http://neoserver/app")
	viper.SetDefault("WFS.AppNamespacePrefix", "app")
	viper.SetDefault("WFS.MaxOffset", 0)
	viper.SetDefault("WFS.CountTimeoutMS", 5000)
	viper.SetDefault("WFS.MaxLockExpirySec", 3600)
	viper.SetDefault("WFS.MaxLocksPerWorkspace", 1000)
	viper.SetDefault("WFS.MaxLocksPerPrincipal", 100)
	viper.SetDefault("WFS.MaxFeaturesPerLock", 10000)
	viper.SetDefault("WFS.LockCleanupIntervalSec", 60)
	viper.SetDefault("WFS.MaxVersionedFeatures", 100000)
	viper.SetDefault("WFS.MaxVersionsPerFeature", 100)
	viper.SetDefault("WFS.MaxOutputBytes", int64(256<<20))
	viper.SetDefault("WFS.MaxTemporaryBytes", int64(1<<30))
	viper.SetDefault("WFS.TemporaryDirectory", "./data/wfs-tmp")
	viper.SetDefault("WFS.MaxConcurrentExports", 2)
	viper.SetDefault("WFS.ExportQueueTimeoutMS", 2000)
	viper.SetDefault("WFS.ExportTimeoutMS", 30000)

	// WCS defaults
	viper.SetDefault("WCS.Enabled", false)
	viper.SetDefault("WCS.MaxCells", int64(16777216))
	viper.SetDefault("WCS.MaxOutputBytes", int64(268435456))
	viper.SetDefault("WCS.MaxDimensions", 8)
	viper.SetDefault("WCS.MaxAxisValues", int64(1000000))
	viper.SetDefault("WCS.MaxSourceGranules", 10000)
	viper.SetDefault("WCS.MaxTemporaryBytes", int64(1073741824))
	viper.SetDefault("WCS.TemporaryDirectory", "./data/wcs-tmp")
	viper.SetDefault("WCS.MaxConcurrentRequests", 0)
	viper.SetDefault("WCS.QueueTimeoutMS", 2000)
	viper.SetDefault("WCS.ProcessingTimeoutMS", 30000)

	// Managed mosaic operational catalog. Static mosaics continue to work when
	// this is disabled.
	viper.SetDefault("MosaicCatalog.Enabled", false)
	viper.SetDefault("MosaicCatalog.DatabasePath", "./data/mosaic-index.duckdb")
	viper.SetDefault("MosaicCatalog.WorkerCount", 2)
	viper.SetDefault("MosaicCatalog.MaxConcurrentJobs", 1)
	viper.SetDefault("MosaicCatalog.MaxGranulesPerJob", int64(1000000))
	viper.SetDefault("MosaicCatalog.BatchSize", 1000)
	viper.SetDefault("MosaicCatalog.MaxRetries", 3)
	viper.SetDefault("MosaicCatalog.ShutdownTimeoutSec", 30)

	// Managed imports are opt-in because they copy source data into server-owned
	// storage and run bounded background transformations.
	viper.SetDefault("Importer.Enabled", false)
	viper.SetDefault("Importer.Root", "./data/imports")
	viper.SetDefault("Importer.TemporaryDirectory", "./data/imports/tmp")
	viper.SetDefault("Importer.WorkerCount", 1)
	viper.SetDefault("Importer.MaxConcurrentJobs", 1)
	viper.SetDefault("Importer.MaxUploadBytes", int64(1<<30))
	viper.SetDefault("Importer.MaxRetainedSourceBytes", int64(10<<30))
	viper.SetDefault("Importer.UploadTimeoutSec", 900)
	viper.SetDefault("Importer.UploadIdleTimeoutSec", 60)
	viper.SetDefault("Importer.MaxSourceBytes", int64(1<<30))
	viper.SetDefault("Importer.MaxExpandedBytes", int64(4<<30))
	viper.SetDefault("Importer.MaxArchiveFiles", 1024)
	viper.SetDefault("Importer.MaxLayers", 256)
	viper.SetDefault("Importer.MaxFeatures", int64(10000000))
	viper.SetDefault("Importer.PreviewFeatures", 100)
	viper.SetDefault("Importer.TransformTimeoutSec", 3600)
	viper.SetDefault("Importer.MaxRetries", 3)
	viper.SetDefault("Importer.ShutdownTimeoutSec", 30)

	// Tiles defaults (OGC API - Tiles)
	viper.SetDefault("Tiles.Enabled", false)
	viper.SetDefault("Tiles.MaxZoom", 22)
	viper.SetDefault("Tiles.MinZoom", 0)
	viper.SetDefault("Tiles.TileSize", 4096)
	viper.SetDefault("Tiles.MaxFeatures", 50000)
	viper.SetDefault("Tiles.MaxVertices", 5000000)
	viper.SetDefault("Tiles.MaxTileBytes", 10<<20)
	viper.SetDefault("Tiles.MaxConcurrentRenders", 0)
	viper.SetDefault("Tiles.RenderQueueTimeoutMS", 2000)
	viper.SetDefault("Tiles.StatementTimeoutMS", 30000)
	viper.SetDefault("Tiles.MaxMetatileFactor", 4)
	viper.SetDefault("Tiles.MaxGutterPixels", 64)
	viper.SetDefault("Tiles.MaxParameterCombinations", 256)

	viper.SetDefault("WMTS.Enabled", false)
	viper.SetDefault("WMTS.MaxFeatureInfoResults", 10)

	// Persistent tile cache defaults. Disabled keeps all prior deployments on
	// the in-memory-only behavior until an operator explicitly opts in.
	viper.SetDefault("PersistentCache.Enabled", false)
	viper.SetDefault("PersistentCache.Backend", "filesystem")
	viper.SetDefault("PersistentCache.DatabasePath", "./data/tile-cache.duckdb")
	viper.SetDefault("PersistentCache.MaxBytes", int64(0))
	viper.SetDefault("PersistentCache.AccessFlushIntervalSec", 30)
	viper.SetDefault("PersistentCache.MaintenanceIntervalSec", 60)
	viper.SetDefault("PersistentCache.Filesystem.Root", "./data/tile-cache")
	viper.SetDefault("PersistentCache.S3.Prefix", "neoserver-tiles")
	viper.SetDefault("PersistentCache.S3.UsePathStyle", false)
	viper.SetDefault("PersistentCache.S3.OwnershipEnabled", true)
	viper.SetDefault("PersistentCache.S3.OwnershipCheckIntervalSec", 60)
	viper.SetDefault("PersistentCache.Jobs.WorkerCount", 2)
	viper.SetDefault("PersistentCache.Jobs.MaxConcurrentJobs", 1)
	viper.SetDefault("PersistentCache.Jobs.MaxConcurrentRenders", 1)
	viper.SetDefault("PersistentCache.Jobs.MaxTilesPerJob", int64(1000000))
	viper.SetDefault("PersistentCache.Jobs.MaxRetries", 3)
	viper.SetDefault("PersistentCache.Jobs.ShutdownTimeoutSec", 30)

	// Auth defaults
	viper.SetDefault("Auth.Enabled", false)
	viper.SetDefault("Auth.Method", "none")
	viper.SetDefault("Auth.ApiKey", "")
	viper.SetDefault("Auth.RequireHTTPS", true)
	viper.SetDefault("Auth.Users", map[string]string{})
	viper.SetDefault("Auth.DefaultRole", "viewer")
	viper.SetDefault("Auth.AllowAPIKeyInQuery", false)
	viper.SetDefault("Auth.Session.TTLSec", 43200)
	viper.SetDefault("Auth.Session.IdleTimeoutSec", 3600)
	viper.SetDefault("Auth.Session.CleanupIntervalSec", 300)
	viper.SetDefault("Auth.OIDC.IssuerURL", "")
	viper.SetDefault("Auth.OIDC.ClientID", "")
	viper.SetDefault("Auth.OIDC.RequiredScopes", []string{})
	viper.SetDefault("Auth.OIDC.SkipIssuerCheck", false)
	viper.SetDefault("Auth.OIDC.BrowserLoginEnabled", true)
	viper.SetDefault("Auth.OIDC.BrowserScopes", []string{"openid", "profile", "email"})
	viper.SetDefault("Auth.OIDC.GroupClaims", []string{"groups", "roles", "group", "role", "realm_access.roles", "cognito:groups"})

	viper.SetDefault("Audit.Enabled", true)
	viper.SetDefault("Audit.DatabasePath", "./data/audit.duckdb")
	viper.SetDefault("Audit.RetentionDays", 90)
	viper.SetDefault("Audit.RecordReads", false)
	viper.SetDefault("Audit.MaxFieldBytes", 256)
	viper.SetDefault("Audit.CleanupIntervalSec", 86400)
	viper.SetDefault("Audit.ShutdownTimeoutSec", 10)
	viper.SetDefault("Audit.WriteTimeoutSec", 2)
	viper.SetDefault("Audit.RetryIntervalSec", 5)
	viper.SetDefault("Audit.RetryBatchSize", 100)

	// Cache defaults (in-memory caching)
	viper.SetDefault("Cache.Enabled", true)
	viper.SetDefault("Cache.Profile", "balanced")
	viper.SetDefault("Cache.MaxMemoryMB", 512)
	viper.SetDefault("Cache.CounterRatio", 10)
	viper.SetDefault("Cache.BufferItems", 64)
	viper.SetDefault("Cache.MetricsEnabled", true)
	viper.SetDefault("Cache.TTLTickSec", 5)
	viper.SetDefault("Cache.FillCoalescingEnabled", true)
	viper.SetDefault("Cache.FillTimeoutSec", 60)
	viper.SetDefault("Cache.Capabilities.Enabled", true)
	viper.SetDefault("Cache.Capabilities.TTLSec", 3600) // 1 hour
	viper.SetDefault("Cache.Capabilities.MaxMemoryMB", 0)
	viper.SetDefault("Cache.Capabilities.MaxEntrySize", 1*1024*1024)
	viper.SetDefault("Cache.Collections.Enabled", true)
	viper.SetDefault("Cache.Collections.TTLSec", 1800) // 30 minutes
	viper.SetDefault("Cache.Collections.MaxMemoryMB", 0)
	viper.SetDefault("Cache.Collections.MaxEntrySize", 4*1024*1024)
	viper.SetDefault("Cache.Features.Enabled", true)
	viper.SetDefault("Cache.Features.TTLSec", 300) // 5 minutes
	viper.SetDefault("Cache.Features.MaxEntries", 1000)
	viper.SetDefault("Cache.Features.MaxEntrySize", 10*1024*1024) // 10MB
	viper.SetDefault("Cache.Features.MaxMemoryMB", 0)
	viper.SetDefault("Cache.Tiles.Enabled", true)
	viper.SetDefault("Cache.Tiles.TTLSec", 900) // 15 minutes
	viper.SetDefault("Cache.Tiles.MaxMemoryMB", 0)
	viper.SetDefault("Cache.Tiles.MaxEntrySize", 10*1024*1024)
	viper.SetDefault("Cache.Counts.Enabled", true)
	viper.SetDefault("Cache.Counts.TTLSec", 60)
	viper.SetDefault("Cache.Counts.MaxMemoryMB", 0)
	viper.SetDefault("Observability.OTel.Enabled", false)
	viper.SetDefault("Observability.OTel.ServiceName", App.Name)
	viper.SetDefault("Observability.OTel.ShutdownTimeoutSec", 5)
	viper.SetDefault("Observability.Pprof.Enabled", false)

	viper.SetDefault("Collections", map[string]any{})
}

// Load loads configuration from TOML + environment, following duckdb-tileserver's conventions.
func Load(configFilename string, debug bool, devel bool) (Config, error) {
	viper.Reset()
	setDefaults()

	if debug {
		viper.Set("Server.Debug", true)
	}
	viper.Set("Server.Devel", devel)

	viper.SetEnvPrefix(App.EnvPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	isExplicit := configFilename != ""
	if isExplicit {
		viper.SetConfigFile(configFilename)
	} else {
		// SetConfigName takes the name WITHOUT extension
		viper.SetConfigName(App.Name)
		viper.SetConfigType("toml")
		viper.AddConfigPath("./config")
		viper.AddConfigPath("/config")
		viper.AddConfigPath("/etc")
	}

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) && !isExplicit {
			// OK - use defaults/env only
		} else {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	// Back-compat: accept DATABASE_URL (common) in addition to prefixed env vars.
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" && viper.GetString("Database.DatabaseURL") == "" {
		viper.Set("Database.DatabaseURL", dbURL)
	}

	// Support NEOSRV_STORE_KEY as a shorthand for Store.EncryptionKey
	if storeKey := os.Getenv("NEOSRV_STORE_KEY"); storeKey != "" && viper.GetString("Store.EncryptionKey") == "" {
		viper.Set("Store.EncryptionKey", storeKey)
	}

	// Parse AUTH_USERS environment variable for basic auth: "user1:pass1,user2:pass2"
	if authUsers := os.Getenv("AUTH_USERS"); authUsers != "" {
		users := parseAuthUsers(authUsers)
		if len(users) > 0 {
			viper.Set("Auth.Users", users)
		}
	}

	// Decode
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	applyCacheProfile(&cfg.Cache)

	// Derived durations (keep TOML simple: ints in seconds)
	cfg.Database.ConnMaxLifetime = time.Duration(viper.GetInt("Database.ConnMaxLifetimeSec")) * time.Second
	cfg.Database.ConnMaxIdleTime = time.Duration(viper.GetInt("Database.ConnMaxIdleTimeSec")) * time.Second

	cfg.Server.BasePath = normalizeBasePath(cfg.Server.BasePath)
	if cfg.Server.UrlBase == "" && isLoopbackHost(cfg.Server.HttpHost) {
		cfg.Server.UrlBase = "http://" + net.JoinHostPort(cfg.Server.HttpHost, fmt.Sprintf("%d", cfg.Server.HttpPort))
	}
	return cfg, validate(cfg)
}

func isLoopbackHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func normalizeBasePath(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// guard against env vars like "\"/api\"" or "\"\""
	s = strings.Trim(s, `"`)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimRight(s, "/")
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

func applyCacheProfile(cache *Cache) {
	if cache == nil || cache.MaxMemoryMB <= 0 {
		return
	}
	cache.Profile = strings.ToLower(strings.TrimSpace(cache.Profile))
	weights := [5]int{16, 16, 208, 256, 16}
	switch cache.Profile {
	case "feature-heavy":
		weights = [5]int{16, 16, 307, 157, 16}
	case "map-heavy":
		weights = [5]int{16, 16, 128, 336, 16}
	}
	values := [5]*int{&cache.Capabilities.MaxMemoryMB, &cache.Collections.MaxMemoryMB, &cache.Features.MaxMemoryMB, &cache.Tiles.MaxMemoryMB, &cache.Counts.MaxMemoryMB}
	remaining := cache.MaxMemoryMB
	for i := 0; i < len(values)-1; i++ {
		if *values[i] == 0 {
			*values[i] = max(1, cache.MaxMemoryMB*weights[i]/512)
		}
		remaining -= *values[i]
	}
	if *values[4] == 0 {
		*values[4] = max(1, remaining)
	}
}

func validate(cfg Config) error {
	if cfg.Server.HttpPort <= 0 || cfg.Server.HttpPort > 65535 {
		return fmt.Errorf("invalid Server.HttpPort: %d", cfg.Server.HttpPort)
	}
	for _, cidr := range cfg.Server.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(cidr)); err != nil {
			return fmt.Errorf("invalid Server.TrustedProxyCIDRs entry %q: %w", cidr, err)
		}
	}
	if cfg.Paging.LimitDefault <= 0 {
		return fmt.Errorf("invalid Paging.LimitDefault: %d", cfg.Paging.LimitDefault)
	}
	if cfg.Paging.LimitMax < cfg.Paging.LimitDefault {
		return fmt.Errorf("invalid Paging.LimitMax: %d < LimitDefault", cfg.Paging.LimitMax)
	}
	if cfg.Paging.CountTimeoutMS < 0 {
		return fmt.Errorf("invalid Paging.CountTimeoutMS: %d", cfg.Paging.CountTimeoutMS)
	}
	// Validate WMS settings
	if cfg.WMS.Enabled {
		if cfg.WMS.MaxWidth <= 0 {
			return fmt.Errorf("invalid WMS.MaxWidth: %d", cfg.WMS.MaxWidth)
		}
		if cfg.WMS.MaxHeight <= 0 {
			return fmt.Errorf("invalid WMS.MaxHeight: %d", cfg.WMS.MaxHeight)
		}
		if cfg.WMS.MaxPixels <= 0 {
			return fmt.Errorf("invalid WMS.MaxPixels: %d", cfg.WMS.MaxPixels)
		}
		if cfg.WMS.MaxRenderFeatures < 0 || cfg.WMS.MaxRenderVertices < 0 || cfg.WMS.MaxRasterMemoryBytes < 0 {
			return errors.New("WMS render feature and vertex limits cannot be negative")
		}
		if cfg.WMS.RenderQueueTimeoutMS < 0 || cfg.WMS.SimplifyPixelTolerance < 0 {
			return errors.New("invalid WMS render queue or simplification settings")
		}
		if cfg.WMS.MaxEnvVariables < 0 || cfg.WMS.MaxEnvValueBytes < 0 || cfg.WMS.MaxContourLevels < 0 || cfg.WMS.MaxGroupDepth < 0 || cfg.WMS.MaxMosaicGranulesPerRender < 0 {
			return errors.New("WMS portrayal limits cannot be negative")
		}
		if cfg.WMS.MaxStyleBodyBytes < 0 || cfg.WMS.MaxStyleAssetBytes < 0 || cfg.WMS.ExternalGraphicTimeoutMS < 0 || cfg.WMS.MaxExternalGraphicDimension < 0 || cfg.WMS.MaxExternalGraphicsPerRender < 0 {
			return errors.New("WMS style and external graphic limits cannot be negative")
		}
		if cfg.WMS.MaxProcessFeatures < 0 || cfg.WMS.MaxProcessCells < 0 || cfg.WMS.MaxProcessMemoryBytes < 0 || cfg.WMS.ProcessTimeoutMS < 0 {
			return errors.New("WMS process limits cannot be negative")
		}
		if cfg.WMS.MaxOutputBytes < 0 {
			return errors.New("WMS.MaxOutputBytes cannot be negative")
		}
		validExtensions := map[string]bool{"dynamic-raster": true, "advanced-labels": true, "rendering-transformations": true, "compositing": true, "z-order": true, "dynamic-style": true, "remote-graphics": true}
		seenExtensions := map[string]bool{}
		for _, extension := range cfg.WMS.Extensions {
			normalized := strings.ToLower(strings.TrimSpace(extension))
			if extension != normalized || !validExtensions[normalized] || seenExtensions[normalized] {
				return fmt.Errorf("invalid, non-canonical, or duplicate WMS extension %q", extension)
			}
			seenExtensions[normalized] = true
		}
	}
	if cfg.Datasource.MosaicMaxGranules < 0 {
		return errors.New("Datasource.MosaicMaxGranules cannot be negative")
	}
	if cfg.Tiles.Enabled || cfg.WMTS.Enabled || cfg.PersistentCache.Enabled {
		if cfg.Tiles.MinZoom < 0 || cfg.Tiles.MaxZoom < cfg.Tiles.MinZoom || cfg.Tiles.MaxZoom > 24 {
			return fmt.Errorf("invalid Tiles zoom range: %d..%d", cfg.Tiles.MinZoom, cfg.Tiles.MaxZoom)
		}
		if cfg.Tiles.TileSize <= 0 || cfg.Tiles.MaxFeatures <= 0 || cfg.Tiles.MaxVertices <= 0 || cfg.Tiles.MaxTileBytes <= 0 {
			return errors.New("Tiles size, feature, vertex, and output limits must be positive")
		}
		if cfg.Tiles.MaxConcurrentRenders < 0 || cfg.Tiles.RenderQueueTimeoutMS <= 0 || cfg.Tiles.StatementTimeoutMS <= 0 {
			return errors.New("invalid Tiles concurrency or timeout setting")
		}
		if cfg.Tiles.MaxMetatileFactor < 0 || cfg.Tiles.MaxGutterPixels < 0 || cfg.Tiles.MaxParameterCombinations < 0 {
			return errors.New("invalid Tiles metatile, gutter, or parameter-combination limit")
		}
	}
	if cfg.WMTS.Enabled && cfg.WMTS.MaxFeatureInfoResults <= 0 {
		return errors.New("WMTS.MaxFeatureInfoResults must be positive")
	}
	if cfg.PersistentCache.Enabled {
		backend := strings.ToLower(strings.TrimSpace(cfg.PersistentCache.Backend))
		if backend != "filesystem" && backend != "s3" {
			return fmt.Errorf("PersistentCache.Backend must be filesystem or s3, got %q", cfg.PersistentCache.Backend)
		}
		if cfg.PersistentCache.DatabasePath == "" || cfg.PersistentCache.MaxBytes <= 0 ||
			cfg.PersistentCache.AccessFlushIntervalSec <= 0 || cfg.PersistentCache.MaintenanceIntervalSec <= 0 {
			return errors.New("persistent cache database, quota, and maintenance intervals must be configured")
		}
		if backend == "filesystem" && strings.TrimSpace(cfg.PersistentCache.Filesystem.Root) == "" {
			return errors.New("PersistentCache.Filesystem.Root is required")
		}
		if backend == "s3" && (strings.TrimSpace(cfg.PersistentCache.S3.Bucket) == "" || strings.TrimSpace(cfg.PersistentCache.S3.Region) == "") {
			return errors.New("PersistentCache.S3.Bucket and Region are required")
		}
		sse := cfg.PersistentCache.S3.ServerSideEncryption
		if sse != "" && sse != "AES256" && sse != "aws:kms" {
			return errors.New("PersistentCache.S3.ServerSideEncryption must be empty, AES256, or aws:kms")
		}
		if backend == "s3" && cfg.PersistentCache.S3.OwnershipEnabled {
			if cfg.PersistentCache.S3.OwnershipCheckIntervalSec <= 0 {
				return errors.New("PersistentCache.S3.OwnershipCheckIntervalSec must be positive")
			}
		}
		jobs := cfg.PersistentCache.Jobs
		if jobs.WorkerCount <= 0 || jobs.MaxConcurrentJobs <= 0 || jobs.MaxConcurrentRenders <= 0 ||
			jobs.MaxTilesPerJob <= 0 || jobs.MaxRetries < 0 || jobs.ShutdownTimeoutSec <= 0 {
			return errors.New("persistent cache job limits must be positive")
		}
	}
	if cfg.WCS.Enabled {
		if cfg.WCS.MaxCells <= 0 || cfg.WCS.MaxOutputBytes <= 0 || cfg.WCS.MaxDimensions < 2 ||
			cfg.WCS.MaxAxisValues <= 0 || cfg.WCS.MaxSourceGranules <= 0 || cfg.WCS.MaxTemporaryBytes <= 0 ||
			strings.TrimSpace(cfg.WCS.TemporaryDirectory) == "" || cfg.WCS.QueueTimeoutMS <= 0 ||
			cfg.WCS.ProcessingTimeoutMS <= 0 || cfg.WCS.MaxConcurrentRequests < 0 {
			return errors.New("WCS limits and timeouts must be positive")
		}
	}
	if cfg.MosaicCatalog.Enabled {
		mosaic := cfg.MosaicCatalog
		if strings.TrimSpace(mosaic.DatabasePath) == "" || mosaic.WorkerCount <= 0 || mosaic.MaxConcurrentJobs <= 0 ||
			mosaic.MaxGranulesPerJob <= 0 || mosaic.BatchSize <= 0 || mosaic.MaxRetries < 0 || mosaic.ShutdownTimeoutSec <= 0 {
			return errors.New("mosaic catalog database and job limits must be configured")
		}
	}
	if cfg.Importer.Enabled {
		item := cfg.Importer
		if item.MaxRetainedSourceBytes < 0 {
			return errors.New("importer retained-source budget must be positive (0 uses the 10 GiB default)")
		}
		if item.UploadTimeoutSec < 0 || item.UploadTimeoutSec > 86400 || item.UploadIdleTimeoutSec < 0 || item.UploadIdleTimeoutSec > 3600 {
			return errors.New("importer upload timeout must be 0..86400 seconds and idle timeout 0..3600 seconds (0 uses defaults)")
		}
		if strings.TrimSpace(item.Root) == "" || strings.TrimSpace(item.TemporaryDirectory) == "" ||
			item.WorkerCount <= 0 || item.MaxConcurrentJobs <= 0 || item.MaxUploadBytes <= 0 ||
			item.MaxSourceBytes <= 0 || item.MaxExpandedBytes <= 0 || item.MaxArchiveFiles <= 0 ||
			item.MaxLayers <= 0 || item.MaxFeatures <= 0 || item.PreviewFeatures <= 0 ||
			item.TransformTimeoutSec <= 0 || item.MaxRetries < 0 || item.ShutdownTimeoutSec <= 0 {
			return errors.New("importer paths, limits, and timeouts must be configured")
		}
	}
	if cfg.Audit.Enabled {
		item := cfg.Audit
		if strings.TrimSpace(item.DatabasePath) == "" || item.RetentionDays <= 0 ||
			item.MaxFieldBytes <= 0 || item.CleanupIntervalSec <= 0 || item.ShutdownTimeoutSec <= 0 ||
			item.WriteTimeoutSec <= 0 || item.RetryIntervalSec <= 0 || item.RetryBatchSize <= 0 || item.RetryBatchSize > 1000 {
			return errors.New("audit database, retention, field, and timeout settings must be configured")
		}
		if filepath.Clean(item.DatabasePath) == filepath.Clean(cfg.Store.Path) {
			return errors.New("Audit.DatabasePath must be separate from Store.Path")
		}
	}
	if cfg.Cache.Enabled {
		switch strings.ToLower(cfg.Cache.Profile) {
		case "balanced", "feature-heavy", "map-heavy":
		default:
			return fmt.Errorf("invalid Cache.Profile: %q", cfg.Cache.Profile)
		}
		if cfg.Cache.MaxMemoryMB <= 0 || cfg.Cache.CounterRatio <= 0 || cfg.Cache.BufferItems <= 0 || cfg.Cache.TTLTickSec <= 0 || cfg.Cache.FillTimeoutSec <= 0 {
			return errors.New("cache memory, counter ratio, buffer size, TTL tick, and fill timeout must be positive")
		}
		parts := []struct {
			name        string
			enabled     bool
			ttl, memory int
		}{
			{"capabilities", cfg.Cache.Capabilities.Enabled, cfg.Cache.Capabilities.TTLSec, cfg.Cache.Capabilities.MaxMemoryMB},
			{"collections", cfg.Cache.Collections.Enabled, cfg.Cache.Collections.TTLSec, cfg.Cache.Collections.MaxMemoryMB},
			{"features", cfg.Cache.Features.Enabled, cfg.Cache.Features.TTLSec, cfg.Cache.Features.MaxMemoryMB},
			{"tiles", cfg.Cache.Tiles.Enabled, cfg.Cache.Tiles.TTLSec, cfg.Cache.Tiles.MaxMemoryMB},
			{"counts", cfg.Cache.Counts.Enabled, cfg.Cache.Counts.TTLSec, cfg.Cache.Counts.MaxMemoryMB},
		}
		total := 0
		for _, part := range parts {
			if part.enabled && (part.ttl <= 0 || part.memory <= 0) {
				return fmt.Errorf("Cache.%s TTL and memory must be positive", part.name)
			}
			if part.enabled {
				total += part.memory
			}
		}
		if total > cfg.Cache.MaxMemoryMB {
			return fmt.Errorf("cache sub-budgets (%d MB) exceed Cache.MaxMemoryMB (%d MB)", total, cfg.Cache.MaxMemoryMB)
		}
		for name, size := range map[string]int{"capabilities": cfg.Cache.Capabilities.MaxEntrySize, "collections": cfg.Cache.Collections.MaxEntrySize, "features": cfg.Cache.Features.MaxEntrySize, "tiles": cfg.Cache.Tiles.MaxEntrySize} {
			if size < 0 {
				return fmt.Errorf("Cache.%s MaxEntrySize cannot be negative", name)
			}
		}
	}
	if !isLoopbackHost(cfg.Server.HttpHost) && cfg.Server.UrlBase == "" {
		return errors.New("Server.UrlBase is required when Server.HttpHost is not loopback")
	}
	if cfg.Server.UrlBase != "" {
		u, err := url.Parse(cfg.Server.UrlBase)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("Server.UrlBase must be an absolute HTTP(S) URL")
		}
	}
	// Validate WFS settings
	if cfg.WFS.Enabled {
		if cfg.WFS.AllowAnonymousMutations && cfg.Auth.Enabled {
			return errors.New("WFS.AllowAnonymousMutations requires Auth.Enabled=false")
		}
		if cfg.WFS.AppNamespacePrefix != "" && !isASCIIXMLName(cfg.WFS.AppNamespacePrefix) {
			return fmt.Errorf("invalid WFS.AppNamespacePrefix: %q", cfg.WFS.AppNamespacePrefix)
		}
		if cfg.WFS.AppNamespace != "" {
			namespace, err := url.Parse(cfg.WFS.AppNamespace)
			if err != nil || namespace.Scheme == "" {
				return fmt.Errorf("invalid WFS.AppNamespace: %q", cfg.WFS.AppNamespace)
			}
		}
		if cfg.WFS.MaxFeatures <= 0 {
			return fmt.Errorf("invalid WFS.MaxFeatures: %d", cfg.WFS.MaxFeatures)
		}
		if cfg.WFS.DefaultCount <= 0 {
			return fmt.Errorf("invalid WFS.DefaultCount: %d", cfg.WFS.DefaultCount)
		}
		if cfg.WFS.DefaultCount > cfg.WFS.MaxFeatures {
			return fmt.Errorf("WFS.DefaultCount (%d) cannot exceed WFS.MaxFeatures (%d)", cfg.WFS.DefaultCount, cfg.WFS.MaxFeatures)
		}
		quotasConfigured := cfg.WFS.MaxLockExpirySec != 0 || cfg.WFS.MaxLocksPerWorkspace != 0 || cfg.WFS.MaxLocksPerPrincipal != 0 ||
			cfg.WFS.MaxFeaturesPerLock != 0 || cfg.WFS.LockCleanupIntervalSec != 0 || cfg.WFS.MaxVersionedFeatures != 0 || cfg.WFS.MaxVersionsPerFeature != 0
		if quotasConfigured && (cfg.WFS.MaxLockExpirySec <= 0 || cfg.WFS.MaxLocksPerWorkspace <= 0 || cfg.WFS.MaxLocksPerPrincipal <= 0 ||
			cfg.WFS.MaxFeaturesPerLock <= 0 || cfg.WFS.LockCleanupIntervalSec <= 0 || cfg.WFS.MaxVersionedFeatures <= 0 || cfg.WFS.MaxVersionsPerFeature <= 0) {
			return errors.New("WFS lock and version quotas must be positive")
		}
		exportsConfigured := cfg.WFS.MaxOutputBytes != 0 || cfg.WFS.MaxTemporaryBytes != 0 || strings.TrimSpace(cfg.WFS.TemporaryDirectory) != "" ||
			cfg.WFS.MaxConcurrentExports != 0 || cfg.WFS.ExportQueueTimeoutMS != 0 || cfg.WFS.ExportTimeoutMS != 0
		if exportsConfigured && (cfg.WFS.MaxOutputBytes <= 0 || cfg.WFS.MaxTemporaryBytes <= 0 || strings.TrimSpace(cfg.WFS.TemporaryDirectory) == "" ||
			cfg.WFS.MaxConcurrentExports <= 0 || cfg.WFS.ExportQueueTimeoutMS <= 0 || cfg.WFS.ExportTimeoutMS <= 0) {
			return errors.New("WFS export limits, directory, concurrency, and timeouts must be positive")
		}
	}
	// Validate Auth settings
	if cfg.Auth.Session.TTLSec < 0 || cfg.Auth.Session.IdleTimeoutSec < 0 || cfg.Auth.Session.CleanupIntervalSec < 0 {
		return errors.New("Auth.Session TTL, idle timeout, and cleanup interval cannot be negative")
	}
	if cfg.Auth.Session.TTLSec > 0 && cfg.Auth.Session.IdleTimeoutSec > cfg.Auth.Session.TTLSec {
		return errors.New("Auth.Session.IdleTimeoutSec cannot exceed Auth.Session.TTLSec")
	}
	if cfg.Auth.OIDC.BrowserLoginEnabled && cfg.Auth.OIDC.IssuerURL != "" {
		hasOpenID := false
		for _, scope := range cfg.Auth.OIDC.BrowserScopes {
			if scope == "openid" {
				hasOpenID = true
				break
			}
		}
		if !hasOpenID {
			return errors.New("Auth.OIDC.BrowserScopes must include openid when browser login is enabled")
		}
	}
	if cfg.Auth.Enabled {
		switch cfg.Auth.Method {
		case "none", "apikey", "basic", "oidc":
			// valid
		default:
			return fmt.Errorf("invalid Auth.Method: %q (must be none, apikey, basic, or oidc)", cfg.Auth.Method)
		}
		if cfg.Auth.Method == "apikey" && cfg.Auth.ApiKey == "" {
			return fmt.Errorf("Auth.ApiKey is required when Auth.Method is apikey")
		}
		if cfg.Auth.Method == "basic" && len(cfg.Auth.Users) == 0 {
			return fmt.Errorf("Auth.Users is required when Auth.Method is basic")
		}
		if cfg.Auth.Method == "oidc" {
			if cfg.Auth.OIDC.IssuerURL == "" {
				return fmt.Errorf("Auth.OIDC.IssuerURL is required when Auth.Method is oidc")
			}
			if cfg.Auth.OIDC.ClientID == "" {
				return fmt.Errorf("Auth.OIDC.ClientID is required when Auth.Method is oidc")
			}
		}
	}
	return nil
}

func isASCIIXMLName(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_') {
				return false
			}
		} else if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

// parseAuthUsers parses a comma-separated list of user:password pairs.
func parseAuthUsers(s string) map[string]string {
	users := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			users[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return users
}
