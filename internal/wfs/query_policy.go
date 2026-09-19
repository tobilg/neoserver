package wfs

import (
	"context"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) featureLimits(ws *workspace.Workspace) (maxFeatures, defaultCount, maxOffset int) {
	maxFeatures, defaultCount, maxOffset = h.cfg.WFS.MaxFeatures, h.cfg.WFS.DefaultCount, h.cfg.WFS.MaxOffset
	if ws.Settings != nil {
		if ws.Settings.WFS.MaxFeatures > 0 {
			maxFeatures = min(maxFeatures, ws.Settings.WFS.MaxFeatures)
		}
		if ws.Settings.WFS.DefaultCount > 0 {
			defaultCount = min(defaultCount, ws.Settings.WFS.DefaultCount)
		}
		if ws.Settings.WFS.MaxOffset > 0 {
			if maxOffset == 0 {
				maxOffset = ws.Settings.WFS.MaxOffset
			} else {
				maxOffset = min(maxOffset, ws.Settings.WFS.MaxOffset)
			}
		}
	}
	defaultCount = min(defaultCount, maxFeatures)
	return
}

func (h *workspaceHandler) countPublication(ctx context.Context, ws *workspace.Workspace, layer *workspace.Layer, service *workspace.Service, params datasource.QueryParams, key string) (int, error) {
	timeout := h.cfg.WFS.CountTimeoutMS
	if ws.Settings != nil && ws.Settings.WFS.CountTimeoutMS > 0 {
		timeout = ws.Settings.WFS.CountTimeoutMS
	}
	if timeout <= 0 {
		timeout = 5000
	}
	duration := time.Duration(timeout) * time.Millisecond
	countCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	// Count the selection independently of paging/projection.
	params.Limit, params.Offset, params.OutputSRID = 0, 0, 0
	params.Properties, params.SortBy = nil, nil
	loader := func(loadCtx context.Context) (int, error) {
		// Shared fills detach cancellation, so the database needs its own bound.
		bounded, done := context.WithTimeout(loadCtx, duration)
		defer done()
		return layer.CountFeatures(bounded, service.DataSource, params)
	}
	if h.cache != nil && key != "" {
		count, _, err := h.cache.LoadCount(countCtx, key, loader)
		return count, err
	}
	return loader(countCtx)
}
