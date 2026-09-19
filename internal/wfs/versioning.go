package wfs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

// Version states per WFS 2.0 spec
const (
	VersionStateValid      = "valid"
	VersionStateSuperseded = "superseded"
	VersionStateRetired    = "retired"
)

// Version navigation keywords
const (
	VersionLAST     = "LAST"
	VersionFIRST    = "FIRST"
	VersionPREVIOUS = "PREVIOUS"
	VersionNEXT     = "NEXT"
	VersionALL      = "ALL"
)

// FeatureVersion represents version metadata for a feature
type FeatureVersion struct {
	// FeatureID is the feature's unique identifier within the layer
	FeatureID string
	// LayerID identifies the layer this feature belongs to
	LayerID string
	// WorkspaceID identifies the workspace
	WorkspaceID string
	// Version is the version number (1 for initial, incrementing on updates)
	Version int
	// PreviousRid is the ResourceId of the previous version (empty for version 1)
	PreviousRid string
	// State is the version state: valid, superseded, or retired
	State string
	// CreatedAt is when this version was created
	CreatedAt time.Time
	// ModifiedBy identifies who created this version (optional)
	ModifiedBy string
}

// ResourceIdWithVersion represents a resource ID with optional version info
type ResourceIdWithVersion struct {
	// TypeName is the feature type name
	TypeName string
	// LocalID is the feature's local ID within the type
	LocalID string
	// Version is the requested version (numeric or keyword like LAST, FIRST)
	Version string
}

// ParseResourceIdWithVersion parses a ResourceId that may include version info
// Format: TypeName.LocalID[@version] where version can be a number or keyword
func ParseResourceIdWithVersion(rid string) (*ResourceIdWithVersion, error) {
	result := &ResourceIdWithVersion{}

	// Check for version suffix
	atIdx := -1
	for i := len(rid) - 1; i >= 0; i-- {
		if rid[i] == '@' {
			atIdx = i
			break
		}
	}

	var baseRid string
	if atIdx > 0 {
		baseRid = rid[:atIdx]
		result.Version = rid[atIdx+1:]
	} else {
		baseRid = rid
		result.Version = VersionLAST // Default to latest version
	}

	// Parse TypeName.LocalID
	dotIdx := -1
	for i := 0; i < len(baseRid); i++ {
		if baseRid[i] == '.' {
			dotIdx = i
			break
		}
	}

	if dotIdx <= 0 {
		return nil, fmt.Errorf("invalid resource ID format: %s", rid)
	}

	result.TypeName = baseRid[:dotIdx]
	result.LocalID = baseRid[dotIdx+1:]

	return result, nil
}

// VersionStore manages feature version metadata per workspace. Transactions
// record inserts/updates/deletes here; only metadata is kept (no feature
// content), so version navigation cannot be served from this store and the
// capabilities document truthfully declares ImplementsFeatureVersioning and
// ImplementsVersionNav as FALSE until content history is implemented.
type VersionStore struct {
	mu          sync.RWMutex
	versions    map[string]map[string][]*FeatureVersion // workspaceID -> featureKey -> versions (sorted by version number)
	maxFeatures int
	maxVersions int
	persist     RuntimePersistence // optional best-effort write-through; nil = memory-only
	logger      *slog.Logger
}

func (vs *VersionStore) setPersistence(p RuntimePersistence, logger *slog.Logger) {
	vs.persist = p
	vs.logger = logger
}

// restore loads persisted version metadata from a previous process. Records
// arrive ordered by workspace, feature, and version number.
func (vs *VersionStore) restore(records []store.WFSFeatureVersionRecord) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	for _, rec := range records {
		if vs.versions[rec.WorkspaceID] == nil {
			vs.versions[rec.WorkspaceID] = make(map[string][]*FeatureVersion)
		}
		key := featureKey(rec.LayerID, rec.FeatureID)
		vs.versions[rec.WorkspaceID][key] = append(vs.versions[rec.WorkspaceID][key], &FeatureVersion{
			FeatureID:   rec.FeatureID,
			LayerID:     rec.LayerID,
			WorkspaceID: rec.WorkspaceID,
			Version:     rec.Version,
			PreviousRid: rec.PreviousRid,
			State:       rec.State,
			CreatedAt:   rec.CreatedAt,
			ModifiedBy:  rec.ModifiedBy,
		})
	}
}

