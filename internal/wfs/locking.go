package wfs

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// Lock action constants
const (
	LockActionAll  = "ALL"  // Lock all features that match, or fail if any cannot be locked
	LockActionSome = "SOME" // Lock only the features that can be locked
)

// Default lock expiry time in seconds
const DefaultLockExpiry = 300 // 5 minutes

// FeatureLock represents a lock on a set of features
type FeatureLock struct {
	// LockID is the unique identifier for this lock
	LockID string
	// WorkspaceID identifies the workspace
	WorkspaceID string
	// FeatureIDs maps feature type to list of feature IDs that are locked
	FeatureIDs map[string][]string // typeName -> []localID
	// ExpiresAt is when this lock expires
	ExpiresAt time.Time
	// OwnerID identifies who owns the lock (optional)
	OwnerID string
	// CreatedAt is when the lock was created
	CreatedAt time.Time
}

// IsExpired returns true if the lock has expired
func (l *FeatureLock) IsExpired() bool {
	return time.Now().After(l.ExpiresAt)
}

// LockStore manages feature locks
type LockStore struct {
	writes          writeGates
	mu              sync.RWMutex
	locks           map[string]*FeatureLock // lockID -> lock
	expired         map[string]time.Time    // lockID -> when it was swept
	maxExpirySec    int
	maxPerWorkspace int
	maxPerPrincipal int
	maxFeatures     int
	persist         RuntimePersistence // optional write-through; nil = memory-only
	logger          *slog.Logger
}

func (ls *LockStore) setPersistence(p RuntimePersistence, logger *slog.Logger) {
	ls.persist = p
	ls.logger = logger
}

// restore loads persisted locks from a previous process into the in-memory
// map. Expired records are skipped; quotas are not re-checked.
func (ls *LockStore) restore(records []store.WFSLockRecord) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	now := time.Now()
	for _, rec := range records {
		if !rec.ExpiresAt.After(now) {
			continue
		}
		ls.locks[rec.LockID] = &FeatureLock{
			LockID:      rec.LockID,
			WorkspaceID: rec.WorkspaceID,
			FeatureIDs:  rec.FeatureIDs,
			ExpiresAt:   rec.ExpiresAt,
			OwnerID:     rec.OwnerID,
			CreatedAt:   rec.CreatedAt,
		}
	}
}

func NewLockStore(maxExpiry, maxWorkspace, maxPrincipal, maxFeatures int) *LockStore {
	if maxExpiry <= 0 {
		maxExpiry = 3600
	}
	if maxWorkspace <= 0 {
		maxWorkspace = 1000
	}
	if maxPrincipal <= 0 {
		maxPrincipal = 100
	}
	if maxFeatures <= 0 {
		maxFeatures = 10000
	}
	return &LockStore{locks: make(map[string]*FeatureLock), expired: make(map[string]time.Time), maxExpirySec: maxExpiry, maxPerWorkspace: maxWorkspace, maxPerPrincipal: maxPrincipal, maxFeatures: maxFeatures}
}

// AcquireLock attempts to lock the specified features
func (ls *LockStore) AcquireLock(workspaceID string, features map[string][]string, expirySeconds int, lockAction string) (*FeatureLock, []string, error) {
	return ls.AcquireLockOwned(workspaceID, features, expirySeconds, lockAction, "")
}

func (ls *LockStore) AcquireLockOwned(workspaceID string, features map[string][]string, expirySeconds int, lockAction, ownerID string) (*FeatureLock, []string, error) {
	return ls.AcquireLockOwnedContext(context.Background(), workspaceID, features, expirySeconds, lockAction, ownerID)
}

