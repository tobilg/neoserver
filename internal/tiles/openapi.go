package tiles

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/tobilg/neoserver/internal/workspace"
)

func tileSchemaRef(name string) *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Ref: "#/components/schemas/" + name}
}

func tileResponse(description, mediaType, schema string) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: tileStringPtr(description),
		Content:     openapi3.Content{mediaType: &openapi3.MediaType{Schema: tileSchemaRef(schema)}},
	}}
}

func tileResponses(ok *openapi3.ResponseRef) *openapi3.Responses {
	responses := openapi3.NewResponses()
	responses.Set("200", ok)
	responses.Set("400", tileResponse("Invalid request", MediaTypeJSON, "Error"))
	responses.Set("401", tileResponse("Authentication required", MediaTypeJSON, "Error"))
	responses.Set("403", tileResponse("Access denied", MediaTypeJSON, "Error"))
	responses.Set("404", tileResponse("Resource not found", MediaTypeJSON, "Error"))
	responses.Set("500", tileResponse("Server error", MediaTypeJSON, "Error"))
	return responses
}

func tilePathParameter(name, description string) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: "path", Required: true, Description: description,
		Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
	}}
}

func tileOperation(id, summary string, parameters openapi3.Parameters, response *openapi3.ResponseRef) *openapi3.Operation {
	return &openapi3.Operation{OperationID: id, Summary: summary, Parameters: parameters, Responses: tileResponses(response)}
}