// persistVersion writes one version record best-effort. Version metadata is
// advisory (nothing user-visible reads it yet), so persistence failures are
// logged and never fail the transaction that recorded the version.
func (vs *VersionStore) persistVersion(v *FeatureVersion) {
	if vs.persist == nil {
		return
	}
	err := vs.persist.PutWFSFeatureVersion(context.Background(), store.WFSFeatureVersionRecord{
		WorkspaceID: v.WorkspaceID,
		LayerID:     v.LayerID,
		FeatureID:   v.FeatureID,
		Version:     v.Version,
		PreviousRid: v.PreviousRid,
		State:       v.State,
		ModifiedBy:  v.ModifiedBy,
		CreatedAt:   v.CreatedAt,
	})
	if err != nil && vs.logger != nil {
		vs.logger.Warn("persist WFS feature version", "layer", v.LayerID, "feature", v.FeatureID, "version", v.Version, "error", err)
	}
}

func NewVersionStore(maxFeatures, maxVersions int) *VersionStore {
	if maxFeatures <= 0 {
		maxFeatures = 100000
	}
	if maxVersions <= 0 {
		maxVersions = 100
	}
	return &VersionStore{versions: make(map[string]map[string][]*FeatureVersion), maxFeatures: maxFeatures, maxVersions: maxVersions}
}

// featureKey generates a key for a feature in the version store
func featureKey(layerID, featureID string) string {
	return layerID + ":" + featureID
}

// RecordInsert records a new feature insertion
func (vs *VersionStore) RecordInsert(workspaceID, layerID, featureID, modifiedBy string) *FeatureVersion {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return vs.recordInsertLocked(workspaceID, layerID, featureID, modifiedBy)
}

func (vs *VersionStore) recordInsertLocked(workspaceID, layerID, featureID, modifiedBy string) *FeatureVersion {

	if vs.versions[workspaceID] == nil {
		vs.versions[workspaceID] = make(map[string][]*FeatureVersion)
	}

	key := featureKey(layerID, featureID)
	vs.ensureFeatureCapacityLocked(workspaceID, key)

	version := &FeatureVersion{
		FeatureID:   featureID,
		LayerID:     layerID,
		WorkspaceID: workspaceID,
		Version:     1,
		PreviousRid: "",
		State:       VersionStateValid,
		CreatedAt:   time.Now(),
		ModifiedBy:  modifiedBy,
	}

	vs.versions[workspaceID][key] = []*FeatureVersion{version}
	vs.persistVersion(version)

	return version
}

// RecordUpdate records a feature update (creates new version, supersedes old)
func (vs *VersionStore) RecordUpdate(workspaceID, layerID, featureID, modifiedBy string) *FeatureVersion {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return vs.recordUpdateLocked(workspaceID, layerID, featureID, modifiedBy)
}

func (vs *VersionStore) recordUpdateLocked(workspaceID, layerID, featureID, modifiedBy string) *FeatureVersion {

	if vs.versions[workspaceID] == nil {
		vs.versions[workspaceID] = make(map[string][]*FeatureVersion)
	}

	key := featureKey(layerID, featureID)
	vs.ensureFeatureCapacityLocked(workspaceID, key)
	versions := vs.versions[workspaceID][key]

	var newVersionNum int
	var previousRid string

	if len(versions) > 0 {
		// Mark the last valid version as superseded
		for i := len(versions) - 1; i >= 0; i-- {
			if versions[i].State == VersionStateValid {
				versions[i].State = VersionStateSuperseded
				newVersionNum = versions[i].Version + 1
				previousRid = fmt.Sprintf("%s.%s@%d", layerID, featureID, versions[i].Version)
				vs.persistVersion(versions[i])
				break
			}
		}
	}

	if newVersionNum == 0 {
		newVersionNum = 1
	}

	version := &FeatureVersion{
		FeatureID:   featureID,
		LayerID:     layerID,
		WorkspaceID: workspaceID,
		Version:     newVersionNum,
		PreviousRid: previousRid,
		State:       VersionStateValid,
		CreatedAt:   time.Now(),
		ModifiedBy:  modifiedBy,
	}

	versions = append(versions, version)
	if len(versions) > vs.maxVersions {
		versions = versions[len(versions)-vs.maxVersions:]
		// Mirror the trim so persisted history does not outgrow the bound.
		if vs.persist != nil {
			if err := vs.persist.DeleteWFSFeatureVersionsBelow(context.Background(), workspaceID, layerID, featureID, versions[0].Version); err != nil && vs.logger != nil {
				vs.logger.Warn("trim persisted WFS feature versions", "layer", layerID, "feature", featureID, "error", err)
			}
		}
	}
	vs.versions[workspaceID][key] = versions
	vs.persistVersion(version)

	return version
}

