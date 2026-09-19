package store

import (
	"context"
	"testing"
)

func TestV24PreservesExistingImportSources(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	job, err := s.CreateImportJob(ctx, CreateImportJobInput{WorkspaceID: "ws", Name: "legacy", SourceKind: "upload", SourcePath: "/old/upload-owned/data.geojson"})
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the immediately preceding schema around an existing job.
	_, err = s.db.Exec(`DROP INDEX import_jobs_workspace; DROP INDEX import_jobs_status;
ALTER TABLE import_jobs DROP COLUMN source_relative_path;
CREATE INDEX import_jobs_workspace ON import_jobs(workspace_id,created_at);
CREATE INDEX import_jobs_status ON import_jobs(status,updated_at);
DELETE FROM schema_info WHERE version=24;
INSERT OR REPLACE INTO schema_info(version) VALUES(23);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.runMigrations(23); err != nil {
		t.Fatal(err)
	}
	restored, err := s.GetImportJob(ctx, job.ID)
	if err != nil || restored.SourcePath != job.SourcePath || restored.SourceRelativePath != "" {
		t.Fatalf("legacy source changed: %+v %v", restored, err)
	}
	relative := "upload-owned/data.geojson"
	if _, err := s.UpdateImportJob(ctx, job.ID, ImportJobUpdate{SourceRelativePath: &relative}); err != nil {
		t.Fatal(err)
	}
	restored, err = s.GetImportJob(ctx, job.ID)
	if err != nil || restored.SourceRelativePath != relative {
		t.Fatalf("relative identity not persisted: %+v %v", restored, err)
	}
}
