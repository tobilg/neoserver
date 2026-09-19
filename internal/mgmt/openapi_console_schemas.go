package mgmt

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/tobilg/neoserver/internal/protocolrequest"
)

func schemaObject(properties openapi3.Schemas, required ...string) *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: properties, Required: required}}
}

func schemaArray(ref string) *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/" + ref}}}
}

func schemaRef(name string) *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Ref: "#/components/schemas/" + name}
}

// getConsoleSchemas contains the contracts that were missing or previously
// represented as untyped objects in the hand-written management specification.
func getConsoleSchemas() openapi3.Schemas {
	stringArray := func() *openapi3.SchemaRef {
		return &openapi3.SchemaRef{Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())}
	}
	dateTime := func() *openapi3.SchemaRef {
		return &openapi3.SchemaRef{Value: openapi3.NewStringSchema().WithFormat("date-time")}
	}
	int64Value := func() *openapi3.SchemaRef { return &openapi3.SchemaRef{Value: openapi3.NewInt64Schema()} }
	stringValue := func() *openapi3.SchemaRef { return &openapi3.SchemaRef{Value: openapi3.NewStringSchema()} }
	boolValue := func() *openapi3.SchemaRef { return &openapi3.SchemaRef{Value: openapi3.NewBoolSchema()} }
	floatValue := func() *openapi3.SchemaRef { return &openapi3.SchemaRef{Value: openapi3.NewFloat64Schema()} }
	claimValue := &openapi3.SchemaRef{Value: &openapi3.Schema{OneOf: openapi3.SchemaRefs{
		{Value: openapi3.NewStringSchema()}, {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
	}}}
	propertyValue := &openapi3.SchemaRef{Value: &openapi3.Schema{OneOf: openapi3.SchemaRefs{
		{Value: openapi3.NewStringSchema()}, {Value: openapi3.NewFloat64Schema()}, {Value: openapi3.NewBoolSchema()},
		{Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
	}}}

	importProperty := schemaObject(openapi3.Schemas{
		"name": stringValue(), "type": stringValue(), "json_type": stringValue(),
		"ordinal": {Value: openapi3.NewIntegerSchema()},
	}, "name", "type")
	importDiscoveredLayer := schemaObject(openapi3.Schemas{
		"name": stringValue(), "title": stringValue(), "description": stringValue(),
		"geometry_column": stringValue(), "geometry_type": stringValue(), "srid": {Value: openapi3.NewIntegerSchema()},
		"id_column": stringValue(), "feature_count": int64Value(), "native_extent": schemaRef("SpatialExtent"),
		"properties": schemaArray("ImportProperty"),
	}, "name", "geometry_column", "geometry_type", "srid", "feature_count")
	importDiscovery := schemaObject(openapi3.Schemas{
		"layers": schemaArray("ImportDiscoveredLayer"), "warnings": stringArray(),
	}, "layers")
	importFieldMapping := schemaObject(openapi3.Schemas{
		"source": stringValue(), "target": stringValue(), "type": stringValue(), "include": boolValue(),
	}, "source", "target", "include")
	importLayerPlan := schemaObject(openapi3.Schemas{
		"source_layer": stringValue(), "public_id": stringValue(), "title": stringValue(), "description": stringValue(),
		"enabled": boolValue(), "public": boolValue(), "allowed_roles": stringArray(), "fields": schemaArray("ImportFieldMapping"),
		"geometry_column": stringValue(), "target_geometry_column": stringValue(), "id_column": stringValue(),
		"source_srid": {Value: openapi3.NewIntegerSchema()}, "target_srid": {Value: openapi3.NewIntegerSchema()},
	}, "source_layer", "public_id", "enabled", "public")
	importPlan := schemaObject(openapi3.Schemas{
		"service_name": stringValue(), "layers": schemaArray("ImportLayerPlan"),
	}, "service_name", "layers")
	importJob := schemaObject(openapi3.Schemas{
		"id": stringValue(), "workspace_id": stringValue(), "name": stringValue(), "source_kind": stringValue(),
		"source_locator": stringValue(), "source_filename": stringValue(),
		"status": {Value: openapi3.NewStringSchema().WithEnum("queued", "running", "awaiting_plan", "ready_to_publish", "publishing", "published", "cancelling", "cancelled", "failed", "rolling_back", "rolled_back")},
		"phase":  {Value: openapi3.NewStringSchema().WithEnum("acquire", "discover", "validate", "transform", "preview", "publish", "rollback")},
		"plan":   schemaRef("ImportPlan"), "discovery": schemaRef("ImportDiscovery"), "service_id": stringValue(),
		"rollback_operation_id": stringValue(), "processed_bytes": int64Value(), "processed_features": int64Value(),
		"processed_layers": {Value: openapi3.NewIntegerSchema()}, "total_layers": {Value: openapi3.NewIntegerSchema()},
		"retry_count": {Value: openapi3.NewIntegerSchema()}, "cancel_requested": boolValue(), "error_message": stringValue(),
		"created_by": stringValue(), "created_at": dateTime(), "started_at": dateTime(), "completed_at": dateTime(), "updated_at": dateTime(),
	}, "id", "workspace_id", "name", "source_kind", "status", "phase", "created_at", "updated_at")
	importEvent := schemaObject(openapi3.Schemas{
		"id": stringValue(), "import_id": stringValue(), "workspace_id": stringValue(), "status": stringValue(), "phase": stringValue(),
		"warnings": stringArray(), "error_message": stringValue(), "processed_bytes": int64Value(), "processed_features": int64Value(),
		"processed_layers": {Value: openapi3.NewIntegerSchema()}, "created_at": dateTime(),
	}, "id", "import_id", "workspace_id", "status", "phase", "created_at")
	createImportURI := schemaObject(openapi3.Schemas{"name": stringValue(), "source_uri": stringValue()}, "name", "source_uri")
	importList := schemaObject(openapi3.Schemas{"imports": schemaArray("ImportJob"), "next_cursor": stringValue()}, "imports")
	importHistory := schemaObject(openapi3.Schemas{"events": schemaArray("ImportEvent")}, "events")
	geoJSONProperties := schemaObject(openapi3.Schemas{})
	geoJSONProperties.Value.AdditionalProperties = openapi3.AdditionalProperties{Schema: propertyValue}
	geoJSONFeature := schemaObject(openapi3.Schemas{
		"type": {Value: openapi3.NewStringSchema().WithEnum("Feature")},
		"id":   stringValue(), "geometry": schemaObject(openapi3.Schemas{"type": stringValue()}),
		"properties": geoJSONProperties,
	}, "type", "geometry", "properties")
	importPreview := schemaObject(openapi3.Schemas{
		"type": {Value: openapi3.NewStringSchema().WithEnum("FeatureCollection")}, "features": schemaArray("GeoJSONFeature"),
	}, "type", "features")

	auditEvent := schemaObject(openapi3.Schemas{
		"id": stringValue(), "timestamp": dateTime(), "request_id": stringValue(), "principal": stringValue(), "auth_method": stringValue(),
		"credential_id": stringValue(),
		"workspace":     stringValue(), "protocol": stringValue(), "operation": stringValue(), "action": stringValue(), "method": stringValue(),
		"path": stringValue(), "status": {Value: openapi3.NewIntegerSchema()}, "duration_ms": int64Value(), "security_event": boolValue(),
	}, "id", "timestamp", "action", "method", "path", "status", "duration_ms", "security_event")
	auditEvent.Value.Properties["credential_id"].Value.Description = "Stable non-secret API-key ID, including key-backed browser sessions; absent for historical events without attribution."
	auditList := schemaObject(openapi3.Schemas{"events": schemaArray("AuditEvent"), "next_cursor": {Value: openapi3.NewStringSchema()}}, "events")
	rolePolicy := schemaObject(openapi3.Schemas{
		"workspace": stringValue(), "service": stringValue(), "operation": stringValue(), "action": stringValue(),
	}, "workspace", "service", "action")
	rolePolicy.Value.Properties["workspace"].Value.Description = "Existing workspace UUID or name (stored as UUID), or * for all workspaces. Removal uses the exact stored scope."
	rolePolicy.Value.Properties["operation"].Value.Description = "A supported operation, or * / empty for all operations with the selected action. WFS stored-query administration requires manage; transactions and locks require write."
	rolePolicy.Value.Extensions = map[string]any{"x-service-operations": protocolrequest.PolicyOperations()}
	rolePolicyList := schemaObject(openapi3.Schemas{
		"policies": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())}}},
	}, "policies")

	tileMatrix := schemaObject(openapi3.Schemas{
		"id": stringValue(), "scaleDenominator": floatValue(), "cellSize": floatValue(), "cornerOfOrigin": stringValue(),
		"pointOfOrigin": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewFloat64Schema())}, "tileWidth": {Value: openapi3.NewIntegerSchema()},
		"tileHeight": {Value: openapi3.NewIntegerSchema()}, "matrixWidth": {Value: openapi3.NewIntegerSchema()}, "matrixHeight": {Value: openapi3.NewIntegerSchema()},
	}, "id", "scaleDenominator", "cellSize", "pointOfOrigin", "tileWidth", "tileHeight", "matrixWidth", "matrixHeight")
	tileMatrixBoundingBox := schemaObject(openapi3.Schemas{
		"crs": stringValue(), "lowerCorner": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewFloat64Schema())},
		"upperCorner": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewFloat64Schema())},
	}, "lowerCorner", "upperCorner")
	tileMatrixSet := schemaObject(openapi3.Schemas{
		"id": stringValue(), "title": stringValue(), "description": stringValue(), "uri": stringValue(), "crs": stringValue(),
		"orderedAxes": stringArray(), "wellKnownScaleSet": stringValue(), "boundingBox": schemaRef("TileMatrixBoundingBox"),
		"tileMatrices": schemaArray("TileMatrix"),
	}, "id", "crs", "tileMatrices")
	tileMatrixSetSummary := schemaObject(openapi3.Schemas{
		"id": stringValue(), "title": stringValue(), "uri": stringValue(), "crs": stringValue(), "built_in": boolValue(),
	}, "id", "built_in")
	tileMatrixSetList := schemaObject(openapi3.Schemas{"tile_matrix_sets": schemaArray("TileMatrixSetSummary")}, "tile_matrix_sets")
	tileMatrixSetDefinitions := schemaObject(openapi3.Schemas{"tile_matrix_sets": schemaArray("TileMatrixSet")}, "tile_matrix_sets")
	tileMatrixSetRecord := schemaObject(openapi3.Schemas{
		"id": stringValue(), "definition": schemaRef("TileMatrixSetDefinition"), "revision": int64Value(),
		"digest": stringValue(), "created_at": dateTime(), "updated_at": dateTime(),
	}, "id", "definition", "revision", "digest", "created_at", "updated_at")

	loginRequest := schemaObject(openapi3.Schemas{
		"method": {Value: openapi3.NewStringSchema().WithEnum("password", "token", "oidc")}, "username": stringValue(),
		"password": stringValue(), "token": stringValue(), "id_token": stringValue(),
	}, "method")
	authWorkspace := schemaObject(openapi3.Schemas{
		"id": stringValue(), "name": stringValue(), "role": stringValue(), "console_access": boolValue(),
	}, "id", "name", "role", "console_access")
	authSession := schemaObject(openapi3.Schemas{"expires_at": dateTime(), "idle_expires_at": dateTime()})
	capabilities := schemaObject(openapi3.Schemas{
		"manage_workspaces": boolValue(), "manage_sessions": boolValue(), "manage_roles": boolValue(), "manage_global_roles": boolValue(),
		"manage_tile_matrix_sets": boolValue(), "read_audit": boolValue(), "manage_global_cache": boolValue(),
		"read_catalog_integrity": boolValue(), "manage_workspace": boolValue(),
	})
	presentedClaimValues := schemaObject(openapi3.Schemas{})
	presentedClaimValues.Value.AdditionalProperties = openapi3.AdditionalProperties{Schema: claimValue}
	presentedClaims := schemaObject(openapi3.Schemas{
		"iss": stringValue(), "sub": stringValue(), "email": stringValue(), "claims": presentedClaimValues,
	})
	// Mirrors meResponse in auth.go. The flat session_* fields and `subject`
	// duplicate the nested `session` object and `principal` for clients that
	// read them directly; `console_access` is the gate that decides whether a
	// principal may enter the console at all, so it must be documented.
	authMe := schemaObject(openapi3.Schemas{
		"authenticated": boolValue(), "principal": stringValue(), "subject": stringValue(),
		"display_name": stringValue(), "email": stringValue(), "auth_method": stringValue(),
		"session": authSession, "session_id": stringValue(),
		"session_expires_at": dateTime(), "session_idle_expires_at": dateTime(),
		"super_admin": boolValue(), "global_role": stringValue(), "console_access": boolValue(),
		"api_key_id": stringValue(), "workspaces": schemaArray("AuthWorkspace"),
		"capabilities": capabilities, "presented_claims": presentedClaims,
	}, "authenticated", "principal", "auth_method", "super_admin", "console_access", "workspaces", "capabilities")
	oidcConfig := schemaObject(openapi3.Schemas{
		"enabled": boolValue(), "issuer": stringValue(), "client_id": stringValue(), "scopes": stringArray(), "redirect_uri": stringValue(), "group_claims": stringArray(),
	}, "enabled", "issuer", "client_id", "scopes", "redirect_uri")
	authConfig := schemaObject(openapi3.Schemas{
		"enabled": boolValue(), "method": stringValue(), "password_login": boolValue(), "token_login": boolValue(), "oidc": oidcConfig,
	}, "enabled", "method", "password_login", "token_login", "oidc")
	serviceFlags := schemaObject(openapi3.Schemas{
		"ogcapi": boolValue(), "wms": boolValue(), "wfs": boolValue(), "wcs": boolValue(), "wmts": boolValue(), "tiles": boolValue(),
	})
	featureFlags := schemaObject(openapi3.Schemas{
		"imports": boolValue(), "audit": boolValue(), "persistent_tile_cache": boolValue(), "mosaic": boolValue(), "pprof": boolValue(),
	})
	consoleLimits := schemaObject(openapi3.Schemas{
		"upload_bytes": int64Value(), "source_bytes": int64Value(), "layers": {Value: openapi3.NewIntegerSchema()},
		"features": int64Value(), "page_size": {Value: openapi3.NewIntegerSchema()},
	})
	consoleConfig := schemaObject(openapi3.Schemas{
		"version": stringValue(), "commit": stringValue(), "title": stringValue(), "base_path": stringValue(), "url_base": stringValue(), "server_started_at": dateTime(),
		"auth": authConfig, "services": serviceFlags, "features": featureFlags,
		"basemap_url": stringValue(), "allowed_paths": stringArray(), "limits": consoleLimits,
	}, "version", "base_path", "auth", "services", "features")
	browserSession := schemaObject(openapi3.Schemas{
		"id": stringValue(), "subject": stringValue(), "email": stringValue(), "display_name": stringValue(), "auth_method": stringValue(),
		"global_role": stringValue(), "remote_addr": stringValue(), "user_agent": stringValue(), "created_at": dateTime(), "last_seen_at": dateTime(),
		"idle_expires_at": dateTime(), "expires_at": dateTime(), "revoked_at": dateTime(),
		"console_access": {Value: openapi3.NewBoolSchema()}, "presented_claims": presentedClaims,
		"roles": {Value: &openapi3.Schema{Type: &openapi3.Types{openapi3.TypeObject}, AdditionalProperties: openapi3.AdditionalProperties{Schema: stringValue()}}},
	}, "id", "subject", "auth_method", "created_at", "last_seen_at", "idle_expires_at", "expires_at", "console_access")
	browserSessionList := schemaObject(openapi3.Schemas{"sessions": schemaArray("BrowserSession")}, "sessions")

	connectionTest := schemaObject(openapi3.Schemas{"ok": boolValue(), "duration_ms": floatValue(), "message": stringValue()}, "ok", "duration_ms")
	connectionInfo := schemaObject(openapi3.Schemas{
		"host": stringValue(), "port": {Value: openapi3.NewIntegerSchema()}, "database": stringValue(), "user": stringValue(),
		"password": stringValue(), "sslmode": stringValue(), "schema": stringValue(), "path": stringValue(), "url": stringValue(),
		"connection_string": stringValue(), "table": stringValue(), "layer": stringValue(), "glob": stringValue(),
		"srid":        {Value: openapi3.NewIntegerSchema().WithMin(1)},
		"layer_srids": {Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, AdditionalProperties: openapi3.AdditionalProperties{Schema: &openapi3.SchemaRef{Value: openapi3.NewIntegerSchema().WithMin(1)}}}},
	})
	coverageInfo := schemaObject(openapi3.Schemas{
		"crs": stringValue(), "srid": {Value: openapi3.NewIntegerSchema()}, "axis_labels": stringArray(),
		"width": {Value: openapi3.NewIntegerSchema()}, "height": {Value: openapi3.NewIntegerSchema()},
		"origin_x": floatValue(), "origin_y": floatValue(), "resolution_x": floatValue(), "resolution_y": floatValue(),
		"envelope": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewFloat64Schema())},
	}, "crs", "axis_labels", "width", "height", "origin_x", "origin_y", "resolution_x", "resolution_y", "envelope")
	discoveredCoverage := schemaObject(openapi3.Schemas{
		"source_coverage": stringValue(), "title": stringValue(), "description": stringValue(), "info": coverageInfo,
	}, "source_coverage", "info")
	coverageDiscoveryList := schemaObject(openapi3.Schemas{"coverages": schemaArray("DiscoveredCoverage")}, "coverages")
	coverageList := schemaObject(openapi3.Schemas{"coverages": schemaArray("Coverage")}, "coverages")
	workspaceCounts := schemaObject(openapi3.Schemas{
		"services": int64Value(), "layers": int64Value(), "coverages": int64Value(), "styles": int64Value(), "api_keys": int64Value(), "imports": int64Value(),
	})
	protocolFlags := schemaObject(openapi3.Schemas{
		"ogcapi": boolValue(), "wms": boolValue(), "wfs": boolValue(), "wcs": boolValue(), "wmts": boolValue(), "ogc_tiles": boolValue(),
	})
	activeJobs := schemaObject(openapi3.Schemas{"imports": {Value: openapi3.NewIntegerSchema()}, "tile_cache": {Value: openapi3.NewIntegerSchema()}})
	workspaceSummary := schemaObject(openapi3.Schemas{
		"workspace_id": stringValue(), "counts": workspaceCounts, "protocols": protocolFlags, "active_jobs": activeJobs,
	}, "workspace_id", "counts", "protocols", "active_jobs")
	deletionList := schemaObject(openapi3.Schemas{"deletions": schemaArray("DeletionOperation"), "next_cursor": {Value: openapi3.NewStringSchema()}}, "deletions")
	zoomProgress := schemaObject(openapi3.Schemas{
		"zoom": {Value: openapi3.NewIntegerSchema()}, "total_tiles": int64Value(), "processed_tiles": int64Value(), "failed_chunks": int64Value(),
	}, "zoom", "total_tiles", "processed_tiles", "failed_chunks")
	tileJobProgress := schemaObject(openapi3.Schemas{"job_id": stringValue(), "zooms": schemaArray("TileZoomProgress")}, "job_id", "zooms")

	return openapi3.Schemas{
		"ImportProperty": importProperty, "ImportDiscoveredLayer": importDiscoveredLayer, "ImportDiscovery": importDiscovery,
		"ImportFieldMapping": importFieldMapping, "ImportLayerPlan": importLayerPlan, "ImportPlan": importPlan, "ImportJob": importJob,
		"ImportEvent": importEvent, "CreateImportURI": createImportURI, "ImportList": importList, "ImportHistory": importHistory,
		"GeoJSONFeature": geoJSONFeature, "ImportPreview": importPreview, "AuditEvent": auditEvent, "AuditList": auditList,
		"RolePolicy": rolePolicy, "RolePolicyList": rolePolicyList, "TileMatrix": tileMatrix, "TileMatrixBoundingBox": tileMatrixBoundingBox,
		"TileMatrixSet": tileMatrixSet, "TileMatrixSetDefinition": tileMatrixSet, "TileMatrixSetRecord": tileMatrixSetRecord, "TileMatrixSetSummary": tileMatrixSetSummary, "TileMatrixSetList": tileMatrixSetList,
		"TileMatrixSetDefinitions": tileMatrixSetDefinitions, "LoginRequest": loginRequest, "AuthWorkspace": authWorkspace,
		"AuthSession": authSession, "AuthMe": authMe, "OIDCConsoleConfig": oidcConfig, "AuthConsoleConfig": authConfig,
		"ConsoleConfig": consoleConfig, "BrowserSession": browserSession, "BrowserSessionList": browserSessionList,
		"ConnectionTest": connectionTest, "WorkspaceCounts": workspaceCounts, "WorkspaceSummary": workspaceSummary,
		"ConnectionInfo": connectionInfo, "CoverageInfo": coverageInfo, "DiscoveredCoverage": discoveredCoverage,
		"CoverageDiscoveryList": coverageDiscoveryList, "CoverageList": coverageList,
		"DeletionList": deletionList, "TileZoomProgress": zoomProgress, "TileJobProgress": tileJobProgress,
	}
}
