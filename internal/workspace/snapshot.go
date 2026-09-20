package workspace

import (
	"reflect"
	"sync"

	"github.com/tobilg/neoserver/internal/datasource"
)

// cloneMetadata copies mutable configuration trees. Runtime handles and compiled
// style documents are deliberately managed separately, never reflected/cloned.
func cloneMetadata[T any](input T) T {
	value := reflect.ValueOf(input)
	if !value.IsValid() {
		return input
	}
	return cloneValue(value).Interface().(T)
}

func cloneValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(cloneValue(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneValue(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneValue(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath == "" {
				out.Field(i).Set(cloneValue(v.Field(i)))
			}
		}
		return out
	default:
		return v
	}
}

func snapshotService(service *Service) *Service {
	if service == nil {
		return nil
	}
	out := *service
	out.ConnectionInfo = cloneMetadata(service.ConnectionInfo)
	out.CacheSettings = cloneMetadata(service.CacheSettings)
	out.Layers = cloneMetadata(service.Layers)
	out.Coverages = cloneMetadata(service.Coverages)
	return &out
}

func snapshotWorkspace(ws *Workspace) *Workspace {
	if ws == nil {
		return nil
	}
	out := &Workspace{ID: ws.ID, Name: ws.Name, Description: ws.Description, TileRevision: ws.TileRevision, capabilitiesRevision: revisionCell(ws.CapabilitiesRevision()), StyleAssetDigest: ws.StyleAssetDigest,
		StyleAssets: cloneMetadata(ws.StyleAssets),
		Settings:    cloneMetadata(ws.Settings), Groups: cloneMetadata(ws.Groups), dataState: ws.dataState,
		Services: make(map[string]*Service, len(ws.Services)), Styles: make(map[string]*Style, len(ws.Styles))}
	for id, service := range ws.Services {
		out.Services[id] = snapshotService(service)
	}
	for name, style := range ws.Styles {
		copy := *style
		copy.Diagnostics = cloneMetadata(style.Diagnostics)
		copy.ValidationErrors = cloneMetadata(style.ValidationErrors)
		out.Styles[name] = &copy // Document is immutable after compilation.
	}
	return out
}

type sourceReference struct {
	readers int
	retired bool
}

// acquireLocked snapshots under the registry lock, then leases every handle.
// No registry lock survives this call; slow requests/jobs cannot block writers.
func (r *Registry) acquireLocked(ws *Workspace) (*Workspace, func(), bool) {
	if ws == nil {
		return nil, func() {}, false
	}
	out := snapshotWorkspace(ws)
	sources := make([]datasource.DataSource, 0, len(ws.Services))
	r.sourcesMu.Lock()
	if r.sources == nil {
		r.sources = make(map[datasource.DataSource]*sourceReference)
	}
	for _, service := range ws.Services {
		if source := service.DataSource; source != nil {
			ref := r.sources[source]
			if ref == nil {
				ref = &sourceReference{}
				r.sources[source] = ref
			}
			ref.readers++
			sources = append(sources, source)
		}
	}
	r.sourcesMu.Unlock()
	var once sync.Once
	return out, func() {
		once.Do(func() {
			for _, source := range sources {
				r.sourcesMu.Lock()
				ref := r.sources[source]
				ref.readers--
				closeNow := ref.readers == 0 && ref.retired
				if closeNow {
					delete(r.sources, source)
				}
				r.sourcesMu.Unlock()
				if closeNow {
					_ = source.Close()
				}
			}
		})
	}, true
}

// retireSource is called after removing a handle from the live graph. Existing
// snapshots retain it until their last reader releases; new readers cannot get it.
func (r *Registry) retireSource(source datasource.DataSource) {
	if source == nil {
		return
	}
	r.sourcesMu.Lock()
	ref := r.sources[source]
	closeNow := ref == nil || ref.readers == 0
	if closeNow {
		delete(r.sources, source)
	} else {
		ref.retired = true
	}
	r.sourcesMu.Unlock()
	if closeNow {
		_ = source.Close()
	}
}

func (r *Registry) AcquireByID(id string) (*Workspace, func(), bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.acquireLocked(r.workspacesByID[id])
}

func (r *Registry) serviceUpdateLock(id string) *sync.Mutex {
	value, _ := r.serviceUpdates.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (r *Registry) removeServiceLocked(ws *Workspace, id string) {
	if r.quiescedServices == nil {
		r.quiescedServices = make(map[string]bool)
	}
	r.quiescedServices[id] = true
	if service := ws.Services[id]; service != nil {
		delete(ws.Services, id)
		r.retireSource(service.DataSource)
	}
}