func (ls *LockStore) AcquireLockOwnedContext(ctx context.Context, workspaceID string, features map[string][]string, expirySeconds int, lockAction, ownerID string) (*FeatureLock, []string, error) {
	release, err := ls.writes.acquire(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	ls.mu.Lock()
	defer ls.mu.Unlock()

	// Clean up expired locks first
	ls.cleanExpiredLocked()
	totalFeatures, workspaceLocks, principalLocks := 0, 0, 0
	for _, ids := range features {
		totalFeatures += len(ids)
	}
	if totalFeatures > ls.maxFeatures {
		return nil, nil, fmt.Errorf("lock feature quota exceeded")
	}
	for _, existing := range ls.locks {
		if existing.WorkspaceID == workspaceID {
			workspaceLocks++
			if existing.OwnerID == ownerID {
				principalLocks++
			}
		}
	}
	if workspaceLocks >= ls.maxPerWorkspace {
		return nil, nil, fmt.Errorf("workspace lock quota exceeded")
	}
	if principalLocks >= ls.maxPerPrincipal {
		return nil, nil, fmt.Errorf("principal lock quota exceeded")
	}

	// Check if any features are already locked
	var alreadyLocked []string
	for typeName, featureIDs := range features {
		// Strip namespace prefix to match gml:id format
		localTypeName := lockDisplayName(typeName)
		for _, fid := range featureIDs {
			if ls.isFeatureLockedLocked(workspaceID, typeName, fid) {
				alreadyLocked = append(alreadyLocked, fmt.Sprintf("%s.%s", localTypeName, fid))
			}
		}
	}

	// If lockAction is ALL, fail if any features are already locked
	if lockAction == LockActionAll && len(alreadyLocked) > 0 {
		return nil, alreadyLocked, fmt.Errorf("cannot lock all features: %d feature(s) already locked", len(alreadyLocked))
	}

	// Remove already locked features from the request (for SOME action)
	if len(alreadyLocked) > 0 {
		alreadyLockedSet := make(map[string]bool)
		for _, rid := range alreadyLocked {
			alreadyLockedSet[rid] = true
		}

		for typeName, featureIDs := range features {
			// Strip namespace prefix to match the format used in alreadyLocked
			localTypeName := lockDisplayName(typeName)
			var unlockedIDs []string
			for _, fid := range featureIDs {
				rid := fmt.Sprintf("%s.%s", localTypeName, fid)
				if !alreadyLockedSet[rid] {
					unlockedIDs = append(unlockedIDs, fid)
				}
			}
			features[typeName] = unlockedIDs
		}
	}

	// Generate lock ID
	lockID := uuid.New().String()

	// Calculate expiry time
	if expirySeconds <= 0 {
		expirySeconds = DefaultLockExpiry
	}
	if expirySeconds > ls.maxExpirySec {
		expirySeconds = ls.maxExpirySec
	}
	expiresAt := time.Now().Add(time.Duration(expirySeconds) * time.Second)

	// Create the lock
	lock := &FeatureLock{
		LockID:      lockID,
		WorkspaceID: workspaceID,
		FeatureIDs:  features,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now(),
		OwnerID:     ownerID,
	}

	ls.locks[lockID] = lock

	// Consistency-first write-through: never grant a lock that would not
	// survive a restart. On persistence failure the in-memory entry is
	// rolled back and the acquire fails.
	if ls.persist != nil {
		err := ls.persist.PutWFSLock(context.Background(), store.WFSLockRecord{
			LockID:      lock.LockID,
			WorkspaceID: lock.WorkspaceID,
			OwnerID:     lock.OwnerID,
			FeatureIDs:  lock.FeatureIDs,
			CreatedAt:   lock.CreatedAt,
			ExpiresAt:   lock.ExpiresAt,
		})
		if err != nil {
			delete(ls.locks, lockID)
			return nil, nil, fmt.Errorf("persist lock: %w", err)
		}
	}

	return lock, alreadyLocked, nil
}

// deletePersistedLock removes a lock row best-effort; expiry-based cleanup is
// the backstop when the delete fails.
func (ls *LockStore) deletePersistedLock(lockID string) {
	if ls.persist == nil {
		return
	}
	if err := ls.persist.DeleteWFSLock(context.Background(), lockID); err != nil && ls.logger != nil {
		ls.logger.Warn("delete persisted WFS lock", "lockId", lockID, "error", err)
	}
}

func (ls *LockStore) ReleaseLockOwned(lockID, ownerID string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	lock, exists := ls.locks[lockID]
	if !exists {
		return fmt.Errorf("lock not found: %s", lockID)
	}
	if lock.OwnerID != "" && lock.OwnerID != ownerID {
		return errors.New("lock is owned by another principal")
	}
	delete(ls.locks, lockID)
	ls.deletePersistedLock(lockID)
	return nil
}

func (ls *LockStore) ValidateLockOwner(lockID, workspaceID, ownerID string) error {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	lock, exists := ls.locks[lockID]
	if !exists || lock.IsExpired() || lock.WorkspaceID != workspaceID {
		return &RequestError{Code: ExceptionInvalidParameterValue, Locator: "lockId", Message: "lock is invalid or expired"}
	}
	if lock.OwnerID != "" && lock.OwnerID != ownerID {
		return &RequestError{Code: ExceptionAuthorizationFailed, Locator: "lockId", Message: "lock is owned by another principal"}
	}
	return nil
}

func lockOwner(r *http.Request) string {
	id, _ := identity.FromContext(r.Context())
	if id == nil {
		return ""
	}
	if id.APIKeyID != "" {
		return "apikey:" + id.APIKeyID
	}
	return string(id.AuthMethod) + ":" + id.Subject
}

// ReleaseLock releases a lock by ID
func (ls *LockStore) ReleaseLock(lockID string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if _, exists := ls.locks[lockID]; !exists {
		return fmt.Errorf("lock not found: %s", lockID)
	}

	delete(ls.locks, lockID)
	ls.deletePersistedLock(lockID)
	return nil
}

// ValidateLock checks if a lock is valid for the given feature
func (ls *LockStore) ValidateLock(lockID, workspaceID, typeName, featureID string) error {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	lock, exists := ls.locks[lockID]
	if !exists {
		if ls.hasExpired(lockID) {
			return &RequestError{
				Code:    ExceptionLockHasExpired,
				Locator: "lockId",
				Message: fmt.Sprintf("Lock has expired: %s", lockID),
			}
		}
		return &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "lockId",
			Message: fmt.Sprintf("Lock not found: %s", lockID),
		}
	}

	if lock.IsExpired() {
		return &RequestError{
			Code:    "LockHasExpired",
			Locator: "lockId",
			Message: fmt.Sprintf("Lock has expired: %s", lockID),
		}
	}

	if lock.WorkspaceID != workspaceID {
		return &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "lockId",
			Message: fmt.Sprintf("Lock belongs to different workspace: %s", lockID),
		}
	}

	// Check if the feature is covered by this lock
	if features, ok := lock.FeatureIDs[typeName]; ok {
		for _, fid := range features {
			if fid == featureID {
				return nil // Found - lock is valid for this feature
			}
		}
	}

	return &RequestError{
		Code:    "MissingParameterValue",
		Locator: "lockId",
		Message: fmt.Sprintf("Feature %s.%s is not covered by lock %s", typeName, featureID, lockID),
	}
}

