package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/store"
)

// DataSourceError reports that a service's configuration was accepted by the
// catalog but its data source could not be opened (unreachable database,
// missing or disallowed path). Handlers present it as a client error.
type DataSourceError struct{ Err error }

func (e *DataSourceError) Error() string { return "open data source: " + e.Err.Error() }
func (e *DataSourceError) Unwrap() error { return e.Err }

var (
	// ErrWorkspaceNotFound is returned when a workspace doesn't exist.
	ErrWorkspaceNotFound = errors.New("workspace not found")

	// ErrServiceNotFound is returned when a service doesn't exist.
	ErrServiceNotFound = errors.New("service not found")

	// ErrLayerNotFound is returned when a layer doesn't exist.
	ErrLayerNotFound = errors.New("layer not found")

	// ErrResourceReferenced prevents publication changes that would leave a
	// persisted layer group with a dangling member.
	ErrResourceReferenced = errors.New("resource is referenced by a layer group")
)

// DataSourceFactory creates a DataSource from service configuration.
type DataSourceFactory func(svc *store.Service) (datasource.DataSource, error)

// Registry manages workspaces at runtime.
type Registry struct {
	capabilitiesRevisions sync.Map // workspace ID -> *atomic.Int64
	store                 store.Store
	workspaces            map[string]*Workspace // keyed by name
	workspacesByID        map[string]*Workspace // keyed by ID
	dataSourceFactory     DataSourceFactory
	cache                 *cache.Manager
	mu                    sync.RWMutex
	sourcesMu             sync.Mutex
	sources               map[datasource.DataSource]*sourceReference
	serviceUpdates        sync.Map        // service ID -> *sync.Mutex; preparation never holds the registry lock
	quiescedServices      map[string]bool // guarded by mu; late refreshes cannot resurrect deleted services
}

// NewRegistry creates a new workspace registry.
func NewRegistry(s store.Store, factory DataSourceFactory) *Registry {
	return &Registry{
		store:             s,
		workspaces:        make(map[string]*Workspace),
		workspacesByID:    make(map[string]*Workspace),
		dataSourceFactory: factory,
	}
}

// SetCacheManager sets the cache manager for cache invalidation.
func (r *Registry) SetCacheManager(cm *cache.Manager) {
	r.cache = cm
}

// invalidateWorkspaceCache invalidates all caches for a workspace.
func (r *Registry) invalidateWorkspaceCache(workspaceID string) {
	r.refreshCapabilitiesRevision(context.Background(), workspaceID)
	if r.cache != nil {
		r.cache.InvalidateWorkspace(workspaceID)
	}
}

// invalidateLayerCache invalidates caches when a layer changes.
func (r *Registry) invalidateLayerCache(workspaceID, layerID string) {
	r.refreshCapabilitiesRevision(context.Background(), workspaceID)
	if r.cache != nil {
		r.cache.InvalidateLayer(workspaceID, layerID)
	}
}

// invalidateCapabilitiesCache invalidates capabilities caches for a workspace.
func (r *Registry) invalidateCapabilitiesCache(workspaceID string) {
	r.refreshCapabilitiesRevision(context.Background(), workspaceID)
	if r.cache != nil {
		r.cache.InvalidateCapabilities(workspaceID)
		r.cache.InvalidateCollections(workspaceID)
	}
}

