package stylegraphics

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
)

func tinyPNG(t *testing.T, size int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, size, size))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestValidateRemoteURL(t *testing.T) {
	cfg := conf.WMS{ExternalGraphicAllowedOrigins: []string{"https://tiles.example.com", "https://Other.Example.com/"}}
	tests := []struct {
		name        string
		href        string
		allowRemote bool
		wantErr     string
	}{
		{"deny by default", "https://tiles.example.com/x.png", false, "disabled"},
		{"allowlisted origin", "https://tiles.example.com/marker.png", true, ""},
		{"case insensitive host", "https://TILES.EXAMPLE.COM/m.png", true, ""},
		{"trailing slash in allowlist entry", "https://other.example.com/m.png", true, ""},
		{"http rejected", "http://tiles.example.com/m.png", true, "HTTPS"},
		{"credentials rejected", "https://user:pw@tiles.example.com/m.png", true, "credentials"},
		{"unlisted origin", "https://evil.example.com/m.png", true, "not allowlisted"},
		{"port changes origin", "https://tiles.example.com:8443/m.png", true, "not allowlisted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(cfg, "ws1", tt.allowRemote)
			parsed, err := url.Parse(tt.href)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			err = r.validateRemoteURL(parsed)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRedirectLimitAndRevalidation(t *testing.T) {
	r := New(conf.WMS{ExternalGraphicAllowedOrigins: []string{"https://ok.example.com"}}, "ws1", true)

	// Each redirect hop is re-validated against the allowlist.
	hop, _ := http.NewRequest(http.MethodGet, "https://evil.example.com/x.png", nil)
	if err := r.client.CheckRedirect(hop, nil); err == nil {
		t.Fatal("redirect to non-allowlisted origin must be rejected")
	}
	good, _ := http.NewRequest(http.MethodGet, "https://ok.example.com/x.png", nil)
	if err := r.client.CheckRedirect(good, nil); err != nil {
		t.Fatalf("allowlisted redirect rejected: %v", err)
	}

	// The redirect chain is capped at five hops.
	via := make([]*http.Request, 5)
	for i := range via {
		via[i] = good
	}
	if err := r.client.CheckRedirect(good, via); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("expected redirect cap error, got %v", err)
	}
}

func TestReadRemoteFetchLimitsAndCache(t *testing.T) {
	pngBody := tinyPNG(t, 4)
	var status int
	var payload []byte
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	cacheDir := t.TempDir()
	cfg := conf.WMS{
		ExternalGraphicAllowedOrigins: []string{ts.URL},
		ExternalGraphicCachePath:      cacheDir,
	}

	t.Run("success and disk cache reuse", func(t *testing.T) {
		status, payload = http.StatusOK, pngBody
		r := New(cfg, "ws1", true)
		r.client = ts.Client()
		href := ts.URL + "/marker.png"
		body, err := r.readRemote(href)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if !bytes.Equal(body, pngBody) {
			t.Fatal("fetched body mismatch")
		}
		// Second read must come from the disk cache: break the origin.
		status = http.StatusInternalServerError
		body, err = r.readRemote(href)
		if err != nil {
			t.Fatalf("cached fetch: %v", err)
		}
		if !bytes.Equal(body, pngBody) {
			t.Fatal("cached body mismatch")
		}
	})

	t.Run("non-2xx rejected", func(t *testing.T) {
		status, payload = http.StatusNotFound, nil
		r := New(cfg, "ws1", true)
		r.client = ts.Client()
		if _, err := r.readRemote(ts.URL + "/missing.png"); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
			t.Fatalf("expected HTTP 404 error, got %v", err)
		}
	})

	t.Run("byte limit enforced", func(t *testing.T) {
		status, payload = http.StatusOK, bytes.Repeat([]byte{0xAB}, 64)
		limited := cfg
		limited.MaxStyleAssetBytes = 16
		r := New(limited, "ws1", true)
		r.client = ts.Client()
		if _, err := r.readRemote(ts.URL + "/big.png"); err == nil || !strings.Contains(err.Error(), "byte limit") {
			t.Fatalf("expected byte limit error, got %v", err)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		r := New(cfg, "ws1", true)
		if _, err := r.readRemote("https://%zz-invalid"); err == nil {
			t.Fatal("expected invalid URL error")
		}
	})
}

func TestReadDataURL(t *testing.T) {
	r := New(conf.WMS{MaxStyleAssetBytes: 32}, "ws1", false)

	t.Run("base64", func(t *testing.T) {
		payload := base64.StdEncoding.EncodeToString([]byte("hello"))
		body, format, err := r.readDataURL("data:image/png;base64," + payload)
		if err != nil {
			t.Fatalf("readDataURL: %v", err)
		}
		if string(body) != "hello" || format != "image/png" {
			t.Fatalf("body=%q format=%q", body, format)
		}
	})

	t.Run("percent encoded", func(t *testing.T) {
		body, format, err := r.readDataURL("data:image/svg+xml,%3Csvg%3E")
		if err != nil {
			t.Fatalf("readDataURL: %v", err)
		}
		if string(body) != "<svg>" || format != "image/svg+xml" {
			t.Fatalf("body=%q format=%q", body, format)
		}
	})

	t.Run("missing comma", func(t *testing.T) {
		if _, _, err := r.readDataURL("data:image/pngbase64AAAA"); err == nil {
			t.Fatal("expected error for missing comma")
		}
	})

	t.Run("bad base64", func(t *testing.T) {
		if _, _, err := r.readDataURL("data:image/png;base64,!!!!"); err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})

	t.Run("over byte limit", func(t *testing.T) {
		big := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 64))
		if _, _, err := r.readDataURL("data:image/png;base64," + big); err == nil || !strings.Contains(err.Error(), "byte limit") {
			t.Fatalf("expected byte limit error, got %v", err)
		}
	})
}

