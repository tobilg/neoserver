package workspace

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

type dataState struct {
	gate     chan struct{}
	revision atomic.Int64
	pending  atomic.Bool
}

func newDataState(revision int64, pending bool) *dataState {
	state := &dataState{gate: make(chan struct{}, 1)}
	state.revision.Store(revision)
	state.pending.Store(pending)
	return state
}

// DataCacheState is shared by snapshots: an old background job must observe
// source edits even though its configuration snapshot remains immutable.
func (w *Workspace) DataCacheState() (int64, bool) {
	if w.dataState == nil {
		return 1, false
	}
	for {
		before := w.dataState.revision.Load()
		pending := w.dataState.pending.Load()
		if before == w.dataState.revision.Load() {
			return before, pending
		}
	}
}

// BeginDataWrite serializes feature writes within one workspace, not globally.
// Persist the dirty marker before touching the source. Always call finish, even
// for rollbacks; its failure leaves the cache bypassed until retry/recovery.
func (r *Registry) BeginDataWrite(ctx context.Context, workspaceID string) (func() error, error) {
	r.mu.RLock()
	ws := r.workspacesByID[workspaceID]
	if ws == nil {
		r.mu.RUnlock()
		return nil, ErrWorkspaceNotFound
	}
	state := ws.dataState
	r.mu.RUnlock()
	select {
	case state.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	state.pending.Store(true)
	state.revision.Add(1)
	persistence, durable := r.store.(store.DataRevisionStore)
	if durable {
		revision, err := persistence.SetDataWritePending(ctx, workspaceID, true)
		if err != nil {
			<-state.gate
			return nil, err
		}
		state.revision.Store(revision)
	}
	r.invalidateWorkspaceCache(workspaceID)
	var once sync.Once
	var finishErr error
	return func() error {
		once.Do(func() {
			defer func() { <-state.gate }()
			defer r.invalidateWorkspaceCache(workspaceID)
			if durable {
				finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				revision, err := persistence.SetDataWritePending(finishCtx, workspaceID, false)
				if err != nil {
					finishErr = err
					return
				}
				state.revision.Store(revision)
			} else {
				state.revision.Add(1)
			}
			state.pending.Store(false)
		})
		return finishErr
	}, nil
}