// Load loads all workspaces from the store into memory.
func (r *Registry) Load(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear existing workspaces
	for _, ws := range r.workspaces {
		for _, svc := range ws.Services {
			if svc.DataSource != nil {
				r.retireSource(svc.DataSource)
			}
		}
	}
	r.workspaces = make(map[string]*Workspace)
	r.workspacesByID = make(map[string]*Workspace)

	// Load workspaces from store
	workspaces, err := r.store.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to load workspaces: %w", err)
	}

	for _, wsData := range workspaces {
		ws := &Workspace{
			ID:          wsData.ID,
			Name:        wsData.Name,
			Description: wsData.Description,
			Services:    make(map[string]*Service),
			Styles:      make(map[string]*Style),
			Groups:      make(map[string]*LayerGroup),
		}

		// Load workspace settings
		wmsSettings, _ := r.store.GetWMSSettings(ctx, wsData.ID)
		wfsSettings, _ := r.store.GetWFSSettings(ctx, wsData.ID)
		ogcapiSettings, _ := r.store.GetOGCAPISettings(ctx, wsData.ID)
		ogcTilesSettings, _ := r.store.GetOGCTilesAPISettings(ctx, wsData.ID)
		var wcsSettings *store.WCSSettings
		if coverageStore, ok := r.store.(store.CoverageStore); ok {
			wcsSettings, _ = coverageStore.GetWCSSettings(ctx, wsData.ID)
		}
		var wmtsSettings *store.WMTSSettings
		tileRevision := int64(1)
		if wmtsStore, ok := r.store.(store.WMTSStore); ok {
			wmtsSettings, _ = wmtsStore.GetWMTSSettings(ctx, wsData.ID)
			if revision, revisionErr := wmtsStore.GetTileRevision(ctx, wsData.ID); revisionErr == nil && revision > 0 {
				tileRevision = revision
			}
		}
		ws.TileRevision = tileRevision
		ws.capabilitiesRevision = r.loadCapabilitiesRevision(ctx, wsData.ID)
		assetDigest, assets, assetErr := r.styleAssetManifest(ctx, ws.ID)
		if assetErr != nil {
			return fmt.Errorf("load workspace style assets: %w", assetErr)
		}
		ws.StyleAssetDigest = assetDigest
		ws.StyleAssets = assets
		dataRevision, pending := int64(1), false
		if revisions, ok := r.store.(store.DataRevisionStore); ok {
			var revisionErr error
			dataRevision, pending, revisionErr = revisions.GetDataRevision(ctx, ws.ID)
			if revisionErr != nil {
				return fmt.Errorf("load data revision: %w", revisionErr)
			}
		}
		ws.dataState = newDataState(dataRevision, pending)
		ws.Settings = &store.WorkspaceSettings{
			WMS:         derefWMSSettings(wmsSettings),
			WFS:         derefWFSSettings(wfsSettings),
			OGCAPI:      derefOGCAPISettings(ogcapiSettings),
			OGCTilesAPI: derefOGCTilesAPISettings(ogcTilesSettings),
			WCS:         derefWCSSettings(wcsSettings),
			WMTS:        derefWMTSSettings(wmtsSettings),
		}

		// Load styles for this workspace
		styles, err := r.store.ListStyles(ctx, wsData.ID)
		if err != nil {
			return fmt.Errorf("failed to load styles for workspace %s: %w", wsData.Name, err)
		}
		for _, styleData := range styles {
			document, diagnostics, valid, validationErrors := compileRuntimeStyle(styleData.Format, styleData.SLDBody)
			style := &Style{
				ID:               styleData.ID,
				Name:             styleData.Name,
				Title:            styleData.Title,
				Description:      styleData.Description,
				SLDBody:          styleData.SLDBody,
				Format:           styleData.Format,
				Document:         document,
				Diagnostics:      diagnostics,
				Valid:            valid,
				ValidationErrors: validationErrors,
			}
			ws.Styles[style.Name] = style
		}
		if groupStore, ok := r.store.(store.LayerGroupStore); ok {
			groups, groupErr := groupStore.ListLayerGroups(ctx, wsData.ID)
			if groupErr != nil {
				return fmt.Errorf("failed to load layer groups for workspace %s: %w", wsData.Name, groupErr)
			}
			for _, group := range groups {
				ws.Groups[group.PublicID] = runtimeLayerGroup(group)
			}
		}

		// Load services for this workspace
		services, err := r.store.ListServices(ctx, wsData.ID)
		if err != nil {
			return fmt.Errorf("failed to load services for workspace %s: %w", wsData.Name, err)
		}

		for _, svcData := range services {
			svc := &Service{
				ID:             svcData.ID,
				Name:           svcData.Name,
				Type:           svcData.Type,
				ConnectionInfo: svcData.ConnectionInfo,
				CacheSettings:  svcData.CacheSettings,
				Enabled:        svcData.Enabled,
				Layers:         make(map[string]*Layer),
				Coverages:      make(map[string]*Coverage),
			}

			// Load layers for this service
			layers, err := r.store.ListLayers(ctx, svcData.ID)
			if err != nil {
				return fmt.Errorf("failed to load layers for service %s: %w", svcData.Name, err)
			}

			for _, layerData := range layers {
				layer := &Layer{
					ID:                  layerData.ID,
					SourceLayer:         layerData.SourceLayer,
					PublicID:            layerData.PublicID,
					Title:               layerData.Title,
					Description:         layerData.Description,
					Enabled:             layerData.Enabled,
					CRSDefault:          layerData.CRSDefault,
					IsSQLView:           layerData.IsSQLView,
					Public:              layerData.Public,
					AllowedRoles:        layerData.AllowedRoles,
					DefaultStyle:        layerData.DefaultStyle,
					Styles:              append([]string(nil), layerData.Styles...),
					NativeExtent:        cloneExtent(layerData.NativeExtent),
					TileCacheQuotaBytes: layerData.TileCacheQuotaBytes,
					TileCacheParameters: cloneTileCacheParameters(layerData.TileCacheParameters),
					TileCacheGeneration: layerData.TileCacheGeneration,
				}
				// Copy dimensions from store layer to runtime layer
				if len(layerData.Dimensions) > 0 {
					layer.Dimensions = make([]*Dimension, len(layerData.Dimensions))
					for i, dim := range layerData.Dimensions {
						layer.Dimensions[i] = &Dimension{
							Name:           dim.Name,
							Units:          dim.Units,
							SourceAxis:     dim.SourceAxis,
							SourceProperty: dim.SourceProperty,
							EndProperty:    dim.EndProperty,
							Default:        dim.Default,
							MultipleValues: dim.MultipleValues,
							NearestValue:   dim.NearestValue,
							Current:        dim.Current,
							Extent:         dim.Extent,
						}
					}
				}
				// Copy SQL view config from store layer to runtime layer
				if layerData.SQLViewConfig != nil {
					layer.SQLViewConfig = &SQLViewConfig{
						SQL:            layerData.SQLViewConfig.SQL,
						GeometryColumn: layerData.SQLViewConfig.GeometryColumn,
						GeometryType:   layerData.SQLViewConfig.GeometryType,
						SRID:           layerData.SQLViewConfig.SRID,
						IDColumn:       layerData.SQLViewConfig.IDColumn,
						ReadOnly:       layerData.SQLViewConfig.ReadOnly,
					}
					if len(layerData.SQLViewConfig.Properties) > 0 {
						layer.SQLViewConfig.Properties = make([]*SQLViewProperty, len(layerData.SQLViewConfig.Properties))
						for i, prop := range layerData.SQLViewConfig.Properties {
							layer.SQLViewConfig.Properties[i] = &SQLViewProperty{
								Name: prop.Name,
								Type: prop.Type,
							}
						}
					}
				}
				svc.Layers[layer.PublicID] = layer
			}

			if coverageStore, ok := r.store.(store.CoverageStore); ok {
				coverages, err := coverageStore.ListCoverages(ctx, svcData.ID)
				if err != nil {
					return fmt.Errorf("failed to load coverages for service %s: %w", svcData.Name, err)
				}
				for _, item := range coverages {
					svc.Coverages[item.PublicID] = runtimeCoverage(item)
				}
			}

			// Initialize data source if service is enabled
			if svc.Enabled && r.dataSourceFactory != nil {
				ds, err := r.dataSourceFactory(svcData)
				if err != nil {
					// Log error but don't fail - service will be unavailable
					fmt.Printf("Warning: failed to initialize data source for service %s: %v\n", svcData.Name, err)
				} else {
					svc.DataSource = ds
					if coverageSource, ok := ds.(datasource.CoverageDataSource); ok {
						svc.CoverageSource = coverageSource
					}
					quarantineInvalidSQLViews(ctx, svc)
				}
			}

			ws.Services[svc.ID] = svc
		}

		r.workspaces[ws.Name] = ws
		r.workspacesByID[ws.ID] = ws
	}

	return nil
}

func quarantineInvalidSQLViews(ctx context.Context, svc *Service) {
	validator, supported := svc.DataSource.(datasource.SQLViewDataSource)
	for _, layer := range svc.Layers {
		if !layer.Enabled || !layer.IsSQLView {
			continue
		}
		if !supported || layer.SQLViewConfig == nil || validator.ValidateSQLView(ctx, layer.SQLViewConfig.SQL) != nil {
			layer.Enabled = false
		}
	}
}

// Get returns a workspace by name.
func (r *Registry) Get(name string) (*Workspace, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ws, ok := r.workspaces[name]
	return snapshotWorkspace(ws), ok
}

// Acquire returns an isolated workspace snapshot with leased datasource handles.
// Call release after all uses, including response streaming, have completed.
func (r *Registry) Acquire(name string) (ws *Workspace, release func(), ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.acquireLocked(r.workspaces[name])
}

// GetByID returns isolated metadata. Use AcquireByID when operating a datasource.
func (r *Registry) GetByID(id string) (*Workspace, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ws, ok := r.workspacesByID[id]
	return snapshotWorkspace(ws), ok
}

// List returns all workspaces.
func (r *Registry) List() []*Workspace {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workspaces := make([]*Workspace, 0, len(r.workspaces))
	for _, ws := range r.workspaces {
		workspaces = append(workspaces, snapshotWorkspace(ws))
	}
	return workspaces
}

