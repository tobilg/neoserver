package workspace

import (
	"context"
	"sync/atomic"

	"github.com/tobilg/neoserver/internal/store"
)

func revisionCell(value int64) *atomic.Int64 {
	cell := &atomic.Int64{}
	cell.Store(max(1, value))
	return cell
}

// CapabilitiesRevision is frozen when the request's workspace snapshot is made.
func (ws *Workspace) CapabilitiesRevision() int64 {
	if ws.capabilitiesRevision == nil {
		return 1
	}
	return ws.capabilitiesRevision.Load()
}

func (r *Registry) loadCapabilitiesRevision(ctx context.Context, id string) *atomic.Int64 {
	cell, _ := r.capabilitiesRevisions.LoadOrStore(id, revisionCell(1))
	r.refreshCapabilitiesRevision(ctx, id)
	return cell.(*atomic.Int64)
}

// Invalidations can run with or without the registry lock. The separate cells
// avoid lock inversion, and monotonic updates tolerate concurrent refreshes.
func (r *Registry) refreshCapabilitiesRevision(ctx context.Context, id string) {
	persistence, ok := r.store.(store.CapabilitiesRevisionStore)
	if !ok {
		return
	}
	value, err := persistence.GetCapabilitiesRevision(ctx, id)
	if err != nil {
		return
	}
	entry, ok := r.capabilitiesRevisions.Load(id)
	if !ok {
		return
	}
	cell := entry.(*atomic.Int64)
	for previous := cell.Load(); previous < value; previous = cell.Load() {
		if cell.CompareAndSwap(previous, value) {
			break
		}
	}
}
