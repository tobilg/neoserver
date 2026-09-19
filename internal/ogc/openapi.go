package ogc

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/tobilg/neoserver/internal/workspace"
)

func schemaRef(name string) *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Ref: "#/components/schemas/" + name}
}

func response(description, mediaType, schema string) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: stringPtr(description),
		Content: openapi3.Content{
			mediaType: &openapi3.MediaType{Schema: schemaRef(schema)},
		},
	}}
}

func geoJSONResponse(description, schema string) *openapi3.ResponseRef {
	result := response(description, "application/geo+json", schema)
	result.Value.Headers = openapi3.Headers{
		"Content-Crs": {Value: &openapi3.Header{Parameter: openapi3.Parameter{
			Description: "URI of the response coordinate reference system, enclosed in angle brackets",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}}},
	}
	return result
}

func responses(ok *openapi3.ResponseRef) *openapi3.Responses {
	result := openapi3.NewResponses()
	result.Set("200", ok)
	result.Set("400", response("Invalid request", "application/json", "Error"))
	result.Set("401", response("Authentication required", "application/json", "Error"))
	result.Set("403", response("Access denied", "application/json", "Error"))
	result.Set("404", response("Resource not found", "application/json", "Error"))
	result.Set("500", response("Server error", "application/json", "Error"))
	return result
}

func stringPtr(value string) *string { return &value }

func pathParameter(name, description string) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: "path", Required: true, Description: description, Style: "simple", Explode: boolPtr(false),
		Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
	}}
}

func queryParameter(name, description string, schema *openapi3.Schema) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: "query", Description: description, Style: "form", Explode: boolPtr(false),
		Schema: &openapi3.SchemaRef{Value: schema},
	}}
}

func getOperation(id, summary string, parameters openapi3.Parameters, ok *openapi3.ResponseRef) *openapi3.Operation {
	return &openapi3.Operation{OperationID: id, Summary: summary, Parameters: parameters, Responses: responses(ok)}
}