// CreateWorkspace creates a new workspace in the store and registry.
func (r *Registry) CreateWorkspace(ctx context.Context, input store.CreateWorkspaceInput) (*Workspace, error) {
	// Create in store
	wsData, err := r.store.CreateWorkspace(ctx, input)
	if err != nil {
		return nil, err
	}

	ws := &Workspace{
		ID:                   wsData.ID,
		Name:                 wsData.Name,
		Description:          wsData.Description,
		TileRevision:         1,
		capabilitiesRevision: r.loadCapabilitiesRevision(ctx, wsData.ID),
		dataState:            newDataState(1, false),
		Services:             make(map[string]*Service),
		Styles:               make(map[string]*Style),
		Groups:               make(map[string]*LayerGroup),
		Settings: &store.WorkspaceSettings{
			WMS:         store.WMSSettings{Enabled: false},
			WFS:         store.WFSSettings{Enabled: false},
			OGCAPI:      store.OGCAPISettings{Enabled: true},
			OGCTilesAPI: store.DefaultOGCTilesAPISettings(),
			WCS:         store.WCSSettings{Enabled: false},
			WMTS:        store.DefaultWMTSSettings(),
		},
	}

	// Add to registry
	r.mu.Lock()
	r.workspaces[ws.Name] = ws
	r.workspacesByID[ws.ID] = ws
	result := snapshotWorkspace(ws)
	r.mu.Unlock()

	return result, nil
}

// UpdateWorkspace updates a workspace.
func (r *Registry) UpdateWorkspace(ctx context.Context, id string, input store.UpdateWorkspaceInput) (*Workspace, error) {
	// Update in store
	wsData, err := r.store.UpdateWorkspace(ctx, id, input)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	ws, ok := r.workspacesByID[id]
	if !ok {
		return nil, ErrWorkspaceNotFound
	}

	// Update name mapping if name changed
	if ws.Name != wsData.Name {
		delete(r.workspaces, ws.Name)
		ws.Name = wsData.Name
		r.workspaces[ws.Name] = ws
	}
	ws.Description = wsData.Description
	r.invalidateCapabilitiesCache(id)

	return snapshotWorkspace(ws), nil
}

// DeleteWorkspace deletes a workspace.
func (r *Registry) DeleteWorkspace(ctx context.Context, id string) error {
	// Delete from store
	if err := r.store.DeleteWorkspace(ctx, id); err != nil {
		return err
	}

	// Invalidate cache before removing from registry
	r.invalidateWorkspaceCache(id)

	r.mu.Lock()
	defer r.mu.Unlock()

	ws, ok := r.workspacesByID[id]
	if !ok {
		return nil
	}

	// Close all data sources
	for _, svc := range ws.Services {
		if svc.DataSource != nil {
			r.retireSource(svc.DataSource)
		}
	}

	delete(r.workspaces, ws.Name)
	delete(r.workspacesByID, id)

	return nil
}

// CreateService creates a new service in a workspace.
func (r *Registry) CreateService(ctx context.Context, input store.CreateServiceInput) (*Service, error) {
	// Create in store
	svcData, err := r.store.CreateService(ctx, input)
	if err != nil {
		return nil, err
	}

	svc := &Service{
		ID:             svcData.ID,
		Name:           svcData.Name,
		Type:           svcData.Type,
		ConnectionInfo: svcData.ConnectionInfo,
		CacheSettings:  svcData.CacheSettings,
		Enabled:        svcData.Enabled,
		Layers:         make(map[string]*Layer),
		Coverages:      make(map[string]*Coverage),
	}

	// Initialize data source if enabled
	if svc.Enabled && r.dataSourceFactory != nil {
		ds, err := datasource.Prepare(ctx, func() (datasource.DataSource, error) { return r.dataSourceFactory(svcData) })
		if err != nil {
			return nil, errors.Join(&DataSourceError{Err: err}, r.store.DeleteService(context.WithoutCancel(ctx), svcData.ID))
		} else {
			svc.DataSource = ds
			if coverageSource, ok := ds.(datasource.CoverageDataSource); ok {
				svc.CoverageSource = coverageSource
			}
		}
	}

	// Add to workspace
	r.mu.Lock()
	ws, ok := r.workspacesByID[input.WorkspaceID]
	if ok {
		ws.AddService(svc)
	}
	result := snapshotService(svc)
	r.mu.Unlock()
	if !ok {
		r.retireSource(svc.DataSource)
		return nil, ErrWorkspaceNotFound
	}

	// Invalidate capabilities cache (service list changed)
	r.invalidateCapabilitiesCache(input.WorkspaceID)

	return result, nil
}

// UpdateService updates a service.
func (r *Registry) UpdateService(ctx context.Context, workspaceID, serviceIdentifier string, input store.UpdateServiceInput) (*Service, error) {
	ws, ok := r.GetByID(workspaceID)
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	service := ws.ResolveService(serviceIdentifier)
	if service == nil {
		return nil, ErrServiceNotFound
	}
	lock := r.serviceUpdateLock(service.ID)
	lock.Lock()
	defer lock.Unlock()
	current, err := r.store.GetService(ctx, service.ID)
	if err != nil {
		return nil, err
	}
	if current.WorkspaceID != workspaceID {
		return nil, ErrServiceNotFound
	}
	candidate := cloneMetadata(current)
	if input.Name != nil {
		candidate.Name = *input.Name
	}
	if input.ConnectionInfo != nil {
		candidate.ConnectionInfo = cloneMetadata(*input.ConnectionInfo)
	}
	if input.Enabled != nil {
		candidate.Enabled = *input.Enabled
	}
	if input.CacheSettings != nil {
		candidate.CacheSettings = cloneMetadata(input.CacheSettings)
	}
	// Re-read the active state after acquiring the service's update lock.
	ws, ok = r.GetByID(workspaceID)
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	service = ws.GetService(current.ID)
	if service == nil {
		return nil, ErrServiceNotFound
	}
	replace := candidate.Enabled && (service.DataSource == nil || !service.Enabled || !reflect.DeepEqual(current.ConnectionInfo, candidate.ConnectionInfo))
	var prepared datasource.DataSource
	if replace && r.dataSourceFactory != nil {
		prepared, err = datasource.Prepare(ctx, func() (datasource.DataSource, error) { return r.dataSourceFactory(candidate) })
		if err != nil {
			return nil, &DataSourceError{Err: err}
		}
	}
	installed := false
	defer func() {
		if prepared != nil && !installed {
			_ = prepared.Close()
		}
	}()
	r.mu.Lock()
	defer r.mu.Unlock()
	active := r.workspacesByID[workspaceID]
	if active == nil {
		return nil, ErrWorkspaceNotFound
	}
	svc := active.GetService(current.ID)
	if svc == nil {
		return nil, ErrServiceNotFound
	}
	saved, err := r.store.UpdateService(ctx, current.ID, input)
	if err != nil {
		return nil, err
	}
	svc.Name, svc.ConnectionInfo, svc.Enabled = saved.Name, cloneMetadata(saved.ConnectionInfo), saved.Enabled
	svc.CacheSettings = cloneMetadata(saved.CacheSettings)
	if replace || !saved.Enabled {
		old := svc.DataSource
		svc.DataSource = prepared
		svc.CoverageSource, _ = prepared.(datasource.CoverageDataSource)
		installed = true
		r.retireSource(old)
	}
	active.TileRevision++
	r.invalidateWorkspaceCache(workspaceID)
	return snapshotService(svc), nil
}