func TestMaxExternalGraphicsPerRender(t *testing.T) {
	payload := "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG(t, 2))
	r := New(conf.WMS{MaxExternalGraphicsPerRender: 2}, "ws1", false)
	for i := 0; i < 2; i++ {
		if _, err := r.Resolve(payload, ""); err != nil {
			t.Fatalf("resolve %d: %v", i+1, err)
		}
	}
	if _, err := r.Resolve(payload, ""); err == nil || !strings.Contains(err.Error(), "count exceeds") {
		t.Fatalf("expected per-render count error, got %v", err)
	}
}

func TestResolveRejectsUnknownScheme(t *testing.T) {
	r := New(conf.WMS{}, "ws1", false)
	for _, href := range []string{"http://x/y.png", "file:///etc/passwd", "ftp://x/y.png", "y.png"} {
		if _, err := r.Resolve(href, ""); err == nil {
			t.Errorf("Resolve(%q) should fail", href)
		}
	}
}

func TestDecodeDimensionBounds(t *testing.T) {
	r := New(conf.WMS{MaxExternalGraphicDimension: 3}, "ws1", false)
	if _, err := r.decode(tinyPNG(t, 2), ""); err != nil {
		t.Fatalf("small image rejected: %v", err)
	}
	if _, err := r.decode(tinyPNG(t, 8), ""); err == nil || !strings.Contains(err.Error(), "dimensions exceed") {
		t.Fatalf("expected dimension error, got %v", err)
	}
	if _, err := r.decode([]byte("not an image"), ""); err == nil {
		t.Fatal("expected decode error for garbage input")
	}
}

func TestRenderSVGElements(t *testing.T) {
	elements := []struct {
		name string
		body string
	}{
		{"circle", `<circle cx="8" cy="8" r="4" fill="#ff0000"/>`},
		{"ellipse", `<ellipse cx="8" cy="8" rx="6" ry="3" fill="#00ff00"/>`},
		{"rect", `<rect x="2" y="2" width="10" height="8" fill="none" stroke="#00f"/>`},
		{"line", `<line x1="0" y1="0" x2="16" y2="16" stroke="#000" stroke-width="2"/>`},
		{"polygon", `<polygon points="2,2 14,2 8,14" fill="#123456" stroke="#000"/>`},
		{"polyline", `<polyline points="2 2, 8 14, 14 2" fill="none" stroke="#333"/>`},
		{"styled via style attr", `<rect x="1" y="1" width="4" height="4" style="fill:#abc;stroke:#def"/>`},
	}
	for _, tt := range elements {
		t.Run(tt.name, func(t *testing.T) {
			svg := `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16">` + tt.body + `</svg>`
			img, err := renderSVG([]byte(svg), 64)
			if err != nil {
				t.Fatalf("renderSVG: %v", err)
			}
			if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
				t.Errorf("bounds = %v, want 16x16", img.Bounds())
			}
		})
	}
}