// GetLock retrieves a lock by ID
// Expired reports whether the lock ID belonged to a lock that has since expired
// and been swept, so callers can answer LockHasExpired rather than "not found".
func (ls *LockStore) Expired(lockID string) bool {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.hasExpired(lockID)
}

func (ls *LockStore) GetLock(lockID string) *FeatureLock {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	return ls.locks[lockID]
}

// isFeatureLockedLocked checks if a feature is locked (must hold write lock)
func (ls *LockStore) isFeatureLockedLocked(workspaceID, typeName, featureID string) bool {
	for _, lock := range ls.locks {
		if lock.IsExpired() {
			continue
		}
		if lock.WorkspaceID != workspaceID {
			continue
		}
		for lockedTypeName, features := range lock.FeatureIDs {
			if !sameLockTypeName(lockedTypeName, typeName) {
				continue
			}
			for _, fid := range features {
				if fid == featureID {
					return true
				}
			}
		}
	}
	return false
}

// expiredRetention is how long a swept lock ID stays known. WFS 2.0 requires a
// request carrying an expired lockId to be answered with LockHasExpired, which
// is impossible once the ID has been forgotten entirely.
const expiredRetention = time.Hour

// cleanExpiredLocked removes expired locks (must hold write lock), keeping their
// IDs long enough to tell an expired lock apart from one that never existed.
func (ls *LockStore) cleanExpiredLocked() {
	now := time.Now()
	for lockID, lock := range ls.locks {
		if lock.IsExpired() {
			delete(ls.locks, lockID)
			ls.expired[lockID] = now
		}
	}
	for lockID, at := range ls.expired {
		if now.Sub(at) > expiredRetention {
			delete(ls.expired, lockID)
		}
	}
}

// hasExpired reports whether this lock ID was swept after expiring. Callers must
// hold at least the read lock.
func (ls *LockStore) hasExpired(lockID string) bool {
	_, ok := ls.expired[lockID]
	return ok
}

// CheckFeatureLock checks if a feature is locked and validates the provided lockId.
// If the feature is locked and no lockId is provided, returns MissingParameterValue.
// If the feature is locked and lockId doesn't match, returns an error.
// If the feature is not locked or the lockId is valid, returns nil.
func (ls *LockStore) CheckFeatureLock(workspaceID, typeName, featureID, providedLockId string) error {
	return ls.CheckFeatureLocks(workspaceID, typeName, []string{featureID}, providedLockId)
}

// CheckFeatureLocks checks a mutation's complete returned-ID set in linear time
// in the number of affected and locked features, without a query row limit.
func (ls *LockStore) CheckFeatureLocks(workspaceID, typeName string, featureIDs []string, providedLockId string) error {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	affected := make(map[string]struct{}, len(featureIDs))
	for _, id := range featureIDs {
		affected[id] = struct{}{}
	}

	// Clean up expired locks conceptually (we can't modify in read lock, but we skip expired)
	// Find if this feature is locked by any active lock
	var holdingLock *FeatureLock
	var featureID string
	for _, lock := range ls.locks {
		if lock.IsExpired() {
			continue
		}
		if lock.WorkspaceID != workspaceID {
			continue
		}
		if lock.LockID == providedLockId {
			continue
		}
		for lockedTypeName, features := range lock.FeatureIDs {
			if !sameLockTypeName(lockedTypeName, typeName) {
				continue
			}
			for _, fid := range features {
				if _, exists := affected[fid]; exists {
					featureID = fid
					holdingLock = lock
					break
				}
			}
		}
		if holdingLock != nil {
			break
		}
	}

	// Feature is not locked - allow operation
	if holdingLock == nil {
		return nil
	}

	// Feature is locked - check if lockId was provided
	if providedLockId == "" {
		return &RequestError{
			Code:    ExceptionMissingParameterValue,
			Locator: "lockId",
			Message: fmt.Sprintf("Feature %s.%s is locked. A lockId must be provided to modify locked features.", typeName, featureID),
		}
	}

	// Check if the provided lockId matches the lock holding this feature
	if providedLockId != holdingLock.LockID {
		return &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "lockId",
			Message: fmt.Sprintf("Feature %s.%s is locked by a different lock. Provided lockId does not match.", typeName, featureID),
		}
	}

	// LockId matches - allow operation
	return nil
}

