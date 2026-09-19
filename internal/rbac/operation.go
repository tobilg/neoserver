package rbac

import (
	"context"
	"net/http"

	"github.com/tobilg/neoserver/internal/httputil"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/protocolrequest"
	"github.com/tobilg/neoserver/internal/workspace"
)

// RequireServiceOperation applies the shared Casbin model to an authenticated
// workspace request. Anonymous requests continue to the protocol handler,
// which remains responsible for each service's public/private setting.
func RequireServiceOperation(enforcer *Enforcer, service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = protocolrequest.Prepare(r, service)
			descriptor := protocolrequest.Get(r)
			if descriptor.Err != nil {
				httputil.HTTPError(w, r, descriptor.Err.Error(), http.StatusBadRequest)
				return
			}
			principal, ok := identity.FromContext(r.Context())
			if !ok || principal == nil {
				next.ServeHTTP(w, r)
				return
			}
			if principal.IsSuperAdmin() {
				next.ServeHTTP(w, r)
				return
			}
			ws, ok := workspace.FromContext(r.Context())
			if !ok || ws == nil {
				httputil.HTTPError(w, r, "workspace not found", http.StatusNotFound)
				return
			}
			role := principal.GetWorkspaceRole(ws.ID)
			if role == "" {
				// A public service is still public to an authenticated caller who
				// has no workspace assignment. The service handler decides that.
				next.ServeHTTP(w, r)
				return
			}
			action := protocolrequest.Action(service, descriptor.Name, r.Method)
			allowed, err := enforcer.CanAccessOperation(role, ws.ID, service, descriptor.Name, action)
			if err != nil {
				httputil.HTTPError(w, r, "authorization check failed", http.StatusInternalServerError)
				return
			}
			if !allowed {
				httputil.HTTPError(w, r, "forbidden: operation is not granted", http.StatusForbidden)
				return
			}
			grant := operationGrant{workspace: ws.ID, service: service, operation: descriptor.Name, action: action}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), operationGrantKey{}, grant)))
		})
	}
}

func requestOperation(r *http.Request) (string, *http.Request) {
	r = protocolrequest.Prepare(r, "wfs")
	return protocolrequest.Get(r).Name, r
}

func operationAction(service, operation, method string) bool {
	return protocolrequest.Action(service, operation, method) != ActionRead
}

type operationGrantKey struct{}
type operationGrant struct{ workspace, service, operation, action string }

// HasOperationGrant recognizes only a decision made by this middleware, never
// a client-supplied role name or header. The grant is bound to the exact request.
func HasOperationGrant(r *http.Request, workspace, service, action string) bool {
	grant, ok := r.Context().Value(operationGrantKey{}).(operationGrant)
	return ok && grant.workspace == workspace && grant.service == service && grant.action == action && grant.operation == protocolrequest.Get(r).Name
}
