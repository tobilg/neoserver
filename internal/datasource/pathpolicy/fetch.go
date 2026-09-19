package pathpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

func fetchHTTPS(ctx context.Context, target *url.URL, allowed []string, opts Options) (string, error) {
	cacheRoot, err := filepath.Abs(opts.RemoteCachePath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheRoot, 0700); err != nil {
		return "", fmt.Errorf("create remote cache: %w", err)
	}
	hash := sha256.Sum256([]byte(target.String()))
	name := hex.EncodeToString(hash[:])
	if ext := filepath.Ext(target.Path); safeExtension(ext) {
		name += strings.ToLower(ext)
	}
	destination, metadataPath := filepath.Join(cacheRoot, name), filepath.Join(cacheRoot, name+".json")
	metadata := cacheMetadata{}
	if raw, err := os.ReadFile(metadataPath); err == nil {
		_ = json.Unmarshal(raw, &metadata)
	}

	transport := &http.Transport{Proxy: nil, DialContext: safeDialContext}
	client := &http.Client{Transport: transport, Timeout: opts.RemoteTimeout}
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
		return "", fmt.Errorf("fetch remote datasource: %w", err)
	}
	defer response.Body.Close()
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

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
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
