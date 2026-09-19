package wms

import (
	"slices"

	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) extensionEnabled(ws *workspace.Workspace, name string) bool {
	if h == nil || ws == nil || ws.Settings == nil {
		return false
	}
	return slices.Contains(h.cfg.WMS.Extensions, name) && slices.Contains(ws.Settings.WMS.Extensions, name)
}

func (h *workspaceHandler) environmentLimits() (int, int) {
	variables, valueBytes := h.cfg.WMS.MaxEnvVariables, h.cfg.WMS.MaxEnvValueBytes
	if variables <= 0 {
		variables = 32
	}
	if valueBytes <= 0 {
		valueBytes = 256
	}
	return variables, valueBytes
}
