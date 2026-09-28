package pathpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type cacheMetadata struct {
	ETag         string `json:"etag"`
	LastModified string `json:"last_modified"`
}

// remoteTransport builds the HTTPS transport; tests substitute a loopback server.
var remoteTransport = func() http.RoundTripper {
	return &http.Transport{Proxy: nil, DialContext: SafeDialContext}
}

// cacheLocation names the cached download for target inside the cache root.
func cacheLocation(target *url.URL, opts Options) (string, error) {
	cacheRoot, err := filepath.Abs(opts.RemoteCachePath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(target.String()))
	name := hex.EncodeToString(hash[:])
	if ext := filepath.Ext(target.Path); safeExtension(ext) {
		name += strings.ToLower(ext)
	}
	return filepath.Join(cacheRoot, name), nil
}

// fetchHTTPS refreshes destination with a conditional GET. When the origin is
// unreachable or fails with a server error, an existing copy is served stale.
func fetchHTTPS(ctx context.Context, target *url.URL, destination string, allowed []string, opts Options) (string, error) {
	cacheRoot, metadataPath := filepath.Dir(destination), destination+".json"
	if err := os.MkdirAll(cacheRoot, 0700); err != nil {
		return "", fmt.Errorf("create remote cache: %w", err)
	}
	metadata := cacheMetadata{}
	if raw, err := os.ReadFile(metadataPath); err == nil {
		_ = json.Unmarshal(raw, &metadata)
	}

	client := &http.Client{Transport: remoteTransport(), Timeout: opts.RemoteTimeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "https" || !exactRemoteAuthorityAllowed(req.URL, allowed) {
			return fmt.Errorf("redirect target is not allowlisted")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", err
	}
	if metadata.ETag != "" {
		req.Header.Set("If-None-Match", metadata.ETag)
	}
	if metadata.LastModified != "" {
		req.Header.Set("If-Modified-Since", metadata.LastModified)
	}
	response, err := client.Do(req)
	if err != nil {
		if serveStale(destination, target, err.Error()) {
			return destination, nil
		}
		return "", fmt.Errorf("fetch remote datasource: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusInternalServerError && serveStale(destination, target, response.Status) {
		return destination, nil
	}
	if response.StatusCode == http.StatusNotModified {
		if _, err := os.Stat(destination); err == nil {
			return destination, nil
		}
		return "", fmt.Errorf("remote cache returned 304 without cached content")
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("remote datasource returned HTTP %d", response.StatusCode)
	}
	temp, err := os.CreateTemp(cacheRoot, ".download-*")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return "", err
	}
	written, copyErr := io.Copy(temp, io.LimitReader(response.Body, opts.RemoteMaxBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > opts.RemoteMaxBytes {
		return "", fmt.Errorf("remote datasource exceeds %d byte limit", opts.RemoteMaxBytes)
	}
	if err := os.Rename(tempName, destination); err != nil {
		return "", fmt.Errorf("commit remote cache: %w", err)
	}
	metadata = cacheMetadata{ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified")}
	if raw, err := json.Marshal(metadata); err == nil {
		metaTemp := metadataPath + ".tmp"
		if os.WriteFile(metaTemp, raw, 0600) == nil {
			_ = os.Rename(metaTemp, metadataPath)
		}
	}
	return destination, nil
}

func safeExtension(extension string) bool {
	if len(extension) < 2 || len(extension) > 16 || extension[0] != '.' {
		return false
	}
	for i := 1; i < len(extension); i++ {
		c := extension[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// serveStale reports whether a previously downloaded copy can stand in for an
// origin that is unavailable.
func serveStale(destination string, target *url.URL, reason string) bool {
	if info, err := os.Stat(destination); err != nil || !info.Mode().IsRegular() {
		return false
	}
	slog.Warn("remote datasource unavailable; serving cached copy", "host", target.Host, "path", target.Path, "reason", reason)
	return true
}

// SafeDialContext resolves the host itself and refuses to connect when any of
// its addresses is non-public, which also defeats DNS rebinding. Use it for
// every outbound fetch of an operator- or tenant-supplied URL.
func SafeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("host has no addresses")
	}
	for _, address := range addresses {
		if isBlockedIP(address.IP) {
			return nil, fmt.Errorf("access to private/link-local address %q is not allowed", host)
		}
	}
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, net.JoinHostPort(strings.Trim(addresses[0].IP.String(), "[]"), port))
}
