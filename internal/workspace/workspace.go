// Package workspace provides runtime workspace management for neoserver.
package workspace

import (
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/store"
)

// Workspace represents a runtime workspace with its services and layers.
type Workspace struct {
	ID          string
	Name        string
	Description string
	// TileRevision changes whenever workspace-wide tile rendering settings
	// change, making durable identities safe across configuration updates.
	TileRevision         int64
	capabilitiesRevision *atomic.Int64
	// StyleAssetDigest identifies the committed asset manifest independently of
	// transient runtime revisions, so cache keys remain safe across restarts.
	StyleAssetDigest string
	StyleAssets      map[string]string
	Services         map[string]*Service    // keyed by service ID
	Styles           map[string]*Style      // keyed by style name
	Groups           map[string]*LayerGroup // keyed by public identifier
	Settings         *store.WorkspaceSettings
	dataState        *dataState
	mu               sync.RWMutex
}

// Style represents a runtime SLD style.
type Style struct {
	ID               string
	Name             string
	Title            string
	Description      string
	SLDBody          string
	Format           string
	Document         *sld.StyledLayerDescriptor
	Diagnostics      []sld.Diagnostic
	Valid            bool
	ValidationErrors []string
}

// CompiledDocument returns the immutable runtime document. The fallback keeps
// tests and integrations that construct Workspace values directly compatible.
func (s *Style) CompiledDocument() (*sld.StyledLayerDescriptor, error) {
	if s == nil {
		return nil, sld.ErrStyleMissing
	}
	if s.Document != nil {
		return s.Document, nil
	}
	doc, _, err := sld.Compile(s.Format, s.SLDBody)
	return doc, err
}

// Service represents a runtime data source within a workspace.
type Service struct {
	ID             string
	Name           string
	Type           store.ServiceType
	ConnectionInfo json.RawMessage
	Enabled        bool
	Layers         map[string]*Layer    // keyed by public ID
	Coverages      map[string]*Coverage // keyed by public ID
	DataSource     datasource.DataSource
	CoverageSource datasource.CoverageDataSource
	CacheSettings  *store.ServiceCacheSettings
}

// Coverage represents a published raster coverage.
type Coverage struct {
	ID                   string
	SourceCoverage       string
	PublicID             string
	Title                string
	Description          string
	Enabled              bool
	Public               bool
	AllowedRoles         []string
	RangeFields          []store.CoverageRangeField
	Dimensions           []*Dimension
	DefaultStyle         string
	Styles               []string
	Resampling           string
	WCS20CoverageSubtype string
	NativeExtent         *store.SpatialExtent
	TileCacheQuotaBytes  int64
	TileCacheGeneration  int64
}

// VisibleToRole applies the same publication rules used for feature layers.
func (c *Coverage) VisibleToRole(role string) bool {
	if c == nil || !c.Enabled {
		return false
	}
	if c.Public || role == "super_admin" || len(c.AllowedRoles) == 0 {
		return true
	}
	for _, allowed := range c.AllowedRoles {
		if allowed == role {
			return true
		}
	}
	return false
}

// GetCoverage resolves an enabled published coverage by public identifier.
func (w *Workspace) GetCoverage(publicID string) (*Service, *Coverage) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, svc := range w.Services {
		if !svc.Enabled {
			continue
		}
		if coverage := svc.Coverages[publicID]; coverage != nil && coverage.Enabled {
			return svc, coverage
		}
	}
	return nil, nil
}

// VisibleCoverages returns enabled coverages visible to a workspace role.
func (w *Workspace) VisibleCoverages(role string) []*Coverage {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var result []*Coverage
	for _, svc := range w.Services {
		if !svc.Enabled || svc.CoverageSource == nil {
			continue
		}
		for _, coverage := range svc.Coverages {
			if coverage.VisibleToRole(role) {
				result = append(result, coverage)
			}
		}
	}
	return result
}

// FeatureCachePolicy resolves this service's feature-cache overrides against
// the process-wide defaults. A non-positive override disables storage.
func (s *Service) FeatureCachePolicy(defaultTTL time.Duration) (bool, time.Duration) {
	enabled, ttl := true, defaultTTL
	if s != nil && s.CacheSettings != nil {
		if s.CacheSettings.FeaturesEnabled != nil {
			enabled = *s.CacheSettings.FeaturesEnabled
		}
		if s.CacheSettings.FeaturesTTLSec != nil {
			ttl = time.Duration(*s.CacheSettings.FeaturesTTLSec) * time.Second
		}
	}
	return enabled && ttl > 0, ttl
}

