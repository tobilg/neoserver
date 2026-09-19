// Package pathpolicy enforces an allowlist over datasource file paths and URLs.
//
// Admin-supplied datasource paths (local files, http/s3 URLs) are otherwise a local
// file-read and SSRF vector: DuckDB's httpfs/filesystem access will happily read
// /etc/passwd or reach http://169.254.169.254/. This package applies a deny-by-default
// glob allowlist plus hard blocks on SSRF-sensitive hosts.
package pathpolicy

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

var (
	mu         sync.RWMutex
	configured []string
	options    Options
)

type Options struct {
	RemoteCachePath string
	RemoteMaxBytes  int64
	RemoteTimeout   time.Duration
}

// Configure sets the process-wide allowlist of glob patterns. Call once at startup.
func Configure(patterns []string) {
	_ = ConfigureWithOptions(patterns, Options{})
}

// ConfigureWithOptions validates and installs the process-wide datasource policy.
func ConfigureWithOptions(patterns []string, opts Options) error {
	for _, pattern := range patterns {
		if _, err := doublestar.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid allowlist pattern %q: %w", pattern, err)
		}
	}
	if opts.RemoteCachePath == "" {
		opts.RemoteCachePath = "./data/remote-cache"
	}
	if opts.RemoteMaxBytes <= 0 {
		opts.RemoteMaxBytes = 1 << 30
	}
	if opts.RemoteTimeout <= 0 {
		opts.RemoteTimeout = 60 * time.Second
	}
	mu.Lock()
	defer mu.Unlock()
	configured = append([]string(nil), patterns...)
	options = opts
	return nil
}

// Check validates path against the process-wide allowlist configured via Configure.
func Check(path string) error {
	mu.RLock()
	allowed := configured
	mu.RUnlock()
	return AllowPath(path, allowed)
}

// Resolve validates a datasource path and returns a canonical local filename.
// HTTPS resources are downloaded through the SSRF-safe fetcher before DuckDB
// sees them, preventing httpfs from bypassing network policy.
func Resolve(ctx context.Context, path string) (string, error) {
	mu.RLock()
	allowed := append([]string(nil), configured...)
	opts := options
	mu.RUnlock()
	if err := AllowPath(path, allowed); err != nil {
		return "", err
	}
	u, _ := url.Parse(path)
	if u != nil && strings.EqualFold(u.Scheme, "http") {
		return "", fmt.Errorf("remote datasource URLs must use HTTPS")
	}
	if u != nil && strings.EqualFold(u.Scheme, "https") {
		if !exactRemoteAuthorityAllowed(u, allowed) {
			return "", fmt.Errorf("HTTPS datasource host must be exactly allowlisted")
		}
		return fetchHTTPS(ctx, u, allowed, opts)
	}
	if isRemote(path) {
		if !exactRemoteAuthorityAllowed(u, allowed) {
			return "", fmt.Errorf("object storage bucket must be exactly allowlisted")
		}
		return path, nil
	}
	return canonicalLocalPath(path, allowed)
}