func TestRenderSVGRejections(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"path element", `<svg width="8" height="8"><path d="M0 0L8 8"/></svg>`, "path"},
		{"script element", `<svg width="8" height="8"><script>alert(1)</script></svg>`, "unsupported"},
		{"image element", `<svg width="8" height="8"><image href="https://x/y.png"/></svg>`, "unsupported"},
		{"not svg", `<div>hi</div>`, "invalid SVG"},
		{"broken xml", `<svg width="8"`, "invalid SVG"},
		{"dimension cap", `<svg width="500" height="500"><circle cx="1" cy="1" r="1"/></svg>`, "exceed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := renderSVG([]byte(tt.body), 64)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestRenderSVGViewBoxScaling(t *testing.T) {
	svg := `<svg width="32" height="32" viewBox="0 0 8 8"><rect x="0" y="0" width="8" height="8" fill="#fff"/></svg>`
	img, err := renderSVG([]byte(svg), 64)
	if err != nil {
		t.Fatalf("renderSVG: %v", err)
	}
	if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
		t.Errorf("bounds = %v, want 32x32", img.Bounds())
	}

	// Without explicit width/height the viewBox supplies the dimensions.
	svg = `<svg viewBox="0 0 24 12"><rect width="24" height="12"/></svg>`
	img, err = renderSVG([]byte(svg), 64)
	if err != nil {
		t.Fatalf("renderSVG viewBox only: %v", err)
	}
	if img.Bounds().Dx() != 24 || img.Bounds().Dy() != 12 {
		t.Errorf("bounds = %v, want 24x12", img.Bounds())
	}

	// Neither dimension nor viewBox falls back to 64x64.
	img, err = renderSVG([]byte(`<svg><circle cx="1" cy="1" r="1"/></svg>`), 0)
	if err != nil {
		t.Fatalf("renderSVG default size: %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Errorf("bounds = %v, want 64x64 default", img.Bounds())
	}
}

func TestParseSVGColor(t *testing.T) {
	tests := []struct {
		in      string
		isBlack bool
	}{
		{"#ff0000", false},
		{"#f00", false},
		{" #00ff00 ", false},
		{"red", true},     // named colors fall back to black
		{"#zzzzzz", true}, // invalid hex falls back to black
		{"", true},
	}
	for _, tt := range tests {
		c := parseSVGColor(tt.in)
		r, g, b, _ := c.RGBA()
		black := r == 0 && g == 0 && b == 0
		if black != tt.isBlack {
			t.Errorf("parseSVGColor(%q) black=%t, want %t", tt.in, black, tt.isBlack)
		}
	}
}

func TestReadAssetNameValidationAndBounds(t *testing.T) {
	root := t.TempDir()
	assetDir := filepath.Join(root, "ws1")
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "big.png"), bytes.Repeat([]byte{1}, 64), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := conf.WMS{StyleAssetPath: root, MaxStyleAssetBytes: 16}
	r := New(cfg, "ws1", false)

	for _, name := range []string{"../escape.png", "a/b.png", `a\b.png`, ""} {
		if _, err := r.readAsset(name); err == nil {
			t.Errorf("readAsset(%q) should fail", name)
		}
	}
	if _, err := r.readAsset("big.png"); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("expected byte limit error, got %v", err)
	}
	if _, err := r.readAsset("missing.png"); err == nil {
		t.Fatal("expected error for missing asset")
	}
}

func TestValidateSVGWrapper(t *testing.T) {
	if err := ValidateSVG([]byte(`<svg width="8" height="8"><circle cx="4" cy="4" r="2"/></svg>`), 16); err != nil {
		t.Fatalf("valid SVG rejected: %v", err)
	}
	if err := ValidateSVG([]byte(`<svg width="8" height="8"><path d="M0 0"/></svg>`), 16); err == nil {
		t.Fatal("path SVG must be rejected")
	}
}
