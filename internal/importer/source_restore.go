package importer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
)

// MkdirTemp's upload suffix is decimal. A deployment root named upload-staging
// is not an owned upload directory. Prefer the job-specific extraction identity;
// otherwise choose an unambiguous upload suffix, checking the restored root when
// more than one numeric upload ancestor exists. Never persist a guessed path.
func legacySourceRelative(job *store.ImportJob, root string) (string, error) {
	parts := strings.Split(filepath.Clean(job.SourcePath), string(filepath.Separator))
	var candidates []string
	var existing []string
	for i, part := range parts[:max(0, len(parts)-1)] {
		if part == "extract-"+job.ID {
			return filepath.Join(parts[i:]...), nil
		}
		if job.SourceKind != "upload" || !strings.HasPrefix(part, "upload-") {
			continue
		}
		suffix := strings.TrimPrefix(part, "upload-")
		candidate := filepath.Join(parts[i:]...)
		// Older/manual consistency sets can have nonnumeric upload names.
		// Accept those only when the restored file proves the relative identity.
		if suffix != "" && regularFile(filepath.Join(root, candidate)) {
			existing = append(existing, candidate)
		}
		if suffix != "" && strings.Trim(suffix, "0123456789") == "" {
			candidates = append(candidates, candidate)
		}
	}
	if len(existing) == 1 {
		return existing[0], nil
	}
	if len(existing) > 1 {
		return "", fmt.Errorf("ambiguous restored source identity for import %s", job.ID)
	}
	if len(candidates) == 0 {
		return "", nil
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("ambiguous legacy source identity for import %s; restore its owned source directory or set source_relative_path from the consistency-set backup", job.ID)
}

func ownedSourceRelative(relative, jobID string) bool {
	if !filepath.IsLocal(relative) {
		return false
	}
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	return len(parts) > 1 && (strings.HasPrefix(parts[0], "upload-") || parts[0] == "extract-"+jobID)
}

func reconcileRetainedSources(ctx context.Context, cfg conf.Importer, durable store.ImportStore) error {
	cursor := ""
	for {
		page, err := durable.ListImportJobsPage(ctx, "", store.ImportListOptions{Limit: 1000, Cursor: cursor})
		if err != nil {
			return err
		}
		for _, job := range page.Imports {
			if job.Status == store.ImportPublished || job.Status == store.ImportCancelled || job.Status == store.ImportRolledBack {
				continue
			}
			relative := job.SourceRelativePath
			if relative == "" {
				// Upgrade pre-v24 jobs only for recognized managed upload/extract
				// directories. Never relocate arbitrary URI/local datasource paths.
				relative, err = legacySourceRelative(job, cfg.TemporaryDirectory)
				if err != nil {
					return err
				}
			}
			if relative == "" {
				continue
			}
			if !ownedSourceRelative(relative, job.ID) {
				return fmt.Errorf("invalid owned source identity for import %s", job.ID)
			}
			candidate := filepath.Join(cfg.TemporaryDirectory, relative)
			if regularFile(candidate) {
				root, rootErr := filepath.EvalSymlinks(cfg.TemporaryDirectory)
				resolved, pathErr := filepath.EvalSymlinks(candidate)
				if rootErr != nil || pathErr != nil {
					return fmt.Errorf("resolve restored source for import %s", job.ID)
				}
				rel, err := filepath.Rel(root, resolved)
				if err != nil || !filepath.IsLocal(rel) {
					return fmt.Errorf("restored source escapes managed directory for import %s", job.ID)
				}
			}
			// Even a missing input must point into the new root: recovery must
			// never read or clean an old mount just because it still exists.
			if candidate != job.SourcePath || relative != job.SourceRelativePath {
				if _, err := durable.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{SourcePath: &candidate, SourceRelativePath: &relative}); err != nil {
					return err
				}
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}