// IsFeatureLocked checks if a specific feature is locked (public version)
func (ls *LockStore) IsFeatureLocked(workspaceID, typeName, featureID string) bool {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	for _, lock := range ls.locks {
		if lock.IsExpired() {
			continue
		}
		if lock.WorkspaceID != workspaceID {
			continue
		}
		for lockedTypeName, features := range lock.FeatureIDs {
			if !sameLockTypeName(lockedTypeName, typeName) {
				continue
			}
			for _, fid := range features {
				if fid == featureID {
					return true
				}
			}
		}
	}
	return false
}

// WFS feature identifiers use the local component of the advertised QName
// (for example Autos.4), while LockFeature queries may carry either Autos or
// cite:Autos. Treat those spellings as the same lock key so a StoredQuery
// cannot acquire a second lock for an already locked feature.
func sameLockTypeName(a, b string) bool {
	left, aSource := decodeLockKey(a)
	right, bSource := decodeLockKey(b)
	if aSource && bSource {
		return left.Source == right.Source
	}
	// Pre-v1 persisted locks lack a durable source binding. Until they expire
	// or are released, conservatively protect their IDs workspace-wide rather
	// than allowing an alias/rename to bypass them after an upgrade/restart.
	if aSource != bSource {
		return true
	}
	return ParseQName(a).LocalPart == ParseQName(b).LocalPart
}

func lockedFeatureIDs(features map[string][]string, typeName string) []string {
	var ids []string
	for lockedTypeName, featureIDs := range features {
		if sameLockTypeName(lockedTypeName, typeName) {
			ids = append(ids, featureIDs...)
		}
	}
	return ids
}

// CleanExpired removes all expired locks (public method)
func (ls *LockStore) CleanExpired() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.cleanExpiredLocked()
}

// ClearWorkspace releases every process-local feature lock for a workspace.
// Catalog publication deletion must not leave lock IDs referring to resources
// that can no longer be resolved.
func (ls *LockStore) ClearWorkspace(workspaceID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for lockID, lock := range ls.locks {
		if lock.WorkspaceID == workspaceID {
			delete(ls.locks, lockID)
		}
	}
}

// XML structures for LockFeature

// XMLLockFeature represents a wfs:LockFeature request
type XMLLockFeature struct {
	XMLName     xml.Name            `xml:"LockFeature"`
	Service     string              `xml:"service,attr"`
	Version     string              `xml:"version,attr"`
	Expiry      string              `xml:"expiry,attr"`
	LockAction  string              `xml:"lockAction,attr"` // ALL or SOME
	LockId      string              `xml:"lockId,attr"`     // For releasing/extending existing locks
	Queries     []XMLQuery          `xml:"Query"`
	StoredQuery *XMLLockStoredQuery `xml:"StoredQuery"`
}

// XMLLockStoredQuery represents a wfs:StoredQuery element in LockFeature
type XMLLockStoredQuery struct {
	ID         string                    `xml:"id,attr"`
	Parameters []XMLLockStoredQueryParam `xml:"Parameter"`
}

// XMLLockStoredQueryParam represents a parameter in a stored query
type XMLLockStoredQueryParam struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// LockFeatureResponse represents the response to a LockFeature request
type LockFeatureResponse struct {
	LockID            string
	FeaturesLocked    []string // ResourceIds of locked features
	FeaturesNotLocked []string // ResourceIds of features that could not be locked
}

// parseLockFeatureKVP parses LockFeature parameters from query string (KVP encoding)
func parseLockFeatureKVP(r *http.Request) XMLLockFeature {
	// Normalize query parameters to uppercase for case-insensitive matching
	q := NormalizeQuery(r)

	req := XMLLockFeature{
		Service:    q.Get("SERVICE"),
		Version:    q.Get("VERSION"),
		Expiry:     q.Get("EXPIRY"),
		LockAction: q.Get("LOCKACTION"),
	}
	if id := q.Get("STOREDQUERY_ID"); id != "" {
		req.StoredQuery = &XMLLockStoredQuery{ID: id, Parameters: []XMLLockStoredQueryParam{{Name: "ID", Value: q.Get("ID")}}}
	}

	// Parse NAMESPACES parameter for type name resolution
	namespacesParam := q.Get("NAMESPACES")
	nsMap := parseNamespacesParam(namespacesParam)

	// Parse TYPENAMES parameter - can be comma-separated for multiple types
	typeNames := q.Get("TYPENAMES")
	if typeNames == "" {
		typeNames = q.Get("TYPENAME") // Also check singular form
	}

	if typeNames != "" {
		// Split by comma for multiple type names
		types := strings.Split(typeNames, ",")
		for _, t := range types {
			t = strings.TrimSpace(t)
			if t != "" {
				// Resolve namespace prefix using NAMESPACES parameter
				resolved := resolveTypeName(t, nsMap)
				req.Queries = append(req.Queries, XMLQuery{
					TypeNames: resolved,
				})
			}
		}
	}

	// Parse FILTER parameter (optional - applies to all queries)
	filterParam := q.Get("FILTER")
	if filterParam != "" && len(req.Queries) > 0 {
		// Apply filter to the first query (WFS 2.0 KVP encoding)
		req.Queries[0].FilterRaw = filterParam
	}

	return req
}

