package cataloglifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/store"
)

type IntegrityIssue struct {
	Store       string `json:"store"`
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Reason      string `json:"reason"`
}

type IntegrityReport struct {
	Healthy  bool             `json:"healthy"`
	Issues   []IntegrityIssue `json:"issues"`
	Repaired int64            `json:"repaired,omitempty"`
}

func (c *Coordinator) AuditIntegrity(ctx context.Context) (*IntegrityReport, error) {
	integrityStore, ok := c.deps.Catalog.(store.CatalogIntegrityStore)
	if !ok {
		return nil, errors.New("catalog integrity operations are unavailable")
	}
	ownership, err := integrityStore.CatalogOwnership(ctx)
	if err != nil {
		return nil, err
	}
	orphans, err := integrityStore.AuditCatalogOrphans(ctx)
	if err != nil {
		return nil, err
	}
	report := &IntegrityReport{Issues: []IntegrityIssue{}}
	for _, orphan := range orphans {
		report.Issues = append(report.Issues, IntegrityIssue{Store: "catalog", Kind: orphan.Kind, ID: orphan.ID,
			Name: orphan.Name, WorkspaceID: orphan.WorkspaceID, Reason: orphan.Reason})
	}
	if c.deps.TileCache != nil {
		owners, err := c.deps.TileCache.LifecycleInventory(ctx)
		if err != nil {
			return nil, err
		}
		for _, owner := range owners {
			reason := ""
			if !ownership.Workspaces[owner.WorkspaceID] {
				reason = "workspace does not exist"
			} else if owner.ResourceID != "" && ownership.Resources[owner.ResourceID] != owner.WorkspaceID {
				reason = "published resource does not exist in workspace"
			}
			if reason != "" {
				report.Issues = append(report.Issues, IntegrityIssue{Store: "tile_cache", Kind: "tile_owner", ID: owner.ResourceID, WorkspaceID: owner.WorkspaceID, Reason: reason})
			}
		}
	}
	if c.deps.Mosaic != nil {
		owners, err := c.deps.Mosaic.LifecycleInventory(ctx)
		if err != nil {
			return nil, err
		}
		for _, owner := range owners {
			if !ownership.Workspaces[owner.WorkspaceID] || ownership.Services[owner.ServiceID] != owner.WorkspaceID {
				report.Issues = append(report.Issues, IntegrityIssue{Store: "mosaic_catalog", Kind: "mosaic_owner", ID: owner.ServiceID, WorkspaceID: owner.WorkspaceID, Reason: "workspace or service does not exist"})
			}
		}
	}
	assetIssues, err := auditAssetDirectories(c.deps.StyleAssetRoot, ownership.Workspaces)
	if err != nil {
		return nil, err
	}
	report.Issues = append(report.Issues, assetIssues...)
	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].Store != report.Issues[j].Store {
			return report.Issues[i].Store < report.Issues[j].Store
		}
		if report.Issues[i].Kind != report.Issues[j].Kind {
			return report.Issues[i].Kind < report.Issues[j].Kind
		}
		if report.Issues[i].WorkspaceID != report.Issues[j].WorkspaceID {
			return report.Issues[i].WorkspaceID < report.Issues[j].WorkspaceID
		}
		return report.Issues[i].ID < report.Issues[j].ID
	})
	report.Healthy = len(report.Issues) == 0
	return report, nil
}

