package wfs

import (
	"context"
	"sync"
)

// A per-workspace, cancellable gate serializes lock grants with transaction
// validation/commit. Idle entries are removed; an unrelated workspace never
// waits for a slow source. This does not lock the catalog/registry graph.
type writeGates struct {
	mu      sync.Mutex
	entries map[string]*writeGate
}
type writeGate struct {
	token chan struct{}
	refs  int
}

func (g *writeGates) acquire(ctx context.Context, workspace string) (func(), error) {
	g.mu.Lock()
	if g.entries == nil {
		g.entries = make(map[string]*writeGate)
	}
	e := g.entries[workspace]
	if e == nil {
		e = &writeGate{token: make(chan struct{}, 1)}
		e.token <- struct{}{}
		g.entries[workspace] = e
	}
	e.refs++
	g.mu.Unlock()
	drop := func() {
		g.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(g.entries, workspace)
		}
		g.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-e.token:
		var once sync.Once
		return func() { once.Do(func() { e.token <- struct{}{}; drop() }) }, nil
	}
}
