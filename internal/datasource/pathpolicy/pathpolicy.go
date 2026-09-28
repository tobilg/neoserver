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
	"net/netip"
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
	// RemoteCacheMaxBytes bounds all cached downloads; zero disables the quota.
	RemoteCacheMaxBytes int64
	// RemoteCacheMaxAge evicts downloads unused for longer; zero disables it.
	RemoteCacheMaxAge time.Duration
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

// Acquire validates a datasource path and leases a canonical local filename.
// HTTPS resources are downloaded through the SSRF-safe fetcher before DuckDB
// sees them, preventing httpfs from bypassing network policy. Hold the lease
// for as long as the path may be opened, and Release it afterwards.
func Acquire(ctx context.Context, path string) (*Lease, error) {
	mu.RLock()
	allowed := append([]string(nil), configured...)
	opts := options
	mu.RUnlock()
	if err := AllowPath(path, allowed); err != nil {
		return nil, err
	}
	u, _ := url.Parse(path)
	if u != nil && strings.EqualFold(u.Scheme, "http") {
		return nil, fmt.Errorf("remote datasource URLs must use HTTPS")
	}
	if u != nil && strings.EqualFold(u.Scheme, "https") {
		if !exactRemoteAuthorityAllowed(u, allowed) {
			return nil, fmt.Errorf("HTTPS datasource host must be exactly allowlisted")
		}
		destination, err := cacheLocation(u, opts)
		if err != nil {
			return nil, err
		}
		// Pin before downloading so a concurrent quota pass cannot evict it.
		pin(destination)
		if _, err := fetchHTTPS(ctx, u, destination, allowed, opts); err != nil {
			unpin(destination)
			return nil, err
		}
		now := time.Now()
		_ = os.Chtimes(destination, now, now)
		enforceRemoteCacheQuota(opts, now)
		return &Lease{Path: destination, Cached: true}, nil
	}
	if IsRemote(path) {
		if !exactRemoteAuthorityAllowed(u, allowed) {
			return nil, fmt.Errorf("object storage bucket must be exactly allowlisted")
		}
		return &Lease{Path: path}, nil
	}
	resolved, err := canonicalLocalPath(path, allowed)
	if err != nil {
		return nil, err
	}
	return &Lease{Path: resolved}, nil
}

// AllowPath returns nil if path is permitted by the allowlist, otherwise an error.
// It is deny-by-default: path must match at least one glob pattern in allowed
// (doublestar syntax, so "**" spans path separators). Remote URLs are additionally
// checked against SSRF-sensitive hosts regardless of the allowlist.
//
// This static check only rejects literal internal IPs and localhost in the host;
// hostnames that resolve to internal addresses via DNS are not caught here. That
// gap is closed at connect time for http(s): Acquire funnels HTTPS through
// fetchHTTPS, whose SafeDialContext re-resolves the host and refuses to dial any
// private/loopback/link-local address (also defeating DNS-rebinding). Object-store
// schemes (s3://, gs://, ...) are handed to DuckDB directly and are NOT routed
// through SafeDialContext; they rely on the exact-authority allowlist plus the
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

	if IsRemote(path) {
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
	if ok || err != nil || IsRemote(pattern) || IsRemote(path) {
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

// IsRemote reports whether path names a network or object-store resource.
func IsRemote(path string) bool {
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

// blockedPrefixes are non-public ranges that net.IP's classifiers do not cover.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT, including some cloud metadata services
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, including broadcast
	netip.MustParsePrefix("::/96"),           // deprecated IPv4-compatible
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("2001::/32"),       // Teredo
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("fec0::/10"),       // deprecated site-local
}

// Translation prefixes carry an IPv4 destination that is checked in turn:
// NAT64 is how IPv6-only networks reach every IPv4 host, so the prefix itself
// cannot be blocked.
var nat64Prefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

var sixToFourPrefix = netip.MustParsePrefix("2002::/16")

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	if embedded, ok := embeddedIPv4(addr); ok {
		return isBlockedIP(net.IP(embedded.AsSlice()))
	}
	return false
}

// embeddedIPv4 extracts the IPv4 destination of a NAT64 or 6to4 address.
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	bytes := addr.As16()
	for _, prefix := range nat64Prefixes {
		if prefix.Contains(addr) {
			return netip.AddrFrom4([4]byte(bytes[12:16])), true
		}
	}
	if sixToFourPrefix.Contains(addr) {
		return netip.AddrFrom4([4]byte(bytes[2:6])), true
	}
	return netip.Addr{}, false
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
		if IsRemote(pattern) {
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