// handleLockFeature handles WFS LockFeature requests
func (h *workspaceHandler) handleLockFeature(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	var req XMLLockFeature

	// Check if this is a KVP (GET) or XML (POST) request
	if r.Method == http.MethodGet || r.ContentLength == 0 {
		// Parse KVP parameters
		req = parseLockFeatureKVP(r)
	} else {
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			WriteException(w, ExceptionOperationParsingFailed, "", "Failed to read request body")
			return
		}

		// Parse XML request
		if err := xml.Unmarshal(body, &req); err != nil {
			WriteException(w, ExceptionOperationParsingFailed, "", fmt.Sprintf("Failed to parse LockFeature request: %v", err))
			return
		}
	}

	// Validate service
	if req.Service != "" && strings.ToUpper(req.Service) != "WFS" {
		WriteException(w, ExceptionInvalidParameterValue, "service", "SERVICE must be WFS")
		return
	}

	// Handle StoredQuery (GetFeatureById)
	if req.StoredQuery != nil {
		if !IsGetFeatureByIdQuery(req.StoredQuery.ID) {
			WriteException(w, ExceptionOperationNotSupported, "storedQueryId", "Only GetFeatureById is supported for locking")
			return
		}
		foundID := false
		for _, param := range req.StoredQuery.Parameters {
			if strings.ToUpper(param.Name) == "ID" {
				foundID = true
				// param.Value is like "Autos.15" - extract typeName and create query with ResourceId filter
				typeName, _, resolved := resolveFeatureIdentifier(ws, param.Value, h.cfg.WFS.AppNamespacePrefix)
				if !resolved {
					WriteException(w, ExceptionNotFound, "ID", "Feature not found")
					return
				}
				req.Queries = []XMLQuery{{
					TypeNames: typeName,
					FilterRaw: fmt.Sprintf(`<fes:Filter xmlns:fes="%s"><fes:ResourceId rid="%s"/></fes:Filter>`, NSFes, escapeXML(param.Value)),
				}}
			}
		}
		if !foundID {
			WriteException(w, ExceptionMissingParameterValue, "ID", "ID is required")
			return
		}
	}

	// If lockId is provided without queries, this is a lock reset/release request
	if req.LockId != "" && len(req.Queries) == 0 && req.StoredQuery == nil {
		// Check if the lock exists and its status
		lock := h.state.Locks.GetLock(req.LockId)
		if lock == nil {
			if h.state.Locks.Expired(req.LockId) {
				WriteException(w, ExceptionLockHasExpired, "lockId", fmt.Sprintf("Lock has expired: %s", req.LockId))
				return
			}
			WriteException(w, ExceptionInvalidParameterValue, "lockId", fmt.Sprintf("Lock not found: %s", req.LockId))
			return
		}
		// Check if the lock has expired - per WFS 2.0, trying to reset an expired lock returns 403
		if lock.IsExpired() {
			WriteException(w, ExceptionLockHasExpired, "lockId", fmt.Sprintf("Lock has expired: %s", req.LockId))
			return
		}
		// Release the valid lock
		err := h.state.Locks.ReleaseLockOwned(req.LockId, lockOwner(r))
		if err != nil {
			WriteException(w, ExceptionInvalidParameterValue, "lockId", fmt.Sprintf("Failed to release lock: %s", req.LockId))
			return
		}
		// Write success response for lock release
		WriteLockFeatureResponse(w, &LockFeatureResponse{
			LockID:         req.LockId,
			FeaturesLocked: []string{},
		})
		return
	}

	// Validate we have at least one type to lock
	if len(req.Queries) == 0 {
		WriteException(w, ExceptionMissingParameterValue, "typenames", "TYPENAMES parameter is required")
		return
	}

	// Parse expiry
	expiry := DefaultLockExpiry
	if req.Expiry != "" {
		if e, err := strconv.Atoi(req.Expiry); err == nil && e > 0 {
			expiry = e
		}
	}

	// Default lock action
	lockAction := LockActionAll
	if strings.ToUpper(req.LockAction) == LockActionSome {
		lockAction = LockActionSome
	}

	// Collect features to lock
	featuresToLock := make(map[string][]string)
	// Preflight the entire request before looking up any source metadata/data.
	for _, query := range req.Queries {
		if _, _, err := h.authorizeLockQuery(r, ws, &GetFeatureRequest{TypeNames: parseTypeNames(query.TypeNames)}); err != nil {
			WriteExceptionFromError(w, err)
			return
		}
	}

	for _, query := range req.Queries {
		layer, service := resolveFeatureLayer(ws, query.TypeNames, h.cfg.WFS.AppNamespacePrefix)
		if layer == nil || service == nil {
			WriteException(w, ExceptionInvalidParameterValue, "typeNames", fmt.Sprintf("Unknown type name: %s", query.TypeNames))
			return
		}

		if service.DataSource == nil {
			WriteException(w, ExceptionNoApplicableCode, "", fmt.Sprintf("Service not available for layer: %s", layer.PublicID))
			return
		}

		// Get features matching the filter
		featureIDs, err := h.getFeatureIDsForLock(ctx, ws, layer.SourceLayer, service.DataSource, query)
		if err != nil {
			WriteExceptionFromError(w, err)
			return
		}

		featuresToLock[sourceLockKey(service, layer, query.TypeNames)] = featureIDs
	}

	// Acquire the lock
	lock, notLocked, err := h.state.Locks.AcquireLockOwnedContext(ctx, ws.ID, featuresToLock, expiry, lockAction, lockOwner(r))
	if err != nil {
		h.logger.Error("wfs lock acquisition failed", "err", err)
		WriteException(w, "CannotLockAllFeatures", "", "could not acquire lock on all requested features")
		return
	}

	// Build response
	response := &LockFeatureResponse{
		LockID:            lock.LockID,
		FeaturesLocked:    []string{},
		FeaturesNotLocked: notLocked,
	}

	for typeName, featureIDs := range lock.FeatureIDs {
		// Strip namespace prefix to match gml:id format (e.g., "Forests.8" not "app:Forests.8")
		localTypeName := lockDisplayName(typeName)
		for _, fid := range featureIDs {
			response.FeaturesLocked = append(response.FeaturesLocked, fmt.Sprintf("%s.%s", localTypeName, fid))
		}
	}

	WriteLockFeatureResponse(w, response)
}

