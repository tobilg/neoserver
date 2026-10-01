package mgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/stac"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/stacsource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *handler) registerSTAC(r chi.Router) {
	r.Get("/settings/stac", h.getSTACSettings)
	r.Put("/settings/stac", h.putSTACSettings)
	r.Route("/stac", func(r chi.Router) {
		r.Get("/resources", h.stacResources)
		r.Get("/collections", h.stacCollections)
		r.Post("/collections", h.stacCreateCollection)
		r.Get("/collections/{stacCollection}", h.stacGetCollection)
		r.Put("/collections/{stacCollection}", h.stacUpdateCollection)
		r.Delete("/collections/{stacCollection}", h.stacDeleteCollection)
		r.Get("/collections/{stacCollection}/items", h.stacItems)
		r.Post("/collections/{stacCollection}/items", h.stacPutItem)
		r.Get("/collections/{stacCollection}/items/{stacItem}", h.stacGetItem)
		r.Put("/collections/{stacCollection}/items/{stacItem}", h.stacPutItem)
		r.Delete("/collections/{stacCollection}/items/{stacItem}", h.stacDeleteItem)
		r.Post("/search", h.stacSearch)
		r.Get("/bindings", h.stacBindings)
		r.Post("/bindings", h.stacPutBinding)
		r.Post("/bindings/preview", h.stacPreviewBinding)
		r.Get("/bindings/{binding}", h.stacGetBinding)
		r.Put("/bindings/{binding}", h.stacPutBinding)
		r.Delete("/bindings/{binding}", h.stacDeleteBinding)
		r.Post("/bindings/{binding}/preview", h.stacPreviewBinding)
		r.Post("/bindings/{binding}/refresh", h.stacRefresh)
		r.Post("/imports", h.stacImport)
		r.Get("/imports/{job}/preview", h.stacImportPreview)
		r.Post("/imports/{job}/publish", h.stacImportPublish)
		r.Delete("/imports/{job}", h.stacCancelJob)
		r.Post("/imports/{job}/retry", h.stacRetryJob)
		r.Get("/jobs", h.stacJobs)
		r.Get("/jobs/{job}", h.stacJob)
		r.Delete("/jobs/{job}", h.stacCancelJob)
		r.Post("/jobs/{job}/retry", h.stacRetryJob)
		r.Post("/assets", h.stacBindAsset)
		r.Delete("/assets", h.stacUnbindAsset)
	})
}
func (h *handler) stacWorkspace(w http.ResponseWriter, r *http.Request) (*workspace.Workspace, func(), bool) {
	if h.stac == nil {
		writeError(w, 503, "STAC unavailable", "enable STAC on the server to manage asset catalogs")
		return nil, func() {}, false
	}
	id, err := h.resolveWorkspaceID(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, 404, "Not Found", "workspace not found")
		return nil, func() {}, false
	}
	ws, release, ok := h.registry.AcquireByID(id)
	if !ok {
		writeError(w, 404, "Not Found", "workspace not found")
	}
	return ws, release, ok
}
func stacError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := stac.Message(r, err, "internal error; see server logs")
	writeError(w, status, http.StatusText(status), message)
}
func readSTACJSON(r *http.Request, v any) error {
	limited := &io.LimitedReader{R: r.Body, N: (8 << 20) + 1}
	dec := json.NewDecoder(limited)
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	if limited.N == 0 {
		return fmt.Errorf("metadata record exceeds 8 MiB")
	}
	return nil
}
func readSTACDocument(r *http.Request) (stacmodel.Document, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 8<<20 {
		return nil, fmt.Errorf("metadata record exceeds 8 MiB")
	}
	return stacmodel.Decode(raw)
}
func (h *handler) getSTACSettings(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	settings := store.STACSettings{}
	if ws.Settings != nil {
		settings = ws.Settings.STAC
	}
	writeJSON(w, 200, settings)
}
func (h *handler) putSTACSettings(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	var settings store.STACSettings
	if err := readSTACJSON(r, &settings); err != nil {
		stacError(w, r, err)
		return
	}
	if err := h.registry.UpdateSTACSettings(r.Context(), ws.ID, settings); err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, settings)
}
func (h *handler) stacResources(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	resources, err := h.stac.Adapter.Resources(r.Context(), ws)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"resources": resources})
}
func (h *handler) stacCollections(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	collections, err := h.stac.Catalog.Collections(r.Context(), ws.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"collections": collections})
}
func (h *handler) stacCreateCollection(w http.ResponseWriter, r *http.Request) {
	h.stacSaveCollection(w, r, true)
}
func (h *handler) stacUpdateCollection(w http.ResponseWriter, r *http.Request) {
	h.stacSaveCollection(w, r, false)
}
func (h *handler) stacSaveCollection(w http.ResponseWriter, r *http.Request, create bool) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	var c staccatalog.Collection
	if err := readSTACJSON(r, &c); err != nil {
		stacError(w, r, err)
		return
	}
	if c.Binding != nil {
		stacError(w, r, fmt.Errorf("publish a source through the bindings workflow"))
		return
	}
	if !create && c.Document.String("id") != chi.URLParam(r, "stacCollection") {
		stacError(w, r, fmt.Errorf("Collection id cannot be changed"))
		return
	}
	if err := h.stac.Catalog.PutCollection(r.Context(), ws.ID, c, create); err != nil {
		stacError(w, r, err)
		return
	}
	cptr, err := h.stac.Catalog.GetCollection(r.Context(), ws.ID, c.Document.String("id"))
	if err != nil {
		stacError(w, r, err)
		return
	}
	status := 200
	if create {
		status = 201
	}
	writeJSON(w, status, cptr)
}
func (h *handler) stacGetCollection(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	c, err := h.stac.Catalog.GetCollection(r.Context(), ws.ID, chi.URLParam(r, "stacCollection"))
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}
func (h *handler) stacDeleteCollection(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	if err := h.stac.Catalog.DeleteCollection(r.Context(), ws.ID, chi.URLParam(r, "stacCollection")); err != nil {
		stacError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (h *handler) stacItems(w http.ResponseWriter, r *http.Request) { h.stacSearch(w, r) }
func (h *handler) stacSearch(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	q, err := stac.DecodeSearch(r)
	if err != nil {
		stacError(w, r, err)
		return
	}
	cols, err := h.stac.Catalog.Collections(r.Context(), ws.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	visible := []string{}
	for _, c := range cols {
		visible = append(visible, c.Document.String("id"))
	}
	if collection := chi.URLParam(r, "stacCollection"); collection != "" {
		q.Collections = []string{collection}
	}
	page, err := h.stac.Catalog.Search(r.Context(), ws.ID, q, visible)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": page.Items, "next_token": page.Next})
}
func (h *handler) stacGetItem(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	item, err := h.stac.Catalog.GetItem(r.Context(), ws.ID, chi.URLParam(r, "stacCollection"), chi.URLParam(r, "stacItem"))
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, item)
}
func (h *handler) stacPutItem(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	doc, err := readSTACDocument(r)
	if err != nil {
		stacError(w, r, err)
		return
	}
	collection := chi.URLParam(r, "stacCollection")
	if doc.String("collection") != collection {
		stacError(w, r, fmt.Errorf("Item Collection must match the URL"))
		return
	}
	if id := chi.URLParam(r, "stacItem"); id != "" && id != doc.String("id") {
		stacError(w, r, fmt.Errorf("Item ID must match the URL"))
		return
	}
	links, _ := doc["links"].([]any)
	hasCollection := false
	for _, v := range links {
		if l, ok := v.(map[string]any); ok && l["rel"] == "collection" {
			hasCollection = true
		}
	}
	if !hasCollection {
		doc["links"] = append(links, stacmodel.Document{"rel": "collection", "href": h.stac.Adapter.Base(ws) + "/stac/collections/" + url.PathEscape(collection)})
	}
	if err = h.stac.Catalog.PutItem(r.Context(), ws.ID, collection, doc, r.Method == http.MethodPut); err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, doc)
}
func (h *handler) stacDeleteItem(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	if err := h.stac.Catalog.DeleteItem(r.Context(), ws.ID, chi.URLParam(r, "stacCollection"), chi.URLParam(r, "stacItem")); err != nil {
		stacError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

type stacBindingRequest struct {
	CollectionID string              `json:"collection_id"`
	Revision     int64               `json:"revision"`
	Binding      staccatalog.Binding `json:"binding"`
}

func (h *handler) bindingCollection(r *http.Request, ws string) (*staccatalog.Collection, error) {
	cols, err := h.stac.Catalog.Collections(r.Context(), ws)
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		if c.Binding != nil && c.Binding.ID == chi.URLParam(r, "binding") {
			return c, nil
		}
	}
	return nil, staccatalog.ErrNotFound
}
func (h *handler) stacBindings(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	cols, err := h.stac.Catalog.Collections(r.Context(), ws.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	out := []stacBindingRequest{}
	for _, c := range cols {
		if c.Binding != nil {
			out = append(out, stacBindingRequest{c.Document.String("id"), c.Revision, *c.Binding})
		}
	}
	writeJSON(w, 200, map[string]any{"bindings": out})
}
func (h *handler) stacGetBinding(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	c, err := h.bindingCollection(r, ws.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, stacBindingRequest{c.Document.String("id"), c.Revision, *c.Binding})
}
func (h *handler) stacPutBinding(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	var input stacBindingRequest
	if err := readSTACJSON(r, &input); err != nil {
		stacError(w, r, err)
		return
	}
	c, err := h.stac.Catalog.GetCollection(r.Context(), ws.ID, input.CollectionID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	if c.Binding != nil {
		if c.Binding.ID != chi.URLParam(r, "binding") {
			stacError(w, r, staccatalog.ErrConflict)
			return
		}
		input.Binding.ID = c.Binding.ID
	} else {
		if c.ItemCount > 0 {
			stacError(w, r, fmt.Errorf("use an empty Collection for a new source binding"))
			return
		}
		input.Binding.ID = uuid.NewString()
	}
	if input.Binding.RefreshIntervalSec == 0 {
		input.Binding.RefreshIntervalSec = h.cfg.STAC.RefreshIntervalSec
	}
	if err = h.stac.Adapter.Validate(r.Context(), ws, &input.Binding); err != nil {
		stacError(w, r, err)
		return
	}
	if err = h.stac.Catalog.PutBinding(r.Context(), ws.ID, input.CollectionID, &input.Binding, input.Revision); err != nil {
		stacError(w, r, err)
		return
	}
	job, err := h.stac.Catalog.CreateJob(r.Context(), ws.ID, input.CollectionID, "refresh", "queued", nil)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 202, job)
}
func (h *handler) stacDeleteBinding(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	c, err := h.bindingCollection(r, ws.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	// Removing a binding removes its derived Collection so an old source policy
	// cannot silently become an independent publication with broader visibility.
	if err = h.stac.Catalog.DeleteCollection(r.Context(), ws.ID, c.Document.String("id")); err != nil {
		stacError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (h *handler) stacPreviewBinding(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	var c *staccatalog.Collection
	var err error
	if chi.URLParam(r, "binding") != "" {
		c, err = h.bindingCollection(r, ws.ID)
	} else {
		var input stacBindingRequest
		if err = readSTACJSON(r, &input); err == nil {
			c, err = h.stac.Catalog.GetCollection(r.Context(), ws.ID, input.CollectionID)
			if err == nil {
				input.Binding.ID = "preview"
				c.Binding = &input.Binding
			}
		}
	}
	if err != nil {
		stacError(w, r, err)
		return
	}
	sample := []stacmodel.Document{}
	stop := errors.New("preview complete")
	document, err := h.stac.Adapter.Scan(r.Context(), ws, c, h.cfg.STAC.MaxItems, func(d stacmodel.Document, _ []staccatalog.LocalAsset) error {
		sample = append(sample, d)
		if len(sample) == 10 {
			return stop
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		stacError(w, r, err)
		return
	}
	if document == nil {
		document = c.Document
	}
	writeJSON(w, 200, map[string]any{"collection": document, "items": sample, "sample_only": true, "message": "The complete source is validated before publication. Unknown extension schemas are preserved but are not fetched or validated."})
}
func (h *handler) stacRefresh(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	c, err := h.bindingCollection(r, ws.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	j, err := h.stac.Catalog.CreateJob(r.Context(), ws.ID, c.Document.String("id"), "refresh", "queued", nil)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 202, j)
}

func (h *handler) stacImport(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	collection := r.URL.Query().Get("collection_id")
	j, err := h.stac.Catalog.CreateJob(r.Context(), ws.ID, collection, "import", "uploading", nil)
	if err != nil {
		stacError(w, r, err)
		return
	}
	err = h.stac.Catalog.Import(r.Context(), j, http.MaxBytesReader(w, r.Body, h.cfg.STAC.MaxUploadBytes), r.URL.Query().Get("base_url"))
	if err != nil {
		_ = h.stac.Catalog.SetJobStatus(context.WithoutCancel(r.Context()), ws.ID, j.ID, "failed", err.Error())
		status, message := stac.Message(r, err, "internal error; see server logs")
		writeJSON(w, status, map[string]any{"code": status, "message": message, "job_id": j.ID})
		return
	}
	j, err = h.stac.Catalog.GetJob(r.Context(), ws.ID, j.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 201, j)
}
func (h *handler) stacImportPreview(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	items, err := h.stac.Catalog.PreviewJob(r.Context(), ws.ID, chi.URLParam(r, "job"))
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "extension_validation": "Core schemas validated; unknown extensions preserved without remote schema retrieval."})
}
func (h *handler) stacImportPublish(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	j, err := h.stac.Catalog.GetJob(r.Context(), ws.ID, chi.URLParam(r, "job"))
	if err != nil {
		stacError(w, r, err)
		return
	}
	var req struct {
		Upsert bool `json:"upsert"`
	}
	if err = readSTACJSON(r, &req); err != nil {
		stacError(w, r, err)
		return
	}
	if err = h.stac.Catalog.Publish(r.Context(), ws.ID, j.ID, req.Upsert, j.Request.Object("collection_document")); err != nil {
		stacError(w, r, err)
		return
	}
	j, err = h.stac.Catalog.GetJob(r.Context(), ws.ID, j.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, j)
}
func (h *handler) stacJobs(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	jobs, err := h.stac.Catalog.Jobs(r.Context(), ws.ID)
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": jobs})
}
func (h *handler) stacJob(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	j, err := h.stac.Catalog.GetJob(r.Context(), ws.ID, chi.URLParam(r, "job"))
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 200, j)
}
func (h *handler) stacCancelJob(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	if err := h.stac.Catalog.Cancel(r.Context(), ws.ID, chi.URLParam(r, "job")); err != nil {
		stacError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (h *handler) stacRetryJob(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	j, err := h.stac.Catalog.Retry(r.Context(), ws.ID, chi.URLParam(r, "job"))
	if err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 202, j)
}

func (h *handler) stacBindAsset(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	var input struct {
		staccatalog.LocalAsset
		WholeFileAuthorized bool `json:"whole_file_authorized"`
	}
	if err := readSTACJSON(r, &input); err != nil {
		stacError(w, r, err)
		return
	}
	if !input.WholeFileAuthorized {
		stacError(w, r, fmt.Errorf("explicit whole-file authorization is required"))
		return
	}
	principal, _ := identity.FromContext(r.Context())
	if principal == nil || !principal.IsSuperAdmin() {
		c, err := h.stac.Catalog.GetCollection(r.Context(), ws.ID, input.CollectionID)
		if err != nil {
			stacError(w, r, err)
			return
		}
		s, l, cov := stacsource.Resolve(ws, c.Binding)
		eligible := s != nil && l == nil && cov != nil && s.Type == store.ServiceTypeRasterFile && len(s.Coverages) == 1
		var cfg store.RasterFileConnectionInfo
		if eligible {
			_ = json.Unmarshal(s.ConnectionInfo, &cfg)
			eligible = cfg.Path == input.Path && (strings.EqualFold(filepath.Ext(cfg.Path), ".tif") || strings.EqualFold(filepath.Ext(cfg.Path), ".tiff"))
		}
		if !eligible {
			writeError(w, 403, "Forbidden", "a super administrator must authorize arbitrary files or multi-resource containers")
			return
		}
	}
	if pathpolicy.IsRemote(input.Path) {
		stacError(w, r, fmt.Errorf("use an external asset URL for remote data"))
		return
	}
	lease, err := pathpolicy.Acquire(r.Context(), input.Path)
	if err != nil {
		stacError(w, r, err)
		return
	}
	defer lease.Release()
	info, err := os.Stat(lease.Path)
	if err != nil || !info.Mode().IsRegular() {
		stacError(w, r, fmt.Errorf("asset must be an existing regular file"))
		return
	}
	if strings.HasSuffix(strings.ToLower(lease.Path), ".duckdb") || strings.HasSuffix(strings.ToLower(lease.Path), ".db") {
		stacError(w, r, fmt.Errorf("database containers cannot be exposed as local assets"))
		return
	}
	for _, protected := range []string{h.cfg.Store.Path, h.cfg.STAC.DatabasePath, h.cfg.MosaicCatalog.DatabasePath, h.cfg.Audit.DatabasePath, h.cfg.PersistentCache.DatabasePath} {
		if protectedInfo, statErr := os.Stat(protected); statErr == nil && os.SameFile(info, protectedInfo) {
			stacError(w, r, fmt.Errorf("server databases cannot be exposed as local assets"))
			return
		}
	}
	if input.ItemID != "" {
		if _, err = h.stac.Catalog.GetItem(r.Context(), ws.ID, input.CollectionID, input.ItemID); err != nil {
			stacError(w, r, err)
			return
		}
	}
	if input.MediaType == "" {
		input.MediaType = "application/octet-stream"
	}
	// Store the path as supplied: the allowlist matches it lexically, and every
	// download re-validates and re-resolves it through pathpolicy.Acquire.
	if err = h.stac.Catalog.BindLocalAsset(r.Context(), ws.ID, "", input.LocalAsset); err != nil {
		stacError(w, r, err)
		return
	}
	writeJSON(w, 201, input.LocalAsset)
}
func (h *handler) stacUnbindAsset(w http.ResponseWriter, r *http.Request) {
	ws, release, ok := h.stacWorkspace(w, r)
	if !ok {
		return
	}
	defer release()
	q := r.URL.Query()
	if err := h.stac.Catalog.DeleteLocalAsset(r.Context(), ws.ID, q.Get("collection_id"), q.Get("item_id"), q.Get("key")); err != nil {
		stacError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