// TileCachePolicy resolves this service's rendered-map cache overrides against
// the process-wide defaults.
func (s *Service) TileCachePolicy(defaultTTL time.Duration) (bool, time.Duration) {
	enabled, ttl := true, defaultTTL
	if s != nil && s.CacheSettings != nil {
		if s.CacheSettings.TilesEnabled != nil {
			enabled = *s.CacheSettings.TilesEnabled
		}
		if s.CacheSettings.TilesTTLSec != nil {
			ttl = time.Duration(*s.CacheSettings.TilesTTLSec) * time.Second
		}
	}
	return enabled && ttl > 0, ttl
}

// Dimension represents a WMS dimension (e.g., time, elevation).
type Dimension struct {
	Name           string // e.g., "time", "elevation"
	Units          string // e.g., "ISO8601" for time
	SourceAxis     string // Coverage axis label used by WCS and raster readers.
	SourceProperty string // Feature attribute containing the dimension start/value.
	EndProperty    string // Optional feature attribute containing an interval end.
	Default        string // Default value
	MultipleValues bool   // Allow multiple values
	NearestValue   bool   // Support nearest value matching
	Current        bool   // Support "current" keyword
	Extent         string // Value extent, e.g., "2000-01-01T00:00:00Z/2000-01-01T00:01:00Z/PT5S"
}

// Layer represents a published feature type from a service.
type Layer struct {
	ID            string
	SourceLayer   string // actual table/layer name in the data source
	PublicID      string // public identifier for URLs
	Title         string
	Description   string
	Enabled       bool
	CRSDefault    int
	Dimensions    []*Dimension   // WMS dimensions (time, elevation, etc.)
	IsSQLView     bool           // true if this layer is a SQL View
	SQLViewConfig *SQLViewConfig // SQL View configuration (if IsSQLView is true)

	// Public marks the layer as readable by anyone who can reach the service
	// (including anonymous requests on a public service), bypassing AllowedRoles.
	Public bool
	// AllowedRoles, when non-empty, restricts read access to these workspace roles
	// (super_admin always allowed). Empty means any principal with workspace access.
	AllowedRoles        []string
	DefaultStyle        string
	Styles              []string
	NativeExtent        *store.SpatialExtent
	TileCacheQuotaBytes int64
	TileCacheParameters *store.TileCacheParameterPolicy
	TileCacheGeneration int64
}

// ResourceKind identifies which source model backs a portrayed collection.
type ResourceKind string

const (
	ResourceFeature  ResourceKind = "feature"
	ResourceCoverage ResourceKind = "coverage"
	ResourceGroup    ResourceKind = "group"
)

type LayerGroup struct {
	ID, PublicID, Title, Description string
	Enabled, Public                  bool
	AllowedRoles                     []string
	Members                          []store.LayerGroupMember
	DefaultStyle                     string
	Styles                           []string
	NativeExtent                     *store.SpatialExtent
	TileCacheQuotaBytes              int64
	TileCacheGeneration              int64
}

// PublishedResource is the common read-only view used by WMS and map tiles.
// Feature layers retain precedence when legacy records share an identifier.
type PublishedResource struct {
	Kind     ResourceKind
	Service  *Service
	Layer    *Layer
	Coverage *Coverage
	Group    *LayerGroup
}

// CacheIdentity is shared by management clients for every portrayed resource.
func (r *PublishedResource) CacheIdentity() (string, int64) {
	if r == nil {
		return "", 0
	}
	if r.Layer != nil {
		return r.Layer.ID, r.Layer.TileCacheQuotaBytes
	}
	if r.Coverage != nil {
		return r.Coverage.ID, r.Coverage.TileCacheQuotaBytes
	}
	if r.Group != nil {
		return r.Group.ID, r.Group.TileCacheQuotaBytes
	}
	return "", 0
}

func (r *PublishedResource) PublicID() string {
	if r == nil {
		return ""
	}
	if r.Layer != nil {
		return r.Layer.PublicID
	}
	if r.Coverage != nil {
		return r.Coverage.PublicID
	}
	if r.Group != nil {
		return r.Group.PublicID
	}
	return ""
}

// GetResource resolves a portrayed resource. An existing feature layer wins
// over a coverage with the same public identifier to preserve existing routes.
func (w *Workspace) GetResource(publicID string) *PublishedResource {
	if layer, service := w.GetLayer(publicID); layer != nil && service != nil {
		return &PublishedResource{Kind: ResourceFeature, Service: service, Layer: layer}
	}
	service, coverage := w.GetCoverage(publicID)
	if service != nil && coverage != nil {
		return &PublishedResource{Kind: ResourceCoverage, Service: service, Coverage: coverage}
	}
	w.mu.RLock()
	group := w.Groups[publicID]
	w.mu.RUnlock()
	if group != nil && group.Enabled {
		return &PublishedResource{Kind: ResourceGroup, Group: group}
	}
	return nil
}

