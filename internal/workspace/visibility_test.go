package workspace

import "testing"

func TestLayerVisibleToRole(t *testing.T) {
	tests := []struct {
		name         string
		public       bool
		allowedRoles []string
		role         string
		want         bool
	}{
		{name: "unrestricted visible to viewer", allowedRoles: nil, role: "viewer", want: true},
		{name: "unrestricted visible to anonymous", allowedRoles: nil, role: "", want: true},
		{name: "public overrides restriction for anonymous", public: true, allowedRoles: []string{"admin"}, role: "", want: true},
		{name: "restricted hidden from viewer", allowedRoles: []string{"admin"}, role: "viewer", want: false},
		{name: "restricted hidden from anonymous", allowedRoles: []string{"admin"}, role: "", want: false},
		{name: "restricted visible to listed role", allowedRoles: []string{"admin", "editor"}, role: "editor", want: true},
		{name: "super_admin always visible", allowedRoles: []string{"admin"}, role: "super_admin", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &Layer{Public: tt.public, AllowedRoles: tt.allowedRoles}
			if got := l.VisibleToRole(tt.role); got != tt.want {
				t.Fatalf("VisibleToRole(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestLayerVisibleToRoleNil(t *testing.T) {
	var l *Layer
	if l.VisibleToRole("admin") {
		t.Fatal("nil layer should not be visible")
	}
}

func TestWorkspaceVisibleLayers(t *testing.T) {
	ws := &Workspace{ID: "ws1", Services: map[string]*Service{}}
	svc := &Service{ID: "svc1", Enabled: true, Layers: map[string]*Layer{}}
	open := &Layer{PublicID: "open", Enabled: true}
	alpha := &Layer{PublicID: "alpha", Enabled: true}
	restricted := &Layer{PublicID: "restricted", Enabled: true, AllowedRoles: []string{"admin"}}
	svc.AddLayer(open)
	svc.AddLayer(alpha)
	svc.AddLayer(restricted)
	ws.Services[svc.ID] = svc

	viewerVisible := ws.VisibleLayers("viewer")
	if len(viewerVisible) != 2 || viewerVisible[0].PublicID != "alpha" || viewerVisible[1].PublicID != "open" {
		t.Fatalf("viewer layers are not stable and sorted: %#v", viewerVisible)
	}

	adminVisible := ws.VisibleLayers("admin")
	if len(adminVisible) != 3 || adminVisible[0].PublicID != "alpha" || adminVisible[1].PublicID != "open" || adminVisible[2].PublicID != "restricted" {
		t.Fatalf("admin layers are not stable and sorted: %#v", adminVisible)
	}
}
