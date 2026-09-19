package mgmt

import "github.com/getkin/kin-openapi/openapi3"

// PathParams holds commonly used path parameter references.
type PathParams struct {
	Workspace *openapi3.ParameterRef
	Service   *openapi3.ParameterRef
	Layer     *openapi3.ParameterRef
	Coverage  *openapi3.ParameterRef
	KeyID     *openapi3.ParameterRef
	RoleID    *openapi3.ParameterRef
	MappingID *openapi3.ParameterRef
	Style     *openapi3.ParameterRef
	CacheType *openapi3.ParameterRef
	Job       *openapi3.ParameterRef
}

// getPathParams returns all commonly used path parameters.
func getPathParams() PathParams {
	return PathParams{
		Workspace: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "workspace",
			Required:    true,
			Description: "Workspace ID",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		Service: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "service",
			Required:    true,
			Description: "Service ID",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		Layer: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "layer",
			Required:    true,
			Description: "Layer ID",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		Coverage: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In: "path", Name: "coverage", Required: true, Description: "Coverage ID or public identifier",
			Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		KeyID: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "keyId",
			Required:    true,
			Description: "API Key ID",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		RoleID: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "roleId",
			Required:    true,
			Description: "Role ID",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		MappingID: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "mappingId",
			Required:    true,
			Description: "Claim Mapping ID",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		Style: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "style",
			Required:    true,
			Description: "Style name",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
		CacheType: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In:          "path",
			Name:        "cacheType",
			Required:    true,
			Description: "Cache type to clear",
			Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema().WithEnum("capabilities", "collections", "features", "counts", "tiles")},
		}},
		Job: &openapi3.ParameterRef{Value: &openapi3.Parameter{
			In: "path", Name: "job", Required: true, Description: "Tile cache job ID",
			Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}},
	}
}

// jsonResp creates a JSON response with the given description and schema.
func jsonResp(description string, schema *openapi3.SchemaRef) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: ptrString(description),
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{Schema: schema},
		},
	}}
}

// noContentResp creates a 204 No Content response.
func noContentResp() *openapi3.ResponseRef {
	return &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: ptrString("No Content"),
	}}
}

// errResp creates an error response with the Error schema.
func errResp(code int, description string) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: ptrString(description),
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/Error"}},
		},
	}}
}

// jsonBody creates a JSON request body with the given description and schema.
func jsonBody(description string, schema *openapi3.SchemaRef) *openapi3.RequestBodyRef {
	return &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{
		Description: description,
		Required:    true,
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{Schema: schema},
		},
	}}
}

// ptrString returns a pointer to the given string.
func ptrString(s string) *string { return &s }

// mkResponses creates an openapi3.Responses from a map.
func mkResponses(m map[string]*openapi3.ResponseRef) *openapi3.Responses {
	r := openapi3.NewResponsesWithCapacity(len(m))
	for k, v := range m {
		r.Set(k, v)
	}
	return r
}
