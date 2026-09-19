package server

import (
	"context"

	"github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/store"
)

// Resolve only the published binding owned by this exact service. Staged
// assets are read by the import worker directly, never through this capability.
func managedAssetResolver(catalog store.ImportStore) duckdb.ManagedResolver {
	return func(ctx context.Context, workspaceID, serviceID, importID string) (string, string, map[string]int, error) {
		asset, err := catalog.GetManagedAssetByService(ctx, serviceID)
		if err != nil || asset == nil || asset.WorkspaceID != workspaceID || asset.ImportID != importID || asset.ServiceID != serviceID {
			return "", "", nil, store.ErrNotFound
		}
		job, err := catalog.GetImportJob(ctx, importID)
		if err != nil || job == nil || job.WorkspaceID != workspaceID || job.ServiceID != serviceID || job.Status != store.ImportPublished {
			return "", "", nil, store.ErrNotFound
		}
		// Legacy files have no embedded CRS metadata. Recover it from the
		// original plan, never from a publication's requested output CRS.
		srids := make(map[string]int)
		if job.Plan != nil {
			for _, layer := range job.Plan.Layers {
				srid := layer.TargetSRID
				if srid == 0 {
					srid = layer.SourceSRID
				}
				if srid > 0 {
					srids[layer.PublicID] = srid
				}
			}
		}
		return asset.Path, asset.EncryptionKey, srids, nil
	}
}
