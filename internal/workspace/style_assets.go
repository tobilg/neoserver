package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/tobilg/neoserver/internal/store"
	"time"
)

// RefreshStyleAssets publishes the revision committed atomically with an asset.
// All workspace consumers (including groups and shared styles) are invalidated.
func (r *Registry) RefreshStyleAssets(ctx context.Context, workspaceID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	defer r.invalidateWorkspaceCache(workspaceID)
	persistence, ok := r.store.(store.WMTSStore)
	if !ok {
		return nil
	}
	// Serialize manifest reads/publication so a delayed refresh cannot replace
	// a newer snapshot with an older hash map.
	r.mu.Lock()
	defer r.mu.Unlock()
	revision, err := persistence.GetTileRevision(ctx, workspaceID)
	if err != nil {
		return err
	}
	digest, assets, err := r.styleAssetManifest(ctx, workspaceID)
	if err != nil {
		return err
	}
	if ws := r.workspacesByID[workspaceID]; ws != nil {
		if revision > ws.TileRevision {
			ws.TileRevision = revision
		}
		ws.StyleAssetDigest = digest
		ws.StyleAssets = assets
	}
	return nil
}

func (r *Registry) styleAssetManifest(ctx context.Context, workspaceID string) (string, map[string]string, error) {
	manifest := make(map[string]string)
	persistence, ok := r.store.(store.StyleAssetStore)
	if !ok {
		return "", manifest, nil
	}
	assets, err := persistence.ListStyleAssets(ctx, workspaceID)
	if err != nil {
		return "", nil, err
	}
	if len(assets) == 0 {
		return "", manifest, nil
	}
	hash := sha256.New()
	// ListStyleAssets is ordered by name, giving restart-stable identities.
	for _, asset := range assets {
		manifest[asset.Name] = asset.SHA256
		fmt.Fprintf(hash, "%d:%s:%s;", len(asset.Name), asset.Name, asset.SHA256)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), manifest, nil
}