func (h *workspaceHandler) buildWorkspaceOpenAPI(ws *workspace.Workspace, role string) *openapi3.T {
	object := func() *openapi3.SchemaRef { return &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()} }
	components := &openapi3.Components{Schemas: openapi3.Schemas{
		"LandingPage": object(), "Conformance": object(), "TileMatrixSets": object(),
		"TileMatrixSet": object(), "Collections": object(), "Collection": object(),
		"TileSets": object(), "TileSet": object(), "TileJSON": object(), "OpenAPI": object(),
		"Error": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
			"type": {Value: openapi3.NewStringSchema()}, "detail": {Value: openapi3.NewStringSchema()},
		}}},
		"Binary": {Value: openapi3.NewStringSchema().WithFormat("binary")},
	}}

	collectionID := tilePathParameter("collectionId", "Published collection identifier")
	tileMatrixSetID := tilePathParameter("tileMatrixSetId", "Tile matrix set identifier")
	tileMatrix := tilePathParameter("tileMatrix", "Tile matrix identifier")
	tileRow := tilePathParameter("tileRow", "Tile row")
	tileCol := tilePathParameter("tileCol", "Tile column")

	paths := openapi3.NewPaths()
	paths.Set("/", &openapi3.PathItem{Get: tileOperation("getTilesLandingPage", "Landing page", nil, tileResponse("Landing page", MediaTypeJSON, "LandingPage"))})
	paths.Set("/conformance", &openapi3.PathItem{Get: tileOperation("getTilesConformance", "Conformance declaration", nil, tileResponse("Conformance declaration", MediaTypeJSON, "Conformance"))})
	paths.Set("/api", &openapi3.PathItem{Get: tileOperation("getTilesOpenAPI", "OpenAPI definition", nil, tileResponse("OpenAPI definition", MediaTypeOpenAPI, "OpenAPI"))})
	paths.Set("/tileMatrixSets", &openapi3.PathItem{Get: tileOperation("getTileMatrixSets", "List tile matrix sets", nil, tileResponse("Tile matrix sets", MediaTypeJSON, "TileMatrixSets"))})
	paths.Set("/tileMatrixSets/{tileMatrixSetId}", &openapi3.PathItem{Get: tileOperation("getTileMatrixSet", "Get a tile matrix set", openapi3.Parameters{tileMatrixSetID}, tileResponse("Tile matrix set", MediaTypeJSON, "TileMatrixSet"))})
	paths.Set("/collections", &openapi3.PathItem{Get: tileOperation("getTileCollections", "List tile-enabled collections", nil, tileResponse("Collections", MediaTypeJSON, "Collections"))})
	paths.Set("/collections/{collectionId}", &openapi3.PathItem{Get: tileOperation("getTileCollection", "Describe a collection", openapi3.Parameters{collectionID}, tileResponse("Collection", MediaTypeJSON, "Collection"))})
	paths.Set("/collections/{collectionId}/tiles", &openapi3.PathItem{Get: tileOperation("getVectorTilesets", "List vector tilesets", openapi3.Parameters{collectionID}, tileResponse("Vector tilesets", MediaTypeJSON, "TileSets"))})
	paths.Set("/collections/{collectionId}/tiles/{tileMatrixSetId}", &openapi3.PathItem{Get: tileOperation("getVectorTileset", "Describe a vector tileset", openapi3.Parameters{collectionID, tileMatrixSetID}, tileResponse("Vector tileset", MediaTypeJSON, "TileSet"))})
	paths.Set("/collections/{collectionId}/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}", &openapi3.PathItem{Get: tileOperation("getVectorTile", "Get a vector tile", openapi3.Parameters{collectionID, tileMatrixSetID, tileMatrix, tileRow, tileCol}, tileResponse("Vector tile", MediaTypeMVT, "Binary"))})
	paths.Set("/collections/{collectionId}/map/tiles", &openapi3.PathItem{Get: tileOperation("getMapTilesets", "List map tilesets", openapi3.Parameters{collectionID}, tileResponse("Map tilesets", MediaTypeJSON, "TileSets"))})
	paths.Set("/collections/{collectionId}/map/tiles/{tileMatrixSetId}", &openapi3.PathItem{Get: tileOperation("getMapTileset", "Describe a map tileset", openapi3.Parameters{collectionID, tileMatrixSetID}, tileResponse("Map tileset", MediaTypeJSON, "TileSet"))})
	paths.Set("/collections/{collectionId}/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}", &openapi3.PathItem{Get: tileOperation("collectionMap.getTile", "Get a map tile", openapi3.Parameters{collectionID, tileMatrixSetID, tileMatrix, tileRow, tileCol}, tileResponse("Map tile", MediaTypePNG, "Binary"))})
	paths.Set("/collections/{collectionId}/tilejson.json", &openapi3.PathItem{Get: tileOperation("getTileJSON", "Get TileJSON metadata", openapi3.Parameters{collectionID}, tileResponse("TileJSON metadata", MediaTypeJSON, "TileJSON"))})

	if datasetMapResource(ws, role) != nil {
		paths.Set("/map/tiles", &openapi3.PathItem{Get: tileOperation("getDatasetMapTilesets", "List workspace map tilesets", nil, tileResponse("Map tilesets", MediaTypeJSON, "TileSets"))})
		paths.Set("/map/tiles/{tileMatrixSetId}", &openapi3.PathItem{Get: tileOperation("getDatasetMapTileset", "Describe a workspace map tileset", openapi3.Parameters{tileMatrixSetID}, tileResponse("Map tileset", MediaTypeJSON, "TileSet"))})
		response := tileResponse("Workspace map tile", MediaTypePNG, "Binary")
		response.Value.Content = openapi3.Content{}
		for _, format := range ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats {
			response.Value.Content[format] = &openapi3.MediaType{Schema: tileSchemaRef("Binary")}
		}
		parameters := openapi3.Parameters{tileMatrixSetID, tileMatrix, tileRow, tileCol}
		for _, name := range []string{"f", "style", "datetime", "time", "elevation"} {
			parameters = append(parameters, &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: name, In: "query", Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}})
		}
		op := tileOperation("datasetMap.getTile", "Get a workspace map tile", parameters, response)
		op.Responses.Set("503", tileResponse("Tile service unavailable or render queue full", MediaTypeJSON, "Error"))
		paths.Set("/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}", &openapi3.PathItem{Get: op})
	}

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: fmt.Sprintf("%s - %s Tiles", h.cfg.Metadata.Title, ws.Name), Description: ws.Description, Version: "1.0.0"},
		Paths:   paths, Components: components,
	}
	if h.cfg.Auth.Enabled && !ws.Settings.OGCTilesAPI.Public {
		doc.Components.SecuritySchemes = openapi3.SecuritySchemes{
			"bearerAuth": {Value: &openapi3.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}},
			"apiKey":     {Value: &openapi3.SecurityScheme{Type: "apiKey", In: "header", Name: "X-API-Key"}},
		}
		doc.Security = openapi3.SecurityRequirements{{"bearerAuth": {}}, {"apiKey": {}}}
	}
	return doc
}

func (h *workspaceHandler) api(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}
	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}
	doc := h.buildWorkspaceOpenAPI(ws, workspaceRole(r, ws.ID))
	doc.Servers = openapi3.Servers{{URL: h.workspaceBaseURL(r, ws.Name)}}
	data, err := json.Marshal(doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to encode OpenAPI document")
		return
	}
	w.Header().Set("Content-Type", MediaTypeOpenAPI)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func tileStringPtr(value string) *string { return &value }
