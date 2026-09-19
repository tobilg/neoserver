package mgmt

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/tobilg/neoserver/internal/store"
)

// getSchemas returns all OpenAPI schema definitions for the Management API.
func getSchemas() openapi3.Schemas {
	errorSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"code":    {Value: openapi3.NewIntegerSchema()},
			"message": {Value: openapi3.NewStringSchema()},
			"detail":  {Value: openapi3.NewStringSchema()},
		},
		Required: []string{"code", "message"},
	}}

	workspaceSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":          {Value: openapi3.NewStringSchema()},
			"name":        {Value: openapi3.NewStringSchema()},
			"description": {Value: openapi3.NewStringSchema()},
			"created_at":  {Value: openapi3.NewStringSchema().WithFormat("date-time")},
			"updated_at":  {Value: openapi3.NewStringSchema().WithFormat("date-time")},
			// WorkspaceResponse carries per-workspace counts; the console
			// renders them on the workspace list.
			"counts": {Ref: "#/components/schemas/WorkspaceCounts"},
		},
		Required: []string{"id", "name", "created_at", "updated_at"},
	}}

	serviceCacheSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"features_enabled": {Value: openapi3.NewBoolSchema()},
			"features_ttl_sec": {Value: openapi3.NewIntegerSchema()},
			"tiles_enabled":    {Value: openapi3.NewBoolSchema()},
			"tiles_ttl_sec":    {Value: openapi3.NewIntegerSchema()},
		},
	}}

	serviceSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":              {Value: openapi3.NewStringSchema()},
			"workspace_id":    {Value: openapi3.NewStringSchema()},
			"name":            {Value: openapi3.NewStringSchema()},
			"type":            {Value: openapi3.NewStringSchema().WithEnum("postgis", "duckdb", "geoparquet", "vectorfile", "rasterfile", "raster_mosaic")},
			"connection_info": {Ref: "#/components/schemas/ConnectionInfo"},
			"cache_settings":  serviceCacheSettingsSchema,
			"enabled":         {Value: openapi3.NewBoolSchema()},
			"created_at":      {Value: openapi3.NewStringSchema().WithFormat("date-time")},
			"updated_at":      {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		},
		Required: []string{"id", "workspace_id", "name", "type", "enabled", "created_at", "updated_at"},
	}}

	dimensionSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"name":            {Value: openapi3.NewStringSchema()},
			"units":           {Value: openapi3.NewStringSchema()},
			"source_axis":     {Value: openapi3.NewStringSchema()},
			"source_property": {Value: openapi3.NewStringSchema()},
			"end_property":    {Value: openapi3.NewStringSchema()},
			"default":         {Value: openapi3.NewStringSchema()},
			"multiple_values": {Value: openapi3.NewBoolSchema()},
			"nearest_value":   {Value: openapi3.NewBoolSchema()},
			"current":         {Value: openapi3.NewBoolSchema()},
			"extent":          {Value: openapi3.NewStringSchema()},
		},
		Required: []string{"name", "units", "extent"},
	}}

	sqlViewPropertySchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"name": {Value: openapi3.NewStringSchema()},
			"type": {Value: openapi3.NewStringSchema()},
		},
		Required: []string{"name", "type"},
	}}

	sqlViewSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"sql":             {Value: openapi3.NewStringSchema()},
			"geometry_column": {Value: openapi3.NewStringSchema()},
			"geometry_type":   {Value: openapi3.NewStringSchema()},
			"srid":            {Value: openapi3.NewIntegerSchema()},
			"id_column":       {Value: openapi3.NewStringSchema()},
			"properties": {Value: &openapi3.Schema{
				Type:  &openapi3.Types{"array"},
				Items: &openapi3.SchemaRef{Ref: "#/components/schemas/SQLViewProperty"},
			}},
			"read_only": {Value: openapi3.NewBoolSchema()},
		},
		Required: []string{"sql", "geometry_column"},
	}}

	spatialExtentSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"min_x": {Value: openapi3.NewFloat64Schema()}, "min_y": {Value: openapi3.NewFloat64Schema()},
		"max_x": {Value: openapi3.NewFloat64Schema()}, "max_y": {Value: openapi3.NewFloat64Schema()},
		"srid": {Value: openapi3.NewIntegerSchema()}, "stale": {Value: openapi3.NewBoolSchema()},
	}, Required: []string{"min_x", "min_y", "max_x", "max_y", "srid"}}}

	layerSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":           {Value: openapi3.NewStringSchema()},
			"service_id":   {Value: openapi3.NewStringSchema()},
			"source_layer": {Value: openapi3.NewStringSchema()},
			"public_id":    {Value: openapi3.NewStringSchema()},
			"title":        {Value: openapi3.NewStringSchema()},
			"description":  {Value: openapi3.NewStringSchema()},
			"enabled":      {Value: openapi3.NewBoolSchema()},
			"crs_default":  {Value: openapi3.NewIntegerSchema()},
			"dimensions": {Value: &openapi3.Schema{
				Type:  &openapi3.Types{"array"},
				Items: &openapi3.SchemaRef{Ref: "#/components/schemas/Dimension"},
			}},
			"is_sql_view":            {Value: openapi3.NewBoolSchema()},
			"sql_view_config":        sqlViewSchema,
			"public":                 {Value: openapi3.NewBoolSchema()},
			"allowed_roles":          {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
			"default_style":          {Value: openapi3.NewStringSchema()},
			"styles":                 {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
			"native_extent":          spatialExtentSchema,
			"tile_cache_quota_bytes": {Value: openapi3.NewInt64Schema()},
			"tile_cache_generation":  {Value: openapi3.NewInt64Schema()},
			"created_at":             {Value: openapi3.NewStringSchema().WithFormat("date-time")},
			"updated_at":             {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		},
		Required: []string{"id", "service_id", "source_layer", "public_id", "enabled", "created_at", "updated_at"},
	}}

	layerGroupMemberSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"resource": {Value: openapi3.NewStringSchema()}, "style": {Value: openapi3.NewStringSchema()},
		"opacity": {Value: openapi3.NewFloat64Schema()}, "composite": {Value: openapi3.NewStringSchema().WithEnum("source-over", "multiply", "screen", "overlay", "darken", "lighten")},
	}, Required: []string{"resource"}}}
	layerGroupSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "workspace_id": {Value: openapi3.NewStringSchema()}, "public_id": {Value: openapi3.NewStringSchema()},
		"title": {Value: openapi3.NewStringSchema()}, "description": {Value: openapi3.NewStringSchema()}, "enabled": {Value: openapi3.NewBoolSchema()},
		"public": {Value: openapi3.NewBoolSchema()}, "allowed_roles": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"members":       {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/LayerGroupMember"}}},
		"default_style": {Value: openapi3.NewStringSchema()}, "styles": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"native_extent": spatialExtentSchema, "tile_cache_quota_bytes": {Value: openapi3.NewInt64Schema()}, "tile_cache_generation": {Value: openapi3.NewInt64Schema()},
	}, Required: []string{"public_id", "members"}}}

	discoveredLayerSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"name":            {Value: openapi3.NewStringSchema()},
			"schema":          {Value: openapi3.NewStringSchema()},
			"title":           {Value: openapi3.NewStringSchema()},
			"description":     {Value: openapi3.NewStringSchema()},
			"geometry_column": {Value: openapi3.NewStringSchema()},
			"geometry_type":   {Value: openapi3.NewStringSchema()},
			"srid":            {Value: openapi3.NewIntegerSchema()},
		},
		Required: []string{"name"},
	}}

	sqlViewColumnSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"name": {Value: openapi3.NewStringSchema()},
			"type": {Value: openapi3.NewStringSchema()},
		},
		Required: []string{"name", "type"},
	}}

	sqlViewDiscoverySchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"columns": {Value: &openapi3.Schema{
				Type:  &openapi3.Types{"array"},
				Items: &openapi3.SchemaRef{Ref: "#/components/schemas/SQLViewColumn"},
			}},
			"geometry_column":     {Value: openapi3.NewStringSchema()},
			"geometry_type":       {Value: openapi3.NewStringSchema()},
			"srid":                {Value: openapi3.NewIntegerSchema()},
			"suggested_id_column": {Value: openapi3.NewStringSchema()},
		},
		Required: []string{"columns", "geometry_column", "geometry_type", "srid"},
	}}

	validateSQLResponseSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"valid":      {Value: openapi3.NewBoolSchema()},
			"error":      {Value: openapi3.NewStringSchema()},
			"discovered": sqlViewDiscoverySchema,
		},
		Required: []string{"valid"},
	}}

	apiKeySchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":           {Value: openapi3.NewStringSchema()},
			"key_prefix":   {Value: openapi3.NewStringSchema()},
			"workspace_id": {Value: openapi3.NewStringSchema()},
			"role_id":      {Value: openapi3.NewStringSchema()},
			"name":         {Value: openapi3.NewStringSchema()},
			"owner_name":   {Value: openapi3.NewStringSchema()},
			"owner_email":  {Value: openapi3.NewStringSchema()},
			"expires_at":   {Value: openapi3.NewStringSchema().WithFormat("date-time")},
			"revoked":      {Value: openapi3.NewBoolSchema()},
			"created_at":   {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		},
		Required: []string{"id", "key_prefix", "role_id", "name", "revoked", "created_at"},
	}}

	apiKeyWithSecretSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type:  &openapi3.Types{"object"},
		AllOf: openapi3.SchemaRefs{apiKeySchema},
		Properties: openapi3.Schemas{
			"key": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Full API key - only returned on creation"}},
		},
	}}

	roleSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":          {Value: openapi3.NewStringSchema()},
			"name":        {Value: openapi3.NewStringSchema()},
			"description": {Value: openapi3.NewStringSchema()},
			"is_system":   {Value: openapi3.NewBoolSchema()},
			"created_at":  {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		},
		Required: []string{"id", "name", "is_system", "created_at"},
	}}

	claimMappingSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":           {Value: openapi3.NewStringSchema()},
			"workspace_id": {Value: openapi3.NewStringSchema()},
			"claim_name":   {Value: openapi3.NewStringSchema()},
			"claim_value":  {Value: openapi3.NewStringSchema()},
			"role_id":      {Value: openapi3.NewStringSchema()},
			"priority":     {Value: openapi3.NewIntegerSchema()},
			"created_at":   {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		},
		Required: []string{"id", "workspace_id", "claim_name", "claim_value", "role_id", "priority", "created_at"},
	}}

	styleSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":                {Value: openapi3.NewStringSchema()},
			"workspace_id":      {Value: openapi3.NewStringSchema()},
			"name":              {Value: openapi3.NewStringSchema()},
			"title":             {Value: openapi3.NewStringSchema()},
			"description":       {Value: openapi3.NewStringSchema()},
			"format":            {Value: openapi3.NewStringSchema()},
			"created_at":        {Value: openapi3.NewStringSchema().WithFormat("date-time")},
			"updated_at":        {Value: openapi3.NewStringSchema().WithFormat("date-time")},
			"valid":             {Value: openapi3.NewBoolSchema()},
			"validation_errors": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
			"diagnostics": {Value: openapi3.NewArraySchema().WithItems(&openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
				"severity": {Value: openapi3.NewStringSchema()}, "code": {Value: openapi3.NewStringSchema()}, "path": {Value: openapi3.NewStringSchema()}, "line": {Value: openapi3.NewIntegerSchema()}, "column": {Value: openapi3.NewIntegerSchema()}, "message": {Value: openapi3.NewStringSchema()},
			}})},
		},
		Required: []string{"id", "workspace_id", "name", "format", "created_at", "updated_at"},
	}}

	styleWithBodySchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Required: []string{"body"},
		Type:     &openapi3.Types{"object"},
		AllOf:    openapi3.SchemaRefs{styleSchema},
		Properties: openapi3.Schemas{
			"body":     {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Authored style body"}},
			"sld_body": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Deprecated SLD/SE compatibility alias for body"}},
		},
	}}

	styleAssetSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "workspace_id": {Value: openapi3.NewStringSchema()}, "name": {Value: openapi3.NewStringSchema()}, "content_type": {Value: openapi3.NewStringSchema()}, "size_bytes": {Value: openapi3.NewInt64Schema()}, "sha256": {Value: openapi3.NewStringSchema()}, "created_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "updated_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
	}, Required: []string{"id", "workspace_id", "name", "content_type", "size_bytes", "sha256"}}}

	wmsSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"enabled":                  {Value: openapi3.NewBoolSchema()},
			"public":                   {Value: openapi3.NewBoolSchema()},
			"max_width":                {Value: openapi3.NewIntegerSchema()},
			"max_height":               {Value: openapi3.NewIntegerSchema()},
			"max_pixels":               {Value: openapi3.NewIntegerSchema()},
			"max_render_features":      {Value: openapi3.NewIntegerSchema()},
			"max_render_vertices":      {Value: openapi3.NewIntegerSchema()},
			"simplify_enabled":         {Value: openapi3.NewBoolSchema()},
			"simplify_pixel_tolerance": {Value: openapi3.NewFloat64Schema()},
			"title":                    {Value: openapi3.NewStringSchema()},
			"abstract":                 {Value: openapi3.NewStringSchema()},
			"default_style":            {Value: openapi3.NewStringSchema()},
			"extensions":               {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema().WithEnum("dynamic-raster", "advanced-labels", "rendering-transformations", "compositing", "z-order", "dynamic-style", "remote-graphics"))},
		},
		Required: []string{"enabled"},
	}}

	wfsSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"enabled":          {Value: openapi3.NewBoolSchema()},
			"public":           {Value: openapi3.NewBoolSchema()},
			"max_features":     {Value: openapi3.NewIntegerSchema()},
			"default_count":    {Value: openapi3.NewIntegerSchema()},
			"max_offset":       {Value: openapi3.NewIntegerSchema()},
			"count_timeout_ms": {Value: openapi3.NewIntegerSchema()},
			"title":            {Value: openapi3.NewStringSchema()},
			"abstract":         {Value: openapi3.NewStringSchema()},
		},
		Required: []string{"enabled"},
	}}

	ogcapiSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"enabled":       {Value: openapi3.NewBoolSchema()},
			"public":        {Value: openapi3.NewBoolSchema()},
			"title":         {Value: openapi3.NewStringSchema()},
			"abstract":      {Value: openapi3.NewStringSchema()},
			"limit_default": {Value: openapi3.NewIntegerSchema()},
			"limit_max":     {Value: openapi3.NewIntegerSchema()},
			"max_offset":    {Value: openapi3.NewIntegerSchema()},
		},
		Required: []string{"enabled"},
	}}

	coverageRangeFieldSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"band": {Value: openapi3.NewIntegerSchema()}, "name": {Value: openapi3.NewStringSchema()},
		"description": {Value: openapi3.NewStringSchema()}, "definition": {Value: openapi3.NewStringSchema()},
		"uom": {Value: openapi3.NewStringSchema()}, "nil_values": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
	}, Required: []string{"band", "name"}}}
	coverageSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "service_id": {Value: openapi3.NewStringSchema()},
		"source_coverage": {Value: openapi3.NewStringSchema()}, "public_id": {Value: openapi3.NewStringSchema()},
		"title": {Value: openapi3.NewStringSchema()}, "description": {Value: openapi3.NewStringSchema()},
		"enabled": {Value: openapi3.NewBoolSchema()}, "public": {Value: openapi3.NewBoolSchema()},
		"allowed_roles":          {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"range_fields":           {Value: openapi3.NewArraySchema().WithItems(coverageRangeFieldSchema.Value)},
		"default_style":          {Value: openapi3.NewStringSchema()},
		"styles":                 {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"resampling":             {Value: openapi3.NewStringSchema().WithEnum("nearest", "bilinear", "cubic")},
		"wcs20_coverage_subtype": {Value: openapi3.NewStringSchema().WithEnum(store.WCS20CoverageSubtypeRectifiedGrid, store.WCS20CoverageSubtypeGrid)},
		"native_extent":          spatialExtentSchema,
		"tile_cache_quota_bytes": {Value: openapi3.NewInt64Schema()},
		"tile_cache_generation":  {Value: openapi3.NewInt64Schema()},
	}, Required: []string{"id", "service_id", "source_coverage", "public_id", "enabled", "public"}}}
	coverageCreateSchema := schemaObject(openapi3.Schemas{}, "source_coverage", "public_id")
	coverageUpdateSchema := schemaObject(openapi3.Schemas{})
	for name, property := range coverageSchema.Value.Properties {
		switch name {
		case "id", "service_id", "native_extent", "tile_cache_generation":
			continue
		}
		coverageCreateSchema.Value.Properties[name] = property
		if name != "source_coverage" {
			coverageUpdateSchema.Value.Properties[name] = property
		}
	}
	wcsSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"enabled": {Value: openapi3.NewBoolSchema()}, "public": {Value: openapi3.NewBoolSchema()},
		"title": {Value: openapi3.NewStringSchema()}, "abstract": {Value: openapi3.NewStringSchema()},
		"max_cells": {Value: openapi3.NewInt64Schema()}, "max_output_bytes": {Value: openapi3.NewInt64Schema()},
		"processing_timeout_ms":  {Value: openapi3.NewIntegerSchema()},
		"extensions":             {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"allowed_subsetting_crs": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"allowed_output_crs":     {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"interpolation_methods":  {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"output_formats":         {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		"max_dimensions":         {Value: openapi3.NewIntegerSchema()},
		"max_axis_values":        {Value: openapi3.NewInt64Schema()},
		"max_source_granules":    {Value: openapi3.NewIntegerSchema()},
		"max_temporary_bytes":    {Value: openapi3.NewInt64Schema()},
	}, Required: []string{"enabled", "public"}}}
	wmtsSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"enabled": {Value: openapi3.NewBoolSchema()}, "public": {Value: openapi3.NewBoolSchema()},
		"title": {Value: openapi3.NewStringSchema()}, "abstract": {Value: openapi3.NewStringSchema()},
		"feature_info_enabled": {Value: openapi3.NewBoolSchema()},
		"vector_tiles_enabled": {Value: openapi3.NewBoolSchema()},
		"provider_name":        {Value: openapi3.NewStringSchema()}, "provider_site": {Value: openapi3.NewStringSchema().WithFormat("uri")},
		"contact_name": {Value: openapi3.NewStringSchema()}, "contact_position": {Value: openapi3.NewStringSchema()},
		"contact_email": {Value: openapi3.NewStringSchema().WithFormat("email")},
	}, Required: []string{"enabled", "public", "feature_info_enabled"}}}

	ogcTilesVectorSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"enabled": {Value: openapi3.NewBoolSchema()},
			"formats": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		},
		Required: []string{"enabled"},
	}}

	ogcTilesMapSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"enabled": {Value: openapi3.NewBoolSchema()},
			"formats": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
		},
		Required: []string{"enabled"},
	}}

	ogcTilesInnerSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"tile_matrix_sets":             {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
			"vector_tiles":                 ogcTilesVectorSettingsSchema,
			"map_tiles":                    ogcTilesMapSettingsSchema,
			"cache_enabled":                {Value: openapi3.NewBoolSchema()},
			"max_features":                 {Value: openapi3.NewIntegerSchema()},
			"max_vertices":                 {Value: openapi3.NewIntegerSchema()},
			"max_tile_bytes":               {Value: openapi3.NewIntegerSchema()},
			"persistent_cache_quota_bytes": {Value: openapi3.NewInt64Schema()},
		},
		Required: []string{"vector_tiles", "map_tiles", "cache_enabled"},
	}}

	ogcTilesSettingsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"enabled":  {Value: openapi3.NewBoolSchema()},
			"public":   {Value: openapi3.NewBoolSchema()},
			"title":    {Value: openapi3.NewStringSchema()},
			"abstract": {Value: openapi3.NewStringSchema()},
			"versions": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())},
			"settings": ogcTilesInnerSettingsSchema,
		},
		Required: []string{"enabled", "settings"},
	}}

	cacheMetricsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"hits":                 {Value: openapi3.NewInt64Schema()},
			"misses":               {Value: openapi3.NewInt64Schema()},
			"hit_rate":             {Value: openapi3.NewFloat64Schema()},
			"entry_count":          {Value: openapi3.NewInt64Schema()},
			"size_bytes":           {Value: openapi3.NewInt64Schema()},
			"max_size_bytes":       {Value: openapi3.NewInt64Schema()},
			"remaining_bytes":      {Value: openapi3.NewInt64Schema()},
			"capacity_utilization": {Value: openapi3.NewFloat64Schema()},
			"keys_evicted":         {Value: openapi3.NewInt64Schema()},
			"bytes_evicted":        {Value: openapi3.NewInt64Schema()},
			"sets_dropped":         {Value: openapi3.NewInt64Schema()},
			"sets_rejected":        {Value: openapi3.NewInt64Schema()},
			"gets_dropped":         {Value: openapi3.NewInt64Schema()},
			"gets_kept":            {Value: openapi3.NewInt64Schema()},
			"oversized_skipped":    {Value: openapi3.NewInt64Schema()},
			"ttl_expired":          {Value: openapi3.NewInt64Schema()},
			"loads_shared":         {Value: openapi3.NewInt64Schema()},
			"load_errors":          {Value: openapi3.NewInt64Schema()},
			"invalidation_keys":    {Value: openapi3.NewInt64Schema()},
		},
	}}

	cacheStatsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"enabled":                 {Value: openapi3.NewBoolSchema()},
			"profile":                 {Value: openapi3.NewStringSchema()},
			"capabilities":            cacheMetricsSchema,
			"collections":             cacheMetricsSchema,
			"features":                cacheMetricsSchema,
			"tiles":                   cacheMetricsSchema,
			"counts":                  cacheMetricsSchema,
			"total_size_bytes":        {Value: openapi3.NewInt64Schema()},
			"total_max_size_bytes":    {Value: openapi3.NewInt64Schema()},
			"total_invalidation_keys": {Value: openapi3.NewInt64Schema()},
		},
	}}

	tileCacheBoundsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"bbox": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewFloat64Schema()).WithMinItems(4).WithMaxItems(4)},
		"crs":  {Value: openapi3.NewStringSchema()},
	}, Required: []string{"bbox", "crs"}}}
	tileCacheJobRequestSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"operation": {Value: openapi3.NewStringSchema().WithEnum("seed", "reseed", "truncate")},
		"resource":  {Value: openapi3.NewStringSchema()}, "all_resources": {Value: openapi3.NewBoolSchema()},
		"tile_type":       {Value: openapi3.NewStringSchema().WithEnum("map", "vector")},
		"tile_matrix_set": {Value: openapi3.NewStringSchema()}, "format": {Value: openapi3.NewStringSchema()},
		"style": {Value: openapi3.NewStringSchema()}, "min_zoom": {Value: openapi3.NewIntegerSchema()},
		"max_zoom": {Value: openapi3.NewIntegerSchema()}, "bounds": tileCacheBoundsSchema,
	}, Required: []string{"operation"}}}
	tileCacheJobSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "workspace_id": {Value: openapi3.NewStringSchema()},
		"request": tileCacheJobRequestSchema, "status": {Value: openapi3.NewStringSchema().WithEnum("queued", "running", "cancelling", "cancelled", "succeeded", "failed")},
		"total_tiles": {Value: openapi3.NewInt64Schema()}, "processed_tiles": {Value: openapi3.NewInt64Schema()},
		"succeeded_tiles": {Value: openapi3.NewInt64Schema()}, "skipped_tiles": {Value: openapi3.NewInt64Schema()},
		"failed_tiles": {Value: openapi3.NewInt64Schema()}, "bytes_written": {Value: openapi3.NewInt64Schema()},
		"bytes_deleted": {Value: openapi3.NewInt64Schema()}, "cancel_requested": {Value: openapi3.NewBoolSchema()},
		"error_message": {Value: openapi3.NewStringSchema()}, "created_by": {Value: openapi3.NewStringSchema()},
		"created_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "started_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		"completed_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "updated_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
	}, Required: []string{"id", "workspace_id", "request", "status", "total_tiles", "processed_tiles", "created_at", "updated_at"}}}
	tileCacheUsageSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"size_bytes": {Value: openapi3.NewInt64Schema()}, "entry_count": {Value: openapi3.NewInt64Schema()},
		"quota_bytes": {Value: openapi3.NewInt64Schema()}, "remaining_bytes": {Value: openapi3.NewInt64Schema()},
		"utilization": {Value: openapi3.NewFloat64Schema()},
	}}}
	tileCacheStatsSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"enabled": {Value: openapi3.NewBoolSchema()}, "backend": {Value: openapi3.NewStringSchema()},
		"global": tileCacheUsageSchema, "workspace": tileCacheUsageSchema, "resource": tileCacheUsageSchema,
		"hits": {Value: openapi3.NewInt64Schema()}, "misses": {Value: openapi3.NewInt64Schema()},
		"writes": {Value: openapi3.NewInt64Schema()}, "write_errors": {Value: openapi3.NewInt64Schema()},
		"evictions": {Value: openapi3.NewInt64Schema()}, "bytes_evicted": {Value: openapi3.NewInt64Schema()},
		"oversized_skipped": {Value: openapi3.NewInt64Schema()}, "orphans_repaired": {Value: openapi3.NewInt64Schema()},
	}}}
	mosaicGranuleSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "workspace_id": {Value: openapi3.NewStringSchema()}, "service_id": {Value: openapi3.NewStringSchema()},
		"generation": {Value: openapi3.NewInt64Schema()}, "source_uri": {Value: openapi3.NewStringSchema()}, "crs": {Value: openapi3.NewStringSchema()},
		"srid": {Value: openapi3.NewIntegerSchema()}, "bbox": {Value: openapi3.NewArraySchema().WithItems(openapi3.NewFloat64Schema()).WithMinItems(4).WithMaxItems(4)},
		"width": {Value: openapi3.NewIntegerSchema()}, "height": {Value: openapi3.NewIntegerSchema()}, "band_count": {Value: openapi3.NewIntegerSchema()},
		"data_type": {Value: openapi3.NewStringSchema()}, "resolution_x": {Value: openapi3.NewFloat64Schema()}, "resolution_y": {Value: openapi3.NewFloat64Schema()},
		"time": {Value: openapi3.NewStringSchema()}, "elevation": {Value: openapi3.NewFloat64Schema()}, "priority": {Value: openapi3.NewIntegerSchema()},
		"size_bytes": {Value: openapi3.NewInt64Schema()}, "modified_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		"footprint": {Ref: "#/components/schemas/GeoJSONFeature"}, "created_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "updated_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
	}, Required: []string{"id", "workspace_id", "service_id", "generation", "source_uri", "crs", "bbox", "width", "height", "band_count", "data_type"}}}
	mosaicHarvestGranuleSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"path": {Value: openapi3.NewStringSchema()}, "time": {Value: openapi3.NewStringSchema()}, "elevation": {Value: openapi3.NewFloat64Schema()}, "priority": {Value: openapi3.NewIntegerSchema()},
	}, Required: []string{"path"}}}
	mosaicHarvestRequestSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"mode": {Value: openapi3.NewStringSchema().WithEnum("append", "synchronize")}, "directory": {Value: openapi3.NewStringSchema()}, "pattern": {Value: openapi3.NewStringSchema()},
		"granules": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/MosaicHarvestGranule"}}},
	}}}
	mosaicHarvestJobSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "workspace_id": {Value: openapi3.NewStringSchema()}, "service_id": {Value: openapi3.NewStringSchema()},
		"request": mosaicHarvestRequestSchema, "status": {Value: openapi3.NewStringSchema().WithEnum("queued", "running", "cancelling", "cancelled", "succeeded", "failed")},
		"total_granules": {Value: openapi3.NewInt64Schema()}, "processed_granules": {Value: openapi3.NewInt64Schema()}, "succeeded_granules": {Value: openapi3.NewInt64Schema()}, "failed_granules": {Value: openapi3.NewInt64Schema()},
		"generation": {Value: openapi3.NewInt64Schema()}, "cancel_requested": {Value: openapi3.NewBoolSchema()}, "error_message": {Value: openapi3.NewStringSchema()}, "created_by": {Value: openapi3.NewStringSchema()},
		"created_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "started_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "completed_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "updated_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
	}, Required: []string{"id", "workspace_id", "service_id", "request", "status", "total_granules", "processed_granules", "created_at", "updated_at"}}}
	deletionRefSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "name": {Value: openapi3.NewStringSchema()},
		"kind": {Value: openapi3.NewStringSchema()}, "reason": {Value: openapi3.NewStringSchema()},
	}, Required: []string{"name", "kind"}}}
	deletionAuxiliarySchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"tile_entries": {Value: openapi3.NewInt64Schema()}, "tile_bytes": {Value: openapi3.NewInt64Schema()},
		"tile_jobs": {Value: openapi3.NewInt64Schema()}, "mosaic_services": {Value: openapi3.NewInt64Schema()},
		"mosaic_granules": {Value: openapi3.NewInt64Schema()}, "mosaic_jobs": {Value: openapi3.NewInt64Schema()},
		"managed_assets": {Value: openapi3.NewInt64Schema()},
	}}}
	deletionRefs := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/DeletionRef"}}}
	deletionPlanSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"scope": {Value: openapi3.NewStringSchema().WithEnum("workspace", "service")}, "workspace_id": {Value: openapi3.NewStringSchema()},
		"target": deletionRefSchema, "services": deletionRefs, "layers": deletionRefs, "coverages": deletionRefs,
		"layer_groups": deletionRefs, "styles": deletionRefs, "style_assets": deletionRefs, "api_keys": deletionRefs,
		"stored_queries": deletionRefs, "claim_mappings": deletionRefs, "policies": deletionRefs, "auxiliary": deletionAuxiliarySchema,
	}, Required: []string{"scope", "workspace_id", "target"}}}
	deletionOperationSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"id": {Value: openapi3.NewStringSchema()}, "scope": {Value: openapi3.NewStringSchema()}, "workspace_id": {Value: openapi3.NewStringSchema()},
		"target_id": {Value: openapi3.NewStringSchema()}, "target_name": {Value: openapi3.NewStringSchema()},
		"status": {Value: openapi3.NewStringSchema().WithEnum("pending", "running", "failed", "completed")},
		"phase":  {Value: openapi3.NewStringSchema().WithEnum("planned", "tombstoned", "jobs_quiesced", "auxiliary_state_removed", "catalog_committed", "runtime_cleared", "completed")},
		"plan":   deletionPlanSchema, "last_error": {Value: openapi3.NewStringSchema()}, "attempt_count": {Value: openapi3.NewIntegerSchema()},
		"created_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")}, "updated_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
		"completed_at": {Value: openapi3.NewStringSchema().WithFormat("date-time")},
	}, Required: []string{"id", "scope", "workspace_id", "target_id", "target_name", "status", "phase", "plan", "created_at", "updated_at"}}}
	deletionConflictSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"code": {Value: openapi3.NewIntegerSchema()}, "message": {Value: openapi3.NewStringSchema()},
		"recurse_required": {Value: openapi3.NewBoolSchema()}, "plan": deletionPlanSchema,
	}, Required: []string{"code", "message", "recurse_required", "plan"}}}
	integrityIssueSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"store": {Value: openapi3.NewStringSchema()}, "kind": {Value: openapi3.NewStringSchema()}, "id": {Value: openapi3.NewStringSchema()},
		"name": {Value: openapi3.NewStringSchema()}, "workspace_id": {Value: openapi3.NewStringSchema()}, "reason": {Value: openapi3.NewStringSchema()},
	}, Required: []string{"store", "kind", "id", "reason"}}}
	integrityReportSchema := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"healthy": {Value: openapi3.NewBoolSchema()}, "repaired": {Value: openapi3.NewInt64Schema()},
		"issues": {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/IntegrityIssue"}}},
	}, Required: []string{"healthy", "issues"}}}

	schemas := openapi3.Schemas{
		"Error":                  errorSchema,
		"Workspace":              workspaceSchema,
		"ServiceCacheSettings":   serviceCacheSettingsSchema,
		"Service":                serviceSchema,
		"ServiceConnectionInput": serviceSchema,
		"Dimension":              dimensionSchema,
		"SQLViewProperty":        sqlViewPropertySchema,
		"SQLView":                sqlViewSchema,
		"SpatialExtent":          spatialExtentSchema,
		"Layer":                  layerSchema,
		"LayerGroupMember":       layerGroupMemberSchema,
		"LayerGroup":             layerGroupSchema,
		"DiscoveredLayer":        discoveredLayerSchema,
		"SQLViewColumn":          sqlViewColumnSchema,
		"SQLViewDiscovery":       sqlViewDiscoverySchema,
		"ValidateSQLResponse":    validateSQLResponseSchema,
		"APIKey":                 apiKeySchema,
		"APIKeyWithSecret":       apiKeyWithSecretSchema,
		"Role":                   roleSchema,
		"ClaimMapping":           claimMappingSchema,
		"Style":                  styleSchema,
		"StyleWithBody":          styleWithBodySchema,
		"StyleAsset":             styleAssetSchema,
		"WMSSettings":            wmsSettingsSchema,
		"WFSSettings":            wfsSettingsSchema,
		"OGCAPISettings":         ogcapiSettingsSchema,
		"CoverageRangeField":     coverageRangeFieldSchema,
		"Coverage":               coverageSchema,
		"CoverageCreate":         coverageCreateSchema,
		"CoverageUpdate":         coverageUpdateSchema,
		"WCSSettings":            wcsSettingsSchema,
		"WMTSSettings":           wmtsSettingsSchema,
		"OGCTilesVectorSettings": ogcTilesVectorSettingsSchema,
		"OGCTilesMapSettings":    ogcTilesMapSettingsSchema,
		"OGCTilesInnerSettings":  ogcTilesInnerSettingsSchema,
		"OGCTilesSettings":       ogcTilesSettingsSchema,
		"CacheMetrics":           cacheMetricsSchema,
		"CacheStats":             cacheStatsSchema,
		"TileCacheBounds":        tileCacheBoundsSchema,
		"TileCacheJobRequest":    tileCacheJobRequestSchema,
		"TileCacheJob":           tileCacheJobSchema,
		"TileCacheUsage":         tileCacheUsageSchema,
		"TileCacheStats":         tileCacheStatsSchema,
		"MosaicGranule":          mosaicGranuleSchema,
		"MosaicHarvestGranule":   mosaicHarvestGranuleSchema,
		"MosaicHarvestRequest":   mosaicHarvestRequestSchema,
		"MosaicHarvestJob":       mosaicHarvestJobSchema,
		"DeletionRef":            deletionRefSchema,
		"DeletionAuxiliary":      deletionAuxiliarySchema,
		"DeletionPlan":           deletionPlanSchema,
		"DeletionOperation":      deletionOperationSchema,
		"DeletionConflict":       deletionConflictSchema,
		"IntegrityIssue":         integrityIssueSchema,
		"IntegrityReport":        integrityReportSchema,
	}
	for name, schema := range getConsoleSchemas() {
		schemas[name] = schema
	}
	return schemas
}
