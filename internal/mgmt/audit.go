package mgmt

import (
	"errors"
	"net/http"
	"time"

	internalaudit "github.com/tobilg/neoserver/internal/audit"
)

func (h *handler) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	if h.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "Audit unavailable", "durable auditing is disabled")
		return
	}
	query := internalaudit.Query{Workspace: r.URL.Query().Get("workspace"), Principal: r.URL.Query().Get("principal"), CredentialID: r.URL.Query().Get("credential_id"), Limit: internalaudit.ParseLimit(r.URL.Query().Get("limit"))}
	query.Cursor, query.Search = r.URL.Query().Get("cursor"), r.URL.Query().Get("q")
	query.Outcome = r.URL.Query().Get("outcome")
	if query.Outcome != "" && query.Outcome != internalaudit.OutcomeFailed && query.Outcome != internalaudit.OutcomeSucceeded {
		writeError(w, http.StatusBadRequest, "Bad Request", "outcome must be failed or succeeded")
		return
	}
	if (r.URL.Query().Has("limit") && (query.Limit < 1 || query.Limit > 1000)) || len(query.Search) > 512 || r.URL.Query().Has("offset") {
		writeError(w, http.StatusBadRequest, "Bad Request", "limit must be 1–1000, q at most 512 bytes; use cursor rather than offset")
		return
	}
	if value := r.URL.Query().Get("since"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "since must be RFC3339")
			return
		}
		query.Since = &parsed
	}
	page, err := h.audit.ListPage(r.Context(), query)
	if errors.Is(err, internalaudit.ErrInvalidCursor) {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid audit cursor")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list audit events")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *handler) runAuditRetention(w http.ResponseWriter, r *http.Request) {
	if h.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "Audit unavailable", "durable auditing is disabled")
		return
	}
	if err := h.audit.Purge(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "audit retention failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