// getFeatureIDsForLock retrieves feature IDs matching the query for locking
func (h *workspaceHandler) getFeatureIDsForLock(ctx context.Context, ws *workspace.Workspace, sourceLayer string, ds datasource.DataSource, query XMLQuery) ([]string, error) {
	// Get layer info
	layerInfo, err := ds.GetLayerInfo(ctx, sourceLayer)
	if err != nil {
		return nil, err
	}

	if layerInfo.IDColumn == "" {
		return nil, fmt.Errorf("layer %s has no ID column - cannot lock features", sourceLayer)
	}

	// Build query params with filter
	params := datasource.QueryParams{
		OutputSRID: layerInfo.SRID,
		Limit:      10000, // Reasonable limit for locking
	}

	// Extract and compile filter if present
	if query.FilterRaw != "" {
		filterXML, err := extractFilterFromXML(query.FilterRaw)
		if err != nil {
			return nil, err
		}
		if filterXML != "" {
			fesFilter, err := ParseFESFilter(filterXML)
			if err != nil {
				return nil, err
			}

			// Build allowed properties set
			allowed := make(map[string]struct{})
			for _, prop := range layerInfo.Properties {
				allowed[prop.Name] = struct{}{}
			}
			if layerInfo.IDColumn != "" {
				allowed[layerInfo.IDColumn] = struct{}{}
			}

			// Extract collection ID
			collectionID := query.TypeNames
			if idx := strings.Index(collectionID, ":"); idx >= 0 {
				collectionID = collectionID[idx+1:]
			}

			filterSQL, filterArgs, _, err := CompileFES(fesFilter, FESCompileOptions{
				StartParamIndex:   1,
				SourceSRID:        layerInfo.SRID,
				GeometryProperty:  layerInfo.GeometryColumn,
				AllowedProperties: allowed,
				CollectionID:      collectionID,
				IDColumn:          layerInfo.IDColumn,
			})
			if err != nil {
				return nil, err
			}

			if filterSQL != "" {
				params.CompiledFilter = filterSQL
				params.CompiledFilterArgs = filterArgs
				params.CompiledFilterParamOffset = 1
			}
		}
	}

	// Query features
	features, err := ds.Query(ctx, sourceLayer, params)
	if err != nil {
		return nil, err
	}

	// Extract IDs from features
	var ids []string
	for _, f := range features {
		var feature map[string]interface{}
		decoder := json.NewDecoder(strings.NewReader(string(f)))
		decoder.UseNumber()
		if err := decoder.Decode(&feature); err != nil {
			continue
		}
		if id, ok := feature["id"]; ok {
			ids = append(ids, fmt.Sprintf("%v", id))
		}
	}

	return ids, nil
}

