package mgmt

import (
	"encoding/json"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

func addSTACOpenAPI(schemas openapi3.Schemas, paths *openapi3.Paths) {
	type M = map[string]any
	str := M{"type": "string"}
	boolean := M{"type": "boolean"}
	integer := M{"type": "integer", "format": "int64"}
	document := M{"type": "object", "additionalProperties": true}
	ref := func(name string) M { return M{"$ref": "#/components/schemas/" + name} }
	array := func(v any) M { return M{"type": "array", "items": v} }
	object := func(props M, required ...string) M {
		return M{"type": "object", "properties": props, "required": required}
	}
	definitions := map[string]M{
		"STACDocument":       document,
		"STACSettings":       object(M{"enabled": boolean, "public": boolean, "title": str, "description": str}, "enabled", "public"),
		"STACValue":          object(M{"property": str, "constant": M{}}),
		"STACAssetMapping":   object(M{"href": ref("STACValue"), "type": str, "title": str, "roles": array(str)}, "href"),
		"STACMapping":        object(M{"id_property": str, "datetime": ref("STACValue"), "start_datetime": ref("STACValue"), "end_datetime": ref("STACValue"), "assets": M{"type": "object", "additionalProperties": ref("STACAssetMapping")}, "properties": M{"type": "object", "additionalProperties": ref("STACValue")}, "extensions": array(str)}),
		"STACBinding":        object(M{"id": str, "service_id": str, "resource_id": str, "resource_kind": M{"type": "string", "enum": []string{"layer", "coverage"}}, "mode": M{"type": "string", "enum": []string{"dataset", "mapped", "raster"}}, "mapping": ref("STACMapping"), "filter": str, "refresh_interval_sec": M{"type": "integer", "minimum": 60, "default": 900}}, "service_id", "resource_id", "resource_kind", "mode", "mapping", "refresh_interval_sec"),
		"STACCollection":     object(M{"document": ref("STACDocument"), "public": boolean, "allowed_roles": array(str), "binding": ref("STACBinding"), "revision": integer, "item_count": integer, "updated_at": str}, "document", "public", "revision", "item_count"),
		"STACBindingRequest": object(M{"collection_id": str, "revision": integer, "binding": ref("STACBinding")}, "collection_id", "revision", "binding"),
		"STACJob":            object(M{"id": str, "workspace_id": str, "collection_id": str, "kind": str, "status": str, "revision": integer, "processed": integer, "error": str, "created_at": str, "updated_at": str, "request": document}, "id", "workspace_id", "collection_id", "kind", "status", "revision", "processed", "created_at", "updated_at"),
		"STACSearch":         object(M{"collections": array(str), "ids": array(str), "bbox": array(M{"type": "number"}), "datetime": str, "intersects": document, "limit": M{"type": "integer", "minimum": 1, "default": 100, "maximum": 1000}, "token": str}),
		"STACPage":           object(M{"items": array(ref("STACDocument")), "next_token": str}, "items"),
		"STACPreview":        object(M{"collection": document, "items": array(ref("STACDocument")), "sample_only": boolean, "message": str, "extension_validation": str}, "items"),
		"STACResource":       object(M{"service_id": str, "id": str, "public_id": str, "kind": str, "provider": str, "title": str, "modes": array(str), "properties": array(object(M{"name": str, "type": str, "json_type": str}, "name")), "id_property": str}, "service_id", "id", "public_id", "kind", "provider", "title", "modes"),
		"STACLocalAsset":     object(M{"collection_id": str, "item_id": str, "key": str, "path": str, "media_type": str, "whole_file_authorized": boolean}, "collection_id", "key", "path", "whole_file_authorized"),
		"STACPublishImport":  object(M{"upsert": boolean}, "upsert"),
	}
	for name, v := range definitions {
		raw, _ := json.Marshal(v)
		var schema openapi3.Schema
		_ = json.Unmarshal(raw, &schema)
		schemas[name] = &openapi3.SchemaRef{Value: &schema}
	}
	list := func(field, model string) M { return object(M{field: array(ref(model))}, field) }
	type route struct {
		method, path, id, response, body string
		code                             string
		schema                           M
	}
	routes := []route{
		{"GET", "/settings/stac", "getSTACSettings", "STACSettings", "", "200", nil}, {"PUT", "/settings/stac", "putSTACSettings", "STACSettings", "STACSettings", "200", nil},
		{"GET", "/stac/resources", "stacListResources", "", "", "200", list("resources", "STACResource")},
		{"GET", "/stac/collections", "stacListCollections", "", "", "200", list("collections", "STACCollection")}, {"POST", "/stac/collections", "stacCreateCollection", "STACCollection", "STACCollection", "201", nil},
		{"GET", "/stac/collections/{stacCollection}", "stacGetCollection", "STACCollection", "", "200", nil}, {"PUT", "/stac/collections/{stacCollection}", "stacUpdateCollection", "STACCollection", "STACCollection", "200", nil}, {"DELETE", "/stac/collections/{stacCollection}", "stacDeleteCollection", "", "", "204", nil},
		{"GET", "/stac/collections/{stacCollection}/items", "stacListItems", "STACPage", "", "200", nil}, {"POST", "/stac/collections/{stacCollection}/items", "stacCreateItem", "STACDocument", "STACDocument", "200", nil},
		{"GET", "/stac/collections/{stacCollection}/items/{stacItem}", "stacGetItem", "STACDocument", "", "200", nil}, {"PUT", "/stac/collections/{stacCollection}/items/{stacItem}", "stacUpdateItem", "STACDocument", "STACDocument", "200", nil}, {"DELETE", "/stac/collections/{stacCollection}/items/{stacItem}", "stacDeleteItem", "", "", "204", nil},
		{"POST", "/stac/search", "stacSearch", "STACPage", "STACSearch", "200", nil},
		{"GET", "/stac/bindings", "stacListBindings", "", "", "200", list("bindings", "STACBindingRequest")}, {"POST", "/stac/bindings", "stacCreateBinding", "STACJob", "STACBindingRequest", "202", nil},
		{"POST", "/stac/bindings/preview", "stacPreviewNewBinding", "STACPreview", "STACBindingRequest", "200", nil},
		{"GET", "/stac/bindings/{binding}", "stacGetBinding", "STACBindingRequest", "", "200", nil}, {"PUT", "/stac/bindings/{binding}", "stacUpdateBinding", "STACJob", "STACBindingRequest", "202", nil}, {"DELETE", "/stac/bindings/{binding}", "stacDeleteBinding", "", "", "204", nil},
		{"POST", "/stac/bindings/{binding}/preview", "stacPreviewBinding", "STACPreview", "", "200", nil}, {"POST", "/stac/bindings/{binding}/refresh", "stacRefreshBinding", "STACJob", "", "202", nil},
		{"POST", "/stac/imports", "stacImport", "STACJob", "", "201", nil}, {"GET", "/stac/imports/{job}/preview", "stacPreviewImport", "STACPreview", "", "200", nil}, {"POST", "/stac/imports/{job}/publish", "stacPublishImport", "STACJob", "STACPublishImport", "200", nil},
		{"DELETE", "/stac/imports/{job}", "stacCancelImport", "", "", "204", nil}, {"POST", "/stac/imports/{job}/retry", "stacRetryImport", "STACJob", "", "202", nil},
		{"GET", "/stac/jobs", "stacListJobs", "", "", "200", list("jobs", "STACJob")}, {"GET", "/stac/jobs/{job}", "stacGetJob", "STACJob", "", "200", nil}, {"DELETE", "/stac/jobs/{job}", "stacCancelJob", "", "", "204", nil}, {"POST", "/stac/jobs/{job}/retry", "stacRetryJob", "STACJob", "", "202", nil},
		{"POST", "/stac/assets", "stacBindAsset", "STACLocalAsset", "STACLocalAsset", "201", nil}, {"DELETE", "/stac/assets", "stacUnbindAsset", "", "", "204", nil},
	}
	for _, r := range routes {
		path := "/workspaces/{workspace}" + r.path
		params := []any{}
		for _, part := range strings.Split(path, "/") {
			if strings.HasPrefix(part, "{") {
				params = append(params, M{"in": "path", "name": strings.Trim(part, "{}"), "required": true, "schema": str})
			}
		}
		if r.id == "stacImport" || r.id == "stacUnbindAsset" {
			keys := []string{"collection_id", "base_url"}
			if r.id == "stacUnbindAsset" {
				keys = []string{"collection_id", "item_id", "key"}
			}
			for _, key := range keys {
				params = append(params, M{"in": "query", "name": key, "required": key == "collection_id" || key == "key", "schema": str})
			}
		}
		if r.id == "stacListItems" {
			for _, key := range []string{"bbox", "datetime", "ids", "token"} {
				params = append(params, M{"in": "query", "name": key, "schema": str})
			}
			params = append(params, M{"in": "query", "name": "limit", "schema": integer})
		}
		response := M{"description": "Success"}
		if r.code != "204" {
			schema := r.schema
			if r.response != "" {
				schema = ref(r.response)
			}
			response["content"] = M{"application/json": M{"schema": schema}}
		}
		operation := M{"operationId": r.id, "tags": []string{"STAC"}, "parameters": params, "responses": M{r.code: response, "400": M{"description": "Invalid input"}, "401": M{"description": "Authentication required"}, "403": M{"description": "Workspace administration required"}, "404": M{"description": "Resource not found"}, "409": M{"description": "Revision or ID conflict"}, "413": M{"description": "Metadata upload exceeds resource limits"}, "503": M{"description": "STAC is disabled"}}}
		if r.body != "" {
			operation["requestBody"] = M{"required": true, "content": M{"application/json": M{"schema": ref(r.body)}}}
		}
		if r.id == "stacImport" {
			operation["requestBody"] = M{"required": true, "content": M{"application/octet-stream": M{"schema": M{"type": "string", "format": "binary"}}}}
		}
		raw, _ := json.Marshal(operation)
		var op openapi3.Operation
		_ = json.Unmarshal(raw, &op)
		entry := paths.Value(path)
		if entry == nil {
			entry = &openapi3.PathItem{}
			paths.Set(path, entry)
		}
		entry.SetOperation(r.method, &op)
	}
}
