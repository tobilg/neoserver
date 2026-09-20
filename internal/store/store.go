package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

var (
	// ErrNotFound is returned when a requested resource doesn't exist.
	ErrNotFound = errors.New("not found")

	// ErrDuplicateKey is returned when a unique constraint is violated.
	ErrDuplicateKey = errors.New("duplicate key")

	// ErrNotInitialized is returned when the store hasn't been initialized.
	ErrNotInitialized = errors.New("store not initialized")

	// ErrInvalidCredentials is returned for auth failures.
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrResourceNotEmpty is returned by safe, non-recursive parent deletion
	// when catalog or auxiliary children still depend on the target.
	ErrResourceNotEmpty = errors.New("resource is not empty")

	// ErrAPIKeyNotRevoked is returned when permanently deleting an API key
	// that is still usable; keys must be revoked first.
	ErrAPIKeyNotRevoked = errors.New("API key must be revoked before it can be deleted")
)

// Store provides access to the encrypted DuckDB backing store.
type Store interface {
	// Workspace operations
	CreateWorkspace(ctx context.Context, input CreateWorkspaceInput) (*Workspace, error)
	GetWorkspace(ctx context.Context, id string) (*Workspace, error)
	GetWorkspaceByName(ctx context.Context, name string) (*Workspace, error)
	ListWorkspaces(ctx context.Context) ([]*Workspace, error)
	UpdateWorkspace(ctx context.Context, id string, input UpdateWorkspaceInput) (*Workspace, error)
	DeleteWorkspace(ctx context.Context, id string) error

	// Service operations
	CreateService(ctx context.Context, input CreateServiceInput) (*Service, error)
	GetService(ctx context.Context, id string) (*Service, error)
	ListServices(ctx context.Context, workspaceID string) ([]*Service, error)
	UpdateService(ctx context.Context, id string, input UpdateServiceInput) (*Service, error)
	DeleteService(ctx context.Context, id string) error

	// Layer operations
	CreateLayer(ctx context.Context, input CreateLayerInput) (*Layer, error)
	GetLayer(ctx context.Context, id string) (*Layer, error)
	GetLayerByPublicID(ctx context.Context, serviceID, publicID string) (*Layer, error)
	ListLayers(ctx context.Context, serviceID string) ([]*Layer, error)
	UpdateLayer(ctx context.Context, id string, input UpdateLayerInput) (*Layer, error)
	DeleteLayer(ctx context.Context, id string) error

	// Style operations
	CreateStyle(ctx context.Context, input CreateStyleInput) (*Style, error)
	GetStyle(ctx context.Context, id string) (*Style, error)
	GetStyleByName(ctx context.Context, workspaceID, name string) (*Style, error)
	ListStyles(ctx context.Context, workspaceID string) ([]*Style, error)
	UpdateStyle(ctx context.Context, id string, input UpdateStyleInput) (*Style, error)
	DeleteStyle(ctx context.Context, id string) error

	// WFS Stored Query operations
	CreateWFSStoredQuery(ctx context.Context, input CreateWFSStoredQueryInput) (*WFSStoredQuery, error)
	GetWFSStoredQuery(ctx context.Context, workspaceID, queryID string) (*WFSStoredQuery, error)
	ListWFSStoredQueries(ctx context.Context, workspaceID string) ([]*WFSStoredQuery, error)
	DeleteWFSStoredQuery(ctx context.Context, workspaceID, queryID string) error

	// Workspace settings operations
	GetWMSSettings(ctx context.Context, workspaceID string) (*WMSSettings, error)
	UpdateWMSSettings(ctx context.Context, workspaceID string, settings WMSSettings) error
	GetWFSSettings(ctx context.Context, workspaceID string) (*WFSSettings, error)
	UpdateWFSSettings(ctx context.Context, workspaceID string, settings WFSSettings) error
	GetOGCAPISettings(ctx context.Context, workspaceID string) (*OGCAPISettings, error)
	UpdateOGCAPISettings(ctx context.Context, workspaceID string, settings OGCAPISettings) error
	GetOGCTilesAPISettings(ctx context.Context, workspaceID string) (*OGCTilesAPISettings, error)
	UpdateOGCTilesAPISettings(ctx context.Context, workspaceID string, settings OGCTilesAPISettings) error

	// Role operations
	CreateRole(ctx context.Context, input CreateRoleInput) (*Role, error)
	GetRole(ctx context.Context, id string) (*Role, error)
	ListRoles(ctx context.Context) ([]*Role, error)
	DeleteRole(ctx context.Context, id string) error

	// API Key operations
	CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*CreateAPIKeyOutput, error)
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, workspaceID *string) ([]*APIKey, error)
	RevokeAPIKey(ctx context.Context, id string) error
	// DeleteAPIKey permanently removes a revoked key and the browser
	// sessions created from it.
	DeleteAPIKey(ctx context.Context, id string) error

	// Claim mapping operations
	CreateClaimMapping(ctx context.Context, input CreateClaimMappingInput) (*ClaimRoleMapping, error)
	GetClaimMapping(ctx context.Context, id string) (*ClaimRoleMapping, error)
	ListClaimMappings(ctx context.Context, workspaceID string) ([]*ClaimRoleMapping, error)
	DeleteClaimMapping(ctx context.Context, id string) error
	ResolveClaimsToRoles(ctx context.Context, claims map[string][]string) (map[string]string, error)

	// Signing key operations
	GetActiveSigningKey(ctx context.Context) (*SigningKey, error)
	RotateSigningKey(ctx context.Context) (*SigningKey, error)

	// Casbin adapter operations (for custom adapter)
	LoadCasbinPolicies(ctx context.Context) ([]*CasbinRule, error)
	SaveCasbinPolicy(ctx context.Context, rule *CasbinRule) error
	RemoveCasbinPolicy(ctx context.Context, rule *CasbinRule) error

	// Lifecycle
	Close() error
}