// VisibleResources returns feature layers followed by non-conflicting
// coverages. Existing feature collection ordering/semantics remain unchanged.
func (w *Workspace) VisibleResources(role string) []*PublishedResource {
	resources := make([]*PublishedResource, 0)
	seen := make(map[string]struct{})
	for _, layer := range w.VisibleLayers(role) {
		_, service := w.GetLayer(layer.PublicID)
		resources = append(resources, &PublishedResource{Kind: ResourceFeature, Service: service, Layer: layer})
		seen[layer.PublicID] = struct{}{}
	}
	for _, coverage := range w.VisibleCoverages(role) {
		if _, conflict := seen[coverage.PublicID]; conflict {
			continue
		}
		service, _ := w.GetCoverage(coverage.PublicID)
		resources = append(resources, &PublishedResource{Kind: ResourceCoverage, Service: service, Coverage: coverage})
		seen[coverage.PublicID] = struct{}{}
	}
	w.mu.RLock()
	groups := make([]*LayerGroup, 0, len(w.Groups))
	for _, group := range w.Groups {
		groups = append(groups, group)
	}
	w.mu.RUnlock()
	for _, group := range groups {
		if _, conflict := seen[group.PublicID]; !conflict && w.GroupVisibleToRole(group, role) {
			resources = append(resources, &PublishedResource{Kind: ResourceGroup, Group: group})
		}
	}
	return resources
}

func (w *Workspace) GroupVisibleToRole(group *LayerGroup, role string) bool {
	return w.groupVisibleToRole(group, role, make(map[string]bool), 0)
}

func (w *Workspace) groupVisibleToRole(group *LayerGroup, role string, visiting map[string]bool, depth int) bool {
	if group == nil || !group.Enabled || depth > 32 || visiting[group.PublicID] {
		return false
	}
	allowed := group.Public || role == "super_admin" || len(group.AllowedRoles) == 0
	if !allowed {
		for _, candidate := range group.AllowedRoles {
			if candidate == role {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return false
	}
	visiting[group.PublicID] = true
	defer delete(visiting, group.PublicID)
	for _, member := range group.Members {
		resource := w.GetResource(member.Resource)
		if resource == nil {
			return false
		}
		switch resource.Kind {
		case ResourceFeature:
			if !resource.Layer.VisibleToRole(role) {
				return false
			}
		case ResourceCoverage:
			if !resource.Coverage.VisibleToRole(role) {
				return false
			}
		case ResourceGroup:
			if !w.groupVisibleToRole(resource.Group, role, visiting, depth+1) {
				return false
			}
		}
	}
	return true
}

// VisibleToRole reports whether a principal holding the given workspace role may
// read this layer. An empty role means an anonymous / no-workspace-role request
// (only reachable on a public service, which bypasses authentication).
//
// The check runs after service-level access has already been established
// (requireAuth), so it only adds per-layer restriction on top of that:
//   - Public layers are always visible (explicit public override).
//   - super_admin always passes.
//   - When AllowedRoles is empty the layer is visible to anyone who reached the
//     service — any workspace role on a private service, or anonymous on a public
//     service. This preserves prior behavior for layers with no restriction set.
//   - Otherwise the role must appear in AllowedRoles.
func (l *Layer) VisibleToRole(role string) bool {
	if l == nil {
		return false
	}
	if l.Public {
		return true
	}
	if role == "super_admin" {
		return true
	}
	if len(l.AllowedRoles) == 0 {
		return true
	}
	for _, r := range l.AllowedRoles {
		if r == role {
			return true
		}
	}
	return false
}

// SQLViewConfig contains configuration for a SQL View layer.
type SQLViewConfig struct {
	SQL            string
	GeometryColumn string
	GeometryType   string
	SRID           int
	IDColumn       string
	Properties     []*SQLViewProperty
	ReadOnly       bool
}

// SQLViewProperty describes a property/column in a SQL view.
type SQLViewProperty struct {
	Name string
	Type string // string, number, integer, boolean
}

// AddService adds a service to the workspace.
func (w *Workspace) AddService(svc *Service) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Services == nil {
		w.Services = make(map[string]*Service)
	}
	w.Services[svc.ID] = svc
}

// GetService returns a service by ID.
func (w *Workspace) GetService(id string) *Service {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Services[id]
}

// GetServiceByName returns a service by name.
func (w *Workspace) GetServiceByName(name string) *Service {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, svc := range w.Services {
		if svc.Name == name {
			return svc
		}
	}
	return nil
}

// ResolveService returns a service by ID or name.
// It first tries to find by ID, then falls back to name lookup.
func (w *Workspace) ResolveService(identifier string) *Service {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Try by ID first
	if svc, ok := w.Services[identifier]; ok {
		return svc
	}

	// Fall back to name lookup
	for _, svc := range w.Services {
		if svc.Name == identifier {
			return svc
		}
	}
	return nil
}

// RemoveService removes a service from the workspace.
func (w *Workspace) RemoveService(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if svc, ok := w.Services[id]; ok {
		if svc.DataSource != nil {
			svc.DataSource.Close()
		}
		delete(w.Services, id)
	}
}

// GetAllLayers returns all enabled layers across all services.
func (w *Workspace) GetAllLayers() []*Layer {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var layers []*Layer
	for _, svc := range w.Services {
		if !svc.Enabled {
			continue
		}
		for _, layer := range svc.Layers {
			if layer.Enabled {
				layers = append(layers, layer)
			}
		}
	}
	sort.Slice(layers, func(i, j int) bool {
		return layers[i].PublicID < layers[j].PublicID
	})
	return layers
}

// GetCatalogLayers returns enabled and disabled published feature layers.
func (w *Workspace) GetCatalogLayers() []*Layer {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var layers []*Layer
	for _, service := range w.Services {
		for _, layer := range service.Layers {
			layers = append(layers, layer)
		}
	}
	return layers
}

// GetCatalogCoverages returns enabled and disabled published coverages.
func (w *Workspace) GetCatalogCoverages() []*Coverage {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var coverages []*Coverage
	for _, service := range w.Services {
		for _, coverage := range service.Coverages {
			coverages = append(coverages, coverage)
		}
	}
	return coverages
}

// VisibleLayers returns all enabled layers across all services that the given
// workspace role may read (see Layer.VisibleToRole). Use it to filter collection
// listings and service capabilities so restricted layers are not disclosed.
func (w *Workspace) VisibleLayers(role string) []*Layer {
	all := w.GetAllLayers()
	visible := make([]*Layer, 0, len(all))
	for _, layer := range all {
		if layer.VisibleToRole(role) {
			visible = append(visible, layer)
		}
	}
	return visible
}

// GetLayer returns a layer by its public ID.
func (w *Workspace) GetLayer(publicID string) (*Layer, *Service) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for _, svc := range w.Services {
		if !svc.Enabled {
			continue
		}
		if layer, ok := svc.Layers[publicID]; ok && layer.Enabled {
			return layer, svc
		}
	}
	return nil, nil
}

