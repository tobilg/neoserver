package importer

import (
	"bytes"
	"context"
	"errors"
	"github.com/klauspost/compress/zip"
	"github.com/tobilg/neoserver/internal/store"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const revisionGeoJSON = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"not-a-number"},"geometry":{"type":"Point","coordinates":[10,20]}}]}`

func revisionFixture(t *testing.T) (*Manager, *store.DuckDBStore, string) {
	t.Helper()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { catalog.Close() })
	ws, err := catalog.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "revisions"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := importerTestConfig(root)
	cfg.MaxUploadBytes = 1 << 20
	cfg.MaxSourceBytes = 1 << 20
	cfg.MaxExpandedBytes = 1 << 20
	cfg.MaxLayers = 10
	cfg.MaxFeatures = 100
	cfg.PreviewFeatures = 10
	cfg.TransformTimeoutSec = 30
	cfg.MaxArchiveFiles = 10
	m, err := New(context.Background(), cfg, catalog, recoveryRegistry{workspaceID: ws.ID}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m, catalog, ws.ID
}

func runRevisionJob(t *testing.T, m *Manager) *store.ImportJob {
	t.Helper()
	job, err := m.claim(context.Background())
	if err != nil || job == nil {
		t.Fatalf("claim: %+v %v", job, err)
	}
	m.run(job)
	job, err = m.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestUploadReviseFailedCastCorrectRestartAndPublish(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(map[bool]string{false: "geojson", true: "archive"}[archive], func(t *testing.T) { testUploadRevision(t, archive) })
	}
}

func testUploadRevision(t *testing.T, archive bool) {
	m, catalog, ws := revisionFixture(t)
	ctx := context.Background()
	var sourceReader io.Reader = strings.NewReader(revisionGeoJSON)
	filename := "points.geojson"
	if archive {
		var contents bytes.Buffer
		writer := zip.NewWriter(&contents)
		file, err := writer.Create("points.geojson")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(revisionGeoJSON)); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		sourceReader = &contents
		filename = "points.zip"
	}
	job, err := m.CreateUpload(ctx, ws, "dataset", filename, "tester", sourceReader)
	if err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	if job.Status != store.ImportAwaitingPlan {
		t.Fatalf("discovery: %+v", job)
	}
	source := job.Discovery.Layers[0]
	plan := store.ImportPlan{ServiceName: "dataset", Layers: []store.ImportLayerPlan{{SourceLayer: source.Name, PublicID: "points", SourceSRID: 4326, TargetSRID: 4326, GeometryColumn: source.GeometryColumn, Enabled: true, Public: true}}}
	if _, err = m.SetPlan(ctx, job.ID, plan); err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	if job.Status != store.ImportReadyToPublish {
		t.Fatalf("transform: %+v", job)
	}
	if !regularFile(job.SourcePath) {
		t.Fatal("ready source was discarded")
	}
	first, err := catalog.GetManagedAssetByImport(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan.Layers[0].Fields = []store.ImportFieldMapping{{Source: "name", Target: "name", Type: "INTEGER", Include: true}}
	if _, err = m.SetPlan(ctx, job.ID, plan); err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	if job.Status != store.ImportFailed {
		t.Fatalf("bad cast: %+v", job)
	}
	kept, err := catalog.GetManagedAssetByImport(ctx, job.ID)
	if err != nil || kept.Path != first.Path || kept.EncryptionKey != first.EncryptionKey || !regularFile(first.Path) {
		t.Fatalf("last good revision lost: %+v %v", kept, err)
	}
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	m2, err := New(ctx, m.cfg, catalog, m.registry, nil, m.logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close(ctx)
	plan.Layers[0].Fields = nil
	plan.Layers[0].PublicID = "corrected"
	if _, err = m2.SetPlan(ctx, job.ID, plan); err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m2)
	if job.Status != store.ImportReadyToPublish {
		t.Fatalf("corrected: %+v", job)
	}
	if regularFile(first.Path) {
		t.Fatal("superseded stage retained")
	}
	preview, err := m2.Preview(ctx, job.ID, "corrected", 10)
	if err != nil || len(preview) != 1 {
		t.Fatalf("preview: %s %v", preview, err)
	}
	job, err = m2.Publish(ctx, job.ID)
	if err != nil || job.Status != store.ImportPublished {
		t.Fatalf("publish: %+v %v", job, err)
	}
	if regularFile(job.SourcePath) {
		t.Fatal("published source retained")
	}
}

func TestCancellationCleansStageAndRecoversAfterRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "restart"}[restart], func(t *testing.T) {
			m, catalog, ws := revisionFixture(t)
			ctx := context.Background()
			job, err := m.CreateUpload(ctx, ws, "cancel", "points.geojson", "tester", strings.NewReader(revisionGeoJSON))
			if err != nil {
				t.Fatal(err)
			}
			running := store.ImportRunning
			job, err = catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &running})
			if err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(m.cfg.Root, ".staging", job.ID+".duckdb")
			for _, path := range []string{stage, stage + ".wal", filepath.Join(m.cfg.Root, ".staging", job.ID+".revision-orphan.duckdb")} {
				if err := os.WriteFile(path, []byte("owned fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := catalog.UpsertStagedManagedAsset(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: ws, Path: stage, EncryptionKey: "key"}); err != nil {
				t.Fatal(err)
			}
			if err := m.Cancel(ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			current, _ := m.Get(ctx, job.ID)
			if current.Status != store.ImportCancelling {
				t.Fatal("cancellation reported complete before worker closed")
			}
			if restart {
				m.Close(ctx)
				next, err := New(ctx, m.cfg, catalog, m.registry, nil, m.logger)
				if err != nil {
					t.Fatal(err)
				}
				defer next.Close(ctx)
			} else if err := m.markCancelled(ctx, job); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			current, _ = m.Get(ctx, job.ID)
			if current.Status != store.ImportCancelled || current.CompletedAt == nil {
				t.Fatalf("not completed: %+v", current)
			}
			if _, err := catalog.GetManagedAssetByImport(ctx, job.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("asset metadata remains: %v", err)
			}
			if regularFile(stage) || regularFile(job.SourcePath) {
				t.Fatal("cancelled data remains")
			}
			entries, _ := os.ReadDir(filepath.Join(m.cfg.Root, ".staging"))
			if len(entries) != 0 {
				t.Fatalf("staging not empty: %v", entries)
			}
			if err := m.Cancel(ctx, job.ID); err != nil {
				t.Fatalf("cleanup not idempotent: %v", err)
			}
		})
	}
}

func TestRetainedSourceBudgetAndMissingSourceReplan(t *testing.T) {
	m, catalog, ws := revisionFixture(t)
	ctx := context.Background()
	m.cfg.MaxRetainedSourceBytes = int64(len(revisionGeoJSON))
	job, err := m.CreateUpload(ctx, ws, "one", "points.geojson", "tester", strings.NewReader(revisionGeoJSON))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CreateUpload(ctx, ws, "two", "points.geojson", "tester", strings.NewReader(revisionGeoJSON)); err == nil {
		t.Fatal("aggregate budget exceeded")
	}
	if err = m.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	job, err = m.CreateUpload(ctx, ws, "three", "points.geojson", "tester", strings.NewReader(revisionGeoJSON))
	if err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	source := job.Discovery.Layers[0]
	if err = os.Remove(job.SourcePath); err != nil {
		t.Fatal(err)
	}
	plan := store.ImportPlan{ServiceName: "three", Layers: []store.ImportLayerPlan{{SourceLayer: source.Name, PublicID: "points", SourceSRID: 4326}}}
	if _, err = m.SetPlan(ctx, job.ID, plan); err == nil {
		t.Fatal("missing-source revision was accepted")
	}
	stored, _ := catalog.GetImportJob(ctx, job.ID)
	if stored.Status != store.ImportAwaitingPlan || stored.Plan != nil {
		t.Fatal("rejected revision mutated job")
	}
}

func TestFailedCancellationCleanupRemainsRetryable(t *testing.T) {
	m, catalog, ws := revisionFixture(t)
	ctx := context.Background()
	job, err := m.CreateUpload(ctx, ws, "cleanup", "points.geojson", "tester", strings.NewReader(revisionGeoJSON))
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(m.cfg.Root, ".staging", job.ID+".duckdb")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(stage, "busy-fixture")
	if err := os.WriteFile(child, []byte("busy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertStagedManagedAsset(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: ws, Path: stage, EncryptionKey: "key"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(ctx, job.ID); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	current, _ := m.Get(ctx, job.ID)
	if current.Status != store.ImportCancelling || current.CompletedAt != nil {
		t.Fatal("cleanup failure falsely reported terminal success")
	}
	failed := store.ImportFailed
	if _, err := catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &failed}); !errors.Is(err, context.Canceled) {
		t.Fatalf("late worker update overrode cancellation: %v", err)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = m.Get(ctx, job.ID)
	if current.Status != store.ImportCancelled {
		t.Fatal("cleanup retry did not complete")
	}
}

func TestUncommittedPublicationPlanCanBeCorrected(t *testing.T) {
	m, catalog, ws := revisionFixture(t)
	ctx := context.Background()
	job, err := m.CreateUpload(ctx, ws, "publication", "points.geojson", "tester", strings.NewReader(revisionGeoJSON))
	if err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	failed, phase := store.ImportFailed, store.ImportPhasePublish
	if _, err := catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &failed, Phase: &phase}); err != nil {
		t.Fatal(err)
	}
	plan := store.ImportPlan{ServiceName: "corrected_name", Layers: []store.ImportLayerPlan{{SourceLayer: job.Discovery.Layers[0].Name, PublicID: "points", SourceSRID: 4326}}}
	if _, err := m.SetPlan(ctx, job.ID, plan); err != nil {
		t.Fatalf("uncommitted publication cannot be corrected: %v", err)
	}
	service, err := catalog.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws, Name: "published", Type: store.ServiceTypePostGIS, ConnectionInfo: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &failed, ServiceID: &service.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetPlan(ctx, job.ID, plan); err == nil {
		t.Fatal("published asset was replanned")
	}
	if err := m.Cancel(ctx, job.ID); err == nil {
		t.Fatal("published asset was cancelled outside rollback")
	}
}
