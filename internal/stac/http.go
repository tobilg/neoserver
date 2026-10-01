// Package stac implements workspace-scoped STAC API 1.0.0.
package stac

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/httputil"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/stacsource"
	"github.com/tobilg/neoserver/internal/workspace"
)

var Conformance = []string{
	"https://api.stacspec.org/v1.0.0/core",
	"https://api.stacspec.org/v1.0.0/collections",
	"https://api.stacspec.org/v1.0.0/ogcapi-features",
	"https://api.stacspec.org/v1.0.0/item-search",
	"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core",
	"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson",
	"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/oas30",
}

type Handler struct {
	Config  conf.Config
	Manager *stacsource.Manager
}

func Register(r chi.Router, cfg conf.Config, manager *stacsource.Manager) {
	h := &Handler{cfg, manager}
	r.Use(h.authorize)
	get := func(path string, handler http.HandlerFunc, parameters ...string) {
		r.Get(path, withQueryParameters(handler, parameters...))
	}
	get("/", h.landing)
	get("/conformance", func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteJSON(w, 200, map[string]any{"conformsTo": Conformance})
	})
	get("/api", h.api)
	get("/api.html", h.apiHTML)
	get("/collections", h.collections, "limit", "after")
	get("/collections/{collectionId}", h.collection)
	get("/collections/{collectionId}/items", h.search, "bbox", "datetime", "limit", "token")
	get("/collections/{collectionId}/items/{itemId}", h.item)
	get("/search", h.search, "bbox", "datetime", "intersects", "collections", "ids", "limit", "token")
	r.Post("/search", withQueryParameters(h.search))
	for _, path := range []string{"/collections/{collectionId}/assets/{assetKey}", "/collections/{collectionId}/items/{itemId}/assets/{assetKey}"} {
		get(path, h.asset)
		r.Head(path, withQueryParameters(h.asset))
	}
}

