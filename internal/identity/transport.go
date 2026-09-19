package identity

import (
	"context"
	"fmt"
	"github.com/tobilg/neoserver/internal/httputil"
	"net"
	"net/http"
	"strings"
)

type transportContextKey struct{}

// IsSecureTransport reports the effective transport decision made by TransportSecurity.
func IsSecureTransport(ctx context.Context) bool {
	secure, _ := ctx.Value(transportContextKey{}).(bool)
	return secure
}

// TransportConfig controls trusted-proxy handling and plaintext credential rejection.
type TransportConfig struct {
	RequireHTTPS       bool
	TrustedProxyCIDRs  []string
	AllowAPIKeyInQuery bool
}

// TransportSecurity returns middleware that determines the effective transport
// without trusting client-controlled forwarding headers. Direct loopback HTTP is
// treated as secure to preserve a deliberate local development workflow.
func TransportSecurity(cfg TransportConfig) (func(http.Handler) http.Handler, error) {
	trusted, err := parseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := remoteIP(r.RemoteAddr)
			peerTrusted := peer != nil && (peer.IsLoopback() || ipInNetworks(peer, trusted))
			secure := r.TLS != nil || (peer != nil && peer.IsLoopback())

			if peerTrusted {
				proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
				secure = secure || strings.EqualFold(proto, "https")
				if client := forwardedClientIP(r.Header.Get("X-Forwarded-For"), peer, trusted); client != nil {
					r.RemoteAddr = client.String()
				}
			}

			ctx := context.WithValue(r.Context(), transportContextKey{}, secure)
			r = r.WithContext(ctx)
			if cfg.RequireHTTPS && !secure && requestHasCredentials(r, cfg.AllowAPIKeyInQuery) {
				httpsRequired(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// RequireSecureTransport rejects non-loopback plaintext requests when enabled.
func RequireSecureTransport(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secure := IsSecureTransport(r.Context())
			if enabled && !secure {
				httpsRequired(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func httpsRequired(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Upgrade", "TLS/1.2, HTTP/1.1")
	httputil.HTTPError(w, r, "HTTPS required", http.StatusUpgradeRequired)
}

func requestHasCredentials(r *http.Request, allowQuery bool) bool {
	if r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" {
		return true
	}
	return allowQuery && r.URL.Query().Get("apikey") != ""
}

func parseTrustedProxyCIDRs(values []string) ([]*net.IPNet, error) {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", value, err)
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func remoteIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func forwardedClientIP(header string, peer net.IP, trusted []*net.IPNet) net.IP {
	if header == "" {
		return nil
	}
	chain := strings.Split(header, ",")
	current := peer
	for i := len(chain) - 1; i >= 0; i-- {
		if current == nil || (!current.IsLoopback() && !ipInNetworks(current, trusted)) {
			break
		}
		next := net.ParseIP(strings.TrimSpace(chain[i]))
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}
