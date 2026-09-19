package rbac

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/casbin/casbin/v2/model"
	"github.com/tobilg/neoserver/internal/store"
)

type failingAdapter struct {
	*MemoryAdapter
	failSave, failLoad bool
}

func TestDeletedRoleGrantsStayRevokedAcrossReload(t *testing.T) {
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	if _, err := catalog.CreateRole(ctx, store.CreateRoleInput{ID: "custom", Name: "Custom"}); err != nil {
		t.Fatal(err)
	}
	e, err := NewEnforcer(NewStoreAdapter(catalog))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AddOperationPolicy("custom", "*", "wms", "GetMap", "read"); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.CanAccessOperation("custom", "workspace", "wms", "GetMap", "read"); err != nil || !ok {
		t.Fatal("fixture grant missing")
	}
	if err := e.DeleteRole(func() error { return catalog.DeleteRole(ctx, "custom") }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if ok, err := e.CanAccessOperation("custom", "workspace", "wms", "GetMap", "read"); err != nil || ok {
			t.Fatalf("deleted grant still active: %v %v", ok, err)
		}
		e, err = NewEnforcer(NewStoreAdapter(catalog))
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (a *failingAdapter) SavePolicy(m model.Model) error {
	if a.failSave {
		return errors.New("injected persistence failure")
	}
	return a.MemoryAdapter.SavePolicy(m)
}
func (a *failingAdapter) LoadPolicy(m model.Model) error {
	if a.failLoad {
		return errors.New("injected load failure")
	}
	return a.MemoryAdapter.LoadPolicy(m)
}

func TestPolicyPersistenceFailureDoesNotWidenAccess(t *testing.T) {
	a := &failingAdapter{MemoryAdapter: NewMemoryAdapter()}
	e, err := NewEnforcer(a)
	if err != nil {
		t.Fatal(err)
	}
	a.failSave = true
	if err := e.AddOperationPolicy("custom", "ws", "wms", "GetMap", "read"); err == nil {
		t.Fatal("save failure ignored")
	}
	if allowed, err := e.CanAccessOperation("custom", "ws", "wms", "GetMap", "read"); err != nil || allowed {
		t.Fatalf("uncommitted grant visible: %v %v", allowed, err)
	}
	a.failLoad = true
	if err := e.AddWorkspacePolicy("custom", "ws", "manage"); err == nil {
		t.Fatal("failure ignored")
	}
	if allowed, err := e.CanManageWorkspace("custom", "ws"); err == nil || allowed {
		t.Fatal("failed rollback must fail closed")
	}
	a.failSave = false
	a.failLoad = false
	if err := e.AddWorkspacePolicy("custom", "ws", "manage"); err == nil {
		t.Fatal("mutation accepted with unknown durable state")
	}
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := e.AddWorkspacePolicy("custom", "ws", "read"); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentPolicyLifecycle(t *testing.T) {
	e, err := NewEnforcerWithDefaults(NewMemoryAdapter())
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(n int) {
			defer workers.Done()
			for i := 0; i < 60; i++ {
				switch n % 4 {
				case 0:
					if err := e.AddOperationPolicy("custom", "ws", "wms", "GetMap", "read"); err != nil {
						t.Error(err)
					}
				case 1:
					if err := e.RemoveOperationPolicy("custom", "ws", "wms", "GetMap", "read"); err != nil {
						t.Error(err)
					}
				case 2:
					if err := e.Reload(); err != nil {
						t.Error(err)
					}
					if err := e.DeleteRole(func() error { return errors.New("assigned") }); err == nil {
						t.Error("failed deletion accepted")
					}
				case 3:
					_, _ = e.CanAccessOperation("custom", "ws", "wms", "GetMap", "read")
					_, _ = e.GetAllPolicies()
				}
				allowed, err := e.CanManageWorkspace("viewer", "ws")
				if err != nil || allowed {
					t.Errorf("viewer gained management: %v %v", allowed, err)
				}
			}
		}(worker)
	}
	workers.Wait()
}
