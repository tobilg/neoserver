package main

import (
	"context"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestTokenOptions(t *testing.T) {
	for _, value := range []string{"", "0", "0d", "-1h", "-1d", "7days", "9999999999999999999d", "wat"} {
		if _, err := tokenDuration(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"24h", "7d", "30m"} {
		if _, err := tokenDuration(value); err != nil {
			t.Error(err)
		}
	}
	if validTokenRole("owner") {
		t.Fatal("accepted unknown role")
	}
}

func TestTokenScope(t *testing.T) {
	if err := validTokenScope("super_admin", ""); err != nil {
		t.Fatal(err)
	}
	if err := validTokenScope("editor", "demo"); err != nil {
		t.Fatal(err)
	}
	if validTokenScope("super_admin", "demo") == nil {
		t.Fatal("accepted a workspace-scoped super_admin token")
	}
	if validTokenScope("editor", "") == nil {
		t.Fatal("accepted an editor token without a workspace")
	}
}

type fakeWorkspaces map[string]*store.Workspace

func (f fakeWorkspaces) GetWorkspace(_ context.Context, id string) (*store.Workspace, error) {
	for _, ws := range f {
		if ws.ID == id {
			return ws, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f fakeWorkspaces) GetWorkspaceByName(_ context.Context, name string) (*store.Workspace, error) {
	if ws, ok := f[name]; ok {
		return ws, nil
	}
	return nil, store.ErrNotFound
}

func TestResolveTokenWorkspace(t *testing.T) {
	workspaces := fakeWorkspaces{"demo": {ID: "ws-1", Name: "demo"}}
	for _, identifier := range []string{"ws-1", "demo"} {
		ws, err := resolveTokenWorkspace(context.Background(), workspaces, identifier)
		if err != nil || ws.ID != "ws-1" {
			t.Fatalf("resolve %q: %+v %v", identifier, ws, err)
		}
	}
	if _, err := resolveTokenWorkspace(context.Background(), workspaces, "missing"); err == nil {
		t.Fatal("resolved a missing workspace")
	}
}
