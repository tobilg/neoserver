package mgmt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/stylegraphics"
)

func (h *handler) styleAssetStore() (store.StyleAssetStore, error) {
	value, ok := h.store.(store.StyleAssetStore)
	if !ok {
		return nil, fmt.Errorf("style asset persistence is unavailable")
	}
	return value, nil
}

func assetWorkspace(h *handler, w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceID, err := h.resolveWorkspaceIDForStyles(r.Context(), chi.URLParam(r, "workspace"))
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return "", false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return "", false
	}
	return workspaceID, true
}

func (h *handler) styleAssetPath(workspaceID, name string) (string, error) {
	if !sld.ValidStyleName(name) {
		return "", fmt.Errorf("invalid asset name")
	}
	root := h.cfg.WMS.StyleAssetPath
	if root == "" {
		root = "./data/style-assets"
	}
	return filepath.Join(root, workspaceID, name), nil
}

func (h *handler) listStyleAssets(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := assetWorkspace(h, w, r)
	if !ok {
		return
	}
	persistence, err := h.styleAssetStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", err.Error())
		return
	}
	assets, err := persistence.ListStyleAssets(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list style assets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace_id": workspaceID, "assets": assets})
}

func (h *handler) getStyleAsset(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := assetWorkspace(h, w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "asset")
	persistence, err := h.styleAssetStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", err.Error())
		return
	}
	asset, err := persistence.GetStyleAsset(r.Context(), workspaceID, name)
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "Not Found", "style asset not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get style asset")
		return
	}
	path, err := h.styleAssetPath(workspaceID, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	w.Header().Set("Content-Type", asset.ContentType)
	body, err := stylegraphics.ReadAssetObject(filepath.Dir(path), name, asset.SHA256, h.cfg.WMS.MaxStyleAssetBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Asset unavailable", "asset payload is missing or does not match its catalog hash")
		return
	}
	w.Header().Set("ETag", `"`+asset.SHA256+`"`)
	http.ServeContent(w, r, asset.Name, asset.UpdatedAt, bytes.NewReader(body))
}

func (h *handler) putStyleAsset(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := assetWorkspace(h, w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "asset")
	path, err := h.styleAssetPath(workspaceID, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	limit := h.cfg.WMS.MaxStyleAssetBytes
	if limit <= 0 {
		limit = 5 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "failed to read asset")
		return
	}
	if int64(len(body)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "Payload Too Large", fmt.Sprintf("asset exceeds the configured %d byte limit", limit))
		return
	}
	contentType, err := validateStyleAsset(body, r.Header.Get("Content-Type"), h.cfg.WMS.MaxExternalGraphicDimension)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	digest := sha256.Sum256(body)
	sha := hex.EncodeToString(digest[:])
	// Publish immutable bytes before committing their catalog reference. Failed
	// commits may leave unreferenced objects, but never replace a live payload.
	if err := stylegraphics.WriteAssetObject(filepath.Dir(path), sha, body); err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to store asset")
		return
	}
	persistence, err := h.styleAssetStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", err.Error())
		return
	}
	asset, err := persistence.UpsertStyleAsset(r.Context(), store.UpsertStyleAssetInput{WorkspaceID: workspaceID, Name: name, ContentType: contentType, SizeBytes: int64(len(body)), SHA256: sha})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to catalog asset")
		return
	}
	if h.registry != nil {
		if err := h.registry.RefreshStyleAssets(r.Context(), workspaceID); err != nil {
			writeError(w, http.StatusInternalServerError, "Asset saved", "asset was saved but runtime cache refresh failed; retry the upload")
			return
		}
	}
	writeJSON(w, http.StatusCreated, asset)
}

func (h *handler) deleteStyleAsset(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := assetWorkspace(h, w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "asset")
	persistence, err := h.styleAssetStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", err.Error())
		return
	}
	if _, err := persistence.GetStyleAsset(r.Context(), workspaceID, name); err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "Not Found", "style asset not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to load style asset")
		return
	}
	styles, listErr := h.store.ListStyles(r.Context(), workspaceID)
	if listErr != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to check style references")
		return
	}
	for _, style := range styles {
		if strings.Contains(style.SLDBody, "asset:"+name) || strings.Contains(style.SLDBody, "asset://"+name) {
			writeError(w, http.StatusConflict, "Conflict", "asset is referenced by style "+style.Name)
			return
		}
	}
	// Deleting the reference is atomic. Retain immutable bytes for readers
	// holding an older workspace snapshot; workspace deletion reclaims them.
	if err = persistence.DeleteStyleAsset(r.Context(), workspaceID, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Not Found", "style asset not found")
		} else {
			writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete style asset")
		}
		return
	}
	if h.registry != nil {
		if err := h.registry.RefreshStyleAssets(r.Context(), workspaceID); err != nil {
			writeError(w, http.StatusInternalServerError, "Asset deleted", "asset was deleted but runtime cache refresh failed")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateStyleAsset(body []byte, declared string, maxDimension int) (string, error) {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	if mediaType == "image/svg+xml" || strings.HasPrefix(strings.TrimSpace(string(body)), "<svg") || strings.HasPrefix(strings.TrimSpace(string(body)), "<?xml") {
		var root struct {
			XMLName       xml.Name
			Width, Height string `xml:",attr"`
			ViewBox       string `xml:"viewBox,attr"`
		}
		if err := xml.Unmarshal(body, &root); err != nil || root.XMLName.Local != "svg" {
			return "", fmt.Errorf("invalid SVG asset")
		}
		if err := stylegraphics.ValidateSVG(body, maxDimension); err != nil {
			return "", err
		}
		return "image/svg+xml", nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("asset must be PNG, JPEG, GIF, or SVG")
	}
	if maxDimension > 0 && (config.Width > maxDimension || config.Height > maxDimension) {
		return "", fmt.Errorf("asset dimensions exceed %d pixels", maxDimension)
	}
	switch format {
	case "png":
		return "image/png", nil
	case "jpeg":
		return "image/jpeg", nil
	case "gif":
		return "image/gif", nil
	default:
		return "", fmt.Errorf("unsupported image format %q", format)
	}
}
