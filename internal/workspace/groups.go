package workspace

import (
	"context"
	"fmt"

	"github.com/tobilg/neoserver/internal/store"
)

func (r *Registry) CreateLayerGroup(ctx context.Context, workspaceID string, input store.CreateLayerGroupInput, maxDepth int) (*LayerGroup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	groupStore, ok := r.store.(store.LayerGroupStore)
	if !ok {
		return nil, fmt.Errorf("layer groups are not supported by the catalog")
	}
	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	input.WorkspaceID = workspaceID
	candidate := runtimeLayerGroup(&store.LayerGroup{PublicID: input.PublicID, Enabled: input.Enabled, Public: input.Public,
		AllowedRoles: input.AllowedRoles, Members: input.Members, DefaultStyle: input.DefaultStyle, Styles: input.Styles})
	if err := validateLayerGroup(ws, candidate, "", maxDepth); err != nil {
		return nil, err
	}
	created, err := groupStore.CreateLayerGroup(ctx, input)
	if err != nil {
		return nil, err
	}
	ws.Groups[created.PublicID] = runtimeLayerGroup(created)
	r.invalidateWorkspaceCache(workspaceID)
	return runtimeLayerGroup(created), nil
}

func (r *Registry) UpdateLayerGroup(ctx context.Context, workspaceID, id string, input store.UpdateLayerGroupInput, maxDepth int) (*LayerGroup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	groupStore, ok := r.store.(store.LayerGroupStore)
	if !ok {
		return nil, fmt.Errorf("layer groups are not supported by the catalog")
	}
	existing, err := groupStore.GetLayerGroup(ctx, id)
	if err != nil || existing.WorkspaceID != workspaceID {
		return nil, store.ErrNotFound
	}
	candidate := *existing
	if input.PublicID != nil {
		candidate.PublicID = *input.PublicID
	}
	if input.Title != nil {
		candidate.Title = *input.Title
	}
	if input.Description != nil {
		candidate.Description = *input.Description
	}
	if input.Enabled != nil {
		candidate.Enabled = *input.Enabled
	}
	if input.Public != nil {
		candidate.Public = *input.Public
	}
	if input.AllowedRoles != nil {
		candidate.AllowedRoles = *input.AllowedRoles
	}
	if input.Members != nil {
		candidate.Members = *input.Members
	}
	if input.DefaultStyle != nil {
		candidate.DefaultStyle = *input.DefaultStyle
	}
	if input.Styles != nil {
		candidate.Styles = *input.Styles
	}
	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	if err := validateLayerGroup(ws, runtimeLayerGroup(&candidate), existing.PublicID, maxDepth); err != nil {
		return nil, err
	}
	updated, err := groupStore.UpdateLayerGroup(ctx, id, input)
	if err != nil {
		return nil, err
	}
	delete(ws.Groups, existing.PublicID)
	ws.Groups[updated.PublicID] = runtimeLayerGroup(updated)
	r.invalidateWorkspaceCache(workspaceID)
	return runtimeLayerGroup(updated), nil
}

func (r *Registry) DeleteLayerGroup(ctx context.Context, workspaceID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	groupStore, ok := r.store.(store.LayerGroupStore)
	if !ok {
		return fmt.Errorf("layer groups are not supported by the catalog")
	}
	existing, err := groupStore.GetLayerGroup(ctx, id)
	if err != nil || existing.WorkspaceID != workspaceID {
		return store.ErrNotFound
	}
	ws, ok := r.workspacesByID[workspaceID]
	if !ok {
		return ErrWorkspaceNotFound
	}
	for _, group := range ws.Groups {
		if group.ID == id {
			continue
		}
		for _, member := range group.Members {
			if member.Resource == existing.PublicID {
				return fmt.Errorf("layer group is referenced by %q", group.PublicID)
			}
		}
	}
	if err := groupStore.DeleteLayerGroup(ctx, id); err != nil {
		return err
	}
	delete(ws.Groups, existing.PublicID)
	r.invalidateWorkspaceCache(workspaceID)
	return nil
}

func validateLayerGroup(ws *Workspace, candidate *LayerGroup, replacedPublicID string, maxDepth int) error {
	if candidate == nil || candidate.PublicID == "" {
		return fmt.Errorf("public_id is required")
	}
	if maxDepth <= 0 {
		maxDepth = 8
	}
	if ws.HasPublishedResourceID(candidate.PublicID) && candidate.PublicID != replacedPublicID {
		return fmt.Errorf("public_id conflicts with an existing resource: %w", store.ErrDuplicateKey)
	}
	if replacedPublicID != "" && candidate.PublicID != replacedPublicID {
		if references := ws.GroupReferences(replacedPublicID); len(references) > 0 {
			return fmt.Errorf("cannot rename layer group referenced by %q", references[0])
		}
	}
	if len(candidate.Members) == 0 {
		return fmt.Errorf("members must not be empty")
	}
	for _, member := range candidate.Members {
		if member.Resource == "" {
			return fmt.Errorf("member resource is required")
		}
		if member.Opacity != nil && (*member.Opacity < 0 || *member.Opacity > 1) {
			return fmt.Errorf("member opacity must be between 0 and 1")
		}
		if member.Composite != "" && member.Composite != "source-over" && member.Composite != "multiply" && member.Composite != "screen" && member.Composite != "overlay" && member.Composite != "darken" && member.Composite != "lighten" {
			return fmt.Errorf("unsupported member composite mode %q", member.Composite)
		}
		if member.Resource != replacedPublicID && ws.GetResource(member.Resource) == nil {
			return fmt.Errorf("member resource %q does not exist", member.Resource)
		}
		if member.Style != "" && ws.GetStyle(member.Style) == nil {
			return fmt.Errorf("member style %q does not exist", member.Style)
		}
	}
	groups := make(map[string]*LayerGroup, len(ws.Groups)+1)
	for name, group := range ws.Groups {
		groups[name] = group
	}
	if replacedPublicID != "" {
		delete(groups, replacedPublicID)
	}
	groups[candidate.PublicID] = candidate
	var visit func(string, map[string]bool, int) error
	visit = func(name string, stack map[string]bool, depth int) error {
		if depth > maxDepth {
			return fmt.Errorf("layer group nesting exceeds %d", maxDepth)
		}
		if stack[name] {
			return fmt.Errorf("layer group cycle includes %q", name)
		}
		group := groups[name]
		if group == nil {
			return nil
		}
		stack[name] = true
		defer delete(stack, name)
		for _, member := range group.Members {
			if _, nested := groups[member.Resource]; nested {
				if err := visit(member.Resource, stack, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(candidate.PublicID, make(map[string]bool), 1)
}
