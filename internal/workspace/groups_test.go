package workspace

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestLayerGroupCatalogIntegrity(t *testing.T) {
	ws := &Workspace{
		Services: map[string]*Service{
			"svc": {
				Enabled: true,
				Layers: map[string]*Layer{
					"roads": {PublicID: "roads", Enabled: false},
				},
				Coverages: map[string]*Coverage{},
			},
		},
		Groups: map[string]*LayerGroup{
			"base":  {ID: "base-id", PublicID: "base", Enabled: true, Members: []store.LayerGroupMember{{Resource: "roads"}}},
			"outer": {ID: "outer-id", PublicID: "outer", Enabled: true, Members: []store.LayerGroupMember{{Resource: "base"}}},
		},
		Styles: map[string]*Style{},
	}

	if !ws.HasPublishedResourceID("roads") || !ws.HasPublishedResourceID("base") {
		t.Fatal("disabled resources and groups must reserve their public identifiers")
	}
	if references := ws.GroupReferences("roads"); len(references) != 1 || references[0] != "base" {
		t.Fatalf("unexpected references: %v", references)
	}

	rename := &LayerGroup{PublicID: "renamed", Enabled: true, Members: []store.LayerGroupMember{{Resource: "roads"}}}
	if err := validateLayerGroup(ws, rename, "base", 8); err == nil || !strings.Contains(err.Error(), "referenced") {
		t.Fatalf("expected referenced rename rejection, got %v", err)
	}
}

func TestLayerGroupRejectsCycles(t *testing.T) {
	ws := &Workspace{
		Services: map[string]*Service{},
		Styles:   map[string]*Style{},
		Groups: map[string]*LayerGroup{
			"first":  {PublicID: "first", Enabled: true, Members: []store.LayerGroupMember{{Resource: "second"}}},
			"second": {PublicID: "second", Enabled: true, Members: []store.LayerGroupMember{{Resource: "first"}}},
		},
	}
	candidate := &LayerGroup{PublicID: "first", Enabled: true, Members: []store.LayerGroupMember{{Resource: "second"}}}
	if err := validateLayerGroup(ws, candidate, "first", 8); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle rejection, got %v", err)
	}
}