// RepairIntegrity explicitly and idempotently removes state whose catalog
// owner no longer exists. It never touches datasource tables or source raster
// files. Callers must obtain operator confirmation before invoking it.
func (c *Coordinator) RepairIntegrity(ctx context.Context) (*IntegrityReport, error) {
	c.integrityMu.Lock()
	defer c.integrityMu.Unlock()
	integrityStore, ok := c.deps.Catalog.(store.CatalogIntegrityStore)
	if !ok {
		return nil, errors.New("catalog integrity operations are unavailable")
	}
	ownership, err := integrityStore.CatalogOwnership(ctx)
	if err != nil {
		return nil, err
	}
	var repaired int64
	if c.deps.TileCache != nil {
		owners, err := c.deps.TileCache.LifecycleInventory(ctx)
		if err != nil {
			return nil, err
		}
		missingWorkspaces := make(map[string]bool)
		missingResources := make(map[string][]string)
		for _, owner := range owners {
			if !ownership.Workspaces[owner.WorkspaceID] {
				missingWorkspaces[owner.WorkspaceID] = true
			} else if owner.ResourceID != "" && ownership.Resources[owner.ResourceID] != owner.WorkspaceID {
				missingResources[owner.WorkspaceID] = append(missingResources[owner.WorkspaceID], owner.ResourceID)
			}
		}
		for workspaceID := range missingWorkspaces {
			if c.deps.TileJobs != nil {
				if err := c.deps.TileJobs.QuiesceLifecycle(ctx, workspaceID, nil); err != nil {
					return nil, err
				}
			}
			deleted, jobs, err := c.deps.TileCache.PurgeLifecycle(ctx, workspaceID, nil)
			if err != nil {
				return nil, err
			}
			repaired += deleted.Entries + jobs
		}
		for workspaceID, resourceIDs := range missingResources {
			resourceIDs = uniqueStrings(resourceIDs)
			if c.deps.TileJobs != nil {
				if err := c.deps.TileJobs.QuiesceLifecycle(ctx, workspaceID, resourceIDs); err != nil {
					return nil, err
				}
			}
			deleted, jobs, err := c.deps.TileCache.PurgeLifecycle(ctx, workspaceID, resourceIDs)
			if err != nil {
				return nil, err
			}
			repaired += deleted.Entries + jobs
		}
	}
	if c.deps.Mosaic != nil {
		owners, err := c.deps.Mosaic.LifecycleInventory(ctx)
		if err != nil {
			return nil, err
		}
		missingWorkspaces := make(map[string]bool)
		missingServices := make(map[string][]string)
		for _, owner := range owners {
			if !ownership.Workspaces[owner.WorkspaceID] {
				missingWorkspaces[owner.WorkspaceID] = true
			} else if ownership.Services[owner.ServiceID] != owner.WorkspaceID {
				missingServices[owner.WorkspaceID] = append(missingServices[owner.WorkspaceID], owner.ServiceID)
			}
		}
		for workspaceID := range missingWorkspaces {
			services, granules, jobs, _ := c.deps.Mosaic.LifecycleStats(ctx, workspaceID, nil)
			if err := c.deps.Mosaic.QuiesceAndDeleteLifecycle(ctx, workspaceID, nil); err != nil {
				return nil, err
			}
			repaired += services + granules + jobs
		}
		for workspaceID, serviceIDs := range missingServices {
			serviceIDs = uniqueStrings(serviceIDs)
			services, granules, jobs, _ := c.deps.Mosaic.LifecycleStats(ctx, workspaceID, serviceIDs)
			if err := c.deps.Mosaic.QuiesceAndDeleteLifecycle(ctx, workspaceID, serviceIDs); err != nil {
				return nil, err
			}
			repaired += services + granules + jobs
		}
	}
	assetRepairs, err := repairAssetDirectories(c.deps.StyleAssetRoot, ownership.Workspaces)
	if err != nil {
		return nil, err
	}
	repaired += assetRepairs
	var catalogRepairs int64
	repairCatalog := func() error {
		var err error
		catalogRepairs, err = integrityStore.RepairCatalogOrphans(ctx)
		return err
	}
	if c.deps.Enforcer != nil {
		err = c.deps.Enforcer.UpdatePolicyStore(repairCatalog)
	} else {
		err = repairCatalog()
	}
	if err != nil {
		return nil, err
	}
	for workspaceID := range ownership.Workspaces {
		if err := c.deps.Registry.RefreshOGCTilesAPISettings(ctx, workspaceID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	repaired += catalogRepairs
	report, err := c.AuditIntegrity(ctx)
	if err != nil {
		return nil, err
	}
	report.Repaired = repaired
	return report, nil
}

func auditAssetDirectories(root string, workspaces map[string]bool) ([]IntegrityIssue, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []IntegrityIssue
	for _, entry := range entries {
		if entry.Name() == ".deleting" || workspaces[entry.Name()] {
			continue
		}
		result = append(result, IntegrityIssue{Store: "style_assets", Kind: "asset_directory", ID: entry.Name(), WorkspaceID: entry.Name(), Reason: "workspace does not exist"})
	}
	return result, nil
}

func repairAssetDirectories(root string, workspaces map[string]bool) (int64, error) {
	issues, err := auditAssetDirectories(root, workspaces)
	if err != nil {
		return 0, err
	}
	var repaired int64
	for _, issue := range issues {
		source := filepath.Join(root, issue.ID)
		info, err := os.Lstat(source)
		if err != nil {
			return repaired, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return repaired, fmt.Errorf("refusing to repair non-directory managed asset path %q", source)
		}
		staged := filepath.Join(root, ".deleting", "repair-"+uuid.NewString())
		if err := os.MkdirAll(filepath.Dir(staged), 0o750); err != nil {
			return repaired, err
		}
		if err := os.Rename(source, staged); err != nil {
			return repaired, err
		}
		if err := os.RemoveAll(staged); err != nil {
			return repaired, err
		}
		repaired++
	}
	return repaired, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
