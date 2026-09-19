package workspace

import (
	"context"
)

// Context keys for workspace-related values.
type contextKey int

const (
	workspaceKey contextKey = iota
	serviceKey
	layerKey
)

// WithWorkspace adds a workspace to the context.
func WithWorkspace(ctx context.Context, ws *Workspace) context.Context {
	return context.WithValue(ctx, workspaceKey, ws)
}

// FromContext returns the workspace from the context.
func FromContext(ctx context.Context) (*Workspace, bool) {
	ws, ok := ctx.Value(workspaceKey).(*Workspace)
	return ws, ok
}

// MustFromContext returns the workspace from the context or panics.
func MustFromContext(ctx context.Context) *Workspace {
	ws, ok := FromContext(ctx)
	if !ok {
		panic("workspace not found in context")
	}
	return ws
}

// WithService adds a service to the context.
func WithService(ctx context.Context, svc *Service) context.Context {
	return context.WithValue(ctx, serviceKey, svc)
}

// ServiceFromContext returns the service from the context.
func ServiceFromContext(ctx context.Context) (*Service, bool) {
	svc, ok := ctx.Value(serviceKey).(*Service)
	return svc, ok
}

// WithLayer adds a layer to the context.
func WithLayer(ctx context.Context, layer *Layer) context.Context {
	return context.WithValue(ctx, layerKey, layer)
}

// LayerFromContext returns the layer from the context.
func LayerFromContext(ctx context.Context) (*Layer, bool) {
	layer, ok := ctx.Value(layerKey).(*Layer)
	return layer, ok
}
