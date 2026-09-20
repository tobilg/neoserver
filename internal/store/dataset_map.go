package store

import (
	"context"
	"errors"
	"fmt"
)

var ErrInvalidDatasetMap = errors.New("workspace map must reference an existing layer group in this workspace that is not being deleted")
var ErrDatasetMapInUse = errors.New("clear or change the workspace map selection before deleting its layer group or a service containing its members")
var ErrGroupResourceDeleting = errors.New("layer group or one of its members is being deleted")

// Caller holds datasetMapMu across validation and persistence.
func (s *DuckDBStore) validateDatasetMapSelection(ctx context.Context, workspaceID, groupID string) error {
	if groupID == "" {
		return nil
	}
	group, err := s.GetLayerGroup(ctx, groupID)
	if errors.Is(err, ErrNotFound) {
		return ErrInvalidDatasetMap
	}
	if err != nil {
		return err
	}
	if group.WorkspaceID != workspaceID {
		return ErrInvalidDatasetMap
	}
	if err := s.validateGroupDeletionState(ctx, workspaceID, group.ID, group.PublicID, group.Members); err != nil {
		if errors.Is(err, ErrGroupResourceDeleting) {
			return ErrInvalidDatasetMap
		}
		return err
	}
	return nil
}

// Group writes share the deletion-registration lock. This closes the interval
// between recording a deletion and removing its resources from the registry.
func (s *DuckDBStore) validateGroupDeletionState(ctx context.Context, workspaceID, groupID, publicID string, members []LayerGroupMember) error {
	operations, err := s.ListPendingCatalogDeletions(ctx)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.WorkspaceID != workspaceID {
			continue
		}
		if operation.Scope == DeletionScopeWorkspace {
			return ErrGroupResourceDeleting
		}
		for _, refs := range [][]DeletionRef{operation.Plan.LayerGroups, operation.Plan.Layers, operation.Plan.Coverages} {
			for _, ref := range refs {
				if ref.Name == publicID || (groupID != "" && ref.ID == groupID) {
					return ErrGroupResourceDeleting
				}
				for _, member := range members {
					if ref.Name == member.Resource {
						return ErrGroupResourceDeleting
					}
				}
			}
		}
	}
	return nil
}

func (s *DuckDBStore) addDatasetMapDeletionBlocker(ctx context.Context, plan *DeletionPlan) error {
	settings, err := s.GetOGCTilesAPISettings(ctx, plan.WorkspaceID)
	if err != nil {
		return err
	}
	for _, group := range plan.LayerGroups {
		if group.ID == settings.Settings.DatasetMapLayerGroupID {
			plan.Blockers = append(plan.Blockers, DeletionRef{ID: group.ID, Name: "Workspace map", Kind: "dataset_map", Reason: fmt.Sprintf("%s (%s)", ErrDatasetMapInUse, group.Name)})
		}
	}
	return nil
}

const invalidDatasetMapPredicate = `coalesce(json_extract_string(ogc_tiles_api_settings, '$.settings.dataset_map_layer_group_id'), '') <> ''
 AND NOT EXISTS (SELECT 1 FROM layer_groups g WHERE g.id=json_extract_string(workspaces.ogc_tiles_api_settings, '$.settings.dataset_map_layer_group_id') AND g.workspace_id=workspaces.id)`