func (vs *VersionStore) ensureFeatureCapacityLocked(workspaceID, key string) {
	features := vs.versions[workspaceID]
	if _, exists := features[key]; exists || len(features) < vs.maxFeatures {
		return
	}
	var oldestKey string
	var oldest time.Time
	for candidate, versions := range features {
		if len(versions) == 0 {
			oldestKey = candidate
			break
		}
		created := versions[len(versions)-1].CreatedAt
		if oldestKey == "" || created.Before(oldest) {
			oldestKey, oldest = candidate, created
		}
	}
	if evicted := features[oldestKey]; len(evicted) > 0 && vs.persist != nil {
		v := evicted[0]
		if err := vs.persist.DeleteWFSFeatureVersions(context.Background(), v.WorkspaceID, v.LayerID, v.FeatureID); err != nil && vs.logger != nil {
			vs.logger.Warn("evict persisted WFS feature versions", "layer", v.LayerID, "feature", v.FeatureID, "error", err)
		}
	}
	delete(features, oldestKey)
}

// RecordDelete records a feature deletion (marks current version as retired)
func (vs *VersionStore) RecordDelete(workspaceID, layerID, featureID string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.recordDeleteLocked(workspaceID, layerID, featureID)
}

func (vs *VersionStore) recordDeleteLocked(workspaceID, layerID, featureID string) {

	if vs.versions[workspaceID] == nil {
		return
	}

	key := featureKey(layerID, featureID)
	versions := vs.versions[workspaceID][key]

	if len(versions) > 0 {
		// Mark all valid versions as retired
		for i := range versions {
			if versions[i].State == VersionStateValid {
				versions[i].State = VersionStateRetired
				vs.persistVersion(versions[i])
			}
		}
	}
}

// GetVersion retrieves a specific version of a feature
func (vs *VersionStore) GetVersion(workspaceID, layerID, featureID string, versionSpec string) *FeatureVersion {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	if vs.versions[workspaceID] == nil {
		return nil
	}

	key := featureKey(layerID, featureID)
	versions := vs.versions[workspaceID][key]

	if len(versions) == 0 {
		return nil
	}

	switch versionSpec {
	case VersionLAST, "":
		// Return the most recent valid version
		for i := len(versions) - 1; i >= 0; i-- {
			if versions[i].State == VersionStateValid {
				return versions[i]
			}
		}
		// If no valid version, return the most recent
		return versions[len(versions)-1]

	case VersionFIRST:
		// Return the first version
		return versions[0]

	case VersionALL:
		// Not applicable for single version retrieval
		return nil

	default:
		// Try to parse as a version number
		var targetVersion int
		if _, err := fmt.Sscanf(versionSpec, "%d", &targetVersion); err == nil {
			for _, v := range versions {
				if v.Version == targetVersion {
					return v
				}
			}
		}
	}

	return nil
}

// GetAllVersions retrieves all versions of a feature
func (vs *VersionStore) GetAllVersions(workspaceID, layerID, featureID string) []*FeatureVersion {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	if vs.versions[workspaceID] == nil {
		return nil
	}

	key := featureKey(layerID, featureID)
	return vs.versions[workspaceID][key]
}

// GetValidVersion returns the current valid version of a feature
func (vs *VersionStore) GetValidVersion(workspaceID, layerID, featureID string) *FeatureVersion {
	return vs.GetVersion(workspaceID, layerID, featureID, VersionLAST)
}

// Clear clears all version tracking data for a workspace
func (vs *VersionStore) Clear(workspaceID string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	delete(vs.versions, workspaceID)
}
