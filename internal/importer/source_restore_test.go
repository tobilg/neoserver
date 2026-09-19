package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestPendingImportSourcesRestoreAtNewRoots(t *testing.T) {
	for _, ready := range []bool{false, true} {
		for _, archive := range []bool{false, true} {
			for _, prefix := range []string{"sources", "upload-staging", "upload-100"} {
				for _, relocate := range []bool{false, true} {
					t.Run(fmt.Sprintf("preview=%t/archive=%t/root=%s/relocate=%t", ready, archive, prefix, relocate), func(t *testing.T) {
						testPendingImportRestore(t, ready, archive, prefix, relocate)
					})
				}
			}
		}
	}
}

func testPendingImportRestore(t *testing.T, ready, archive bool, prefix string, relocate bool) {
	m, catalog, ws := revisionFixture(t)
	ctx := context.Background()
	m.cfg.TemporaryDirectory = filepath.Join(t.TempDir(), prefix)
	if err := os.MkdirAll(m.cfg.TemporaryDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	var sourceReader io.Reader = strings.NewReader(revisionGeoJSON)
	filename := "points.geojson"
	if archive {
		var contents bytes.Buffer
		writer := zip.NewWriter(&contents)
		file, err := writer.Create("nested/points.geojson")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(revisionGeoJSON)); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		sourceReader, filename = &contents, "points.zip"
	}
	job, err := m.CreateUpload(ctx, ws, "restore", filename, "test", sourceReader)
	if err != nil {
		t.Fatal(err)
	}
	job = runRevisionJob(t, m)
	source := job.Discovery.Layers[0]
	plan := store.ImportPlan{ServiceName: "restored", Layers: []store.ImportLayerPlan{{SourceLayer: source.Name, PublicID: "points", SourceSRID: 4326, TargetSRID: 4326, GeometryColumn: source.GeometryColumn, Enabled: true}}}
	if ready {
		if _, err = m.SetPlan(ctx, job.ID, plan); err != nil {
			t.Fatal(err)
		}
		job = runRevisionJob(t, m)
		if job.Status != store.ImportReadyToPublish {
			t.Fatalf("setup: %+v", job)
		}
	}
	if err = m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if prefix != "sources" {
		empty := ""
		if _, err = catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{SourceRelativePath: &empty}); err != nil {
			t.Fatal(err)
		}
	}
	next := m.cfg
	if relocate {
		restored := t.TempDir()
		next.Root = filepath.Join(restored, "imports")
		next.TemporaryDirectory = filepath.Join(restored, "sources")
		if err = os.Rename(m.cfg.Root, next.Root); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(m.cfg.TemporaryDirectory, next.TemporaryDirectory); err != nil {
			t.Fatal(err)
		}
	}
	if err = ReconcileManagedPaths(ctx, next, catalog); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(ctx, next, catalog, m.registry, nil, m.logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recovered.SetPlan(ctx, job.ID, plan); err != nil {
		t.Fatalf("restored source unavailable: %v", err)
	}
	result := runRevisionJob(t, recovered)
	if result.Status != store.ImportReadyToPublish {
		t.Fatalf("restored revision failed: %+v", result)
	}
	if err = recovered.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if regularFile(result.SourcePath) {
		t.Fatal("restored source not cleaned after cancellation")
	}
	recovered.Close(ctx)
}

func TestLegacySourceAmbiguityFailsClosed(t *testing.T) {
	job := &store.ImportJob{ID: "job", SourceKind: "upload", SourcePath: "/old/upload-100/upload-200/points.geojson"}
	if _, err := legacySourceRelative(job, t.TempDir()); err == nil {
		t.Fatal("ambiguous missing source accepted")
	}
	job.SourcePath = "/old/upload-staging/upload-200/points.geojson"
	if relative, err := legacySourceRelative(job, t.TempDir()); err != nil || relative != "upload-200/points.geojson" {
		t.Fatalf("legacy relative=%q error=%v", relative, err)
	}
}

func TestOwnedSourceIdentityRejectsArbitraryPaths(t *testing.T) {
	for _, path := range []string{"../upload-x/a", "/tmp/upload-x/a", "data/a", "extract-other/a", "upload-x"} {
		if ownedSourceRelative(path, "job") {
			t.Fatalf("unowned path accepted: %s", path)
		}
	}
}
