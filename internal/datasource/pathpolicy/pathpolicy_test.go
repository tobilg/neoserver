package pathpolicy

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestAllowPath(t *testing.T) {
	allowed := []string{
		"./data/**",
		"data/**",
		"/srv/geo/**",
		"s3://my-bucket/**",
		"https://tiles.example.com/**",
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"relative allowed", "data/roads.parquet", false},
		{"relative dot allowed", "./data/sub/roads.parquet", false},
		{"absolute allowed", "/srv/geo/roads.gpkg", false},
		{"s3 allowed", "s3://my-bucket/layers/x.parquet", false},
		{"https allowed", "https://tiles.example.com/data/x.parquet", false},

		{"local not allowlisted", "/etc/passwd", true},
		{"other s3 bucket", "s3://other-bucket/x.parquet", true},
		{"other host", "https://evil.example.net/x", true},
		{"traversal rejected", "data/../etc/passwd", true},
		{"empty rejected", "", true},

		// SSRF-sensitive hosts are rejected even if the allowlist were broad.
		{"metadata ip", "https://169.254.169.254/latest/meta-data/", true},
		{"loopback", "http://127.0.0.1/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AllowPath(tt.path, allowed)
			if (err != nil) != tt.wantErr {
				t.Errorf("AllowPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.gpkg")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.gpkg")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	Configure([]string{filepath.Join(root, "**")})
	if _, err := Resolve(context.Background(), link); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}

func TestAllowPathDenyByDefault(t *testing.T) {
	// Empty allowlist denies everything.
	if err := AllowPath("data/x.parquet", nil); err == nil {
		t.Error("expected empty allowlist to deny all paths")
	}
}

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "::1", "169.254.169.254", "10.0.0.5", "192.168.1.1", "172.16.0.1", "0.0.0.0"}
	for _, ip := range blocked {
		if !isBlockedIP(net.ParseIP(ip)) {
			t.Errorf("expected %s to be blocked", ip)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34"}
	for _, ip := range allowed {
		if isBlockedIP(net.ParseIP(ip)) {
			t.Errorf("expected %s to be allowed", ip)
		}
	}
}

func TestSafeDialContextRejectsDNSToLoopback(t *testing.T) {
	// Defeats DNS-rebinding: a hostname resolving to a loopback/private address
	// must be refused at dial time even though the literal host is not an IP.
	// "localhost" resolves to 127.0.0.1 / ::1.
	_, err := safeDialContext(context.Background(), "tcp", "localhost:80")
	if err == nil {
		t.Fatal("expected safeDialContext to reject a host resolving to loopback")
	}
}

func TestExactRemoteAuthorityRejectsUserinfo(t *testing.T) {
	allowed := []string{"https://tiles.example.com/**"}
	u, _ := url.Parse("https://user:pass@tiles.example.com/x")
	if exactRemoteAuthorityAllowed(u, allowed) {
		t.Fatal("expected URL with embedded credentials to be rejected")
	}
}

func TestMetadataIPBlockedWithBroadAllowlist(t *testing.T) {
	// Even a wildcard allowlist must not permit link-local/metadata addresses.
	if err := AllowPath("http://169.254.169.254/latest/meta-data/", []string{"**"}); err == nil {
		t.Error("expected metadata IP to be blocked regardless of allowlist")
	}
	if err := AllowPath("http://10.0.0.5/internal", []string{"**"}); err == nil {
		t.Error("expected private IP to be blocked regardless of allowlist")
	}
}

func TestAllowPathNormalizesLocalForms(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		pattern, path string
		allowed       bool
	}{
		{"./rasters/**", "rasters/mosaic/tile_1.tif", true},
		{"./rasters/**", filepath.Join(cwd, "rasters", "a.tif"), true},
		{"rasters/**", "./rasters/a.tif", true},
		{filepath.Join(cwd, "rasters", "**"), "rasters/a.tif", true},
		{"./rasters/**", "other/a.tif", false},
		{"./rasters/**", filepath.Join(cwd, "other", "a.tif"), false},
		{"./rasters/*.tif", "rasters/deeper/a.tif", false},
	} {
		err := AllowPath(tc.path, []string{tc.pattern})
		if (err == nil) != tc.allowed {
			t.Errorf("AllowPath(%q, %q) = %v, allowed %v", tc.path, tc.pattern, err, tc.allowed)
		}
	}
}

func TestResolveAcceptsAbsolutePathForRelativePattern(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rasters"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "rasters", "tile.tif")
	if err := os.WriteFile(file, []byte("tif"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	Configure([]string{"./rasters/**"})
	defer Configure(nil)
	resolved, err := Resolve(context.Background(), file)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", file, err)
	}
	if want, _ := filepath.EvalSymlinks(file); resolved != want {
		t.Fatalf("resolved %q, want %q", resolved, want)
	}
}
