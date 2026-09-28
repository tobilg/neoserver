package server

import (
	"net/http"
	"time"

	"github.com/tobilg/neoserver/internal/protocolrequest"
)

// exportOperations are the protocol reads whose response size scales with the
// data, not with a fixed render or tile budget.
var exportOperations = map[string]map[string]bool{
	"wfs":    {"GETFEATURE": true, "GETFEATUREWITHLOCK": true, "GETPROPERTYVALUE": true},
	"wcs":    {"GETCOVERAGE": true},
	"ogcapi": {"GETFEATURES": true},
}

// exportWriteDeadline gives bulk data responses Server.ExportWriteTimeoutSec
// instead of the server-wide WriteTimeout, which would otherwise truncate a
// large WFS/WCS/OGC API download to a slow client mid-stream. Processing stays
// bounded by each protocol's own limits (row caps, export and processing
// timeouts); this only governs how long the client may take to receive it.
//
// It must run after protocolrequest.Middleware and before any middleware that
// wraps the ResponseWriter without Unwrap.
func exportWriteDeadline(timeout, serverWriteTimeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if timeout <= 0 || timeout <= serverWriteTimeout {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d := protocolrequest.Get(r)
			if d.Err == nil && exportOperations[d.Service][d.Name] && r.Method != http.MethodOptions {
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout))
			}
			next.ServeHTTP(w, r)
		})
	}
}