// OGC Features requires rejecting query parameters absent from the API
// definition. Validate per route so a search-only parameter cannot silently
// change meaning on a Collection or a single Item.
func withQueryParameters(next http.HandlerFunc, names ...string) http.HandlerFunc {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			Error(w, 400, "invalid query string")
			return
		}
		for name, values := range values {
			if !allowed[name] || len(values) != 1 {
				Error(w, 400, "unknown or repeated query parameter: "+name)
				return
			}
		}
		next(w, r)
	}
}
func (h *Handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, ok := workspace.FromContext(r.Context())
		if !ok || ws.Settings == nil || !ws.Settings.STAC.Enabled || h.Manager == nil {
			Error(w, 404, "STAC is not enabled in this workspace")
			return
		}
		if !ws.Settings.STAC.Public {
			principal, ok := identity.FromContext(r.Context())
			if !ok || principal == nil {
				Error(w, 401, "authentication required")
				return
			}
			if !principal.IsSuperAdmin() && !rbac.HasOperationGrant(r, ws.ID, "stac", "read") {
				Error(w, 403, "STAC read access is required")
				return
			}
		}
		w.Header().Set("Cache-Control", "private, no-store")
		next.ServeHTTP(w, r)
	})
}
func Role(r *http.Request, ws *workspace.Workspace) string {
	if id, ok := identity.FromContext(r.Context()); ok && id != nil {
		if id.IsSuperAdmin() {
			return "super_admin"
		}
		return id.GetWorkspaceRole(ws.ID)
	}
	return ""
}
func (h *Handler) base(ws *workspace.Workspace) string { return h.Manager.Adapter.Base(ws) + "/stac" }
func link(rel, href, typ string) stacmodel.Document {
	return stacmodel.Document{"rel": rel, "href": href, "type": typ}
}
func (h *Handler) landing(w http.ResponseWriter, r *http.Request) {
	ws, _ := workspace.FromContext(r.Context())
	base := h.base(ws)
	s := ws.Settings.STAC
	title := s.Title
	if title == "" {
		title = ws.Name + " STAC"
	}
	description := s.Description
	if description == "" {
		description = "Spatiotemporal assets published in " + ws.Name
	}
	links := []any{link("self", base+"/", "application/json"), link("root", base+"/", "application/json"), link("data", base+"/collections", "application/json"), link("conformance", base+"/conformance", "application/json"), link("service-desc", base+"/api", "application/vnd.oai.openapi+json;version=3.0"), link("service-doc", base+"/api.html", "text/html")}
	for _, method := range []string{"GET", "POST"} {
		l := link("search", base+"/search", "application/geo+json")
		l["method"] = method
		links = append(links, l)
	}
	collections, err := h.Manager.Catalog.Collections(r.Context(), ws.ID)
	if err != nil {
		Error(w, 500, "unable to list Collections")
		return
	}
	for _, c := range collections {
		if stacsource.Visible(ws, c, Role(r, ws)) {
			links = append(links, link("child", base+"/collections/"+url.PathEscape(c.Document.String("id")), "application/json"))
		}
	}
	httputil.WriteJSON(w, 200, stacmodel.Document{"type": "Catalog", "stac_version": "1.1.0", "id": ws.Name, "title": title, "description": description, "conformsTo": Conformance, "links": links})
}
func (h *Handler) decorate(r *http.Request, ws *workspace.Workspace, doc stacmodel.Document) (stacmodel.Document, error) {
	collection, item := doc.String("id"), ""
	if doc.String("type") == "Feature" {
		collection, item = doc.String("collection"), doc.String("id")
	}
	assets, err := h.Manager.Catalog.LocalAssets(r.Context(), ws.ID, collection, item)
	if err != nil {
		return nil, err
	}
	return h.decorateAssets(ws, doc, assets), nil
}
func (h *Handler) decorateAssets(ws *workspace.Workspace, doc stacmodel.Document, assets []staccatalog.LocalAsset) stacmodel.Document {
	d := doc.Clone()
	base := h.base(ws)
	collection := d.String("id")
	item := ""
	typ := "application/json"
	if d.String("type") == "Feature" {
		collection = d.String("collection")
		item = d.String("id")
		typ = "application/geo+json"
	}
	location := base + "/collections/" + url.PathEscape(collection)
	collectionURL := location
	if item != "" {
		location += "/items/" + url.PathEscape(item)
	}
	links := []any{}
	if old, ok := d["links"].([]any); ok {
		for _, v := range old {
			l, ok := v.(map[string]any)
			if !ok {
				continue
			}
			rel, _ := l["rel"].(string)
			switch rel {
			case "self", "root", "parent", "collection", "items":
				continue
			}
			href, _ := l["href"].(string)
			u, err := url.Parse(href)
			if err == nil && stacmodel.PublicURL(href) == nil && (u.Scheme == "https" || u.Scheme == "http") {
				links = append(links, l)
			}
		}
	}
	links = append(links, link("self", location, typ), link("root", base+"/", "application/json"))
	if item != "" {
		links = append(links, link("parent", collectionURL, "application/json"), link("collection", collectionURL, "application/json"))
	} else {
		links = append(links, link("parent", base+"/", "application/json"), link("items", location+"/items", "application/geo+json"))
		d["itemType"] = "feature"
	}
	d["links"] = links
	if len(assets) > 0 {
		a := d.Object("assets")
		if a == nil {
			a = stacmodel.Document{}
		}
		for _, v := range assets {
			a[v.Key] = stacmodel.Document{"href": location + "/assets/" + url.PathEscape(v.Key), "type": v.MediaType, "roles": []string{"data"}}
		}
		d["assets"] = a
	}
	return d
}
func (h *Handler) visibleCollection(w http.ResponseWriter, r *http.Request) (*workspace.Workspace, *staccatalog.Collection, bool) {
	ws, _ := workspace.FromContext(r.Context())
	c, err := h.Manager.Catalog.GetCollection(r.Context(), ws.ID, chi.URLParam(r, "collectionId"))
	if err != nil || !stacsource.Visible(ws, c, Role(r, ws)) {
		Error(w, 404, "Collection not found")
		return ws, nil, false
	}
	return ws, c, true
}
func (h *Handler) collection(w http.ResponseWriter, r *http.Request) {
	ws, c, ok := h.visibleCollection(w, r)
	if !ok {
		return
	}
	doc, err := h.decorate(r, ws, c.Document)
	if err != nil {
		Error(w, 500, "unable to read Collection")
		return
	}
	httputil.WriteJSON(w, 200, doc)
}
func (h *Handler) collections(w http.ResponseWriter, r *http.Request) {
	ws, _ := workspace.FromContext(r.Context())
	items, err := h.Manager.Catalog.Collections(r.Context(), ws.ID)
	if err != nil {
		Error(w, 500, "unable to list Collections")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit <= 0 {
			Error(w, 400, "limit must be a positive integer")
			return
		}
		if limit > 1000 {
			limit = 1000
		}
	}
	after := r.URL.Query().Get("after")
	docs := []stacmodel.Document{}
	next := ""
	for _, c := range items {
		if c.Document.String("id") <= after || !stacsource.Visible(ws, c, Role(r, ws)) {
			continue
		}
		if len(docs) == limit {
			next = docs[len(docs)-1].String("id")
			break
		}
		d, err := h.decorate(r, ws, c.Document)
		if err != nil {
			Error(w, 500, "unable to read Collection")
			return
		}
		docs = append(docs, d)
	}
	base := h.base(ws)
	links := []any{link("self", base+"/collections"+querySuffix(r.URL.Query()), "application/json"), link("root", base+"/", "application/json"), link("parent", base+"/", "application/json")}
	if next != "" {
		q := r.URL.Query()
		q.Set("after", next)
		links = append(links, link("next", base+"/collections?"+q.Encode(), "application/json"))
	}
	httputil.WriteJSON(w, 200, stacmodel.Document{"collections": docs, "links": links})
}
func (h *Handler) item(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.visibleCollection(w, r)
	if !ok {
		return
	}
	doc, err := h.Manager.Catalog.GetItem(r.Context(), ws.ID, chi.URLParam(r, "collectionId"), chi.URLParam(r, "itemId"))
	if err != nil {
		Error(w, 404, "Item not found")
		return
	}
	doc, err = h.decorate(r, ws, doc)
	if err != nil {
		Error(w, 500, "unable to read Item")
		return
	}
	httputil.WriteGeoJSON(w, 200, doc)
}
func DecodeSearch(r *http.Request) (staccatalog.Search, error) {
	var q staccatalog.Search
	if r.Method == http.MethodPost {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			return q, fmt.Errorf("Content-Type must be application/json")
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(raw) > 1<<20 {
			return q, fmt.Errorf("search body exceeds 1 MiB")
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return q, fmt.Errorf("expected a search object")
		}
		for _, key := range []string{"limit", "bbox", "intersects", "datetime", "ids", "collections", "token"} {
			if value, present := fields[key]; present && string(value) == "null" {
				return q, fmt.Errorf("%s cannot be null", key)
			}
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err = dec.Decode(&q); err != nil {
			return q, fmt.Errorf("invalid search JSON: %w", err)
		}
		if _, present := fields["limit"]; present && q.Limit <= 0 {
			return q, fmt.Errorf("limit must be a positive integer")
		}
		if _, present := fields["bbox"]; present && len(q.BBox) == 0 {
			return q, fmt.Errorf("bbox must contain four or six coordinates")
		}
		if _, present := fields["datetime"]; present && q.Datetime == "" {
			return q, fmt.Errorf("datetime must not be empty")
		}
		if _, present := fields["collections"]; present && len(q.Collections) == 0 {
			return q, fmt.Errorf("collections must not be empty")
		}
		if _, present := fields["ids"]; present && len(q.IDs) == 0 {
			return q, fmt.Errorf("ids must not be empty")
		}
		var extra any
		if err = dec.Decode(&extra); err != io.EOF {
			return q, fmt.Errorf("expected one search object")
		}
	} else {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return q, fmt.Errorf("invalid query string")
		}
		for k, v := range values {
			if len(v) > 1 {
				return q, fmt.Errorf("duplicate parameter %s", k)
			}
		}
		if v, ok := values["limit"]; ok {
			n, err := strconv.Atoi(v[0])
			if err != nil || n <= 0 {
				return q, fmt.Errorf("limit must be a positive integer")
			}
			q.Limit = n
		}
		if v, ok := values["bbox"]; ok {
			for _, s := range strings.Split(v[0], ",") {
				n, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return q, fmt.Errorf("invalid bbox")
				}
				q.BBox = append(q.BBox, n)
			}
		}
		if v, ok := values["intersects"]; ok {
			q.Intersects, err = stacmodel.Decode([]byte(v[0]))
			if err != nil {
				return q, fmt.Errorf("invalid intersects geometry")
			}
		}
		if v, ok := values["collections"]; ok {
			q.Collections = strings.Split(v[0], ",")
		}
		if v, ok := values["ids"]; ok {
			q.IDs = strings.Split(v[0], ",")
		}
		if v, present := values["datetime"]; present && v[0] == "" {
			return q, fmt.Errorf("datetime must not be empty")
		}
		q.Datetime = values.Get("datetime")
		q.Token = values.Get("token")
	}
	return q, q.Validate()
}
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	ws, _ := workspace.FromContext(r.Context())
	q, err := DecodeSearch(r)
	if err != nil {
		Error(w, 400, err.Error())
		return
	}
	endpoint := h.base(ws) + "/search"
	if id := chi.URLParam(r, "collectionId"); id != "" {
		if _, _, ok := h.visibleCollection(w, r); !ok {
			return
		}
		q.Collections = []string{id}
		endpoint = h.base(ws) + "/collections/" + url.PathEscape(id) + "/items"
	}
	collections, err := h.Manager.Catalog.Collections(r.Context(), ws.ID)
	if err != nil {
		Error(w, 500, "unable to search Items")
		return
	}
	visible := []string{}
	for _, c := range collections {
		if stacsource.Visible(ws, c, Role(r, ws)) {
			visible = append(visible, c.Document.String("id"))
		}
	}
	page, err := h.Manager.Catalog.Search(r.Context(), ws.ID, q, visible)
	if err != nil {
		status, message := Message(r, err, "unable to search Items")
		Error(w, status, message)
		return
	}
	assets, err := h.Manager.Catalog.LocalAssetsForDocuments(r.Context(), ws.ID, page.Items)
	if err != nil {
		Error(w, 500, "unable to read Item assets")
		return
	}
	for i, d := range page.Items {
		page.Items[i] = h.decorateAssets(ws, d, assets[d.String("collection")+"\x00"+d.String("id")])
	}
	self := link("self", endpoint+querySuffix(r.URL.Query()), "application/geo+json")
	if r.Method == http.MethodPost {
		self["method"] = "POST"
		self["body"] = q
		self["merge"] = false
	}
	links := []any{link("root", h.base(ws)+"/", "application/json"), self}
	if page.Next != "" {
		next := link("next", endpoint, "application/geo+json")
		if r.Method == http.MethodPost {
			next["method"] = "POST"
			next["body"] = stacmodel.Document{"token": page.Next}
			next["merge"] = true
		} else {
			values := r.URL.Query()
			values.Set("token", page.Next)
			next["href"] = endpoint + "?" + values.Encode()
		}
		links = append(links, next)
	}
	httputil.WriteGeoJSON(w, 200, stacmodel.Document{"type": "FeatureCollection", "features": page.Items, "links": links, "numberReturned": len(page.Items)})
}
func querySuffix(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}
func (h *Handler) asset(w http.ResponseWriter, r *http.Request) {
	ws, c, ok := h.visibleCollection(w, r)
	if !ok {
		return
	}
	collection := c.Document.String("id")
	item := chi.URLParam(r, "itemId")
	if item != "" {
		if _, err := h.Manager.Catalog.GetItem(r.Context(), ws.ID, collection, item); err != nil {
			Error(w, 404, "Item not found")
			return
		}
	}
	a, err := h.Manager.Catalog.LocalAsset(r.Context(), ws.ID, collection, item, chi.URLParam(r, "assetKey"))
	if err != nil {
		Error(w, 404, "asset not found")
		return
	}
	lease, err := pathpolicy.Acquire(r.Context(), a.Path)
	if err != nil || lease == nil {
		Error(w, 404, "asset is unavailable")
		return
	}
	defer lease.Release()
	if pathpolicy.IsRemote(lease.Path) {
		Error(w, 404, "asset is not a local file")
		return
	}
	file, err := os.Open(lease.Path)
	if err != nil {
		Error(w, 404, "asset is unavailable")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		Error(w, 404, "asset is unavailable")
		return
	}
	w.Header().Set("Content-Type", a.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(lease.Path)}))
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
func Error(w http.ResponseWriter, status int, message string) {
	httputil.WriteJSON(w, status, stacmodel.Document{"code": http.StatusText(status), "description": message})
}

