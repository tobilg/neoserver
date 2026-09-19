package mgmt

import (
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/httputil"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

type createImportRequest struct {
	Name      string `json:"name"`
	SourceURI string `json:"source_uri"`
}

func (h *handler) importWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.importer == nil {
		writeError(w, http.StatusServiceUnavailable, "Importer unavailable", "enable Importer to run managed vector imports")
		return "", false
	}
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return "", false
	}
	return workspaceID, true
}

func importPrincipal(r *http.Request) string {
	if principal, ok := identity.FromContext(r.Context()); ok && principal != nil {
		return principal.Subject
	}
	return ""
}

func (h *handler) createImport(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.importWorkspace(w, r)
	if !ok {
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var job *store.ImportJob
	var err error
	switch mediaType {
	case "multipart/form-data":
		var cancel func()
		r, cancel = httputil.UploadRequest(w, r, time.Duration(h.cfg.Importer.UploadTimeoutSec)*time.Second, time.Duration(h.cfg.Importer.UploadIdleTimeoutSec)*time.Second)
		defer cancel()
		if parseErr := r.ParseMultipartForm(1 << 20); parseErr != nil {
			var timeout net.Error
			var tooLarge *http.MaxBytesError
			if errors.As(parseErr, &tooLarge) {
				writeError(w, 413, "Upload too large", "file exceeds the configured upload limit")
				return
			}
			if errors.As(parseErr, &timeout) && timeout.Timeout() {
				writeError(w, 408, "Upload timed out", "upload exceeded the duration or idle limit; retry on a faster connection")
				return
			}
			writeError(w, http.StatusBadRequest, "Bad Request", "invalid multipart upload")
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, fileErr := r.FormFile("file")
		if fileErr != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "file is required")
			return
		}
		defer file.Close()
		job, err = h.importer.CreateUpload(r.Context(), workspaceID, r.FormValue("name"), header.Filename, importPrincipal(r), file)
	case "application/json", "":
		var request createImportRequest
		if readJSON(r, &request) != nil || strings.TrimSpace(request.SourceURI) == "" {
			writeError(w, http.StatusBadRequest, "Bad Request", "name and source_uri JSON is required")
			return
		}
		job, err = h.importer.CreateURI(r.Context(), workspaceID, request.Name, request.SourceURI, importPrincipal(r))
	default:
		writeError(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "use application/json or multipart/form-data")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid import", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *handler) listImports(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.importWorkspace(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			writeError(w, 400, "Bad Request", "limit must be between 1 and 1000")
			return
		}
	}
	page, err := h.importer.ListPage(r.Context(), workspaceID, store.ImportListOptions{Limit: limit, Cursor: query.Get("cursor"), Status: query.Get("status"), Search: query.Get("search")})
	if err != nil {
		if errors.Is(err, store.ErrInvalidImportList) {
			writeError(w, 400, "Bad Request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list imports")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *handler) workspaceImport(w http.ResponseWriter, r *http.Request) (*store.ImportJob, bool) {
	workspaceID, ok := h.importWorkspace(w, r)
	if !ok {
		return nil, false
	}
	job, err := h.importer.Get(r.Context(), chi.URLParam(r, "import"))
	if err != nil || job.WorkspaceID != workspaceID {
		writeError(w, http.StatusNotFound, "Not Found", "import not found")
		return nil, false
	}
	return job, true
}
func (h *handler) getImport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if ok {
		writeJSON(w, http.StatusOK, job)
	}
}

func (h *handler) getImportHistory(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if !ok {
		return
	}
	importStore, ok := h.store.(store.ImportStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "import history is unavailable")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := importStore.ListImportJobEvents(r.Context(), job.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list import history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}
func (h *handler) setImportPlan(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if !ok {
		return
	}
	var plan store.ImportPlan
	if readJSON(r, &plan) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid import plan")
		return
	}
	updated, err := h.importer.SetPlan(r.Context(), job.ID, plan)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid import plan", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, updated)
}
func (h *handler) previewImport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	features, err := h.importer.Preview(r.Context(), job.ID, r.URL.Query().Get("layer"), limit)
	if err != nil {
		writeError(w, http.StatusConflict, "Preview unavailable", err.Error())
		return
	}
	values := make([]any, 0, len(features))
	for _, feature := range features {
		var value any
		if json.Unmarshal(feature, &value) == nil {
			values = append(values, value)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"type": "FeatureCollection", "features": values})
}
func (h *handler) publishImport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if !ok {
		return
	}
	updated, err := h.importer.Publish(r.Context(), job.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "Import publication failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, updated)
}
func (h *handler) retryImport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if !ok {
		return
	}
	updated, err := h.importer.Retry(r.Context(), job.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "Import retry failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, updated)
}
func (h *handler) cancelImport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if !ok {
		return
	}
	if err := h.importer.Cancel(r.Context(), job.ID); err != nil {
		writeError(w, http.StatusConflict, "Import cancellation failed", err.Error())
		return
	}
	updated, _ := h.importer.Get(r.Context(), job.ID)
	writeJSON(w, http.StatusAccepted, updated)
}
func (h *handler) rollbackImport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceImport(w, r)
	if !ok {
		return
	}
	updated, err := h.importer.Rollback(r.Context(), job.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "Import rollback failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, updated)
}