// RefreshService rebuilds an enabled service's runtime data source without
// mutating its catalog record. Operational indexes use this after atomically
// activating a new generation.
func (r *Registry) RefreshService(ctx context.Context, workspaceID, serviceIdentifier string) error {
	r.mu.RLock()
	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		r.mu.RUnlock()
		return ErrWorkspaceNotFound
	}
	service := ws.ResolveService(serviceIdentifier)
	serviceID := serviceIdentifier
	enabled := true
	if service != nil {
		serviceID, enabled = service.ID, service.Enabled
	}
	if r.quiescedServices[serviceID] {
		r.mu.RUnlock()
		return ErrServiceNotFound
	}
	r.mu.RUnlock()
	lock := r.serviceUpdateLock(serviceID)
	lock.Lock()
	defer lock.Unlock()
	stored, err := r.store.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	if stored.WorkspaceID != workspaceID {
		return ErrServiceNotFound
	}
	enabled = stored.Enabled
	var newSource datasource.DataSource
	if enabled && r.dataSourceFactory != nil {
		newSource, err = datasource.Prepare(ctx, func() (datasource.DataSource, error) { return r.dataSourceFactory(stored) })
		if err != nil {
			return err
		}
	}
	if service == nil {
		layers, listErr := r.store.ListLayers(ctx, serviceID)
		if listErr != nil {
			if newSource != nil {
				_ = newSource.Close()
			}
			return listErr
		}
		service = &Service{ID: stored.ID, Name: stored.Name, Type: stored.Type, ConnectionInfo: stored.ConnectionInfo,
			CacheSettings: stored.CacheSettings, Enabled: stored.Enabled, Layers: make(map[string]*Layer), Coverages: make(map[string]*Coverage), DataSource: newSource}
		for _, item := range layers {
			runtime := runtimeFeatureLayer(item)
			service.Layers[runtime.PublicID] = runtime
		}
		if coverageStore, ok := r.store.(store.CoverageStore); ok {
			coverages, coverageErr := coverageStore.ListCoverages(ctx, serviceID)
			if coverageErr != nil {
				if newSource != nil {
					_ = newSource.Close()
				}
				return coverageErr
			}
			for _, item := range coverages {
				service.Coverages[item.PublicID] = runtimeCoverage(item)
			}
		}
		if newSource != nil {
			service.CoverageSource, _ = newSource.(datasource.CoverageDataSource)
			quarantineInvalidSQLViews(ctx, service)
		}
	}
	r.mu.Lock()
	ws, ok = r.workspacesByID[workspaceID]
	if !ok || r.quiescedServices[serviceID] {
		r.mu.Unlock()
		if newSource != nil {
			_ = newSource.Close()
		}
		if !ok {
			return ErrWorkspaceNotFound
		}
		return ErrServiceNotFound
	}
	current := ws.GetService(serviceID)
	if current == nil {
		ws.Services[serviceID] = service
		ws.TileRevision++
		r.mu.Unlock()
		r.invalidateWorkspaceCache(workspaceID)
		return nil
	}
	oldSource := current.DataSource
	current.DataSource = newSource
	current.CoverageSource, _ = newSource.(datasource.CoverageDataSource)
	ws.TileRevision++
	r.mu.Unlock()
	if oldSource != nil {
		r.retireSource(oldSource)
	}
	r.invalidateWorkspaceCache(workspaceID)
	return nil
}

