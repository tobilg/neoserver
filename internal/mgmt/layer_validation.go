package mgmt

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
)

func (h *handler) validateLayerSource(ctx context.Context, workspaceID, serviceID, source string) (*datasource.LayerInfo, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ws, release, ok := h.registry.AcquireByID(workspaceID)
	if !ok {
		return nil, http.StatusNotFound, errors.New("workspace not found")
	}
	defer release()
	service := ws.ResolveService(serviceID)
	if service == nil || service.DataSource == nil {
		return nil, http.StatusServiceUnavailable, errors.New("store is unavailable; reconnect it before publishing")
	}
	ds := service.DataSource
	info, err := ds.GetLayerInfo(ctx, source)
	if err == nil && info != nil {
		// Metadata may have been cached during discovery. An actual bounded
		// read catches a dropped source or revoked permissions before publishing.
		_, err = ds.Query(ctx, source, datasource.QueryParams{Limit: 1, OutputSRID: info.SRID})
		if err == nil {
			return info, http.StatusOK, nil
		}
	}
	if err := ds.Health(ctx); err != nil {
		return nil, http.StatusServiceUnavailable, errors.New("store connection is unavailable; reconnect it and retry")
	}
	return nil, http.StatusUnprocessableEntity, errors.New("source_layer does not exist or cannot be read; rediscover the store and check source permissions")
}