// handleGetFeatureWithLock handles WFS GetFeatureWithLock requests
func (h *workspaceHandler) handleGetFeatureWithLock(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	// GetFeatureWithLock is essentially GetFeature + LockFeature combined
	ctx := r.Context()
	q := NormalizeQuery(r)

	// Default values from query params
	expiry := DefaultLockExpiry
	if expiryStr := q.Get("EXPIRY"); expiryStr != "" {
		if e, err := strconv.Atoi(expiryStr); err == nil && e > 0 {
			expiry = e
		}
	}

	lockAction := LockActionAll
	if strings.ToUpper(q.Get("LOCKACTION")) == LockActionSome {
		lockAction = LockActionSome
	}

	// Parse request - for POST, parse XML body first
	var typeNames []string
	var filter string
	var count int
	var resultType string

	if r.Method == http.MethodPost {
		body, err := GetBodyBytes(r)
		if err == nil && len(body) > 0 {
			// Parse namespace bindings to resolve dynamic prefixes
			nsMap := parseXMLWithNamespaces(body)

			var xmlReq XMLGetFeatureWithLock
			if err := xml.Unmarshal(body, &xmlReq); err == nil {
				// Extract expiry and lock action from XML
				if xmlReq.Expiry != "" {
					if e, err := strconv.Atoi(xmlReq.Expiry); err == nil && e > 0 {
						expiry = e
					}
				}
				if strings.ToUpper(xmlReq.LockAction) == LockActionSome {
					lockAction = LockActionSome
				}
				if xmlReq.Count != "" {
					if c, err := strconv.Atoi(xmlReq.Count); err == nil {
						count = c
					}
				}
				if xmlReq.ResultType != "" {
					resultType = strings.ToLower(xmlReq.ResultType)
				}

				// Extract typeNames from Query elements
				for _, query := range xmlReq.Queries {
					if query.TypeNames != "" {
						rawNames := parseTypeNames(query.TypeNames)
						for _, name := range rawNames {
							resolved := resolveTypeName(name, nsMap)
							typeNames = append(typeNames, resolved)
						}
					}
					// Extract filter from Query
					if query.FilterRaw != "" {
						filterXML, err := extractFilterFromXML(query.FilterRaw)
						if err != nil {
							WriteExceptionFromError(w, err)
							return
						}
						if filterXML != "" {
							filter = filterXML
						}
					}
				}

				// Handle StoredQuery (GetFeatureById)
				if xmlReq.StoredQuery != nil {
					if !IsGetFeatureByIdQuery(xmlReq.StoredQuery.ID) {
						WriteException(w, ExceptionOperationNotSupported, "storedQueryId", "Only GetFeatureById is supported for locking")
						return
					}
					foundID := false
					for _, param := range xmlReq.StoredQuery.Parameters {
						if strings.ToUpper(param.Name) == "ID" {
							foundID = true
							// param.Value is like "Autos.15" - extract typeName
							typeName, _, resolved := resolveFeatureIdentifier(ws, param.Value, h.cfg.WFS.AppNamespacePrefix)
							if !resolved {
								WriteException(w, ExceptionNotFound, "ID", "Feature not found")
								return
							}
							typeNames = []string{typeName}
							// Create ResourceId filter
							filter = fmt.Sprintf(`<fes:Filter xmlns:fes="%s"><fes:ResourceId rid="%s"/></fes:Filter>`, NSFes, escapeXML(param.Value))
						}
					}
					if !foundID {
						WriteException(w, ExceptionMissingParameterValue, "ID", "ID is required")
						return
					}
				}
			}
		}
	}

	// Fall back to ParseGetFeatureRequest for KVP or if XML parsing didn't find typeNames
	maxFeatures, defaultCount, maxOffset := h.featureLimits(ws)
	req, err := ParseGetFeatureRequest(r, maxFeatures, defaultCount)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}

	// Override with XML values if present
	if len(typeNames) > 0 {
		req.TypeNames = typeNames
	}
	if filter != "" {
		req.Filter = filter
	}
	if count > 0 {
		req.Count = min(count, maxFeatures)
	}
	if resultType != "" {
		req.ResultType = resultType
	}
	if req.StoredQueryID != "" {
		if !IsGetFeatureByIdQuery(req.StoredQueryID) {
			WriteException(w, ExceptionOperationNotSupported, "storedQueryId", "Only GetFeatureById is supported for locking")
			return
		}
		id := req.StoredQueryParams["ID"]
		name, _, ok := resolveFeatureIdentifier(ws, id, h.cfg.WFS.AppNamespacePrefix)
		if !ok {
			WriteException(w, ExceptionNotFound, "ID", "Feature not found")
			return
		}
		req.TypeNames, req.ResourceID, req.Filter = []string{name}, []string{id}, ""
	}
	if maxOffset > 0 && req.StartIndex > maxOffset {
		WriteException(w, ExceptionInvalidParameterValue, "startIndex", "STARTINDEX exceeds the configured maximum")
		return
	}

	if len(req.TypeNames) == 0 {
		WriteException(w, ExceptionMissingParameterValue, "typeNames", "TYPENAMES parameter is required")
		return
	}

	// Validate resultType - "hits" is not allowed for GetFeatureWithLock
	// per WFS 2.0 spec (ISO 19142: 13.2.4.3), features must be returned to provide the lockId
	if req.ResultType == ResultTypeHits {
		WriteException(w, ExceptionInvalidParameterValue, "resultType", "resultType 'hits' is not supported for GetFeatureWithLock. Features must be returned to provide the lockId.")
		return
	}

	typeName := req.TypeNames[0]
	layer, service, err := h.authorizeLockQuery(r, ws, req)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}
	responseTypeName := publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)

	layerInfo, err := service.DataSource.GetLayerInfo(ctx, layer.SourceLayer)
	if err != nil {
		h.writeInternalError(w, "Failed to get layer info", err)
		return
	}

	outputSRID := req.SRID
	if outputSRID == 0 {
		outputSRID = layerInfo.SRID
		if outputSRID == 0 {
			outputSRID = 4326
		}
	}

	params, err := h.buildQueryParams(req, layerInfo, outputSRID, typeName)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}

	// Query features
	features, err := service.DataSource.Query(ctx, layer.SourceLayer, params)
	if err != nil {
		h.writeInternalError(w, "Query failed", err)
		return
	}

	// Extract feature IDs for locking
	featuresToLock := make(map[string][]string)
	var featureIDs []string
	for _, f := range features {
		var feature map[string]interface{}
		decoder := json.NewDecoder(strings.NewReader(string(f)))
		decoder.UseNumber()
		if err := decoder.Decode(&feature); err != nil {
			continue
		}
		if id, ok := feature["id"]; ok {
			featureIDs = append(featureIDs, fmt.Sprintf("%v", id))
		}
	}
	key := sourceLockKey(service, layer, typeName)
	featuresToLock[key] = featureIDs

	// Acquire lock
	lock, _, err := h.state.Locks.AcquireLockOwnedContext(ctx, ws.ID, featuresToLock, expiry, lockAction, lockOwner(r))
	if err != nil {
		h.logger.Error("wfs lock acquisition failed", "err", err)
		WriteException(w, "CannotLockAllFeatures", "", "could not acquire lock on all requested features")
		return
	}

	// Build a set of successfully locked feature IDs
	lockedIDs := make(map[string]bool)
	for _, fid := range lockedFeatureIDs(lock.FeatureIDs, key) {
		lockedIDs[fid] = true
	}

	// Filter features to only include those that were successfully locked
	// This is important for lockAction=SOME where some features may already be locked
	var lockedFeatures []json.RawMessage
	for _, f := range features {
		var feature map[string]interface{}
		decoder := json.NewDecoder(strings.NewReader(string(f)))
		decoder.UseNumber()
		if err := decoder.Decode(&feature); err != nil {
			continue
		}
		if id, ok := feature["id"]; ok {
			idStr := fmt.Sprintf("%v", id)
			if lockedIDs[idStr] {
				lockedFeatures = append(lockedFeatures, f)
			}
		}
	}

	// Get total count (of locked features)
	totalCount := len(lockedFeatures)

	featuresBytes := make([][]byte, len(lockedFeatures))
	for i, f := range lockedFeatures {
		featuresBytes[i] = []byte(f)
	}

	baseURL := h.workspaceBaseURL(r, ws.Name)

	// Write response with lock ID in response XML (WFS 2.0 spec requirement)
	WriteGMLFeatureCollectionWithLock(w, layerInfo, featuresBytes, totalCount, req.StartIndex, req.Count,
		h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix, outputSRID, baseURL, responseTypeName, lock.LockID)
}

// WriteLockFeatureResponse writes a LockFeature response
func WriteLockFeatureResponse(w http.ResponseWriter, response *LockFeatureResponse) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<wfs:LockFeatureResponse xmlns:wfs="%s" xmlns:fes="%s" lockId="%s">
`, NSWfs, NSFes, escapeXML(response.LockID))

	if len(response.FeaturesLocked) > 0 {
		fmt.Fprintf(w, "  <wfs:FeaturesLocked>\n")
		for _, rid := range response.FeaturesLocked {
			fmt.Fprintf(w, `    <fes:ResourceId rid="%s"/>`+"\n", escapeXML(rid))
		}
		fmt.Fprintf(w, "  </wfs:FeaturesLocked>\n")
	}

	if len(response.FeaturesNotLocked) > 0 {
		fmt.Fprintf(w, "  <wfs:FeaturesNotLocked>\n")
		for _, rid := range response.FeaturesNotLocked {
			fmt.Fprintf(w, `    <fes:ResourceId rid="%s"/>`+"\n", escapeXML(rid))
		}
		fmt.Fprintf(w, "  </wfs:FeaturesNotLocked>\n")
	}

	fmt.Fprintf(w, "</wfs:LockFeatureResponse>")
}
