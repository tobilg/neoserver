package importer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

// The caller must have stopped processing the job and closed its database.
// Keep the cancellation intent durable until every cleanup step has succeeded.
func (m *Manager) cleanupCancelled(ctx context.Context, job *store.ImportJob) error {
	current, err := m.store.GetImportJob(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.ServiceID != "" {
		return errors.New("cannot clean a cancelled import that still owns a published service")
	}
	if err = m.cleanupTemporarySource(current.SourcePath); err != nil {
		return err
	}
	asset, err := m.store.GetManagedAssetByImport(ctx, job.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if asset != nil {
		if err := removeOwnedFile(asset.Path); err != nil {
			return err
		}
		if err := removeOwnedFile(asset.Path + ".wal"); err != nil {
			return err
		}
	}
	if err := removeOwnedFile(filepath.Join(m.cfg.Root, job.WorkspaceID, job.ID+".duckdb")); err != nil {
		return err
	}
	if err := m.removeUnselectedRevisions(job.ID, ""); err != nil {
		return err
	}
	if err := m.store.DeleteManagedAsset(ctx, job.ID); err != nil {
		return err
	}
	status, now, empty := store.ImportCancelled, time.Now().UTC(), ""
	_, err = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &status, CompletedAt: &now, ManagedPath: &empty, ErrorMessage: &empty})
	return err
}

func (m *Manager) removeUnselectedRevisions(id, selected string) error {
	root := filepath.Join(m.cfg.Root, ".staging")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasPrefix(name, id+".revision-") || name == id+".duckdb" || name == id+".duckdb.wal") {
			continue
		}
		path := filepath.Join(root, name)
		if path == selected || path == selected+".wal" {
			continue
		}
		if err := removeOwnedFile(path); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) cleanupAbandonedRevisions(ctx context.Context) error {
	entries, err := os.ReadDir(filepath.Join(m.cfg.Root, ".staging"))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		id, _, ok := strings.Cut(entry.Name(), ".revision-")
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := m.store.GetImportJob(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return err
		}
		asset, err := m.store.GetManagedAssetByImport(ctx, id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		selected := ""
		if asset != nil {
			selected = asset.Path
		}
		if err := m.removeUnselectedRevisions(id, selected); err != nil {
			return err
		}
	}
	return nil
}

// Retained sources do not expire implicitly: users can revise until they
// publish or cancel. Bound aggregate retention, including extracted archives.
func (m *Manager) remainingSourceBytes() (int64, error) {
	limit := m.cfg.MaxRetainedSourceBytes
	if limit == 0 {
		limit = 10 << 30
	}
	var used int64
	err := filepath.WalkDir(m.cfg.TemporaryDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			used += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if used > limit {
		return 0, fmt.Errorf("retained import sources exceed %d byte budget; publish or cancel unused imports", limit)
	}
	return limit - used, nil
}

func (m *Manager) logSourceUsage() {
	if m.logger == nil {
		return
	}
	remaining, err := m.remainingSourceBytes()
	if err != nil {
		m.logger.Warn("import retained-source usage unavailable", "error", err)
		return
	}
	limit := m.cfg.MaxRetainedSourceBytes
	if limit == 0 {
		limit = 10 << 30
	}
	m.logger.Info("import source retention", "retained_bytes", limit-remaining, "limit_bytes", limit, "policy", "until publish or cancel; no automatic expiry")
}