// DeleteService deletes a service.
func (r *Registry) DeleteService(ctx context.Context, workspaceID, serviceIdentifier string) error {
	snapshot, ok := r.GetByID(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	svc := snapshot.ResolveService(serviceIdentifier)
	if svc == nil {
		return ErrServiceNotFound
	}
	actualServiceID := svc.ID
	lock := r.serviceUpdateLock(actualServiceID)
	lock.Lock()
	defer lock.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	ws := r.workspacesByID[workspaceID]
	if ws == nil {
		return ErrWorkspaceNotFound
	}
	svc = ws.GetService(actualServiceID)
	if svc == nil {
		return ErrServiceNotFound
	}
	for _, layer := range svc.Layers {
		if len(ws.GroupReferences(layer.PublicID)) > 0 {
			return fmt.Errorf("%w: %s", ErrResourceReferenced, layer.PublicID)
		}
	}
	for _, coverage := range svc.Coverages {
		if len(ws.GroupReferences(coverage.PublicID)) > 0 {
			return fmt.Errorf("%w: %s", ErrResourceReferenced, coverage.PublicID)
		}
	}

	// Delete from store using actual service ID
	if err := r.store.DeleteService(ctx, actualServiceID); err != nil {
		return err
	}

	// Invalidate workspace cache (service and all its layers removed)
	r.invalidateWorkspaceCache(workspaceID)

	r.removeServiceLocked(ws, actualServiceID)
	return nil
}

// CreateLayer creates a new layer in a service.
func (r *Registry) CreateLayer(ctx context.Context, workspaceID, serviceIdentifier string, input store.CreateLayerInput) (*Layer, error) {
	r.mu.RLock()
	if ws := r.workspacesByID[workspaceID]; ws != nil {
		if ws.HasPublishedResourceID(input.PublicID) {
			r.mu.RUnlock()
			return nil, store.ErrDuplicateKey
		}
	}
	r.mu.RUnlock()
	// Create in store
	layerData, err := r.store.CreateLayer(ctx, input)
	if err != nil {
		return nil, err
	}

	layer := &Layer{
		ID:                  layerData.ID,
		SourceLayer:         layerData.SourceLayer,
		PublicID:            layerData.PublicID,
		Title:               layerData.Title,
		Description:         layerData.Description,
		Enabled:             layerData.Enabled,
		CRSDefault:          layerData.CRSDefault,
		IsSQLView:           layerData.IsSQLView,
		Public:              layerData.Public,
		AllowedRoles:        layerData.AllowedRoles,
		DefaultStyle:        layerData.DefaultStyle,
		Styles:              append([]string(nil), layerData.Styles...),
		NativeExtent:        cloneExtent(layerData.NativeExtent),
		TileCacheQuotaBytes: layerData.TileCacheQuotaBytes,
		TileCacheParameters: cloneTileCacheParameters(layerData.TileCacheParameters),
		TileCacheGeneration: layerData.TileCacheGeneration,
	}
	// Copy dimensions from store layer to runtime layer
	if len(layerData.Dimensions) > 0 {
		layer.Dimensions = make([]*Dimension, len(layerData.Dimensions))
		for i, dim := range layerData.Dimensions {
			layer.Dimensions[i] = &Dimension{
				Name:           dim.Name,
				Units:          dim.Units,
				SourceAxis:     dim.SourceAxis,
				SourceProperty: dim.SourceProperty,
				EndProperty:    dim.EndProperty,
				Default:        dim.Default,
				MultipleValues: dim.MultipleValues,
				NearestValue:   dim.NearestValue,
				Current:        dim.Current,
				Extent:         dim.Extent,
			}
		}
	}
	// Copy SQL view config from store layer to runtime layer
	if layerData.SQLViewConfig != nil {
		layer.SQLViewConfig = &SQLViewConfig{
			SQL:            layerData.SQLViewConfig.SQL,
			GeometryColumn: layerData.SQLViewConfig.GeometryColumn,
			GeometryType:   layerData.SQLViewConfig.GeometryType,
			SRID:           layerData.SQLViewConfig.SRID,
			IDColumn:       layerData.SQLViewConfig.IDColumn,
			ReadOnly:       layerData.SQLViewConfig.ReadOnly,
		}
		if len(layerData.SQLViewConfig.Properties) > 0 {
			layer.SQLViewConfig.Properties = make([]*SQLViewProperty, len(layerData.SQLViewConfig.Properties))
			for i, prop := range layerData.SQLViewConfig.Properties {
				layer.SQLViewConfig.Properties[i] = &SQLViewProperty{
					Name: prop.Name,
					Type: prop.Type,
				}
			}
		}
	}

	// Add to service
	r.mu.Lock()
	ws, ok := r.workspacesByID[workspaceID]
	if ok {
		// Resolve service identifier (could be ID or name)
		svc := ws.ResolveService(serviceIdentifier)
		if svc != nil {
			svc.AddLayer(layer)
		}
	}
	result := cloneMetadata(layer)
	r.mu.Unlock()

	// Invalidate cache (layer list changed)
	r.invalidateLayerCache(workspaceID, layer.PublicID)

	return result, nil
}

// UpdateLayer updates a layer.
func (r *Registry) UpdateLayer(ctx context.Context, workspaceID, serviceIdentifier, layerID string, input store.UpdateLayerInput) (*Layer, error) {
	// Resolve and verify ownership before mutating the backing store.
	r.mu.RLock()
	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		r.mu.RUnlock()
		return nil, ErrWorkspaceNotFound
	}
	svc := ws.ResolveService(serviceIdentifier)
	if svc == nil {
		r.mu.RUnlock()
		return nil, ErrServiceNotFound
	}
	actualServiceID := svc.ID
	r.mu.RUnlock()

	oldLayer, err := r.store.GetLayer(ctx, layerID)
	if err != nil {
		return nil, err
	}
	if oldLayer.ServiceID != actualServiceID {
		return nil, ErrLayerNotFound
	}
	if input.PublicID != nil && *input.PublicID != oldLayer.PublicID {
		r.mu.RLock()
		if current := r.workspacesByID[workspaceID]; current != nil {
			if current.HasPublishedResourceID(*input.PublicID) {
				r.mu.RUnlock()
				return nil, store.ErrDuplicateKey
			}
			if len(current.GroupReferences(oldLayer.PublicID)) > 0 {
				r.mu.RUnlock()
				return nil, fmt.Errorf("%w: %s", ErrResourceReferenced, oldLayer.PublicID)
			}
		}
		r.mu.RUnlock()
	}

	// Update in store
	layerData, err := r.store.UpdateLayer(ctx, layerID, input)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	ws, ok = r.workspacesByID[workspaceID]
	if !ok {
		return nil, ErrWorkspaceNotFound
	}

	// Resolve service identifier (could be ID or name)
	svc = ws.ResolveService(actualServiceID)
	if svc == nil {
		return nil, ErrServiceNotFound
	}

	// Update layer in service
	layer := svc.Layers[oldLayer.PublicID]
	if layer == nil {
		return nil, ErrLayerNotFound
	}

	// Update mapping if public ID changed
	if oldLayer.PublicID != layerData.PublicID {
		delete(svc.Layers, oldLayer.PublicID)
		svc.Layers[layerData.PublicID] = layer
	}

	layer.PublicID = layerData.PublicID
	layer.Title = layerData.Title
	layer.Description = layerData.Description
	layer.Enabled = layerData.Enabled
	layer.CRSDefault = layerData.CRSDefault
	layer.Public = layerData.Public
	layer.AllowedRoles = append([]string(nil), layerData.AllowedRoles...)
	layer.DefaultStyle = layerData.DefaultStyle
	layer.Styles = append([]string(nil), layerData.Styles...)
	layer.NativeExtent = cloneExtent(layerData.NativeExtent)
	layer.TileCacheQuotaBytes = layerData.TileCacheQuotaBytes
	layer.TileCacheParameters = cloneTileCacheParameters(layerData.TileCacheParameters)
	layer.TileCacheGeneration = layerData.TileCacheGeneration
	// Copy dimensions from store layer to runtime layer
	if len(layerData.Dimensions) > 0 {
		layer.Dimensions = make([]*Dimension, len(layerData.Dimensions))
		for i, dim := range layerData.Dimensions {
			layer.Dimensions[i] = &Dimension{
				Name:           dim.Name,
				Units:          dim.Units,
				SourceAxis:     dim.SourceAxis,
				SourceProperty: dim.SourceProperty,
				EndProperty:    dim.EndProperty,
				Default:        dim.Default,
				MultipleValues: dim.MultipleValues,
				NearestValue:   dim.NearestValue,
				Current:        dim.Current,
				Extent:         dim.Extent,
			}
		}
	} else {
		layer.Dimensions = nil
	}
	// Copy SQL view config from store layer to runtime layer
	if layerData.SQLViewConfig != nil {
		layer.SQLViewConfig = &SQLViewConfig{
			SQL:            layerData.SQLViewConfig.SQL,
			GeometryColumn: layerData.SQLViewConfig.GeometryColumn,
			GeometryType:   layerData.SQLViewConfig.GeometryType,
			SRID:           layerData.SQLViewConfig.SRID,
			IDColumn:       layerData.SQLViewConfig.IDColumn,
			ReadOnly:       layerData.SQLViewConfig.ReadOnly,
		}
		if len(layerData.SQLViewConfig.Properties) > 0 {
			layer.SQLViewConfig.Properties = make([]*SQLViewProperty, len(layerData.SQLViewConfig.Properties))
			for i, prop := range layerData.SQLViewConfig.Properties {
				layer.SQLViewConfig.Properties[i] = &SQLViewProperty{
					Name: prop.Name,
					Type: prop.Type,
				}
			}
		}
	}

	// A layer-group map cache is keyed by the group identifier, so a child
	// update must invalidate the workspace tile namespace as well.
	if len(ws.GroupReferences(layer.PublicID)) > 0 {
		r.invalidateWorkspaceCache(workspaceID)
	} else {
		r.invalidateLayerCache(workspaceID, layer.PublicID)
	}

	return cloneMetadata(layer), nil
}

