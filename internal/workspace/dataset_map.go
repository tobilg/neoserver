package workspace

import (
	"context"
	"github.com/tobilg/neoserver/internal/store"
)

// RefreshOGCTilesAPISettings publishes settings changed by catalog integrity repair.
func (r *Registry) RefreshOGCTilesAPISettings(ctx context.Context, workspaceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ws, ok := r.workspacesByID[workspaceID]
	if !ok || ws.Settings == nil {
		return nil
	}
	settings, err := r.store.GetOGCTilesAPISettings(ctx, workspaceID)
	if err != nil {
		return err
	}
	ws.Settings.OGCTilesAPI = cloneMetadata(*settings)
	if persistence, ok := r.store.(store.WMTSStore); ok {
		revision, err := persistence.GetTileRevision(ctx, workspaceID)
		if err != nil {
			return err
		}
		ws.TileRevision = revision
	}
	r.invalidateWorkspaceCache(workspaceID)
	return nil
}