// AddLayer adds a layer to a service.
func (s *Service) AddLayer(layer *Layer) {
	if s.Layers == nil {
		s.Layers = make(map[string]*Layer)
	}
	s.Layers[layer.PublicID] = layer
}

// RemoveLayer removes a layer from a service.
func (s *Service) RemoveLayer(publicID string) {
	delete(s.Layers, publicID)
}

// AddStyle adds a style to the workspace.
func (w *Workspace) AddStyle(style *Style) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Styles == nil {
		w.Styles = make(map[string]*Style)
	}
	w.Styles[style.Name] = style
}

// GetStyle returns a style by name.
func (w *Workspace) GetStyle(name string) *Style {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Styles[name]
}

// RemoveStyle removes a style from the workspace.
func (w *Workspace) RemoveStyle(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.Styles, name)
}

// GetAllStyles returns all styles in the workspace.
func (w *Workspace) GetAllStyles() []*Style {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var styles []*Style
	for _, style := range w.Styles {
		styles = append(styles, style)
	}
	return styles
}

// GetAllLayerGroups returns a snapshot of the workspace's catalogued groups.
func (w *Workspace) GetAllLayerGroups() []*LayerGroup {
	w.mu.RLock()
	defer w.mu.RUnlock()

	groups := make([]*LayerGroup, 0, len(w.Groups))
	for _, group := range w.Groups {
		groups = append(groups, group)
	}
	return groups
}

// HasPublishedResourceID checks the complete runtime catalog, including
// disabled resources. Public identifiers must remain unambiguous if a resource
// is enabled later.
func (w *Workspace) HasPublishedResourceID(publicID string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if _, ok := w.Groups[publicID]; ok {
		return true
	}
	for _, service := range w.Services {
		if _, ok := service.Layers[publicID]; ok {
			return true
		}
		if _, ok := service.Coverages[publicID]; ok {
			return true
		}
	}
	return false
}

// GroupReferences returns the groups that directly reference a resource.
func (w *Workspace) GroupReferences(publicID string) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var result []string
	for _, group := range w.Groups {
		for _, member := range group.Members {
			if member.Resource == publicID {
				result = append(result, group.PublicID)
				break
			}
		}
	}
	return result
}
