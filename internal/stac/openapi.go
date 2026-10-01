package stac

import (
	"encoding/json"
	"html"
	"net/http"

	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/workspace"
)

// OpenAPI describes the workspace root, with GET and POST search sharing the
// same parameter schema. No deployment-global STAC paths are advertised.
func OpenAPI(base string) stacmodel.Document {
	type M = map[string]any
	str := M{"type": "string"}
	obj := M{"type": "object", "additionalProperties": true}
	array := func(items any) M { return M{"type": "array", "items": items} }
	bbox := M{"type": "array", "items": M{"type": "number"}, "minItems": 4, "maxItems": 6}
	limit := M{"type": "integer", "minimum": 1, "maximum": 1000, "default": 100}
	searchProps := M{"bbox": bbox, "datetime": M{"type": "string", "description": "RFC 3339 instant or interval; .. denotes an open endpoint"}, "intersects": obj, "collections": array(str), "ids": array(str), "limit": limit, "token": str}
	param := func(name, in string, schema any, required bool) M {
		p := M{"name": name, "in": in, "required": required, "schema": schema}
		if in == "query" {
			p["style"] = "form"
			p["explode"] = false
		}
		return p
	}
	paths := M{}
	for _, path := range []string{"/", "/conformance", "/collections", "/collections/{collectionId}", "/collections/{collectionId}/items", "/collections/{collectionId}/items/{itemId}", "/search"} {
		media := "application/json"
		if path == "/search" || path == "/collections/{collectionId}/items" || path == "/collections/{collectionId}/items/{itemId}" {
			media = "application/geo+json"
		}
		params := []any{}
		if path == "/collections/{collectionId}" || path == "/collections/{collectionId}/items" || path == "/collections/{collectionId}/items/{itemId}" {
			params = append(params, param("collectionId", "path", str, true))
		}
		if path == "/collections/{collectionId}/items/{itemId}" {
			params = append(params, param("itemId", "path", str, true))
		}
		if path == "/search" {
			for _, key := range []string{"bbox", "datetime", "intersects", "collections", "ids", "limit", "token"} {
				p := param(key, "query", searchProps[key], false)
				if key == "collections" || key == "ids" || key == "bbox" {
					p["style"] = "form"
					p["explode"] = false
				}
				if key == "intersects" {
					p["schema"] = str
					p["description"] = "GeoJSON geometry serialized as JSON"
				}
				params = append(params, p)
			}
		}
		if path == "/collections/{collectionId}/items" {
			for _, key := range []string{"bbox", "datetime", "limit", "token"} {
				p := param(key, "query", searchProps[key], false)
				if key == "bbox" {
					p["style"] = "form"
					p["explode"] = false
				}
				params = append(params, p)
			}
		}
		if path == "/collections" {
			params = append(params, param("limit", "query", limit, false), param("after", "query", str, false))
		}
		response := M{"description": "Successful response", "content": M{media: M{"schema": obj}}}
		responses := M{"200": response, "400": M{"description": "Invalid request"}, "401": M{"description": "Authentication required"}, "403": M{"description": "Access denied"}, "404": M{"description": "Resource not found"}}
		operation := M{"responses": responses}
		if len(params) > 0 {
			operation["parameters"] = params
		}
		item := M{"get": operation}
		if path == "/search" {
			item["post"] = M{"requestBody": M{"required": true, "content": M{"application/json": M{"schema": M{"type": "object", "properties": searchProps}}}}, "responses": responses}
		}
		paths[path] = item
	}
	return stacmodel.Document{"openapi": "3.0.3", "info": M{"title": "Workspace STAC API", "version": "1.0.0", "description": "STAC API Core, Collections, Features and Item Search. Items use STAC 1.1.0; imports also support 1.0.0."}, "servers": []M{{"url": base}}, "paths": paths}
}
func (h *Handler) api(w http.ResponseWriter, r *http.Request) {
	ws, _ := workspace.FromContext(r.Context())
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.0")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(OpenAPI(h.base(ws)))
}
func (h *Handler) apiHTML(w http.ResponseWriter, r *http.Request) {
	ws, _ := workspace.FromContext(r.Context())
	base := html.EscapeString(h.base(ws))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Workspace STAC API</title><main><h1>Workspace STAC API</h1><p>Browse Collections and search spatiotemporal asset metadata using STAC API 1.0.0.</p><ul><li><a href="` + base + `/api">OpenAPI definition</a></li><li><a href="` + base + `/collections">Collections</a></li><li><a href="` + base + `/search">Item Search</a></li><li><a href="` + base + `/conformance">Conformance classes</a></li></ul><p>Search supports bbox, datetime, intersects, collections, ids and limit. POST the same parameters as JSON to /search. Follow next links for pagination. The default page contains up to 100 Items; the maximum is 1000.</p></main></html>`))
}