// DeleteLayer deletes a layer.
func (r *Registry) DeleteLayer(ctx context.Context, workspaceID, serviceIdentifier, layerID string) error {
	// Resolve and verify ownership before mutating the backing store.
	r.mu.RLock()
	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		r.mu.RUnlock()
		return ErrWorkspaceNotFound
	}
	svc := ws.ResolveService(serviceIdentifier)
	if svc == nil {
		r.mu.RUnlock()
		return ErrServiceNotFound
	}
	actualServiceID := svc.ID
	r.mu.RUnlock()

	layer, err := r.store.GetLayer(ctx, layerID)
	if err != nil {
		return err
	}
	if layer.ServiceID != actualServiceID {
		return ErrLayerNotFound
	}
	r.mu.RLock()
	if current := r.workspacesByID[workspaceID]; current != nil && len(current.GroupReferences(layer.PublicID)) > 0 {
		r.mu.RUnlock()
		return fmt.Errorf("%w: %s", ErrResourceReferenced, layer.PublicID)
	}
	r.mu.RUnlock()

	// Delete from store
	if err := r.store.DeleteLayer(ctx, layerID); err != nil {
		return err
	}

	// Invalidate cache (layer removed)
	r.invalidateLayerCache(workspaceID, layer.PublicID)

	r.mu.Lock()
	defer r.mu.Unlock()

	ws, ok = r.workspacesByID[workspaceID]
	if !ok {
		return nil
	}

	// Resolve service identifier (could be ID or name)
	svc = ws.ResolveService(actualServiceID)
	if svc != nil {
		svc.RemoveLayer(layer.PublicID)
	}

	return nil
}

// CreateStyle creates a new style in a workspace.
func (r *Registry) CreateStyle(ctx context.Context, workspaceID string, input store.CreateStyleInput) (*Style, error) {
	// Create in store
	styleData, err := r.store.CreateStyle(ctx, input)
	if err != nil {
		return nil, err
	}

	document, diagnostics, valid, validationErrors := compileRuntimeStyle(styleData.Format, styleData.SLDBody)
	style := &Style{
		ID:               styleData.ID,
		Name:             styleData.Name,
		Title:            styleData.Title,
		Description:      styleData.Description,
		SLDBody:          styleData.SLDBody,
		Format:           styleData.Format,
		Document:         document,
		Diagnostics:      diagnostics,
		Valid:            valid,
		ValidationErrors: validationErrors,
	}

	// Add to workspace
	r.mu.Lock()
	ws, ok := r.workspacesByID[workspaceID]
	if ok {
		ws.AddStyle(style)
	}
	result := cloneMetadata(style)
	r.mu.Unlock()
	if r.cache != nil {
		r.cache.InvalidateTiles(workspaceID)
	}

	return result, nil
}

// UpdateStyle updates a style.
func (r *Registry) UpdateStyle(ctx context.Context, workspaceID, styleID string, input store.UpdateStyleInput) (*Style, error) {
	// Get current style to know the old name
	oldStyle, err := r.store.GetStyle(ctx, styleID)
	if err != nil {
		return nil, err
	}

	// Update in store
	styleData, err := r.store.UpdateStyle(ctx, styleID, input)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		r.mu.Unlock()
		return nil, ErrWorkspaceNotFound
	}

	// Update style in workspace
	style := ws.Styles[oldStyle.Name]
	if style == nil {
		// Style might not be loaded yet, create it
		style = &Style{}
		ws.Styles[styleData.Name] = style
	} else if oldStyle.Name != styleData.Name {
		// Update mapping if name changed
		delete(ws.Styles, oldStyle.Name)
		ws.Styles[styleData.Name] = style
	}

	style.ID = styleData.ID
	style.Name = styleData.Name
	style.Title = styleData.Title
	style.Description = styleData.Description
	style.SLDBody = styleData.SLDBody
	style.Format = styleData.Format
	style.Document, style.Diagnostics, style.Valid, style.ValidationErrors = compileRuntimeStyle(styleData.Format, styleData.SLDBody)
	result := cloneMetadata(style)
	r.mu.Unlock()

	// Invalidate tile cache (style affects rendering)
	if r.cache != nil {
		r.cache.InvalidateTiles(workspaceID)
	}

	return result, nil
}

func compileRuntimeStyle(format, body string) (*sld.StyledLayerDescriptor, []sld.Diagnostic, bool, []string) {
	doc, diagnostics, err := sld.Compile(format, body)
	if err != nil {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Message)
		}
		if len(messages) == 0 {
			messages = []string{err.Error()}
		}
		return nil, diagnostics, false, messages
	}
	return doc, diagnostics, true, nil
}

