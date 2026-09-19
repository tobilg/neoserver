package store

import (
	"context"
	"testing"
)

func TestBaselinePreservesImportSources(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	job, err := s.CreateImportJob(ctx, CreateImportJobInput{WorkspaceID: "ws", Name: "legacy", SourceKind: "upload", SourcePath: "/old/upload-owned/data.geojson"})
	if err != nil {
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
