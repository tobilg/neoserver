package mgmt

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
)

func (h *handler) mosaicContext(r *http.Request) (string, string, error) {
	workspaceID, err := h.resolveWorkspaceID(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		return "", "", err
	}
	serviceID, err := h.resolveServiceID(r.Context(), workspaceID, chi.URLParam(r, "service"))
	return workspaceID, serviceID, err
}

func (h *handler) requireMosaic(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if h.mosaic == nil {
		writeError(w, http.StatusServiceUnavailable, "Mosaic catalog unavailable", "enable MosaicCatalog to manage harvested granules")
		return "", "", false
	}
	workspaceID, serviceID, err := h.mosaicContext(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace or service not found")
		return "", "", false
	}
	return workspaceID, serviceID, true
}

func (h *handler) listMosaicGranules(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, ok := h.requireMosaic(w, r)
	if !ok {
		return
	}
	filter := mosaiccatalog.GranuleFilter{}
	filter.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	filter.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	filter.Time = r.URL.Query().Get("time")
	if value := r.URL.Query().Get("elevation"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "elevation must be numeric")
			return
		}
		filter.Elevation = &parsed
	}
	if value := r.URL.Query().Get("bbox"); value != "" {
		parts := strings.Split(value, ",")
		if len(parts) != 4 {
			writeError(w, http.StatusBadRequest, "Bad Request", "bbox must contain four coordinates")
			return
		}
		var bbox [4]float64
		for index := range bbox {
			parsed, err := strconv.ParseFloat(strings.TrimSpace(parts[index]), 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "Bad Request", "bbox coordinates must be numeric")
				return
			}
			bbox[index] = parsed
		}
		if bbox[0] >= bbox[2] || bbox[1] >= bbox[3] {
			writeError(w, http.StatusBadRequest, "Bad Request", "bbox is invalid")
			return
		}
		filter.BBox = &bbox
	}
	items, err := h.mosaic.ListGranules(r.Context(), workspaceID, serviceID, filter)
	if err != nil {
		writeMosaicError(w, err)
		return
	}
	if items == nil {
		items = []*mosaiccatalog.Granule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"granules": items})
}

func (h *handler) getMosaicGranule(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, ok := h.requireMosaic(w, r)
	if !ok {
		return
	}
	item, err := h.mosaic.GetGranule(r.Context(), workspaceID, serviceID, chi.URLParam(r, "granule"))
	if err != nil {
		writeMosaicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *handler) deleteMosaicGranule(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, ok := h.requireMosaic(w, r)
	if !ok {
		return
	}
	if err := h.mosaic.DeleteGranule(r.Context(), workspaceID, serviceID, chi.URLParam(r, "granule")); err != nil {
		writeMosaicError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) createMosaicHarvestJob(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, ok := h.requireMosaic(w, r)
	if !ok {
		return
	}
	var request mosaiccatalog.HarvestRequest
	if readJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	createdBy := ""
	if principal, ok := identity.FromContext(r.Context()); ok && principal != nil {
		createdBy = principal.Subject
	}
	job, err := h.mosaic.CreateJob(r.Context(), workspaceID, serviceID, createdBy, request)
	if err != nil {
		writeMosaicError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *handler) listMosaicHarvestJobs(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, ok := h.requireMosaic(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := h.mosaic.ListJobs(r.Context(), workspaceID, serviceID, limit)
	if err != nil {
		writeMosaicError(w, err)
		return
	}
	if jobs == nil {
		jobs = []*mosaiccatalog.Job{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *handler) getMosaicHarvestJob(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, ok := h.requireMosaic(w, r)
	if !ok {
		return
	}
	job, err := h.mosaic.GetJob(r.Context(), workspaceID, serviceID, chi.URLParam(r, "job"))
	if err != nil {
		writeMosaicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *handler) cancelMosaicHarvestJob(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, ok := h.requireMosaic(w, r)
	if !ok {
		return
	}
	jobID := chi.URLParam(r, "job")
	if err := h.mosaic.CancelJob(r.Context(), workspaceID, serviceID, jobID); err != nil {
		writeMosaicError(w, err)
		return
	}
	job, _ := h.mosaic.GetJob(r.Context(), workspaceID, serviceID, jobID)
	writeJSON(w, http.StatusAccepted, job)
}

func writeMosaicError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mosaiccatalog.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not Found", err.Error())
	case errors.Is(err, mosaiccatalog.ErrServiceNotMosaic):
		writeError(w, http.StatusUnprocessableEntity, "Invalid mosaic operation", err.Error())
	default:
		writeError(w, http.StatusUnprocessableEntity, "Mosaic operation failed", err.Error())
	}
}