// CoverageStore extends Store with WCS-specific persistence. Keeping this a
// separate interface avoids coupling feature-only consumers to WCS operations.
type CoverageStore interface {
	CreateCoverage(ctx context.Context, input CreateCoverageInput) (*Coverage, error)
	GetCoverage(ctx context.Context, id string) (*Coverage, error)
	GetCoverageByPublicID(ctx context.Context, workspaceID, publicID string) (*Coverage, error)
	ListCoverages(ctx context.Context, serviceID string) ([]*Coverage, error)
	UpdateCoverage(ctx context.Context, id string, input UpdateCoverageInput) (*Coverage, error)
	DeleteCoverage(ctx context.Context, id string) error
	GetWCSSettings(ctx context.Context, workspaceID string) (*WCSSettings, error)
	UpdateWCSSettings(ctx context.Context, workspaceID string, settings WCSSettings) error
}

// WMTSStore extends the catalog with independently activatable WMTS settings.
type WMTSStore interface {
	GetWMTSSettings(ctx context.Context, workspaceID string) (*WMTSSettings, error)
	UpdateWMTSSettings(ctx context.Context, workspaceID string, settings WMTSSettings) error
	GetTileRevision(ctx context.Context, workspaceID string) (int64, error)
}

type LayerGroupStore interface {
	CreateLayerGroup(ctx context.Context, input CreateLayerGroupInput) (*LayerGroup, error)
	GetLayerGroup(ctx context.Context, id string) (*LayerGroup, error)
	GetLayerGroupByPublicID(ctx context.Context, workspaceID, publicID string) (*LayerGroup, error)
	ListLayerGroups(ctx context.Context, workspaceID string) ([]*LayerGroup, error)
	UpdateLayerGroup(ctx context.Context, id string, input UpdateLayerGroupInput) (*LayerGroup, error)
	DeleteLayerGroup(ctx context.Context, id string) error
}

// StyleAssetStore persists metadata for binary portrayal assets. Asset bytes
// intentionally remain in the configured filesystem asset root.
type StyleAssetStore interface {
	UpsertStyleAsset(ctx context.Context, input UpsertStyleAssetInput) (*StyleAsset, error)
	GetStyleAsset(ctx context.Context, workspaceID, name string) (*StyleAsset, error)
	ListStyleAssets(ctx context.Context, workspaceID string) ([]*StyleAsset, error)
	DeleteStyleAsset(ctx context.Context, workspaceID, name string) error
}

// CatalogDeletionStore exposes durable, referentially-complete catalog
// deletion primitives without expanding Store for lightweight test doubles.
type CatalogDeletionStore interface {
	PlanWorkspaceDeletion(ctx context.Context, workspaceID string) (*DeletionPlan, error)
	PlanServiceDeletion(ctx context.Context, workspaceID, serviceID string) (*DeletionPlan, error)
	BeginCatalogDeletion(ctx context.Context, plan DeletionPlan) (*DeletionOperation, bool, error)
	GetCatalogDeletion(ctx context.Context, id string) (*DeletionOperation, error)
	FindCatalogDeletionByTarget(ctx context.Context, scope DeletionScope, targetID string) (*DeletionOperation, error)
	ListPendingCatalogDeletions(ctx context.Context) ([]*DeletionOperation, error)
	UpdateCatalogDeletion(ctx context.Context, id string, status DeletionStatus, phase DeletionPhase, lastError string) error
	CommitCatalogDeletion(ctx context.Context, operationID string) error
}

type CatalogDeletionListStore interface {
	ListCatalogDeletions(ctx context.Context, limit int) ([]*DeletionOperation, error)
}

type CatalogDeletionPageStore interface {
	ListCatalogDeletionPage(ctx context.Context, query DeletionQuery) (*DeletionPage, error)
}

// CatalogIntegrityStore supports explicit upgrade audits and idempotent repair
// of orphan rows produced by releases that predate durable deletion.
type CatalogIntegrityStore interface {
	AuditCatalogOrphans(ctx context.Context) ([]CatalogOrphan, error)
	RepairCatalogOrphans(ctx context.Context) (int64, error)
	CatalogOwnership(ctx context.Context) (*CatalogOwnership, error)
}

// DuckDBStore implements Store using an encrypted DuckDB database.
type DuckDBStore struct {
	datasetMapMu  sync.Mutex   // Serializes map references with group mutations and deletion registration.
	sessionMu     sync.Mutex   // Serializes conditional token rotations.
	roleMu        sync.RWMutex // Serializes role deletion against new assignments.
	db            *sql.DB
	encryptionKey string
}

// Config holds configuration for opening the store.
type Config struct {
	Path          string
	EncryptionKey string
}

// Health verifies that the encrypted catalog database is reachable.
func (s *DuckDBStore) Health(ctx context.Context) error { return s.db.PingContext(ctx) }
