package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

type leaseTestSource struct {
	datasource.DataSource
	closes atomic.Int64
}

func (s *leaseTestSource) Health(context.Context) error {
	if s.closes.Load() != 0 {
		return errors.New("closed")
	}
	return nil
}
func (s *leaseTestSource) Close() error { s.closes.Add(1); return nil }

func TestWorkspaceMetadataSnapshotsDoNotRaceOrAlias(t *testing.T) {
	r := NewRegistry(newMockStore(), nil)
	created, err := r.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "snapshots"})
	if err != nil {
		t.Fatal(err)
	}
	created.Name = "caller-owned"
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 300 {
			ws, _ := r.GetByID(created.ID)
			_ = ws.TileRevision
			_ = ws.Settings.OGCTilesAPI.Enabled
			ws.Settings.OGCTilesAPI.Title = "caller-owned"
		}
	}()
	close(start)
	for i := range 300 {
		if err := r.UpdateOGCTilesAPISettings(context.Background(), created.ID, store.OGCTilesAPISettings{Enabled: i%2 == 0, Title: "stored"}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	current, _ := r.GetByID(created.ID)
	if current.Name != "snapshots" || current.Settings.OGCTilesAPI.Title != "stored" {
		t.Fatal("caller modified registry through snapshot")
	}
}

func TestDatasourceReplacementWaitsForLeaseNotForGlobalReaders(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	var sources []*leaseTestSource
	r := NewRegistry(catalog, func(*store.Service) (datasource.DataSource, error) {
		source := &leaseTestSource{}
		sources = append(sources, source)
		return source, nil
	})
	defer r.Close()
	ws, err := r.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "leases"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := r.CreateService(context.Background(), store.CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: store.ServiceTypePostGIS, Enabled: true, ConnectionInfo: json.RawMessage(`{"host":"old"}`)})
	if err != nil {
		t.Fatal(err)
	}
	old, release, ok := r.AcquireByID(ws.ID)
	if !ok {
		t.Fatal("workspace missing")
	}
	defer release()
	newConnection := json.RawMessage(`{"host":"new"}`)
	if _, err := r.UpdateService(context.Background(), ws.ID, svc.ID, store.UpdateServiceInput{ConnectionInfo: &newConnection}); err != nil {
		t.Fatal(err)
	}
	if sources[0].closes.Load() != 0 || string(old.GetService(svc.ID).ConnectionInfo) != `{"host":"old"}` {
		t.Fatal("active snapshot was mutated or closed")
	}
	current, currentRelease, _ := r.AcquireByID(ws.ID)
	defer currentRelease()
	if current.GetService(svc.ID).DataSource == old.GetService(svc.ID).DataSource {
		t.Fatal("replacement not published")
	}
	release()
	release()
	if sources[0].closes.Load() != 1 || sources[1].closes.Load() != 0 {
		t.Fatal("retired handle not closed exactly once")
	}
}

func TestFailedServiceUpdateLeavesCatalogAndRuntimeAndRetries(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	var calls atomic.Int64
	r := NewRegistry(catalog, func(svc *store.Service) (datasource.DataSource, error) {
		calls.Add(1)
		if string(svc.ConnectionInfo) == `{"host":"bad"}` {
			return nil, errors.New("connection rejected")
		}
		return &leaseTestSource{}, nil
	})
	defer r.Close()
	ws, err := r.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "updates"})
	if err != nil {
		t.Fatal(err)
	}
	good := json.RawMessage(`{"host":"good"}`)
	svc, err := r.CreateService(context.Background(), store.CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: store.ServiceTypePostGIS, Enabled: true, ConnectionInfo: good})
	if err != nil {
		t.Fatal(err)
	}
	bad := json.RawMessage(`{"host":"bad"}`)
	for range 2 {
		if _, err := r.UpdateService(context.Background(), ws.ID, svc.ID, store.UpdateServiceInput{ConnectionInfo: &bad}); err == nil {
			t.Fatal("bad connection succeeded")
		}
	}
	stored, err := catalog.GetService(context.Background(), svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := r.GetByID(ws.ID)
	if string(stored.ConnectionInfo) != string(good) || string(current.GetService(svc.ID).ConnectionInfo) != string(good) || calls.Load() != 3 {
		t.Fatal("rejected update persisted or retry skipped")
	}
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, _ = r.GetByID(ws.ID)
	if string(current.GetService(svc.ID).ConnectionInfo) != string(good) {
		t.Fatal("restart loaded rejected config")
	}
}

