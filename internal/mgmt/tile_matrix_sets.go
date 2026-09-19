package mgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tiles"
)

func (h *handler) tileMatrixSetStore(w http.ResponseWriter) (store.TileMatrixSetStore, bool) {
	catalog, ok := h.store.(store.TileMatrixSetStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "tile matrix set persistence is unavailable")
	}
	return catalog, ok
}

func (h *handler) listTileMatrixSets(w http.ResponseWriter, r *http.Request) {
	items := tiles.GetSupportedTileMatrixSets()
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{"id": item.ID, "title": item.Title, "uri": item.URI, "crs": item.CRS,
			"built_in": item.ID == tiles.TMSWebMercatorQuad || item.ID == tiles.TMSWorldCRS84Quad})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tile_matrix_sets": result})
}

func (h *handler) getTileMatrixSet(w http.ResponseWriter, r *http.Request) {
	catalog, ok := h.tileMatrixSetStore(w)
	if !ok {
		return
	}
	item, err := catalog.GetTileMatrixSet(r.Context(), chi.URLParam(r, "tileMatrixSet"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "tile matrix set not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *handler) putTileMatrixSet(w http.ResponseWriter, r *http.Request) {
	catalog, ok := h.tileMatrixSetStore(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "tileMatrixSet")
	var definition tiles.TileMatrixSetDefinition
	if readJSON(r, &definition) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid tile matrix set definition")
		return
	}
	if definition.ID == "" {
		definition.ID = id
	}
	if definition.ID != id {
		writeError(w, http.StatusBadRequest, "Bad Request", "definition id must match the URL")
		return
	}
	if id == tiles.TMSWebMercatorQuad || id == tiles.TMSWorldCRS84Quad {
		writeError(w, http.StatusConflict, "Conflict", "built-in tile matrix sets are immutable")
		return
	}
	if err := tiles.ValidateTileMatrixSetDefinition(&definition); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid tile matrix set", err.Error())
		return
	}
	canonical, _ := json.Marshal(definition)
	digestBytes := sha256.Sum256(canonical)
	item, err := catalog.UpsertTileMatrixSet(r.Context(), id, canonical, hex.EncodeToString(digestBytes[:]))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to persist tile matrix set")
		return
	}
	if err = reloadTileMatrixSets(r.Context(), catalog); err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "tile matrix set was saved but runtime reload failed")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *handler) deleteTileMatrixSet(w http.ResponseWriter, r *http.Request) {
	catalog, ok := h.tileMatrixSetStore(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "tileMatrixSet")
	if id == tiles.TMSWebMercatorQuad || id == tiles.TMSWorldCRS84Quad {
		writeError(w, http.StatusConflict, "Conflict", "built-in tile matrix sets are immutable")
		return
	}
	workspaces, err := h.store.ListWorkspaces(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to inspect tile matrix set references")
		return
	}
	var referencedBy []string
	for _, workspace := range workspaces {
		settings, settingsErr := h.store.GetOGCTilesAPISettings(r.Context(), workspace.ID)
		if settingsErr != nil {
			writeError(w, http.StatusInternalServerError, "Internal Error", "failed to inspect tile matrix set references")
			return
		}
		if slices.Contains(settings.Settings.TileMatrixSets, id) {
			referencedBy = append(referencedBy, workspace.Name)
		}
	}
	if len(referencedBy) > 0 {
		writeError(w, http.StatusConflict, "Conflict", "tile matrix set is referenced by workspaces: "+strings.Join(referencedBy, ", "))
		return
	}
	if err = catalog.DeleteTileMatrixSet(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "tile matrix set not found")
		return
	}
	if err := reloadTileMatrixSets(r.Context(), catalog); err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "tile matrix set was deleted but runtime reload failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func reloadTileMatrixSets(ctx context.Context, catalog store.TileMatrixSetStore) error {
	records, err := catalog.ListTileMatrixSets(ctx)
	if err != nil {
		return err
	}
	definitions := make([]*tiles.TileMatrixSetDefinition, 0, len(records))
	for _, record := range records {
		var definition tiles.TileMatrixSetDefinition
		if err := json.Unmarshal(record.Definition, &definition); err != nil {
			return err
		}
		definitions = append(definitions, &definition)
	}
	return tiles.ReplaceCustomTileMatrixSets(definitions)
}
