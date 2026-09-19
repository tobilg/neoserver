package wfs

import (
	"fmt"
	"net/http"

	"github.com/tobilg/neoserver/internal/workspace"
)

// authorizeFeatureQuery is intentionally independent of cache admission and
// loading: every reader must be authorized against the current publication.
func (h *workspaceHandler) authorizeFeatureQuery(r *http.Request, ws *workspace.Workspace, req *GetFeatureRequest) (*workspace.Layer, *workspace.Service, error) {
	if len(req.TypeNames) == 0 {
		return nil, nil, &RequestError{Code: ExceptionMissingParameterValue, Locator: "typeNames", Message: "TYPENAMES parameter is required"}
	}
	if len(req.TypeNames) != 1 {
		return nil, nil, &RequestError{Code: ExceptionOperationNotSupported, Locator: "typeNames", Message: "Spatial joins (multiple type names in a single query) are not supported. Query each type separately."}
	}
	layer, service := resolveFeatureLayer(ws, req.TypeNames[0], h.cfg.WFS.AppNamespacePrefix)
	if layer == nil || service == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		return nil, nil, &RequestError{Code: ExceptionInvalidParameterValue, Locator: "typeNames", Message: fmt.Sprintf("Unknown type name: %s", req.TypeNames[0])}
	}
	if service.DataSource == nil {
		return nil, nil, &RequestError{Code: ExceptionNoApplicableCode, Message: fmt.Sprintf("Service not available for layer: %s", layer.PublicID)}
	}
	return layer, service, nil
}

// A write-operation grant never implies visibility of every publication, and
// read-only SQL views cannot acquire locks on their underlying physical source.
func (h *workspaceHandler) authorizeLockQuery(r *http.Request, ws *workspace.Workspace, req *GetFeatureRequest) (*workspace.Layer, *workspace.Service, error) {
	layer, service, err := h.authorizeFeatureQuery(r, ws, req)
	if err != nil {
		return nil, nil, err
	}
	if layer.IsSQLView || (layer.SQLViewConfig != nil && layer.SQLViewConfig.ReadOnly) {
		return nil, nil, &RequestError{Code: ExceptionOperationNotSupported, Locator: "typeNames", Message: "Read-only SQL views cannot be locked"}
	}
	return layer, service, nil
}
