package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/tobilg/neoserver/internal/store"
)

func (r *Registry) UpdateSTACSettings(ctx context.Context, workspaceID string, settings store.STACSettings) error {
	catalog, ok := r.store.(store.STACSettingsStore)
	if !ok {
		return fmt.Errorf("catalog does not support STAC settings")
	}
	if err := catalog.UpdateSTACSettings(ctx, workspaceID, settings); err != nil {
		return err
	}
	r.mu.Lock()
	if ws := r.workspacesByID[workspaceID]; ws != nil {
		if ws.Settings == nil {
			ws.Settings = &store.WorkspaceSettings{}
		}
		ws.Settings.STAC = settings
	}
	r.mu.Unlock()
	r.invalidateWorkspaceCache(workspaceID)
	return nil
}

// PublicationDeleted reports whether the catalog confirms that a workspace, or
// a layer or coverage in it, no longer exists. It never infers deletion from
// the runtime registry alone, which may lag behind the catalog.
func (r *Registry) PublicationDeleted(ctx context.Context, workspaceID, kind, resourceID string) (bool, error) {
	var err error
	switch kind {
	case "":
		_, err = r.store.GetWorkspace(ctx, workspaceID)
	case "layer":
		var layer *store.Layer
		if layer, err = r.store.GetLayer(ctx, resourceID); err == nil && layer == nil {
			return true, nil
		}
	case "coverage":
		coverages, ok := r.store.(store.CoverageStore)
		if !ok {
			return false, fmt.Errorf("catalog does not support coverages")
		}
		_, err = coverages.GetCoverage(ctx, resourceID)
	default:
		return false, fmt.Errorf("unknown publication kind %q", kind)
	}
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	return false, err
}
