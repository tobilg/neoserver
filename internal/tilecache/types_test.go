package tilecache

import (
	"strings"
	"testing"
)

func TestIdentityValidate(t *testing.T) {
	valid := testIdentity(1)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Identity)
	}{
		{"missing workspace", func(i *Identity) { i.WorkspaceID = "" }},
		{"zero revision", func(i *Identity) { i.WorkspaceRevision = 0 }},
		{"missing resource", func(i *Identity) { i.ResourceID = "" }},
		{"zero generation", func(i *Identity) { i.Generation = 0 }},
		{"missing matrix set", func(i *Identity) { i.MatrixSet = "" }},
		{"negative zoom", func(i *Identity) { i.Zoom = -1 }},
		{"negative column", func(i *Identity) { i.Column = -1 }},
		{"negative row", func(i *Identity) { i.Row = -1 }},
		{"bad tile type", func(i *Identity) { i.TileType = "raster" }},
		{"bad format", func(i *Identity) { i.Format = "image/tiff" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := testIdentity(1)
			tt.mutate(&identity)
			if err := identity.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestCanonicalKeyDistinguishesIdentities(t *testing.T) {
	base := testIdentity(1)
	variants := []func(*Identity){
		func(i *Identity) { i.Column = 2 },
		func(i *Identity) { i.WorkspaceRevision = 2 },
		func(i *Identity) { i.Generation = 2 },
		func(i *Identity) { i.StyleDigest = "other" },
		func(i *Identity) { i.Format = "image/webp" },
		func(i *Identity) { i.TileType = "vector" },
	}
	seen := map[string]bool{base.CanonicalKey(): true}
	for i, mutate := range variants {
		identity := testIdentity(1)
		mutate(&identity)
		key := identity.CanonicalKey()
		if seen[key] {
			t.Errorf("variant %d collides with an earlier canonical key", i)
		}
		seen[key] = true
	}
}

func TestObjectKeyLayoutAndSanitization(t *testing.T) {
	identity := testIdentity(3)
	key := identity.ObjectKey("neoserver-tiles")
	if !strings.HasPrefix(key, "neoserver-tiles/v1/workspace/w1/layer/1/map/WebMercatorQuad/2/1/3/default/") {
		t.Fatalf("unexpected key layout: %s", key)
	}
	if !strings.HasSuffix(key, ".png") {
		t.Fatalf("expected png extension: %s", key)
	}

	// Prefix slashes are trimmed; empty style becomes "default".
	identity.StyleDigest = ""
	key = identity.ObjectKey("/pre/")
	if !strings.HasPrefix(key, "pre/v1/") || !strings.Contains(key, "/default/") {
		t.Fatalf("prefix/style handling wrong: %s", key)
	}

	// Hostile identity parts cannot escape the prefix.
	identity.WorkspaceID = "../../../etc"
	identity.ResourceID = `lay/er\x`
	key = identity.ObjectKey("p")
	if strings.Contains(key, "..") {
		t.Fatalf("object key contains dot-dot: %s", key)
	}
	if strings.Contains(key, `\`) {
		t.Fatalf("object key contains backslash: %s", key)
	}
}

func TestSanitizePathPart(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"..", "_"},
		{"a..b", "a_b"},
		{"....", "__"},
		{"a/b", "a_b"},
		{`a\b`, "a_b"},
		{"a\x00b", "a_b"},
		{"  spaced  ", "spaced"},
	}
	for _, tt := range tests {
		if got := sanitizePathPart(tt.in); got != tt.want {
			t.Errorf("sanitizePathPart(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtensionForFormat(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"image/png", "png"},
		{"PNG", "png"},
		{"image/jpeg", "jpg"},
		{"jpg", "jpg"},
		{"image/webp", "webp"},
		{"application/vnd.mapbox-vector-tile", "mvt"},
		{"pbf", "mvt"},
		{" mvt ", "mvt"},
		{"image/tiff", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := extensionForFormat(tt.in); got != tt.want {
			t.Errorf("extensionForFormat(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