// DeleteStyle deletes a style.
func (r *Registry) DeleteStyle(ctx context.Context, workspaceID, styleID string) error {
	// Get style to know the name
	style, err := r.store.GetStyle(ctx, styleID)
	if err != nil {
		return err
	}

	// Delete from store
	if err := r.store.DeleteStyle(ctx, styleID); err != nil {
		return err
	}

	// Invalidate tile cache (style affects rendering)
	if r.cache != nil {
		r.cache.InvalidateTiles(workspaceID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		return nil
	}

	ws.RemoveStyle(style.Name)
	return nil
}

// UpdateWMSSettings updates WMS settings for a workspace.
func (r *Registry) UpdateWMSSettings(ctx context.Context, workspaceID string, settings store.WMSSettings) error {
	if err := r.store.UpdateWMSSettings(ctx, workspaceID, settings); err != nil {
		return err
	}

	r.mu.Lock()
	ws, ok := r.workspacesByID[workspaceID]
	if ok && ws.Settings != nil {
		ws.Settings.WMS = cloneMetadata(settings)
	}
	r.mu.Unlock()

	r.invalidateWorkspaceCache(workspaceID)

	return nil
}

// UpdateWFSSettings updates WFS settings for a workspace.
func (r *Registry) UpdateWFSSettings(ctx context.Context, workspaceID string, settings store.WFSSettings) error {
	if err := r.store.UpdateWFSSettings(ctx, workspaceID, settings); err != nil {
		return err
	}

	r.mu.Lock()
	ws, ok := r.workspacesByID[workspaceID]
	if ok && ws.Settings != nil {
		ws.Settings.WFS = cloneMetadata(settings)
	}
	r.mu.Unlock()

	r.invalidateWorkspaceCache(workspaceID)

	return nil
}

// UpdateOGCAPISettings updates OGC API settings for a workspace.
func (r *Registry) UpdateOGCAPISettings(ctx context.Context, workspaceID string, settings store.OGCAPISettings) error {
	if err := r.store.UpdateOGCAPISettings(ctx, workspaceID, settings); err != nil {
		return err
	}

	r.mu.Lock()
	ws, ok := r.workspacesByID[workspaceID]
	if ok && ws.Settings != nil {
		ws.Settings.OGCAPI = cloneMetadata(settings)
	}
	r.mu.Unlock()

	r.invalidateWorkspaceCache(workspaceID)

	return nil
}

// UpdateOGCTilesAPISettings updates OGC Tiles API settings for a workspace.
func (r *Registry) UpdateOGCTilesAPISettings(ctx context.Context, workspaceID string, settings store.OGCTilesAPISettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.UpdateOGCTilesAPISettings(ctx, workspaceID, settings); err != nil {
		return err
	}

	ws, ok := r.workspacesByID[workspaceID]
	if ok && ws.Settings != nil {
		ws.Settings.OGCTilesAPI = cloneMetadata(settings)
		ws.TileRevision++
	}

	r.invalidateWorkspaceCache(workspaceID)

	return nil
}

// UpdateWCSSettings updates WCS settings for a workspace.
func (r *Registry) UpdateWCSSettings(ctx context.Context, workspaceID string, settings store.WCSSettings) error {
	coverageStore, ok := r.store.(store.CoverageStore)
	if !ok {
		return errors.New("coverage persistence is unavailable")
	}
	if err := coverageStore.UpdateWCSSettings(ctx, workspaceID, settings); err != nil {
		return err
	}
	r.mu.Lock()
	if ws := r.workspacesByID[workspaceID]; ws != nil && ws.Settings != nil {
		ws.Settings.WCS = cloneMetadata(settings)
	}
	r.mu.Unlock()
	r.invalidateWorkspaceCache(workspaceID)
	return nil
}

// UpdateWMTSSettings updates both durable and runtime WMTS configuration.
func (r *Registry) UpdateWMTSSettings(ctx context.Context, workspaceID string, settings store.WMTSSettings) error {
	wmtsStore, ok := r.store.(store.WMTSStore)
	if !ok {
		return errors.New("WMTS persistence is unavailable")
	}
	if err := wmtsStore.UpdateWMTSSettings(ctx, workspaceID, settings); err != nil {
		return err
	}
	r.mu.Lock()
	if ws := r.workspacesByID[workspaceID]; ws != nil && ws.Settings != nil {
		ws.Settings.WMTS = cloneMetadata(settings)
	}
	r.mu.Unlock()
	r.invalidateWorkspaceCache(workspaceID)
	return nil
}

// DiscoverCoverages discovers publishable raster coverages from a service.
func (r *Registry) DiscoverCoverages(ctx context.Context, workspaceID, serviceIdentifier string) ([]*datasource.DiscoveredCoverage, error) {
	ws, release, ok := r.AcquireByID(workspaceID)
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	defer release()
	service := ws.ResolveService(serviceIdentifier)
	if service == nil {
		return nil, ErrServiceNotFound
	}
	if service.CoverageSource == nil {
		return nil, errors.New("service does not support raster coverages")
	}
	return service.CoverageSource.DiscoverCoverages(ctx)
}

// CreateCoverage persists and publishes a coverage in the runtime registry.
func (r *Registry) CreateCoverage(ctx context.Context, workspaceID string, input store.CreateCoverageInput) (*Coverage, error) {
	coverageStore, ok := r.store.(store.CoverageStore)
	if !ok {
		return nil, errors.New("coverage persistence is unavailable")
	}
	r.mu.RLock()
	if ws := r.workspacesByID[workspaceID]; ws != nil {
		if ws.HasPublishedResourceID(input.PublicID) {
			r.mu.RUnlock()
			return nil, store.ErrDuplicateKey
		}
	}
	r.mu.RUnlock()
	item, err := coverageStore.CreateCoverage(ctx, input)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ws := r.workspacesByID[workspaceID]
	if ws == nil {
		return nil, ErrWorkspaceNotFound
	}
	svc := ws.GetService(input.ServiceID)
	if svc == nil {
		return nil, ErrServiceNotFound
	}
	coverage := runtimeCoverage(item)
	svc.Coverages[item.PublicID] = coverage
	r.invalidateCapabilitiesCache(workspaceID)
	return cloneMetadata(coverage), nil
}

// UpdateCoverage updates mutable coverage publication metadata.
func (r *Registry) UpdateCoverage(ctx context.Context, workspaceID, serviceID, coverageID string, input store.UpdateCoverageInput) (*Coverage, error) {
	coverageStore, ok := r.store.(store.CoverageStore)
	if !ok {
		return nil, errors.New("coverage persistence is unavailable")
	}
	old, err := coverageStore.GetCoverage(ctx, coverageID)
	if err != nil {
		return nil, err
	}
	if input.PublicID != nil && *input.PublicID != old.PublicID {
		r.mu.RLock()
		if ws := r.workspacesByID[workspaceID]; ws != nil {
			if ws.HasPublishedResourceID(*input.PublicID) {
				r.mu.RUnlock()
				return nil, store.ErrDuplicateKey
			}
			if len(ws.GroupReferences(old.PublicID)) > 0 {
				r.mu.RUnlock()
				return nil, fmt.Errorf("%w: %s", ErrResourceReferenced, old.PublicID)
			}
		}
		r.mu.RUnlock()
	}
	item, err := coverageStore.UpdateCoverage(ctx, coverageID, input)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ws := r.workspacesByID[workspaceID]
	if ws == nil {
		return nil, ErrWorkspaceNotFound
	}
	svc := ws.GetService(serviceID)
	if svc == nil {
		return nil, ErrServiceNotFound
	}
	coverage := runtimeCoverage(item)
	if old.PublicID != item.PublicID {
		delete(svc.Coverages, old.PublicID)
	}
	svc.Coverages[item.PublicID] = coverage
	r.invalidateWorkspaceCache(workspaceID)
	return cloneMetadata(coverage), nil
}

// DeleteCoverage removes a published coverage.
func (r *Registry) DeleteCoverage(ctx context.Context, workspaceID, serviceID, coverageID string) error {
	coverageStore, ok := r.store.(store.CoverageStore)
	if !ok {
		return errors.New("coverage persistence is unavailable")
	}
	item, err := coverageStore.GetCoverage(ctx, coverageID)
	if err != nil {
		return err
	}
	r.mu.RLock()
	if ws := r.workspacesByID[workspaceID]; ws != nil && len(ws.GroupReferences(item.PublicID)) > 0 {
		r.mu.RUnlock()
		return fmt.Errorf("%w: %s", ErrResourceReferenced, item.PublicID)
	}
	r.mu.RUnlock()
	if err := coverageStore.DeleteCoverage(ctx, coverageID); err != nil {
		return err
	}
	r.mu.Lock()
	if ws := r.workspacesByID[workspaceID]; ws != nil {
		if svc := ws.GetService(serviceID); svc != nil {
			for publicID, item := range svc.Coverages {
				if item.ID == coverageID {
					delete(svc.Coverages, publicID)
				}
			}
		}
	}
	r.mu.Unlock()
	r.invalidateCapabilitiesCache(workspaceID)
	return nil
}

// DiscoverLayers discovers layers from a service's data source.
func (r *Registry) DiscoverLayers(ctx context.Context, workspaceID, serviceIdentifier string) ([]*datasource.DiscoveredLayer, error) {
	ws, release, ok := r.AcquireByID(workspaceID)
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	defer release()
	service := ws.ResolveService(serviceIdentifier)
	if service == nil {
		return nil, ErrServiceNotFound
	}
	if service.DataSource == nil {
		return nil, errors.New("service data source not initialized")
	}
	return service.DataSource.DiscoverLayers(ctx)
}

// Remove removes a workspace from the registry without touching the store.
// Use this when the workspace has already been deleted from the store.
func (r *Registry) Remove(id string) {
	r.invalidateWorkspaceCache(id)
	r.mu.Lock()
	defer r.mu.Unlock()

	ws, ok := r.workspacesByID[id]
	if !ok {
		return
	}

	// Close all data sources
	for _, svc := range ws.Services {
		if svc.DataSource != nil {
			r.retireSource(svc.DataSource)
		}
	}

	delete(r.workspaces, ws.Name)
	delete(r.workspacesByID, id)
}

// QuiesceService removes a service from the live registry without mutating the
// catalog. Durable deletion uses this immediately after writing its tombstone
// so OGC requests cannot observe partially removed child state.
func (r *Registry) QuiesceService(workspaceID, serviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ws := r.workspacesByID[workspaceID]
	if ws == nil {
		return ErrWorkspaceNotFound
	}
	if ws.GetService(serviceID) == nil {
		return ErrServiceNotFound
	}
	r.removeServiceLocked(ws, serviceID)
	r.invalidateWorkspaceCache(workspaceID)
	return nil
}

// InvalidateWorkspace exposes full workspace invalidation to lifecycle
// coordinators after auxiliary and catalog state has been committed.
func (r *Registry) InvalidateWorkspace(workspaceID string) {
	r.invalidateWorkspaceCache(workspaceID)
}

// Close closes all data sources in all workspaces.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ws := range r.workspaces {
		for _, svc := range ws.Services {
			if svc.DataSource != nil {
				r.retireSource(svc.DataSource)
			}
		}
	}

	r.workspaces = make(map[string]*Workspace)
	r.workspacesByID = make(map[string]*Workspace)

	return nil
}

