package mgmt

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/store"
)

// resolveWorkspaceIDForStyles resolves a workspace identifier (name or ID) to actual workspace ID.
func (h *handler) resolveWorkspaceIDForStyles(ctx context.Context, identifier string) (string, error) {
	// First try to get by ID
	ws, err := h.store.GetWorkspace(ctx, identifier)
	if err == nil {
		return ws.ID, nil
	}

	// If not found by ID, try by name
	if err == store.ErrNotFound {
		ws, err = h.store.GetWorkspaceByName(ctx, identifier)
		if err == nil {
			return ws.ID, nil
		}
	}

	return "", err
}

// StyleResponse is the API response for a style (without SLD body).
type StyleResponse struct {
	ID               string           `json:"id"`
	WorkspaceID      string           `json:"workspace_id"`
	Name             string           `json:"name"`
	Title            string           `json:"title,omitempty"`
	Description      string           `json:"description,omitempty"`
	Format           string           `json:"format"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Valid            bool             `json:"valid"`
	ValidationErrors []string         `json:"validation_errors,omitempty"`
	Diagnostics      []sld.Diagnostic `json:"diagnostics,omitempty"`
}

// StyleWithBodyResponse is the API response for a style with SLD body.
type StyleWithBodyResponse struct {
	StyleResponse
	Body    string `json:"body"`
	SLDBody string `json:"sld_body,omitempty"`
}

// CreateStyleRequest is the request to create a style.
type CreateStyleRequest struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	SLDBody     string `json:"sld_body,omitempty"`
	Format      string `json:"format,omitempty"`
}

// UpdateStyleRequest is the request to update a style.
type UpdateStyleRequest struct {
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	SLDBody     string `json:"sld_body,omitempty"`
	Format      string `json:"format,omitempty"`
}

func styleResponseFromStore(style *store.Style) StyleResponse {
	response := StyleResponse{ID: style.ID, WorkspaceID: style.WorkspaceID, Name: style.Name, Title: style.Title, Description: style.Description, Format: style.Format, CreatedAt: style.CreatedAt, UpdatedAt: style.UpdatedAt, Valid: true}
	_, diagnostics, err := sld.Compile(style.Format, style.SLDBody)
	response.Diagnostics = diagnostics
	if err != nil {
		response.Valid = false
		for _, diagnostic := range diagnostics {
			response.ValidationErrors = append(response.ValidationErrors, diagnostic.Message)
		}
		if len(response.ValidationErrors) == 0 {
			response.ValidationErrors = []string{err.Error()}
		}
	}
	return response
}

func styleBodyResponse(style *store.Style) StyleWithBodyResponse {
	response := StyleWithBodyResponse{StyleResponse: styleResponseFromStore(style), Body: style.SLDBody}
	if sld.IsSLDFormat(style.Format) {
		response.SLDBody = style.SLDBody
	}
	return response
}

func resolveAuthoredStyleBody(format, body, legacy string) (string, string, error) {
	canonical, err := sld.NormalizeFormat(format)
	if err != nil {
		return "", "", err
	}
	if body != "" && legacy != "" && body != legacy {
		return "", "", fmt.Errorf("body and sld_body must be identical when both are provided")
	}
	if legacy != "" && !sld.IsSLDFormat(canonical) {
		return "", "", fmt.Errorf("sld_body is only valid for SLD/SE formats")
	}
	if body == "" {
		body = legacy
	}
	if strings.TrimSpace(body) == "" {
		return "", "", fmt.Errorf("body is required")
	}
	return canonical, body, nil
}

func (h *handler) validateStyleBodySize(body string) error {
	if limit := h.cfg.WMS.MaxStyleBodyBytes; limit > 0 && int64(len(body)) > limit {
		return fmt.Errorf("style body exceeds the configured %d byte limit", limit)
	}
	return nil
}

// listStyles handles GET /workspaces/{workspace}/styles
func (h *handler) listStyles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForStyles(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	styles, err := h.store.ListStyles(ctx, workspaceID)
	if err != nil {
		h.logger.Error("failed to list styles", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list styles")
		return
	}

	var result []StyleResponse
	for _, s := range styles {
		result = append(result, styleResponseFromStore(s))
	}

	if result == nil {
		result = []StyleResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"styles":       result,
	})
}

// createStyle handles POST /workspaces/{workspace}/styles
func (h *handler) createStyle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForStyles(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req CreateStyleRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	if !sld.ValidStyleName(req.Name) {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid style name")
		return
	}

	format, body, bodyErr := resolveAuthoredStyleBody(req.Format, req.Body, req.SLDBody)
	if bodyErr != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", bodyErr.Error())
		return
	}
	if err := h.validateStyleBodySize(body); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "Payload Too Large", err.Error())
		return
	}

	if _, _, compileErr := sld.Compile(format, body); compileErr != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid style: "+compileErr.Error())
		return
	}

	input := store.CreateStyleInput{
		WorkspaceID: workspaceID,
		Name:        req.Name,
		Title:       req.Title,
		Description: req.Description,
		SLDBody:     body,
		Format:      format,
	}

	runtimeStyle, err := h.registry.CreateStyle(ctx, workspaceID, input)
	if err != nil {
		if err == store.ErrDuplicateKey {
			writeError(w, http.StatusConflict, "Conflict", "style with this name already exists")
			return
		}
		h.logger.Error("failed to create style", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create style")
		return
	}
	style, err := h.store.GetStyle(ctx, runtimeStyle.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to read created style")
		return
	}

	writeJSON(w, http.StatusCreated, styleBodyResponse(style))
}

// getStyle handles GET /workspaces/{workspace}/styles/{style}
func (h *handler) getStyle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	styleName := chi.URLParam(r, "style")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForStyles(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	style, err := h.store.GetStyleByName(ctx, workspaceID, styleName)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "style not found")
			return
		}
		h.logger.Error("failed to get style", "style", styleName, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get style")
		return
	}

	writeJSON(w, http.StatusOK, styleBodyResponse(style))
}

// updateStyle handles PUT /workspaces/{workspace}/styles/{style}
func (h *handler) updateStyle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	styleName := chi.URLParam(r, "style")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForStyles(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req UpdateStyleRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	// Get existing style first
	existingStyle, err := h.store.GetStyleByName(ctx, workspaceID, styleName)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "style not found")
			return
		}
		h.logger.Error("failed to get style", "style", styleName, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get style")
		return
	}

	requestedBody := req.Body
	if requestedBody == "" {
		requestedBody = req.SLDBody
	}
	effectiveFormat := existingStyle.Format
	if req.Format != "" {
		var formatErr error
		effectiveFormat, formatErr = sld.NormalizeFormat(req.Format)
		if formatErr != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", formatErr.Error())
			return
		}
		if effectiveFormat != existingStyle.Format && requestedBody == "" {
			writeError(w, http.StatusBadRequest, "Bad Request", "changing format requires body")
			return
		}
	}
	if requestedBody != "" {
		format, body, bodyErr := resolveAuthoredStyleBody(effectiveFormat, req.Body, req.SLDBody)
		if bodyErr != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", bodyErr.Error())
			return
		}
		effectiveFormat, requestedBody = format, body
		if err := h.validateStyleBodySize(requestedBody); err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "Payload Too Large", err.Error())
			return
		}
		if _, _, compileErr := sld.Compile(effectiveFormat, requestedBody); compileErr != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "invalid style: "+compileErr.Error())
			return
		}
	}
	if req.Name != "" && req.Name != existingStyle.Name {
		if refs := h.styleReferences(workspaceID, existingStyle.Name); len(refs) > 0 {
			writeError(w, http.StatusConflict, "Conflict", "style is referenced by: "+strings.Join(refs, ", "))
			return
		}
	}
	if req.Name != "" && !sld.ValidStyleName(req.Name) {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid style name")
		return
	}

	input := store.UpdateStyleInput{}
	if req.Name != "" {
		input.Name = &req.Name
	}
	if req.Title != "" {
		input.Title = &req.Title
	}
	if req.Description != "" {
		input.Description = &req.Description
	}
	if requestedBody != "" {
		input.SLDBody = &requestedBody
	}
	if req.Format != "" {
		input.Format = &effectiveFormat
	}

	_, err = h.registry.UpdateStyle(ctx, workspaceID, existingStyle.ID, input)
	if err != nil {
		if err == store.ErrDuplicateKey {
			writeError(w, http.StatusConflict, "Conflict", "style with this name already exists")
			return
		}
		h.logger.Error("failed to update style", "style", styleName, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update style")
		return
	}
	style, err := h.store.GetStyle(ctx, existingStyle.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to read updated style")
		return
	}

	writeJSON(w, http.StatusOK, styleBodyResponse(style))
}

// deleteStyle handles DELETE /workspaces/{workspace}/styles/{style}
func (h *handler) deleteStyle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	styleName := chi.URLParam(r, "style")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForStyles(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	// Get existing style first
	existingStyle, err := h.store.GetStyleByName(ctx, workspaceID, styleName)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "style not found")
			return
		}
		h.logger.Error("failed to get style", "style", styleName, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get style")
		return
	}
	if refs := h.styleReferences(workspaceID, existingStyle.Name); len(refs) > 0 {
		writeError(w, http.StatusConflict, "Conflict", "style is referenced by: "+strings.Join(refs, ", "))
		return
	}

	if err := h.registry.DeleteStyle(ctx, workspaceID, existingStyle.ID); err != nil {
		h.logger.Error("failed to delete style", "style", styleName, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete style")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