// AllowPath returns nil if path is permitted by the allowlist, otherwise an error.
// It is deny-by-default: path must match at least one glob pattern in allowed
// (doublestar syntax, so "**" spans path separators). Remote URLs are additionally
// checked against SSRF-sensitive hosts regardless of the allowlist.
//
// This static check only rejects literal internal IPs and localhost in the host;
// hostnames that resolve to internal addresses via DNS are not caught here. That
// gap is closed at connect time for http(s): Resolve funnels HTTPS through
// fetchHTTPS, whose safeDialContext re-resolves the host and refuses to dial any
// private/loopback/link-local address (also defeating DNS-rebinding). Object-store
// schemes (s3://, gs://, ...) are handed to DuckDB directly and are NOT routed
// through safeDialContext; they rely on the exact-authority allowlist plus the
// provider's fixed endpoint, so a custom object-store endpoint override must never
// be accepted without allowlisting.
func AllowPath(path string, allowed []string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %q", path)
	}

	if isRemote(path) {
		if err := checkRemoteHost(path); err != nil {
			return err
		}
	}

	for _, pat := range allowed {
		ok, err := matchPattern(pat, path)
		if err != nil {
			return fmt.Errorf("invalid allowlist pattern %q: %w", pat, err)
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("path %q is not permitted by the datasource allowlist", path)
}

// matchPattern matches a path against one allowlist pattern. Local paths and
// patterns are also compared in absolute, cleaned form, so "./data/**" permits
// "data/x.gpkg" and "/abs/cwd/data/x.gpkg" alike. Containment is still enforced
// separately on the resolved filesystem path.
func matchPattern(pattern, path string) (bool, error) {
	ok, err := doublestar.Match(pattern, path)
	if ok || err != nil || isRemote(pattern) || isRemote(path) {
		return ok, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, nil
	}
	absPattern, err := absolutePattern(pattern)
	if err != nil {
		return false, nil
	}
	return doublestar.Match(absPattern, absPath)
}

// absolutePattern anchors a relative glob at the working directory, escaping
// glob metacharacters that happen to appear in the directory name.
func absolutePattern(pattern string) (string, error) {
	if filepath.IsAbs(pattern) {
		return filepath.Clean(pattern), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	replacements := []string{"*", `\*`, "?", `\?`, "[", `\[`, "{", `\{`}
	if filepath.Separator != '\\' {
		replacements = append([]string{`\`, `\\`}, replacements...)
	}
	escaped := strings.NewReplacer(replacements...).Replace(filepath.Clean(cwd))
	return escaped + string(filepath.Separator) + filepath.Clean(pattern), nil
}

func isRemote(path string) bool {
	lower := strings.ToLower(path)
	for _, scheme := range []string{"http://", "https://", "s3://", "gs://", "gcs://", "azure://", "az://", "r2://"} {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

func checkRemoteHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	host := u.Hostname()
	if host == "" {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("access to localhost is not allowed")
	}
	// Only http(s) hosts are treated as network endpoints; for object-store schemes
	// (s3://bucket/...) the host is a bucket name, not a routable address.
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("access to private/link-local address %q is not allowed", host)
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified()
}

func exactRemoteAuthorityAllowed(target *url.URL, allowed []string) bool {
	if target == nil || target.Hostname() == "" || target.User != nil {
		return false
	}
	for _, pattern := range allowed {
		prefix := pattern
		if index := strings.IndexAny(prefix, "*?["); index >= 0 {
			prefix = prefix[:index]
		}
		u, err := url.Parse(prefix)
		if err == nil && strings.EqualFold(u.Scheme, target.Scheme) && strings.EqualFold(u.Hostname(), target.Hostname()) && u.Port() == target.Port() {
			return true
		}
	}
	return false
}

func canonicalLocalPath(path string, allowed []string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve datasource path: %w", err)
	}
	for _, pattern := range allowed {
		if isRemote(pattern) {
			continue
		}
		matched, _ := matchPattern(pattern, path)
		if !matched {
			continue
		}
		rootPattern := pattern
		if index := strings.IndexAny(rootPattern, "*?["); index >= 0 {
			rootPattern = rootPattern[:index]
		}
		rootPattern = strings.TrimRight(rootPattern, string(filepath.Separator))
		if rootPattern == "" {
			// A deliberately broad pattern such as "**" has the filesystem
			// root as its containment boundary.
			rootPattern = string(filepath.Separator)
		}
		rootAbs, err := filepath.Abs(rootPattern)
		if err != nil {
			continue
		}
		rootReal, err := filepath.EvalSymlinks(rootAbs)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootReal, real)
		if err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return real, nil
		}
	}
	return "", fmt.Errorf("canonical path %q escapes the datasource allowlist", real)
}