// Status maps a catalog error to an HTTP status. Database failures other than
// rejected input, and cancelled or timed-out work, are server errors. Other
// errors are validation failures raised by the catalog and its models.
func Status(err error) int {
	switch {
	case errors.Is(err, staccatalog.ErrNotFound):
		return 404
	case errors.Is(err, staccatalog.ErrConflict):
		return 409
	case isInternal(err):
		return 500
	}
	return 400
}
func isInternal(err error) bool {
	var dbErr *duckdb.Error
	if errors.As(err, &dbErr) {
		return !rejectedInput(dbErr)
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) || errors.Is(err, driver.ErrBadConn)
}
func rejectedInput(err *duckdb.Error) bool {
	switch err.Type {
	case duckdb.ErrorTypeConstraint, duckdb.ErrorTypeInvalidInput, duckdb.ErrorTypeConversion, duckdb.ErrorTypeOutOfRange:
		return true
	}
	return false
}

// Message returns the client-facing text for a failed request. Internal
// failures are logged and replaced by a generic message, and database messages
// are never shown to clients.
func Message(r *http.Request, err error, fallback string) (int, string) {
	status := Status(err)
	if status == 500 {
		slog.Default().ErrorContext(r.Context(), "STAC request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		return status, fallback
	}
	var dbErr *duckdb.Error
	if errors.As(err, &dbErr) {
		return status, "the request was rejected as invalid"
	}
	return status, err.Error()
}
