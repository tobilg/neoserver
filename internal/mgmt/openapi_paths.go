package mgmt

import "github.com/getkin/kin-openapi/openapi3"

// getPaths returns all API path definitions.
func getPaths(params PathParams) *openapi3.Paths {
	paths := openapi3.NewPaths()
	public := openapi3.SecurityRequirements{}
	paths.Set("/console/config", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Authentication"}, Summary: "Get browser console configuration", OperationID: "getConsoleConfig", Security: &public,
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Non-secret console configuration", schemaRef("ConsoleConfig"))}),
	}})
	paths.Set("/auth/login", &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Authentication"}, Summary: "Create a browser session", OperationID: "login", Security: &public,
		RequestBody: jsonBody("Browser login method", schemaRef("LoginRequest")), Responses: mkResponses(map[string]*openapi3.ResponseRef{
			"201": jsonResp("Authenticated identity", schemaRef("AuthMe")), "401": errResp(401, "Authentication failed"), "403": errResp(403, "Login method disabled"),
		}),
	}})
	paths.Set("/auth/me", &openapi3.PathItem{Get: &openapi3.Operation{Tags: []string{"Authentication"}, Summary: "Get current identity and console capabilities", OperationID: "getAuthMe", Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Current identity", schemaRef("AuthMe"))})}})
	paths.Set("/auth/logout", &openapi3.PathItem{Post: &openapi3.Operation{Tags: []string{"Authentication"}, Summary: "Revoke the current browser session", OperationID: "logout", Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": noContentResp()})}})
	paths.Set("/auth/refresh", &openapi3.PathItem{Post: &openapi3.Operation{Tags: []string{"Authentication"}, Summary: "Rotate the current browser session", OperationID: "refreshSession", Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Refreshed identity", schemaRef("AuthMe"))})}})
	paths.Set("/auth/sessions", &openapi3.PathItem{Get: &openapi3.Operation{Tags: []string{"Authentication"}, Summary: "List browser sessions", OperationID: "listBrowserSessions", Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Browser sessions", schemaRef("BrowserSessionList"))})}})
	sessionParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "session", In: "path", Required: true, Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	paths.Set("/auth/sessions/{session}", &openapi3.PathItem{Delete: &openapi3.Operation{Tags: []string{"Authentication"}, Summary: "Revoke a browser session", OperationID: "revokeBrowserSession", Parameters: openapi3.Parameters{sessionParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": noContentResp(), "404": errResp(404, "Session not found")})}})
	recurseParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "recurse", In: "query", Description: "Recursively remove dependent catalog and operational state using a durable asynchronous operation.", Schema: &openapi3.SchemaRef{Value: openapi3.NewBoolSchema()}}}

	// ============== Workspaces ==============
	paths.Set("/workspaces", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Workspaces"},
			Summary:     "List workspaces",
			Description: "Returns all workspaces. Requires super_admin; non-super-admin discovery uses /auth/me.",
			OperationID: "listWorkspaces",
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of workspaces", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"workspaces": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Workspace"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"Workspaces"},
			Summary:     "Create workspace",
			Description: "Creates a new workspace. Requires super_admin role.",
			OperationID: "createWorkspace",
			RequestBody: jsonBody("Workspace to create", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"name":        {Value: openapi3.NewStringSchema()},
					"description": {Value: openapi3.NewStringSchema()},
				},
				Required: []string{"name"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created workspace", &openapi3.SchemaRef{Ref: "#/components/schemas/Workspace"}),
				"409": errResp(409, "A workspace with this name already exists"),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Workspaces"},
			Summary:     "Get workspace",
			Description: "Returns a workspace by ID.",
			OperationID: "getWorkspace",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Workspace details", &openapi3.SchemaRef{Ref: "#/components/schemas/Workspace"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Workspaces"},
			Summary:     "Update workspace",
			Description: "Updates a workspace. Requires admin access to the workspace. Omitted or null description leaves it unchanged; an empty string clears it.",
			OperationID: "updateWorkspace",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("Workspace updates", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"name":        {Value: openapi3.NewStringSchema()},
					"description": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Nullable: true}},
				},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated workspace", &openapi3.SchemaRef{Ref: "#/components/schemas/Workspace"}),
				"409": errResp(409, "A workspace with this name already exists"),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
		Delete: &openapi3.Operation{
			Tags:        []string{"Workspaces"},
			Summary:     "Delete workspace",
			Description: "Deletes an empty workspace synchronously. Non-empty workspaces return 409 unless recurse=true, which starts a durable deletion operation. Requires super_admin role.",
			OperationID: "deleteWorkspace",
			Parameters:  openapi3.Parameters{params.Workspace, recurseParameter},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"202": jsonResp("Recursive deletion accepted", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionOperation"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
				"404": errResp(404, "Workspace not found"),
				"409": jsonResp("Workspace has dependencies", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionConflict"}),
			}),
		},
	})
	paths.Set("/workspaces/{workspace}/summary", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Workspaces"}, Summary: "Get workspace console summary", OperationID: "getWorkspaceSummary", Parameters: openapi3.Parameters{params.Workspace},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Workspace summary", schemaRef("WorkspaceSummary"))}),
	}})
	paths.Set("/workspaces/{workspace}/roles", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Roles"}, Summary: "List roles assignable in a workspace", OperationID: "listWorkspaceRoles", Parameters: openapi3.Parameters{params.Workspace},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Workspace roles", &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{"roles": schemaArray("Role")}}})}),
	}})
	paths.Set("/workspaces/{workspace}/tile-matrix-sets", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Tile Matrix Sets"}, Summary: "List enabled workspace tile matrix sets", OperationID: "listWorkspaceTileMatrixSets", Parameters: openapi3.Parameters{params.Workspace},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Enabled tile matrix sets", schemaRef("TileMatrixSetDefinitions"))}),
	}})
	paths.Set("/workspaces/{workspace}/deletion-plan", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Catalog Lifecycle"}, Summary: "Inspect workspace deletion dependencies", OperationID: "getWorkspaceDeletionPlan",
		Parameters: openapi3.Parameters{params.Workspace}, Responses: mkResponses(map[string]*openapi3.ResponseRef{
			"200": jsonResp("Deletion dependency plan", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionPlan"}), "404": errResp(404, "Workspace not found"),
		}),
	}})

	// ============== Services ==============
	paths.Set("/workspaces/{workspace}/services", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Services"},
			Summary:     "List services",
			Description: "Returns all services in a workspace.",
			OperationID: "listServices",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of services", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"services": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Service"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"Services"},
			Summary:     "Create service",
			Description: "Creates a new data source service in a workspace.",
			OperationID: "createService",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("Service to create", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"name":            {Value: openapi3.NewStringSchema()},
					"type":            {Value: openapi3.NewStringSchema().WithEnum("postgis", "duckdb", "geoparquet", "vectorfile", "rasterfile", "raster_mosaic")},
					"connection_info": {Ref: "#/components/schemas/ConnectionInfo"},
					"cache_settings":  {Ref: "#/components/schemas/ServiceCacheSettings"},
					"enabled":         {Value: openapi3.NewBoolSchema()},
				},
				Required: []string{"name", "type", "connection_info"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created service", &openapi3.SchemaRef{Ref: "#/components/schemas/Service"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
	})
	paths.Set("/workspaces/{workspace}/services/test-connection", &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Services"}, Summary: "Test a service connection before saving", OperationID: "testNewServiceConnection", Parameters: openapi3.Parameters{params.Workspace},
		RequestBody: jsonBody("Service connection", schemaRef("ServiceConnectionInput")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Connection test result", schemaRef("ConnectionTest"))}),
	}})

	paths.Set("/workspaces/{workspace}/services/{service}", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Services"},
			Summary:     "Get service",
			Description: "Returns a service by ID, including connection info.",
			OperationID: "getService",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Service details", &openapi3.SchemaRef{Ref: "#/components/schemas/Service"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Service not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Services"},
			Summary:     "Update service",
			Description: "Updates a service configuration.",
			OperationID: "updateService",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service},
			RequestBody: jsonBody("Service updates", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"name":            {Value: openapi3.NewStringSchema()},
					"connection_info": {Ref: "#/components/schemas/ConnectionInfo"},
					"cache_settings":  {Ref: "#/components/schemas/ServiceCacheSettings"},
					"enabled":         {Value: openapi3.NewBoolSchema()},
				},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated service", &openapi3.SchemaRef{Ref: "#/components/schemas/Service"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Service not found"),
			}),
		},
		Delete: &openapi3.Operation{
			Tags:        []string{"Services"},
			Summary:     "Delete service",
			Description: "Deletes an empty service synchronously. Non-empty services return 409 unless recurse=true, which also removes the transitive dependent-group closure.",
			OperationID: "deleteService",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service, recurseParameter},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"202": jsonResp("Recursive deletion accepted", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionOperation"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Service not found"),
				"409": jsonResp("Service has dependencies", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionConflict"}),
			}),
		},
	})
	paths.Set("/workspaces/{workspace}/services/{service}/test-connection", &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Services"}, Summary: "Test an existing service connection", OperationID: "testExistingServiceConnection", Parameters: openapi3.Parameters{params.Workspace, params.Service},
		RequestBody: jsonBody("Optional connection updates", schemaRef("ServiceConnectionInput")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Connection test result", schemaRef("ConnectionTest"))}),
	}})
	paths.Set("/workspaces/{workspace}/services/{service}/deletion-plan", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Catalog Lifecycle"}, Summary: "Inspect service deletion dependencies", OperationID: "getServiceDeletionPlan",
		Parameters: openapi3.Parameters{params.Workspace, params.Service}, Responses: mkResponses(map[string]*openapi3.ResponseRef{
			"200": jsonResp("Deletion dependency plan", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionPlan"}), "404": errResp(404, "Service not found"),
		}),
	}})

	paths.Set("/workspaces/{workspace}/services/{service}/discover", &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Services"},
			Summary:     "Discover layers",
			Description: "Discovers available layers from the data source. Returns geometry tables/views for PostGIS, spatial tables for DuckDB.",
			OperationID: "discoverLayers",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Discovered layers", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"layers": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/DiscoveredLayer"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"500": errResp(500, "Discovery failed"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/services/{service}/validate-sql", &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Services"},
			Summary:     "Validate SQL view",
			Description: "Validates a read-only SQL view query and discovers its columns and geometry metadata.",
			OperationID: "validateSQLView",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service},
			RequestBody: jsonBody("SQL query to validate", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"sql": {Value: openapi3.NewStringSchema()},
				},
				Required: []string{"sql"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("SQL validation result", &openapi3.SchemaRef{Ref: "#/components/schemas/ValidateSQLResponse"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace or service not found"),
				"503": errResp(503, "Data source unavailable"),
			}),
		},
	})

	// ============== Layers ==============
	paths.Set("/workspaces/{workspace}/services/{service}/layers", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Layers"},
			Summary:     "List layers",
			Description: "Returns all published layers for a service.",
			OperationID: "listLayers",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of layers", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"workspace_id": {Value: openapi3.NewStringSchema()},
						"service_id":   {Value: openapi3.NewStringSchema()},
						"layers": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Layer"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"Layers"},
			Summary:     "Create layer",
			Description: "Publishes a readable discovered layer. Enabled publication validates the source. Set enabled=false explicitly to save an offline draft; enabling later requires validation. SQL views are read-only.",
			OperationID: "createLayer",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service},
			RequestBody: jsonBody("Layer to publish", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"source_layer": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Name of the source table/layer in the data source"}},
					"public_id":    {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Public identifier for URLs (defaults to source_layer)"}},
					"title":        {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Human-readable title"}},
					"description":  {Value: openapi3.NewStringSchema()},
					"enabled":      {Value: openapi3.NewBoolSchema()},
					"crs_default":  {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Description: "Default CRS EPSG code (defaults to 4326)"}},
					"dimensions": {Value: &openapi3.Schema{
						Type:  &openapi3.Types{"array"},
						Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Dimension"},
					}},
					"sql_view":      {Ref: "#/components/schemas/SQLView"},
					"public":        {Value: openapi3.NewBoolSchema()},
					"allowed_roles": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
				},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created layer", &openapi3.SchemaRef{Ref: "#/components/schemas/Layer"}),
				"409": errResp(409, "public_id is already published in this workspace"),
				"422": errResp(422, "Source layer is missing or unreadable"),
				"503": errResp(503, "Datasource unavailable"),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/services/{service}/layers/{layer}", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Layers"},
			Summary:     "Get layer",
			Description: "Returns layer configuration by ID.",
			OperationID: "getLayer",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service, params.Layer},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Layer details", &openapi3.SchemaRef{Ref: "#/components/schemas/Layer"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Layer not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Layers"},
			Summary:     "Update layer",
			Description: "Updates layer configuration. Omitted/null title, description and dimensions preserve existing values; empty strings clear title/description and an empty dimensions array removes all dimensions. Enabling a regular layer validates its source.",
			OperationID: "updateLayer",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service, params.Layer},
			RequestBody: jsonBody("Layer updates", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"default_style": {Value: openapi3.NewStringSchema()},
					"styles":        {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
					"public_id":     {Value: openapi3.NewStringSchema()},
					"title":         {Value: openapi3.NewStringSchema()},
					"description":   {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Nullable: true}},
					"enabled":       {Value: openapi3.NewBoolSchema()},
					"crs_default":   {Value: openapi3.NewIntegerSchema()},
					"dimensions": {Value: &openapi3.Schema{
						Type:  &openapi3.Types{"array"},
						Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Dimension"},
					}},
					"public":        {Value: openapi3.NewBoolSchema()},
					"allowed_roles": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
				},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated layer", &openapi3.SchemaRef{Ref: "#/components/schemas/Layer"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Layer not found"),
				"409": errResp(409, "public_id conflicts or layer is referenced by a layer group"),
				"422": errResp(422, "Source layer is missing or unreadable"),
				"503": errResp(503, "Datasource unavailable"),
			}),
		},
		Delete: &openapi3.Operation{
			Tags:        []string{"Layers"},
			Summary:     "Delete layer",
			Description: "Unpublishes and deletes a layer.",
			OperationID: "deleteLayer",
			Parameters:  openapi3.Parameters{params.Workspace, params.Service, params.Layer},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Layer not found"),
				"409": errResp(409, "Layer is referenced by a layer group"),
			}),
		},
	})

	// ============== Coverages ==============
	paths.Set("/workspaces/{workspace}/services/{service}/discover-coverages", &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Coverages"}, Summary: "Discover raster coverages", OperationID: "discoverCoverages",
		Parameters: openapi3.Parameters{params.Workspace, params.Service}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Discovered coverages", schemaRef("CoverageDiscoveryList")), "400": errResp(400, "Unsupported raster source")}),
	}})
	paths.Set("/workspaces/{workspace}/services/{service}/coverages", &openapi3.PathItem{
		Get:  &openapi3.Operation{Tags: []string{"Coverages"}, Summary: "List coverages", OperationID: "listCoverages", Parameters: openapi3.Parameters{params.Workspace, params.Service}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Published coverages", schemaRef("CoverageList"))})},
		Post: &openapi3.Operation{Tags: []string{"Coverages"}, Summary: "Publish coverage", OperationID: "createCoverage", Parameters: openapi3.Parameters{params.Workspace, params.Service}, RequestBody: jsonBody("Coverage publication", schemaRef("CoverageCreate")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"201": jsonResp("Created coverage", &openapi3.SchemaRef{Ref: "#/components/schemas/Coverage"}), "400": errResp(400, "Invalid coverage")})},
	})
	paths.Set("/workspaces/{workspace}/services/{service}/coverages/{coverage}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Coverages"}, Summary: "Get coverage", OperationID: "getCoverage", Parameters: openapi3.Parameters{params.Workspace, params.Service, params.Coverage}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Coverage", &openapi3.SchemaRef{Ref: "#/components/schemas/Coverage"}), "404": errResp(404, "Coverage not found")})},
		Put:    &openapi3.Operation{Tags: []string{"Coverages"}, Summary: "Update coverage", OperationID: "updateCoverage", Parameters: openapi3.Parameters{params.Workspace, params.Service, params.Coverage}, RequestBody: jsonBody("Mutable coverage metadata", schemaRef("CoverageUpdate")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Updated coverage", &openapi3.SchemaRef{Ref: "#/components/schemas/Coverage"}), "409": errResp(409, "Coverage is referenced by a layer group")})},
		Delete: &openapi3.Operation{Tags: []string{"Coverages"}, Summary: "Delete coverage", OperationID: "deleteCoverage", Parameters: openapi3.Parameters{params.Workspace, params.Service, params.Coverage}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": noContentResp(), "409": errResp(409, "Coverage is referenced by a layer group")})},
	})

	// ============== API Keys ==============
	paths.Set("/workspaces/{workspace}/apikeys", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"API Keys"},
			Summary:     "List API keys",
			Description: "Returns all API keys for a workspace.",
			OperationID: "listAPIKeys",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of API keys", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"api_keys": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/APIKey"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"API Keys"},
			Summary:     "Create API key",
			Description: "Creates a new API key. The full key is only returned once on creation.",
			OperationID: "createAPIKey",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("API key to create", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"name":        {Value: openapi3.NewStringSchema()},
					"role_id":     {Value: openapi3.NewStringSchema().WithEnum("admin", "editor", "viewer")},
					"owner_name":  {Value: openapi3.NewStringSchema()},
					"owner_email": {Value: openapi3.NewStringSchema()},
					"expires_at":  {Value: openapi3.NewStringSchema().WithFormat("date-time")},
				},
				Required: []string{"name", "role_id"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created API key with secret", &openapi3.SchemaRef{Ref: "#/components/schemas/APIKeyWithSecret"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/apikeys/{keyId}", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"API Keys"},
			Summary:     "Get API key",
			Description: "Returns API key metadata (without the secret).",
			OperationID: "getAPIKey",
			Parameters:  openapi3.Parameters{params.Workspace, params.KeyID},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("API key details", &openapi3.SchemaRef{Ref: "#/components/schemas/APIKey"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "API key not found"),
			}),
		},
		Delete: &openapi3.Operation{
			Tags:        []string{"API Keys"},
			Summary:     "Revoke API key",
			Description: "Revokes an API key, making it unusable.",
			OperationID: "revokeAPIKey",
			Parameters:  openapi3.Parameters{params.Workspace, params.KeyID},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "API key not found"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/apikeys/{keyId}/permanent", &openapi3.PathItem{
		Delete: &openapi3.Operation{
			Tags:        []string{"API Keys"},
			Summary:     "Delete revoked API key",
			Description: "Permanently removes a revoked API key and the browser sessions created from it. The key no longer appears in listings. Active keys must be revoked first.",
			OperationID: "deleteAPIKey",
			Parameters:  openapi3.Parameters{params.Workspace, params.KeyID},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "API key not found"),
				"409": errResp(409, "API key is not revoked"),
			}),
		},
	})

	// ============== Roles ==============
	paths.Set("/roles", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Roles"},
			Summary:     "List roles",
			Description: "Returns all roles. Requires super_admin role.",
			OperationID: "listRoles",
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of roles", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"roles": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Role"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"Roles"},
			Summary:     "Create role",
			Description: "Creates a custom role. Requires super_admin role.",
			OperationID: "createRole",
			RequestBody: jsonBody("Role to create", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"id":          {Value: openapi3.NewStringSchema()},
					"name":        {Value: openapi3.NewStringSchema()},
					"description": {Value: openapi3.NewStringSchema()},
				},
				Required: []string{"id"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created role", &openapi3.SchemaRef{Ref: "#/components/schemas/Role"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
	})

	paths.Set("/roles/{roleId}/deletion-plan", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Roles"}, Summary: "Inspect role deletion dependencies", OperationID: "getRoleDeletionPlan",
		Parameters: openapi3.Parameters{params.RoleID},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Role dependencies", &openapi3.SchemaRef{Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"}, Required: []string{"role_id", "is_system", "dependencies"}, Properties: openapi3.Schemas{
				"role_id": {Value: openapi3.NewStringSchema()}, "is_system": {Value: openapi3.NewBoolSchema()},
				"dependencies": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, AdditionalProperties: openapi3.AdditionalProperties{Schema: &openapi3.SchemaRef{Value: openapi3.NewIntegerSchema()}}}},
			},
		}}), "404": errResp(404, "Role not found"), "503": errResp(503, "Dependency inspection unavailable")}),
	}})
	paths.Set("/roles/{roleId}", &openapi3.PathItem{
		Delete: &openapi3.Operation{
			Tags:        []string{"Roles"},
			Summary:     "Delete role",
			Description: "Deletes an unassigned custom role and its policies atomically. Remove credential and publication assignments first. System roles cannot be deleted. Requires super_admin role.",
			OperationID: "deleteRole",
			Parameters:  openapi3.Parameters{params.RoleID},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"400": errResp(400, "Cannot delete system role"),
				"409": errResp(409, "Role still has assignments; inspect its deletion plan"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
				"404": errResp(404, "Role not found"),
			}),
		},
	})

	// ============== Claim Mappings (Workspace-scoped) ==============
	paths.Set("/workspaces/{workspace}/claim-mappings", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Claim Mappings"},
			Summary:     "List claim mappings",
			Description: "Returns all OIDC/JWT claim mappings for a workspace.",
			OperationID: "listClaimMappings",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of claim mappings", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"claim_mappings": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/ClaimMapping"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"Claim Mappings"},
			Summary:     "Create claim mapping",
			Description: "Creates a mapping from an OIDC/JWT claim to a role for this workspace.",
			OperationID: "createClaimMapping",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("Claim mapping to create", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"claim_name":  {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "JWT claim name (e.g., groups, roles)"}},
					"claim_value": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Claim value to match"}},
					"role_id":     {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Role to assign"}},
					"priority":    {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Description: "Priority (higher = checked first)"}},
				},
				Required: []string{"claim_name", "claim_value", "role_id"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created claim mapping", &openapi3.SchemaRef{Ref: "#/components/schemas/ClaimMapping"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/claim-mappings/{mappingId}", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Claim Mappings"},
			Summary:     "Get claim mapping",
			Description: "Returns a claim mapping by ID.",
			OperationID: "getClaimMapping",
			Parameters:  openapi3.Parameters{params.Workspace, params.MappingID},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Claim mapping details", &openapi3.SchemaRef{Ref: "#/components/schemas/ClaimMapping"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Claim mapping not found"),
			}),
		},
		Delete: &openapi3.Operation{
			Tags:        []string{"Claim Mappings"},
			Summary:     "Delete claim mapping",
			Description: "Deletes a claim mapping.",
			OperationID: "deleteClaimMapping",
			Parameters:  openapi3.Parameters{params.Workspace, params.MappingID},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Claim mapping not found"),
			}),
		},
	})

	// ============== Global Claim Mappings ==============
	paths.Set("/claim-mappings", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Claim Mappings"},
			Summary:     "List global claim mappings",
			Description: "Returns all global OIDC/JWT claim mappings. Requires super_admin role.",
			OperationID: "listGlobalClaimMappings",
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of global claim mappings", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"claim_mappings": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/ClaimMapping"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"Claim Mappings"},
			Summary:     "Create global claim mapping",
			Description: "Creates a global mapping from an OIDC/JWT claim to a role. Requires super_admin role.",
			OperationID: "createGlobalClaimMapping",
			RequestBody: jsonBody("Claim mapping to create", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"claim_name":  {Value: openapi3.NewStringSchema()},
					"claim_value": {Value: openapi3.NewStringSchema()},
					"role_id":     {Value: openapi3.NewStringSchema()},
					"priority":    {Value: openapi3.NewIntegerSchema()},
				},
				Required: []string{"claim_name", "claim_value", "role_id"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created claim mapping", &openapi3.SchemaRef{Ref: "#/components/schemas/ClaimMapping"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
	})

	paths.Set("/claim-mappings/{mappingId}", &openapi3.PathItem{
		Delete: &openapi3.Operation{
			Tags:        []string{"Claim Mappings"},
			Summary:     "Delete global claim mapping",
			Description: "Deletes a global claim mapping. Requires super_admin role.",
			OperationID: "deleteGlobalClaimMapping",
			Parameters:  openapi3.Parameters{params.MappingID},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
				"404": errResp(404, "Claim mapping not found"),
			}),
		},
	})

	// ============== Styles ==============
	paths.Set("/workspaces/{workspace}/styles", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Styles"},
			Summary:     "List styles",
			Description: "Returns all managed styles for a workspace.",
			OperationID: "listStyles",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("List of styles", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"styles": {Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Style"},
						}},
					},
				}}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
			}),
		},
		Post: &openapi3.Operation{
			Tags:        []string{"Styles"},
			Summary:     "Create style",
			Description: "Creates a managed SLD/SE, CSS, YSLD, or Mapbox style in the workspace.",
			OperationID: "createStyle",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("Style to create", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"name":        {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Unique style name"}},
					"title":       {Value: openapi3.NewStringSchema()},
					"description": {Value: openapi3.NewStringSchema()},
					"body":        {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Authored style content"}},
					"sld_body":    {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Deprecated SLD/SE alias for body"}},
					"format":      {Value: openapi3.NewStringSchema().WithEnum("sld_1.0.0", "sld_1.1.0", "se_1.1.0", "css", "ysld", "mapbox")},
				},
				Required: []string{"name"},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"201": jsonResp("Created style", &openapi3.SchemaRef{Ref: "#/components/schemas/StyleWithBody"}),
				"400": errResp(400, "Bad Request - invalid SLD"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"409": errResp(409, "Conflict - style name already exists"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/styles/{style}", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Styles"},
			Summary:     "Get style",
			Description: "Returns a style by name, including its authored body.",
			OperationID: "getStyle",
			Parameters:  openapi3.Parameters{params.Workspace, params.Style},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Style details with SLD body", &openapi3.SchemaRef{Ref: "#/components/schemas/StyleWithBody"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Style not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Styles"},
			Summary:     "Update style",
			Description: "Updates an existing style.",
			OperationID: "updateStyle",
			Parameters:  openapi3.Parameters{params.Workspace, params.Style},
			RequestBody: jsonBody("Style updates", &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: openapi3.Schemas{
					"name":        {Value: openapi3.NewStringSchema()},
					"title":       {Value: openapi3.NewStringSchema()},
					"description": {Value: openapi3.NewStringSchema()},
					"body":        {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Authored style content"}},
					"sld_body":    {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "SLD XML content"}},
					"format":      {Value: openapi3.NewStringSchema().WithEnum("sld_1.0.0", "sld_1.1.0", "se_1.1.0", "css", "ysld", "mapbox")},
				},
			}}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated style", &openapi3.SchemaRef{Ref: "#/components/schemas/StyleWithBody"}),
				"400": errResp(400, "Bad Request - invalid SLD"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Style not found"),
				"409": errResp(409, "Conflict - style name already exists"),
			}),
		},
		Delete: &openapi3.Operation{
			Tags:        []string{"Styles"},
			Summary:     "Delete style",
			Description: "Deletes a style from the workspace.",
			OperationID: "deleteStyle",
			Parameters:  openapi3.Parameters{params.Workspace, params.Style},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"204": noContentResp(),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Style not found"),
			}),
		},
	})

	assetParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "asset", In: "path", Required: true, Description: "Workspace-local graphic asset name", Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	paths.Set("/workspaces/{workspace}/style-assets", &openapi3.PathItem{Get: &openapi3.Operation{Tags: []string{"Styles"}, Summary: "List style assets", Description: "Lists metadata for managed PNG, JPEG, GIF, and SVG portrayal assets.", OperationID: "listStyleAssets", Parameters: openapi3.Parameters{params.Workspace}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Style assets", &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{"assets": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/StyleAsset"}}}}}})})}})
	assetBinaryContent := openapi3.Content{}
	for _, mediaType := range []string{"image/png", "image/jpeg", "image/gif", "image/svg+xml"} {
		assetBinaryContent[mediaType] = &openapi3.MediaType{Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Format: "binary"}}}
	}
	paths.Set("/workspaces/{workspace}/style-assets/{asset}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Styles"}, Summary: "Download style asset", OperationID: "getStyleAsset", Parameters: openapi3.Parameters{params.Workspace, assetParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": {Value: &openapi3.Response{Description: ptrString("Asset payload"), Content: assetBinaryContent}}, "404": errResp(404, "Style asset not found")})},
		Put:    &openapi3.Operation{Tags: []string{"Styles"}, Summary: "Create or replace style asset", Description: "Atomically stores a bounded workspace-local graphic payload.", OperationID: "putStyleAsset", Parameters: openapi3.Parameters{params.Workspace, assetParameter}, RequestBody: &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Required: true, Content: assetBinaryContent}}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"201": jsonResp("Stored asset", &openapi3.SchemaRef{Ref: "#/components/schemas/StyleAsset"}), "400": errResp(400, "Invalid image"), "413": errResp(413, "Asset too large")})},
		Delete: &openapi3.Operation{Tags: []string{"Styles"}, Summary: "Delete style asset", OperationID: "deleteStyleAsset", Parameters: openapi3.Parameters{params.Workspace, assetParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": noContentResp(), "404": errResp(404, "Style asset not found"), "409": errResp(409, "Asset is referenced")})},
	})

	// ============== Workspace Settings ==============
	paths.Set("/workspaces/{workspace}/settings/wms", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Get WMS settings",
			Description: "Returns the WMS service settings for a workspace.",
			OperationID: "getWMSSettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("WMS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WMSSettings"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Update WMS settings",
			Description: "Updates the WMS service settings for a workspace.",
			OperationID: "updateWMSSettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("WMS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WMSSettings"}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated WMS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WMSSettings"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/settings/wfs", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Get WFS settings",
			Description: "Returns the WFS service settings for a workspace.",
			OperationID: "getWFSSettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("WFS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WFSSettings"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Update WFS settings",
			Description: "Updates the WFS service settings for a workspace.",
			OperationID: "updateWFSSettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("WFS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WFSSettings"}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated WFS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WFSSettings"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/settings/ogcapi", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Get OGC API settings",
			Description: "Returns the OGC API Features settings for a workspace.",
			OperationID: "getOGCAPISettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("OGC API settings", &openapi3.SchemaRef{Ref: "#/components/schemas/OGCAPISettings"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Update OGC API settings",
			Description: "Updates the OGC API Features settings for a workspace.",
			OperationID: "updateOGCAPISettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("OGC API settings", &openapi3.SchemaRef{Ref: "#/components/schemas/OGCAPISettings"}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated OGC API settings", &openapi3.SchemaRef{Ref: "#/components/schemas/OGCAPISettings"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/settings/ogc-tiles", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Get OGC API Tiles settings",
			Description: "Returns OGC API Tiles settings for a workspace.",
			OperationID: "getOGCTilesAPISettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("OGC API Tiles settings", &openapi3.SchemaRef{Ref: "#/components/schemas/OGCTilesSettings"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
		Put: &openapi3.Operation{
			Tags:        []string{"Settings"},
			Summary:     "Update OGC API Tiles settings",
			Description: "Updates OGC API Tiles settings within the global server ceilings.",
			OperationID: "updateOGCTilesAPISettings",
			Parameters:  openapi3.Parameters{params.Workspace},
			RequestBody: jsonBody("OGC API Tiles settings", &openapi3.SchemaRef{Ref: "#/components/schemas/OGCTilesSettings"}),
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Updated OGC API Tiles settings", &openapi3.SchemaRef{Ref: "#/components/schemas/OGCTilesSettings"}),
				"400": errResp(400, "Bad Request"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
	})
	paths.Set("/workspaces/{workspace}/settings/wcs", &openapi3.PathItem{
		Get: &openapi3.Operation{Tags: []string{"Settings"}, Summary: "Get WCS settings", OperationID: "getWCSSettings", Parameters: openapi3.Parameters{params.Workspace}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("WCS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WCSSettings"})})},
		Put: &openapi3.Operation{Tags: []string{"Settings"}, Summary: "Update WCS settings", OperationID: "updateWCSSettings", Parameters: openapi3.Parameters{params.Workspace}, RequestBody: jsonBody("WCS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WCSSettings"}), Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Updated WCS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WCSSettings"}), "400": errResp(400, "Limit exceeds server ceiling")})},
	})
	paths.Set("/workspaces/{workspace}/settings/wmts", &openapi3.PathItem{
		Get: &openapi3.Operation{Tags: []string{"Settings"}, Summary: "Get WMTS settings", OperationID: "getWMTSSettings", Parameters: openapi3.Parameters{params.Workspace}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("WMTS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WMTSSettings"})})},
		Put: &openapi3.Operation{Tags: []string{"Settings"}, Summary: "Update WMTS settings", OperationID: "updateWMTSSettings", Parameters: openapi3.Parameters{params.Workspace}, RequestBody: jsonBody("WMTS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WMTSSettings"}), Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Updated WMTS settings", &openapi3.SchemaRef{Ref: "#/components/schemas/WMTSSettings"}), "400": errResp(400, "Bad Request")})},
	})

	paths.Set("/workspaces/{workspace}/tile-cache/stats", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Tile Cache"}, Summary: "Get persistent tile cache usage", OperationID: "getPersistentTileCacheStats",
		Parameters: openapi3.Parameters{params.Workspace}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Persistent tile cache statistics", &openapi3.SchemaRef{Ref: "#/components/schemas/TileCacheStats"})}),
	}})
	paths.Set("/workspaces/{workspace}/tile-cache/jobs", &openapi3.PathItem{
		Get:  &openapi3.Operation{Tags: []string{"Tile Cache"}, Summary: "List tile cache jobs", OperationID: "listTileCacheJobs", Parameters: openapi3.Parameters{params.Workspace}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Tile cache jobs", &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{"jobs": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/TileCacheJob"}}}}}})})},
		Post: &openapi3.Operation{Tags: []string{"Tile Cache"}, Summary: "Create seed, reseed, or truncate job", OperationID: "createTileCacheJob", Parameters: openapi3.Parameters{params.Workspace}, RequestBody: jsonBody("Tile cache job", &openapi3.SchemaRef{Ref: "#/components/schemas/TileCacheJobRequest"}), Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Accepted tile cache job", &openapi3.SchemaRef{Ref: "#/components/schemas/TileCacheJob"}), "422": errResp(422, "Invalid job bounds or selector")})},
	})
	paths.Set("/workspaces/{workspace}/tile-cache/jobs/{job}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Tile Cache"}, Summary: "Get tile cache job", OperationID: "getTileCacheJob", Parameters: openapi3.Parameters{params.Workspace, params.Job}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Tile cache job", &openapi3.SchemaRef{Ref: "#/components/schemas/TileCacheJob"}), "404": errResp(404, "Job not found")})},
		Delete: &openapi3.Operation{Tags: []string{"Tile Cache"}, Summary: "Cancel tile cache job", OperationID: "cancelTileCacheJob", Parameters: openapi3.Parameters{params.Workspace, params.Job}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Cancelling tile cache job", &openapi3.SchemaRef{Ref: "#/components/schemas/TileCacheJob"}), "409": errResp(409, "Job is already terminal")})},
	})
	paths.Set("/workspaces/{workspace}/tile-cache/jobs/{job}/progress", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Tile Cache"}, Summary: "Get per-zoom tile cache job progress", OperationID: "getTileCacheJobProgress", Parameters: openapi3.Parameters{params.Workspace, params.Job},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Tile cache job progress", schemaRef("TileJobProgress")), "404": errResp(404, "Job not found")}),
	}})

	granuleParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "granule", In: "path", Required: true, Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	queryParameter := func(name, description string, schema *openapi3.Schema) *openapi3.ParameterRef {
		return &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: name, In: "query", Description: description, Schema: &openapi3.SchemaRef{Value: schema}}}
	}
	bboxParameter := queryParameter("bbox", "Native-CRS minx,miny,maxx,maxy intersection filter", openapi3.NewStringSchema())
	timeParameter := queryParameter("time", "Exact RFC 3339 granule time", openapi3.NewStringSchema())
	elevationParameter := queryParameter("elevation", "Exact numeric granule elevation", openapi3.NewFloat64Schema())
	limitParameter := queryParameter("limit", "Maximum records to return", openapi3.NewInt64Schema())
	offsetParameter := queryParameter("offset", "Zero-based record offset", openapi3.NewInt64Schema())
	mosaicGranulesResponse := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"granules": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicGranule"}}},
	}}}
	mosaicJobsResponse := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"jobs": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicHarvestJob"}}},
	}}}
	paths.Set("/workspaces/{workspace}/services/{service}/mosaic/granules", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Mosaic Catalog"}, Summary: "List active mosaic granules", OperationID: "listMosaicGranules", Parameters: openapi3.Parameters{params.Workspace, params.Service, bboxParameter, timeParameter, elevationParameter, limitParameter, offsetParameter},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Active granule generation", mosaicGranulesResponse), "503": errResp(503, "Mosaic catalog disabled")}),
	}})
	paths.Set("/workspaces/{workspace}/services/{service}/mosaic/granules/{granule}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Mosaic Catalog"}, Summary: "Get mosaic granule", OperationID: "getMosaicGranule", Parameters: openapi3.Parameters{params.Workspace, params.Service, granuleParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Mosaic granule", &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicGranule"}), "404": errResp(404, "Granule not found")})},
		Delete: &openapi3.Operation{Tags: []string{"Mosaic Catalog"}, Summary: "Remove granule through a new atomic generation", OperationID: "deleteMosaicGranule", Parameters: openapi3.Parameters{params.Workspace, params.Service, granuleParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": noContentResp(), "404": errResp(404, "Granule not found"), "422": errResp(422, "Mosaic must retain at least one granule")})},
	})
	paths.Set("/workspaces/{workspace}/services/{service}/mosaic/harvest-jobs", &openapi3.PathItem{
		Get:  &openapi3.Operation{Tags: []string{"Mosaic Catalog"}, Summary: "List mosaic harvest jobs", OperationID: "listMosaicHarvestJobs", Parameters: openapi3.Parameters{params.Workspace, params.Service, limitParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Harvest jobs", mosaicJobsResponse)})},
		Post: &openapi3.Operation{Tags: []string{"Mosaic Catalog"}, Summary: "Create atomic mosaic harvest job", OperationID: "createMosaicHarvestJob", Parameters: openapi3.Parameters{params.Workspace, params.Service}, RequestBody: jsonBody("Harvest request", &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicHarvestRequest"}), Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Accepted harvest job", &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicHarvestJob"}), "422": errResp(422, "Invalid harvest request")})},
	})
	paths.Set("/workspaces/{workspace}/services/{service}/mosaic/harvest-jobs/{job}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Mosaic Catalog"}, Summary: "Get mosaic harvest job", OperationID: "getMosaicHarvestJob", Parameters: openapi3.Parameters{params.Workspace, params.Service, params.Job}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Harvest job", &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicHarvestJob"}), "404": errResp(404, "Job not found")})},
		Delete: &openapi3.Operation{Tags: []string{"Mosaic Catalog"}, Summary: "Cancel mosaic harvest job", OperationID: "cancelMosaicHarvestJob", Parameters: openapi3.Parameters{params.Workspace, params.Service, params.Job}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Cancelling harvest job", &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicHarvestJob"}), "404": errResp(404, "Job not found")})},
	})

	// ============== Cache Management ==============
	paths.Set("/cache/stats", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Cache"},
			Summary:     "Get cache statistics",
			Description: "Returns statistics for all cache types including hit rates and memory usage. Requires super_admin role.",
			OperationID: "getCacheStats",
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Cache statistics", &openapi3.SchemaRef{Ref: "#/components/schemas/CacheStats"}),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
	})

	paths.Set("/cache/clear", &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Cache"},
			Summary:     "Clear all caches",
			Description: "Clears all cache types (capabilities, collections, features, counts, tiles). Requires super_admin role.",
			OperationID: "clearAllCaches",
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Cache cleared successfully", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"message": {Value: openapi3.NewStringSchema()},
					},
				}}),
				"400": errResp(400, "Caching not enabled"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
	})

	paths.Set("/cache/clear/{cacheType}", &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Cache"},
			Summary:     "Clear specific cache type",
			Description: "Clears a specific cache type. Valid types: capabilities, collections, features, counts, tiles. Requires super_admin role.",
			OperationID: "clearCacheByType",
			Parameters:  openapi3.Parameters{params.CacheType},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Cache cleared successfully", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"message": {Value: openapi3.NewStringSchema()},
						"type":    {Value: openapi3.NewStringSchema()},
					},
				}}),
				"400": errResp(400, "Invalid cache type or caching not enabled"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden - requires super_admin role"),
			}),
		},
	})

	paths.Set("/workspaces/{workspace}/cache/clear", &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Cache"},
			Summary:     "Clear workspace cache",
			Description: "Clears all caches for a specific workspace. Requires workspace admin access.",
			OperationID: "clearWorkspaceCache",
			Parameters:  openapi3.Parameters{params.Workspace},
			Responses: mkResponses(map[string]*openapi3.ResponseRef{
				"200": jsonResp("Workspace cache cleared successfully", &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"message":   {Value: openapi3.NewStringSchema()},
						"workspace": {Value: openapi3.NewStringSchema()},
					},
				}}),
				"400": errResp(400, "Caching not enabled"),
				"401": errResp(401, "Unauthorized"),
				"403": errResp(403, "Forbidden"),
				"404": errResp(404, "Workspace not found"),
			}),
		},
	})

	groupParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "group", In: "path", Required: true, Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	groupsResponse := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"layer_groups": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/LayerGroup"}}},
	}}}
	paths.Set("/workspaces/{workspace}/layer-groups", &openapi3.PathItem{
		Get:  &openapi3.Operation{Tags: []string{"Layer Groups"}, Summary: "List layer groups", OperationID: "listLayerGroups", Parameters: openapi3.Parameters{params.Workspace}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Layer groups", groupsResponse)})},
		Post: &openapi3.Operation{Tags: []string{"Layer Groups"}, Summary: "Create layer group", OperationID: "createLayerGroup", Parameters: openapi3.Parameters{params.Workspace}, RequestBody: jsonBody("Layer group", &openapi3.SchemaRef{Ref: "#/components/schemas/LayerGroup"}), Responses: mkResponses(map[string]*openapi3.ResponseRef{"201": jsonResp("Created layer group", &openapi3.SchemaRef{Ref: "#/components/schemas/LayerGroup"}), "400": errResp(400, "Invalid layer group")})},
	})
	paths.Set("/workspaces/{workspace}/layer-groups/{group}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Layer Groups"}, Summary: "Get layer group", OperationID: "getLayerGroup", Parameters: openapi3.Parameters{params.Workspace, groupParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Layer group", &openapi3.SchemaRef{Ref: "#/components/schemas/LayerGroup"}), "404": errResp(404, "Not found")})},
		Put:    &openapi3.Operation{Tags: []string{"Layer Groups"}, Summary: "Update layer group", OperationID: "updateLayerGroup", Parameters: openapi3.Parameters{params.Workspace, groupParameter}, RequestBody: jsonBody("Layer group update", &openapi3.SchemaRef{Ref: "#/components/schemas/LayerGroup"}), Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Updated layer group", &openapi3.SchemaRef{Ref: "#/components/schemas/LayerGroup"}), "400": errResp(400, "Invalid layer group")})},
		Delete: &openapi3.Operation{Tags: []string{"Layer Groups"}, Summary: "Delete layer group", OperationID: "deleteLayerGroup", Parameters: openapi3.Parameters{params.Workspace, groupParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": {Value: &openapi3.Response{Description: ptrString("Deleted")}}, "409": errResp(409, "Group is referenced")})},
	})

	operationParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "operation", In: "path", Required: true, Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	paths.Set("/deletions", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Catalog Lifecycle"}, Summary: "List visible deletion operations", OperationID: "listDeletionOperations",
		Parameters: openapi3.Parameters{
			{Value: openapi3.NewQueryParameter("workspace").WithSchema(openapi3.NewStringSchema()).WithDescription("Workspace UUID (not name); applies to deleted workspaces too. Requires administrator access.")},
			{Value: openapi3.NewQueryParameter("status").WithSchema(openapi3.NewStringSchema().WithEnum("pending", "running", "failed", "completed", "actionable")).WithDescription("Optional status; actionable includes pending, running and failed")},
			{Value: openapi3.NewQueryParameter("limit").WithSchema(openapi3.NewIntegerSchema().WithMin(1).WithMax(1000)).WithDescription("Page size, default 200; authorization is applied before pagination")},
			{Value: openapi3.NewQueryParameter("cursor").WithSchema(openapi3.NewStringSchema()).WithDescription("Opaque next_cursor; restart pagination when scope or filters change")},
		},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Deletion operations", schemaRef("DeletionList")), "400": errResp(400, "Invalid filter or cursor"), "403": errResp(403, "Workspace administrator required")}),
	}})
	paths.Set("/deletions/{operation}", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Catalog Lifecycle"}, Summary: "Get deletion operation", OperationID: "getDeletionOperation", Parameters: openapi3.Parameters{operationParameter},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Deletion operation", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionOperation"}), "404": errResp(404, "Deletion operation not found")}),
	}})
	paths.Set("/deletions/{operation}/retry", &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Catalog Lifecycle"}, Summary: "Retry deletion operation", OperationID: "retryDeletionOperation", Parameters: openapi3.Parameters{operationParameter},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Deletion retry accepted", &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionOperation"}), "404": errResp(404, "Deletion operation not found")}),
	}})
	confirmParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "confirm", In: "query", Required: true, Description: "Must be true to authorize removal of confirmed orphan state.", Schema: &openapi3.SchemaRef{Value: openapi3.NewBoolSchema()}}}
	paths.Set("/catalog/integrity", &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Catalog Lifecycle"}, Summary: "Audit catalog ownership integrity", OperationID: "getCatalogIntegrity",
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Catalog integrity report", &openapi3.SchemaRef{Ref: "#/components/schemas/IntegrityReport"}), "403": errResp(403, "Super administrator required")}),
	}})
	paths.Set("/catalog/integrity/repair", &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Catalog Lifecycle"}, Summary: "Repair orphaned state from older releases", OperationID: "repairCatalogIntegrity", Parameters: openapi3.Parameters{confirmParameter},
		Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Post-repair integrity report", &openapi3.SchemaRef{Ref: "#/components/schemas/IntegrityReport"}), "400": errResp(400, "Explicit confirmation required"), "403": errResp(403, "Super administrator required")}),
	}})
	importParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "import", In: "path", Required: true, Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	multipartImport := schemaObject(openapi3.Schemas{
		"name": {Value: openapi3.NewStringSchema()}, "file": {Value: openapi3.NewStringSchema().WithFormat("binary")},
	}, "name", "file")
	importBody := &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Description: "URI JSON or uploaded vector dataset", Required: true, Content: openapi3.Content{
		"application/json":    &openapi3.MediaType{Schema: schemaRef("CreateImportURI")},
		"multipart/form-data": &openapi3.MediaType{Schema: multipartImport},
	}}}
	paths.Set("/workspaces/{workspace}/imports", &openapi3.PathItem{
		Get: &openapi3.Operation{Tags: []string{"Imports"}, Summary: "List durable imports", OperationID: "listImports", Parameters: openapi3.Parameters{params.Workspace,
			{Value: openapi3.NewQueryParameter("limit").WithSchema(openapi3.NewIntegerSchema().WithMin(1).WithMax(1000)).WithDescription("Page size; defaults to 100")},
			{Value: openapi3.NewQueryParameter("cursor").WithSchema(openapi3.NewStringSchema()).WithDescription("Opaque next_cursor from the preceding page")},
			{Value: openapi3.NewQueryParameter("status").WithSchema(openapi3.NewStringSchema()).WithDescription("Import status, or actionable to exclude terminal history")},
			{Value: openapi3.NewQueryParameter("search").WithSchema(openapi3.NewStringSchema()).WithDescription("Case-insensitive import name search")},
		}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Import jobs", schemaRef("ImportList"))})},
		Post: &openapi3.Operation{Tags: []string{"Imports"}, Summary: "Create URI or multipart import", OperationID: "createImport", Parameters: openapi3.Parameters{params.Workspace}, RequestBody: importBody, Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Import accepted", schemaRef("ImportJob")), "422": errResp(422, "Invalid import")})},
	})
	paths.Set("/workspaces/{workspace}/imports/{import}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Imports"}, Summary: "Get import", OperationID: "getImport", Parameters: openapi3.Parameters{params.Workspace, importParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Import", schemaRef("ImportJob"))})},
		Delete: &openapi3.Operation{Tags: []string{"Imports"}, Summary: "Cancel import", OperationID: "cancelImport", Parameters: openapi3.Parameters{params.Workspace, importParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Cancellation accepted", schemaRef("ImportJob"))})},
	})
	for path, operationID := range map[string]string{
		"/workspaces/{workspace}/imports/{import}/publish":  "publishImport",
		"/workspaces/{workspace}/imports/{import}/retry":    "retryImport",
		"/workspaces/{workspace}/imports/{import}/rollback": "rollbackImport",
	} {
		paths.Set(path, &openapi3.PathItem{Post: &openapi3.Operation{Tags: []string{"Imports"}, Summary: operationID, OperationID: operationID, Parameters: openapi3.Parameters{params.Workspace, importParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Operation accepted", schemaRef("ImportJob"))})}})
	}
	paths.Set("/workspaces/{workspace}/imports/{import}/plan", &openapi3.PathItem{Put: &openapi3.Operation{Tags: []string{"Imports"}, Summary: "Validate and set import plan", OperationID: "setImportPlan", Parameters: openapi3.Parameters{params.Workspace, importParameter}, RequestBody: jsonBody("Import plan", schemaRef("ImportPlan")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"202": jsonResp("Plan accepted", schemaRef("ImportJob"))})}})
	paths.Set("/workspaces/{workspace}/imports/{import}/preview", &openapi3.PathItem{Get: &openapi3.Operation{Tags: []string{"Imports"}, Summary: "Preview planned import features", OperationID: "previewImport", Parameters: openapi3.Parameters{params.Workspace, importParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("GeoJSON preview", schemaRef("ImportPreview"))})}})
	paths.Set("/workspaces/{workspace}/imports/{import}/history", &openapi3.PathItem{Get: &openapi3.Operation{Tags: []string{"Imports"}, Summary: "List import transition history", OperationID: "getImportHistory", Parameters: openapi3.Parameters{params.Workspace, importParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Import history", schemaRef("ImportHistory"))})}})
	tmsParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "tileMatrixSet", In: "path", Required: true, Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	paths.Set("/tile-matrix-sets", &openapi3.PathItem{Get: &openapi3.Operation{Tags: []string{"Tile Matrix Sets"}, Summary: "List supported tile matrix sets", OperationID: "listTileMatrixSets", Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Tile matrix sets", schemaRef("TileMatrixSetList"))})}})
	paths.Set("/tile-matrix-sets/{tileMatrixSet}", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Tile Matrix Sets"}, Summary: "Get custom tile matrix set record", OperationID: "getTileMatrixSet", Parameters: openapi3.Parameters{tmsParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Tile matrix set", schemaRef("TileMatrixSetRecord"))})},
		Put:    &openapi3.Operation{Tags: []string{"Tile Matrix Sets"}, Summary: "Create or replace custom tile matrix set", OperationID: "putTileMatrixSet", Parameters: openapi3.Parameters{tmsParameter}, RequestBody: jsonBody("OGC TileMatrixSet definition", schemaRef("TileMatrixSetDefinition")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Tile matrix set", schemaRef("TileMatrixSetRecord"))})},
		Delete: &openapi3.Operation{Tags: []string{"Tile Matrix Sets"}, Summary: "Delete custom tile matrix set", OperationID: "deleteTileMatrixSet", Parameters: openapi3.Parameters{tmsParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": {Value: &openapi3.Response{Description: ptrString("Deleted")}}})},
	})
	paths.Set("/audit", &openapi3.PathItem{Get: &openapi3.Operation{Tags: []string{"Audit"}, Summary: "List durable audit events", OperationID: "listAuditEvents", Parameters: openapi3.Parameters{
		{Value: openapi3.NewQueryParameter("cursor").WithSchema(openapi3.NewStringSchema()).WithDescription("Opaque next_cursor from the preceding page; newest first, stable timestamp and ID ordering")},
		{Value: openapi3.NewQueryParameter("q").WithSchema(openapi3.NewStringSchema().WithMaxLength(512)).WithDescription("Case-insensitive substring search across retained event attribution, operation, path and status")},
		{Value: openapi3.NewQueryParameter("workspace").WithSchema(openapi3.NewStringSchema()).WithDescription("Exact recorded workspace scope")},
		{Value: openapi3.NewQueryParameter("principal").WithSchema(openapi3.NewStringSchema()).WithDescription("Exact recorded principal")},
		{Value: openapi3.NewQueryParameter("credential_id").WithSchema(openapi3.NewStringSchema()).WithDescription("Stable non-secret API-key ID; never the key secret")},
		{Value: openapi3.NewQueryParameter("since").WithSchema(openapi3.NewStringSchema().WithFormat("date-time")).WithDescription("Only events at or after this time")},
		{Value: openapi3.NewQueryParameter("outcome").WithSchema(openapi3.NewStringSchema().WithEnum("failed", "succeeded")).WithDescription("failed: status 400 or higher; succeeded: below 400")},
		{Value: openapi3.NewQueryParameter("limit").WithSchema(openapi3.NewIntegerSchema().WithMin(1).WithMax(1000)).WithDescription("Maximum events; defaults to 100")},
	}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Audit events", schemaRef("AuditList"))})}})
	paths.Set("/audit/retention", &openapi3.PathItem{Post: &openapi3.Operation{Tags: []string{"Audit"}, Summary: "Run audit retention", OperationID: "runAuditRetention", Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": {Value: &openapi3.Response{Description: ptrString("Retention complete")}}})}})
	rolePolicyParameter := &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: "roleId", In: "path", Required: true, Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}
	paths.Set("/roles/{roleId}/policies", &openapi3.PathItem{
		Get:    &openapi3.Operation{Tags: []string{"Roles"}, Summary: "List role policies", OperationID: "listRolePolicies", Parameters: openapi3.Parameters{rolePolicyParameter}, Responses: mkResponses(map[string]*openapi3.ResponseRef{"200": jsonResp("Role policies", schemaRef("RolePolicyList"))})},
		Post:   &openapi3.Operation{Tags: []string{"Roles"}, Summary: "Grant a service or operation", OperationID: "addRolePolicy", Parameters: openapi3.Parameters{rolePolicyParameter}, RequestBody: jsonBody("Operation policy", schemaRef("RolePolicy")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"201": {Value: &openapi3.Response{Description: ptrString("Granted")}}})},
		Delete: &openapi3.Operation{Tags: []string{"Roles"}, Summary: "Remove a service or operation grant", OperationID: "removeRolePolicy", Parameters: openapi3.Parameters{rolePolicyParameter}, RequestBody: jsonBody("Operation policy", schemaRef("RolePolicy")), Responses: mkResponses(map[string]*openapi3.ResponseRef{"204": {Value: &openapi3.Response{Description: ptrString("Removed")}}})},
	})
	// Middleware failures use the same typed envelope as handler failures.
	for _, item := range paths.Map() {
		for _, operation := range item.Operations() {
			for code, response := range map[string]*openapi3.ResponseRef{
				"401": errResp(401, "Authentication required or credential rejected"),
				"403": errResp(403, "Insufficient permission, disabled login, or invalid CSRF"),
				"413": errResp(413, "Request body exceeds the configured limit"),
				"426": errResp(426, "HTTPS required"),
				"429": errResp(429, "Request rate exceeded"),
				"503": errResp(503, "Service temporarily unavailable"),
			} {
				if operation.Responses.Value(code) == nil {
					operation.Responses.Set(code, response)
				}
			}
		}
	}
	paths.Value("/roles").Post.Responses.Set("409", errResp(409, "Role ID already exists or has been retired"))
	paths.Value("/workspaces/{workspace}/imports").Post.Responses.Set("408", errResp(408, "Upload idle or total timeout exceeded"))
	return paths
}