func TestCancelledPreparationDoesNotBlockReadersAndClosesLateResult(t *testing.T) {
	start, unblock := make(chan struct{}), make(chan struct{})
	source := &leaseTestSource{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := datasource.Prepare(ctx, func() (datasource.DataSource, error) { close(start); <-unblock; return source, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline err=%v", err)
	}
	<-start
	close(unblock)
	deadline := time.Now().Add(time.Second)
	for source.closes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if source.closes.Load() != 1 {
		t.Fatal("late candidate leaked")
	}
}

type rejectedUpdateCatalog struct {
	store.Store
	reject bool
}

func (s *rejectedUpdateCatalog) UpdateService(ctx context.Context, id string, input store.UpdateServiceInput) (*store.Service, error) {
	if s.reject {
		return nil, errors.New("synthetic catalog write failure")
	}
	return s.Store.UpdateService(ctx, id, input)
}

func TestCatalogFailureDisposesCandidateAndPreservesActiveSource(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	wrapper := &rejectedUpdateCatalog{Store: catalog}
	var sources []*leaseTestSource
	r := NewRegistry(wrapper, func(*store.Service) (datasource.DataSource, error) {
		source := &leaseTestSource{}
		sources = append(sources, source)
		return source, nil
	})
	defer r.Close()
	ws, err := r.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "catalog-failure"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := r.CreateService(context.Background(), store.CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: store.ServiceTypePostGIS, Enabled: true, ConnectionInfo: json.RawMessage(`{"host":"old"}`)})
	if err != nil {
		t.Fatal(err)
	}
	wrapper.reject = true
	connection := json.RawMessage(`{"host":"candidate"}`)
	if _, err := r.UpdateService(context.Background(), ws.ID, svc.ID, store.UpdateServiceInput{ConnectionInfo: &connection}); err == nil {
		t.Fatal("failed catalog write succeeded")
	}
	current, _ := r.GetByID(ws.ID)
	stored, err := catalog.GetService(context.Background(), svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.ConnectionInfo) != `{"host":"old"}` || current.GetService(svc.ID).DataSource != sources[0] || sources[0].closes.Load() != 0 || sources[1].closes.Load() != 1 {
		t.Fatal("candidate leaked or active source replaced")
	}
}

func TestSlowRefreshCannotResurrectQuiescedServiceOrBlockOtherWorkspaces(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	old, candidate := &leaseTestSource{}, &leaseTestSource{}
	started, unblock := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	r := NewRegistry(catalog, func(*store.Service) (datasource.DataSource, error) {
		if calls.Add(1) == 1 {
			return old, nil
		}
		close(started)
		<-unblock
		return candidate, nil
	})
	defer r.Close()
	ws, err := r.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "slow"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := r.CreateService(context.Background(), store.CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: store.ServiceTypePostGIS, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, release, _ := r.AcquireByID(ws.ID)
	defer release()
	result := make(chan error, 1)
	go func() { result <- r.RefreshService(context.Background(), ws.ID, svc.ID) }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	if err := r.QuiesceService(ws.ID, svc.ID); err != nil {
		t.Fatal(err)
	}
	if old.closes.Load() != 0 {
		t.Fatal("quiescence closed leased source")
	}
	close(unblock)
	if err := <-result; !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("late refresh=%v", err)
	}
	current, _ := r.GetByID(ws.ID)
	if current.GetService(svc.ID) != nil || candidate.closes.Load() != 1 {
		t.Fatal("service resurrected or candidate leaked")
	}
	release()
	if old.closes.Load() != 1 {
		t.Fatal("retired source leaked")
	}
}

func TestCreateServiceWrapsDataSourceOpenFailures(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	opened := errors.New("path \"/etc/passwd\" is not permitted by the datasource allowlist")
	registry := NewRegistry(catalog, func(*store.Service) (datasource.DataSource, error) { return nil, opened })
	defer registry.Close()
	ctx := context.Background()
	ws, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "errors"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "bad", Type: store.ServiceTypeRasterFile, ConnectionInfo: []byte(`{"path":"/etc/passwd"}`), Enabled: true})
	var dsErr *DataSourceError
	if !errors.As(err, &dsErr) || !errors.Is(err, opened) {
		t.Fatalf("expected DataSourceError wrapping the open failure, got %v", err)
	}
	if services, _ := catalog.ListServices(ctx, ws.ID); len(services) != 0 {
		t.Fatalf("failed service was kept: %v", services)
	}
}