func (h *workspaceHandler) buildWorkspaceOpenAPI(ws *workspace.Workspace) *openapi3.T {
	link := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"}, Required: []string{"href", "rel"},
		Properties: openapi3.Schemas{
			"href":  {Value: openapi3.NewStringSchema().WithFormat("uri-reference")},
			"rel":   {Value: openapi3.NewStringSchema()},
			"type":  {Value: openapi3.NewStringSchema()},
			"title": {Value: openapi3.NewStringSchema()},
		},
	}}
	links := &openapi3.SchemaRef{Value: openapi3.NewArraySchema().WithItems(link.Value)}
	collection := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"}, Required: []string{"id", "links"},
		Properties: openapi3.Schemas{
			"id": {Value: openapi3.NewStringSchema()}, "title": {Value: openapi3.NewStringSchema()},
			"description": {Value: openapi3.NewStringSchema()}, "links": links,
			"crs":        {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema().WithFormat("uri"))},
			"storageCrs": {Value: openapi3.NewStringSchema().WithFormat("uri")},
			"extent":     {Value: openapi3.NewObjectSchema()},
		},
	}}
	feature := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"}, Required: []string{"type", "geometry", "properties"},
		Properties: openapi3.Schemas{
			"type": {Value: openapi3.NewStringSchema().WithEnum("Feature")},
			"id":   {Value: &openapi3.Schema{}}, "geometry": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Nullable: true}},
			"properties": {Value: openapi3.NewObjectSchema()}, "links": links,
		},
	}}
	components := &openapi3.Components{Schemas: openapi3.Schemas{
		"Link": link,
		"Error": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Required: []string{"code", "title"}, Properties: openapi3.Schemas{
			"code": {Value: openapi3.NewIntegerSchema()}, "title": {Value: openapi3.NewStringSchema()}, "detail": {Value: openapi3.NewStringSchema()},
		}}},
		"LandingPage": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Required: []string{"title", "links"}, Properties: openapi3.Schemas{
			"title": {Value: openapi3.NewStringSchema()}, "description": {Value: openapi3.NewStringSchema()}, "links": links,
		}}},
		"Conformance": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Required: []string{"conformsTo"}, Properties: openapi3.Schemas{
			"conformsTo": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema().WithFormat("uri"))},
		}}},
		"Collection": collection,
		"Collections": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Required: []string{"collections", "links"}, Properties: openapi3.Schemas{
			"collections": {Value: openapi3.NewArraySchema().WithItems(collection.Value)}, "links": links,
		}}},
		"Feature": feature,
		"FeatureCollection": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Required: []string{"type", "features", "numberReturned"}, Properties: openapi3.Schemas{
			"type":           {Value: openapi3.NewStringSchema().WithEnum("FeatureCollection")},
			"features":       {Value: openapi3.NewArraySchema().WithItems(feature.Value)},
			"numberMatched":  {Value: openapi3.NewIntegerSchema()},
			"numberReturned": {Value: openapi3.NewIntegerSchema()},
			"timeStamp":      {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "links": links,
		}}},
		"Queryables": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
		"OpenAPI":    {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
		"HTML":       {Value: openapi3.NewStringSchema()},
	}}

	collectionID := pathParameter("collectionId", "Published collection identifier")
	featureID := pathParameter("featureId", "Feature identifier")
	limitDefault, limitMax := h.cfg.Paging.LimitDefault, h.cfg.Paging.LimitMax
	if ws.Settings.OGCAPI.LimitDefault > 0 {
		limitDefault = min(limitDefault, ws.Settings.OGCAPI.LimitDefault)
	}
	if ws.Settings.OGCAPI.LimitMax > 0 {
		limitMax = min(limitMax, ws.Settings.OGCAPI.LimitMax)
	}
	limitSchema := openapi3.NewIntegerSchema()
	limitSchema.Min = float64Ptr(1)
	if limitMax > 0 {
		limitSchema.Max = float64Ptr(float64(limitMax))
	}
	if limitDefault > 0 {
		limitSchema.Default = limitDefault
	}
	offsetSchema := openapi3.NewIntegerSchema()
	offsetSchema.Min = float64Ptr(0)
	bboxSchema := openapi3.NewArraySchema().WithItems(openapi3.NewFloat64Schema()).WithMinItems(4).WithMaxItems(6)
	itemsParameters := make(openapi3.Parameters, 0, len(itemsQueryParameterNames))
	for _, name := range itemsQueryParameterNames {
		description := ""
		schema := openapi3.NewStringSchema()
		switch name {
		case "limit":
			description, schema = "Maximum number of features to return", limitSchema
		case "offset":
			description, schema = "Zero-based page offset", offsetSchema
		case "bbox":
			description, schema = "Bounding box in bbox-crs", bboxSchema
		case "datetime":
			description = "RFC 3339 instant or interval"
		case "crs":
			description = "Response CRS from the collection crs list"
		case "bbox-crs":
			description = "CRS of bbox"
		case "filter":
			description = "CQL2 text filter"
		case "filter-lang":
			description, schema = "Filter encoding", openapi3.NewStringSchema().WithEnum("cql2-text")
			schema.Default = "cql2-text"
		case "filter-crs":
			description = "CRS of geometry literals in filter"
		case "properties":
			description = "Comma-separated properties to include"
		case "sortby":
			description = "Comma-separated sort properties"
		}
		itemsParameters = append(itemsParameters, queryParameter(name, description, schema))
	}
	withAuthQuery := func(parameters openapi3.Parameters) openapi3.Parameters {
		result := append(openapi3.Parameters(nil), parameters...)
		if h.cfg.Auth.AllowAPIKeyInQuery {
			result = append(result, queryParameter("apikey", "API key (query transport is opt-in; prefer X-API-Key)", openapi3.NewStringSchema()))
		}
		return result
	}

	paths := openapi3.NewPaths()
	paths.Set("/", &openapi3.PathItem{Get: getOperation("getLandingPage", "Landing page", withAuthQuery(nil), response("Landing page", "application/json", "LandingPage"))})
	paths.Set("/conformance", &openapi3.PathItem{Get: getOperation("getConformance", "Conformance declaration", withAuthQuery(nil), response("Conformance declaration", "application/json", "Conformance"))})
	paths.Set("/collections", &openapi3.PathItem{Get: getOperation("getCollections", "List collections", withAuthQuery(nil), response("Collections", "application/json", "Collections"))})
	paths.Set("/collections/{collectionId}", &openapi3.PathItem{Get: getOperation("getCollection", "Describe a collection", withAuthQuery(openapi3.Parameters{collectionID}), response("Collection", "application/json", "Collection"))})
	paths.Set("/collections/{collectionId}/queryables", &openapi3.PathItem{Get: getOperation("getQueryables", "Get collection queryables", withAuthQuery(openapi3.Parameters{collectionID}), response("JSON Schema describing queryables", queryablesType, "Queryables"))})
	paths.Set("/collections/{collectionId}/items", &openapi3.PathItem{Get: getOperation("getFeatures", "Query collection features", withAuthQuery(append(openapi3.Parameters{collectionID}, itemsParameters...)), geoJSONResponse("GeoJSON feature collection", "FeatureCollection"))})
	paths.Set("/collections/{collectionId}/items/{featureId}", &openapi3.PathItem{Get: getOperation("getFeature", "Get a feature", withAuthQuery(openapi3.Parameters{collectionID, featureID, queryParameter("crs", "Response CRS", openapi3.NewStringSchema())}), geoJSONResponse("GeoJSON feature", "Feature"))})
	paths.Set("/api", &openapi3.PathItem{Get: getOperation("getOpenAPI", "OpenAPI definition", withAuthQuery(nil), response("OpenAPI definition", "application/vnd.oai.openapi+json;version=3.0", "OpenAPI"))})
	paths.Set("/api.html", &openapi3.PathItem{Get: getOperation("getOpenAPIDocumentation", "Interactive API documentation", withAuthQuery(nil), response("Swagger UI", "text/html", "HTML"))})

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: fmt.Sprintf("%s - %s", h.cfg.Metadata.Title, ws.Name), Description: ws.Description, Version: "1.0.0"},
		Paths:   paths, Components: components,
	}
	if h.cfg.Auth.Enabled && !ws.Settings.OGCAPI.Public {
		doc.Components.SecuritySchemes = openapi3.SecuritySchemes{
			"bearerAuth": {Value: &openapi3.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}},
			"apiKey":     {Value: &openapi3.SecurityScheme{Type: "apiKey", In: "header", Name: "X-API-Key"}},
		}
		doc.Security = openapi3.SecurityRequirements{{"bearerAuth": {}}, {"apiKey": {}}}
	}
	return doc
}

func float64Ptr(value float64) *float64 { return &value }
func boolPtr(value bool) *bool          { return &value }