// GetServiceConnectionInfo unmarshals service connection info.
func GetServiceConnectionInfo[T any](svc *Service) (*T, error) {
	var info T
	if err := json.Unmarshal(svc.ConnectionInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal connection info: %w", err)
	}
	return &info, nil
}

// derefWMSSettings safely dereferences WMS settings pointer.
func derefWMSSettings(s *store.WMSSettings) store.WMSSettings {
	if s == nil {
		return store.WMSSettings{}
	}
	return *s
}

// derefWFSSettings safely dereferences WFS settings pointer.
func derefWFSSettings(s *store.WFSSettings) store.WFSSettings {
	if s == nil {
		return store.WFSSettings{}
	}
	return *s
}

// derefOGCAPISettings safely dereferences OGC API settings pointer.
func derefOGCAPISettings(s *store.OGCAPISettings) store.OGCAPISettings {
	if s == nil {
		return store.OGCAPISettings{Enabled: true} // Default to enabled
	}
	return *s
}

// derefOGCTilesAPISettings safely dereferences OGC Tiles API settings pointer.
func derefOGCTilesAPISettings(s *store.OGCTilesAPISettings) store.OGCTilesAPISettings {
	if s == nil {
		return store.DefaultOGCTilesAPISettings()
	}
	return *s
}

func derefWCSSettings(s *store.WCSSettings) store.WCSSettings {
	if s == nil {
		return store.WCSSettings{}
	}
	return *s
}

func derefWMTSSettings(s *store.WMTSSettings) store.WMTSSettings {
	if s == nil {
		return store.DefaultWMTSSettings()
	}
	return *s
}

func cloneExtent(value *store.SpatialExtent) *store.SpatialExtent {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTileCacheParameters(value *store.TileCacheParameterPolicy) *store.TileCacheParameterPolicy {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Styles = append([]string(nil), value.Styles...)
	copy.Times = append([]string(nil), value.Times...)
	copy.Elevations = append([]string(nil), value.Elevations...)
	return &copy
}

func runtimeFeatureLayer(item *store.Layer) *Layer {
	result := &Layer{ID: item.ID, SourceLayer: item.SourceLayer, PublicID: item.PublicID, Title: item.Title, Description: item.Description,
		Enabled: item.Enabled, CRSDefault: item.CRSDefault, IsSQLView: item.IsSQLView, Public: item.Public,
		AllowedRoles: append([]string(nil), item.AllowedRoles...), DefaultStyle: item.DefaultStyle, Styles: append([]string(nil), item.Styles...),
		NativeExtent: cloneExtent(item.NativeExtent), TileCacheQuotaBytes: item.TileCacheQuotaBytes,
		TileCacheParameters: cloneTileCacheParameters(item.TileCacheParameters), TileCacheGeneration: item.TileCacheGeneration}
	if len(item.Dimensions) > 0 {
		result.Dimensions = make([]*Dimension, len(item.Dimensions))
		for i, dim := range item.Dimensions {
			result.Dimensions[i] = &Dimension{Name: dim.Name, Units: dim.Units, SourceAxis: dim.SourceAxis, SourceProperty: dim.SourceProperty, EndProperty: dim.EndProperty, Default: dim.Default, MultipleValues: dim.MultipleValues, NearestValue: dim.NearestValue, Current: dim.Current, Extent: dim.Extent}
		}
	}
	if item.SQLViewConfig != nil {
		result.SQLViewConfig = &SQLViewConfig{SQL: item.SQLViewConfig.SQL, GeometryColumn: item.SQLViewConfig.GeometryColumn, GeometryType: item.SQLViewConfig.GeometryType, SRID: item.SQLViewConfig.SRID, IDColumn: item.SQLViewConfig.IDColumn, ReadOnly: true}
		for _, prop := range item.SQLViewConfig.Properties {
			result.SQLViewConfig.Properties = append(result.SQLViewConfig.Properties, &SQLViewProperty{Name: prop.Name, Type: prop.Type})
		}
	}
	return result
}

func runtimeCoverage(item *store.Coverage) *Coverage {
	result := &Coverage{
		ID: item.ID, SourceCoverage: item.SourceCoverage, PublicID: item.PublicID,
		Title: item.Title, Description: item.Description, Enabled: item.Enabled,
		Public: item.Public, AllowedRoles: item.AllowedRoles, RangeFields: item.RangeFields,
		DefaultStyle: item.DefaultStyle, Styles: append([]string(nil), item.Styles...), Resampling: item.Resampling,
		WCS20CoverageSubtype: item.WCS20CoverageSubtype,
		NativeExtent:         cloneExtent(item.NativeExtent), TileCacheQuotaBytes: item.TileCacheQuotaBytes,
		TileCacheGeneration: item.TileCacheGeneration,
	}
	if len(item.Dimensions) > 0 {
		result.Dimensions = make([]*Dimension, len(item.Dimensions))
		for index, dim := range item.Dimensions {
			result.Dimensions[index] = &Dimension{Name: dim.Name, Units: dim.Units, SourceAxis: dim.SourceAxis, SourceProperty: dim.SourceProperty, EndProperty: dim.EndProperty, Default: dim.Default, MultipleValues: dim.MultipleValues, NearestValue: dim.NearestValue, Current: dim.Current, Extent: dim.Extent}
		}
	}
	return result
}

func runtimeLayerGroup(item *store.LayerGroup) *LayerGroup {
	if item == nil {
		return nil
	}
	return &LayerGroup{ID: item.ID, PublicID: item.PublicID, Title: item.Title, Description: item.Description,
		Enabled: item.Enabled, Public: item.Public, AllowedRoles: append([]string(nil), item.AllowedRoles...),
		Members: append([]store.LayerGroupMember(nil), item.Members...), DefaultStyle: item.DefaultStyle,
		Styles: append([]string(nil), item.Styles...), NativeExtent: cloneExtent(item.NativeExtent),
		TileCacheQuotaBytes: item.TileCacheQuotaBytes, TileCacheGeneration: item.TileCacheGeneration}
}
