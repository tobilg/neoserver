package stylegraphics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
)

func TestManagedAndSVGGraphics(t *testing.T) {
	root := t.TempDir()
	workspace := "workspace"
	if err := os.MkdirAll(filepath.Join(root, workspace), 0o750); err != nil {
		t.Fatal(err)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><circle cx="8" cy="8" r="6" fill="#ff0000"/></svg>`)
	if err := os.WriteFile(filepath.Join(root, workspace, "marker.svg"), svg, 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := New(conf.WMS{StyleAssetPath: root, MaxStyleAssetBytes: 1024, MaxExternalGraphicDimension: 64}, workspace, false)
	image, err := resolver.Resolve("asset:marker.svg", "image/svg+xml")
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != 16 || image.Bounds().Dy() != 16 {
		t.Fatalf("bounds=%v", image.Bounds())
	}
	if _, err = resolver.Resolve("../marker.svg", "image/svg+xml"); err == nil {
		t.Fatal("unsafe reference accepted")
	}
}

func TestRemoteGraphicsAreDenyByDefault(t *testing.T) {
	resolver := New(conf.WMS{}, "workspace", false)
	if _, err := resolver.Resolve("https://example.com/marker.png", "image/png"); err == nil {
		t.Fatal("remote graphic accepted")
	}
}
