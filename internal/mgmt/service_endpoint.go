package mgmt

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

// postgisEndpoint is the network target of a PostGIS connection.
type postgisEndpoint struct {
	host string
	port int
}

func parsePostGISEndpoint(raw json.RawMessage) (postgisEndpoint, bool) {
	var info struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &info) != nil {
		return postgisEndpoint{}, false
	}
	port := info.Port
	if port == 0 {
		port = 5432
	}
	return postgisEndpoint{host: strings.ToLower(strings.Trim(strings.TrimSpace(info.Host), "[]")), port: port}, true
}

// authorizeServiceEndpoint keeps workspace administrators from pointing a
// PostGIS service, or a connection test, at an arbitrary network address.
// Only super_admin may choose a new endpoint. Others may keep the endpoint the
// service already has, or use one listed in Datasource.DatabaseHosts. File and
// remote sources are governed by Datasource.AllowedPaths instead.
//
// previous is the stored connection info, or nil for a new service. It writes
// a 403 and returns false when the caller may not use the endpoint.
func (h *handler) authorizeServiceEndpoint(w http.ResponseWriter, r *http.Request, serviceType store.ServiceType, previous, candidate json.RawMessage) bool {
	if serviceType != store.ServiceTypePostGIS {
		return true
	}
	if caller, ok := identity.FromContext(r.Context()); ok && caller.IsSuperAdmin() {
		return true
	}
	target, ok := parsePostGISEndpoint(candidate)
	if !ok {
		// Malformed connection info fails later with a precise 400.
		return true
	}
	if previous != nil {
		if existing, ok := parsePostGISEndpoint(previous); ok && existing == target {
			return true
		}
	}
	if databaseHostAllowed(h.cfg.Datasource.DatabaseHosts, target) {
		return true
	}
	writeError(w, http.StatusForbidden, "Forbidden",
		"only super administrators can connect PostGIS services to a host that is not listed in Datasource.DatabaseHosts")
	return false
}

func databaseHostAllowed(allowed []string, target postgisEndpoint) bool {
	for _, entry := range allowed {
		host, port, err := conf.ParseDatabaseHost(entry)
		if err != nil {
			continue
		}
		if host == target.host && (port == 0 || port == target.port) {
			return true
		}
	}
	return false
}
