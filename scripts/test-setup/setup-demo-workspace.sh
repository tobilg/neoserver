#!/usr/bin/env bash
#
# Setup script for neoserver demo workspace
# Creates a workspace with PostGIS data source, enables WMS/WFS/WCS/OGC API,
# and publishes the vector and raster conformance fixtures.
#
# Prerequisites:
#   - neoserver running (docker compose up server)
#   - PostGIS running (docker compose --profile postgis up db)
#
# Usage:
#   ./scripts/setup-demo-workspace.sh [OPTIONS] [TOKEN]
#
# Options:
#   --public         Make OGC services publicly accessible (no authentication required)
#   --conformance    Enable the deterministic settings used by both test lanes
#
# If TOKEN is not provided, the script will attempt to read it from
# the NEOSERVER_TOKEN environment variable.

set -euo pipefail

# Configuration
BASE_URL="${NEOSERVER_URL:-http://localhost:9000}"
WORKSPACE_NAME="demo"
SERVICE_NAME="postgis-local"
PUBLIC_ACCESS=false
CONFORMANCE_MODE=false

# PostGIS connection (as seen from inside docker network)
PG_HOST="db"
PG_PORT=5432
PG_USER="postgres"
PG_PASSWORD="postgres"
PG_DATABASE="postgis"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Parse arguments
TOKEN=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --public)
            PUBLIC_ACCESS=true
            shift
            ;;
        --conformance)
            CONFORMANCE_MODE=true
            shift
            ;;
        -*)
            log_error "Unknown option: $1"
            exit 1
            ;;
        *)
            TOKEN="$1"
            shift
            ;;
    esac
done

# Fall back to environment variable if no token argument provided
TOKEN="${TOKEN:-${NEOSERVER_TOKEN:-}}"

if [[ -z "$TOKEN" ]]; then
    log_error "No token provided!"
    echo ""
    echo "Usage: $0 [OPTIONS] [TOKEN]"
    echo ""
    echo "Options:"
    echo "  --public         Make OGC services publicly accessible (no authentication required)"
    echo "  --conformance    Enable deterministic WCS, Tiles, and WMTS settings"
    echo ""
    echo "You can also set the NEOSERVER_TOKEN environment variable."
    echo ""
    echo "To get a token, run:"
    echo "  neoserver create-token --store-path ./data/neoserver.db --role super_admin --expires 24h"
    echo ""
    echo "Or check the server startup logs for the bootstrap token."
    exit 1
fi

log_info "Using neoserver at: $BASE_URL"

# Wait for server to be ready
wait_for_server() {
    log_info "Waiting for server to be ready..."
    local max_attempts=30
    local attempt=1

    while [[ $attempt -le $max_attempts ]]; do
        if curl -s "${BASE_URL}/ready" > /dev/null 2>&1; then
            log_success "Server is ready"
            return 0
        fi
        echo -n "."
        sleep 1
        ((attempt++))
    done

    echo ""
    log_error "Server did not become ready after ${max_attempts} seconds"
    exit 1
}

# API helper function
api_call() {
    local method="$1"
    local endpoint="$2"
    local data="${3:-}"

    local url="${BASE_URL}${endpoint}"
    local args=(-s -X "$method" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")

    if [[ -n "$data" ]]; then
        args+=(-d "$data")
    fi

    curl "${args[@]}" "$url"
}

# Check if resource exists (returns 0 if exists)
resource_exists() {
    local endpoint="$1"
    local response
    response=$(api_call GET "$endpoint" 2>/dev/null)

    if echo "$response" | grep -q '"code":'; then
        return 1
    fi
    return 0
}

# Step 1: Create workspace
create_workspace() {
    log_info "Creating workspace: $WORKSPACE_NAME"

    if resource_exists "/api/v1/workspaces/$WORKSPACE_NAME"; then
        log_warn "Workspace '$WORKSPACE_NAME' already exists, skipping creation"
        return 0
    fi

    local response
    response=$(api_call POST "/api/v1/workspaces" "{
        \"name\": \"$WORKSPACE_NAME\",
        \"description\": \"Demo workspace with PostGIS data\"
    }")

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to create workspace: $response"
        exit 1
    fi

    log_success "Workspace created"
}

# Step 2: Create PostGIS service
create_service() {
    log_info "Creating PostGIS service: $SERVICE_NAME"

    if resource_exists "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME"; then
        log_warn "Service '$SERVICE_NAME' already exists, skipping creation"
        return 0
    fi

    local response
    response=$(api_call POST "/api/v1/workspaces/$WORKSPACE_NAME/services" "{
        \"name\": \"$SERVICE_NAME\",
        \"type\": \"postgis\",
        \"connection_info\": {
            \"host\": \"$PG_HOST\",
            \"port\": $PG_PORT,
            \"database\": \"$PG_DATABASE\",
            \"user\": \"$PG_USER\",
            \"password\": \"$PG_PASSWORD\",
            \"sslmode\": \"disable\",
            \"schemas\": [\"public\", \"cite\"]
        }
    }")

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to create service: $response"
        exit 1
    fi

    log_success "PostGIS service created"
}

# Step 3: Discover layers
discover_layers() {
    local response=""
    local attempt
    for attempt in $(seq 1 30); do
        if response=$(api_call POST "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/discover") &&
            ! grep -q '"code":' <<<"$response" &&
            jq -e '.layers | type == "array" and length > 0' >/dev/null 2>&1 <<<"$response"; then
            if [[ "$CONFORMANCE_MODE" != "true" ]] ||
                jq -e '([.layers[]?.name] | index("cite.BasicPolygons") != null) and ([.layers[]?.name] | index("cite.RoadSegments") != null)' >/dev/null 2>&1 <<<"$response"; then
                jq -r '.layers[]?.name // empty' <<<"$response"
                return 0
            fi
        fi
        log_warn "Layer discovery is not ready yet (attempt $attempt/30)" >&2
        sleep 1
    done

    log_error "Failed to discover layers after 30 attempts: $response" >&2
    exit 1
}

# Step 4: Publish a layer
publish_layer() {
    local source_layer="$1"
    local public_id="$2"
    local title="$3"

    log_info "Publishing layer: $public_id (from $source_layer)"

    local existing
    existing=$(api_call GET "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers")
    if echo "$existing" | jq -e --arg public_id "$public_id" '.layers[]? | select(.public_id == $public_id)' >/dev/null; then
        log_warn "Layer '$public_id' already exists, skipping"
        return 0
    fi

    local public_str="false"
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        public_str="true"
    fi

    local response
    response=$(api_call POST "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers" "{
        \"source_layer\": \"$source_layer\",
        \"public_id\": \"$public_id\",
        \"title\": \"$title\",
        \"public\": $public_str
    }")

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to publish layer: $response"
        return 1
    fi

    log_success "Layer '$public_id' published"
}

# Bind one deterministic named style so native WMS tests exercise explicit
# STYLES selection instead of treating the missing fixture as a product skip.
configure_assurance_style() {
    local style_name="assurance-blue"
    local style_path="/api/v1/workspaces/$WORKSPACE_NAME/styles/$style_name"
    local response

    if ! resource_exists "$style_path"; then
        local style_body
        style_body='<StyledLayerDescriptor version="1.0.0" xmlns="http://www.opengis.net/sld" xmlns:ogc="http://www.opengis.net/ogc"><NamedLayer><Name>assurance</Name><UserStyle><Title>Assurance blue polygons</Title><FeatureTypeStyle><Rule><PolygonSymbolizer><Fill><CssParameter name="fill">#3388ff</CssParameter></Fill><Stroke><CssParameter name="stroke">#1f4f99</CssParameter><CssParameter name="stroke-width">2</CssParameter></Stroke></PolygonSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>'
        local style_payload
        style_payload=$(jq -n --arg name "$style_name" --arg title "Assurance blue polygons" --arg body "$style_body" \
            '{name: $name, title: $title, format: "sld_1.0.0", body: $body}')
        response=$(api_call POST "/api/v1/workspaces/$WORKSPACE_NAME/styles" "$style_payload")
        if echo "$response" | grep -q '"code":'; then
            log_error "Failed to create assurance style: $response"
            return 1
        fi
    fi

    response=$(api_call GET "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers")
    local layer_id
    layer_id=$(echo "$response" | jq -r '.layers[] | select(.public_id=="cite:BasicPolygons") | .id' | head -n 1)
    if [[ -z "$layer_id" || "$layer_id" == "null" ]]; then
        log_error "cite:BasicPolygons layer is required for the named-style assurance fixture"
        return 1
    fi
    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers/$layer_id" \
        "{\"styles\":[\"$style_name\"]}")
    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to bind assurance style: $response"
        return 1
    fi
    log_success "Named WMS assurance style configured"
}

# Step 4b: Configure time dimension for cite:Autos (CITE WMS compliance)
configure_autos_time_dimension() {
    log_info "Configuring time dimension for cite:Autos layer..."

    # First, get the layer ID for Autos
    local response
    response=$(api_call GET "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers")

    local layer_id
    layer_id=$(echo "$response" | jq -r '.layers[] | select(.public_id=="cite:Autos") | .id')

    if [[ -z "$layer_id" || "$layer_id" == "null" ]]; then
        log_warn "cite:Autos layer not found, skipping time dimension configuration"
        return 0
    fi

    # Update the layer with time dimension
    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers/$layer_id" '{
        "title": "cite:Autos",
        "dimensions": [
            {
                "name": "time",
                "units": "ISO8601",
                "source_property": "time",
                "default": "2000-01-01T00:00:00Z",
                "multiple_values": true,
                "nearest_value": true,
                "extent": "2000-01-01T00:00:00Z/2000-01-01T00:01:00Z/PT5S"
            }
        ]
    }')

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to configure time dimension: $response"
        return 1
    fi

    log_success "Time dimension configured for cite:Autos"
}

# Step 4c: Configure elevation dimension for cite:LakesWithElevation.
configure_lakes_elevation_dimension() {
    log_info "Configuring elevation dimension for cite:LakesWithElevation layer..."

    local response
    response=$(api_call GET "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers")

    local layer_id
    layer_id=$(echo "$response" | jq -r '.layers[] | select(.public_id=="cite:LakesWithElevation") | .id')

    if [[ -z "$layer_id" || "$layer_id" == "null" ]]; then
        log_warn "cite:LakesWithElevation layer not found, skipping elevation dimension configuration"
        return 0
    fi

    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/layers/$layer_id" '{
        "title": "cite:LakesWithElevation",
        "dimensions": [
            {
                "name": "elevation",
                "units": "EPSG:5030",
                "source_property": "elev",
                "default": "500",
                "multiple_values": true,
                "nearest_value": true,
                "extent": "470,480,500"
            }
        ]
    }')

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to configure elevation dimension: $response"
        return 1
    fi

    log_success "Elevation dimension configured for cite:LakesWithElevation"
}

# Step 4d: Publish one discovered deterministic PostGIS raster fixture.
publish_coverage_fixture() {
    local discovery="$1"
    local source_name="$2"
    local public_id="$3"
    local title="$4"
    local wcs20_coverage_subtype="${5:-}"
    local source_coverage
    source_coverage=$(echo "$discovery" | jq -r --arg source_name "$source_name" '.coverages[]? | select(.source_coverage==$source_name) | .source_coverage')
    if [[ -z "$source_coverage" || "$source_coverage" == "null" ]]; then
        log_warn "PostGIS WCS fixture '$source_name' not found, skipping coverage publication"
        return 0
    fi

    local response
    response=$(api_call GET "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/coverages")
    if echo "$response" | jq -e --arg public_id "$public_id" '.coverages[]? | select((.PublicID // .public_id)==$public_id)' >/dev/null; then
        log_warn "Coverage '$public_id' already exists, skipping"
        return 0
    fi

    local public_str="false"
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        public_str="true"
    fi

    local subtype_json=""
    if [[ -n "$wcs20_coverage_subtype" ]]; then
        subtype_json=", \"wcs20_coverage_subtype\": \"$wcs20_coverage_subtype\""
    fi

    response=$(api_call POST "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/coverages" "{
        \"source_coverage\": \"$source_coverage\",
        \"public_id\": \"$public_id\",
        \"title\": \"$title\",
        \"enabled\": true,
        \"public\": $public_str$subtype_json
    }")
    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to publish WCS fixture '$public_id': $response"
        return 1
    fi

    log_success "Coverage '$public_id' published"
}

publish_wcs_fixtures() {
    log_info "Discovering raster coverages..."
    local response
    response=$(api_call POST "/api/v1/workspaces/$WORKSPACE_NAME/services/$SERVICE_NAME/discover-coverages")
    if echo "$response" | grep -q '"code":'; then
        log_warn "Raster discovery unavailable, skipping WCS fixtures: $response"
        return 0
    fi
    publish_coverage_fixture "$response" "public.wcs_fixture:rast" "wcs_fixture" "WCS conformance fixture"
    if [[ "$CONFORMANCE_MODE" == "true" ]]; then
        publish_coverage_fixture "$response" "public.wcs_fixture_aux:rast" "wcs_fixture_aux" "WCS auxiliary conformance fixture" "GridCoverage"
    fi
}

# Step 5: Enable OGC API Features
enable_ogcapi() {
    local public_str="false"
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        public_str="true"
    fi

    log_info "Enabling OGC API Features (public: $public_str)..."

    local response
    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/settings/ogcapi" "{
        \"enabled\": true,
        \"public\": $public_str,
        \"title\": \"Demo OGC API Features\",
        \"abstract\": \"OGC API Features for the demo workspace\"
    }")

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to enable OGC API: $response"
        exit 1
    fi

    log_success "OGC API Features enabled"
}

# Step 6: Enable WMS
enable_wms() {
    local public_str="false"
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        public_str="true"
    fi

    log_info "Enabling WMS service (public: $public_str)..."

    local response
    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/settings/wms" "{
        \"enabled\": true,
        \"public\": $public_str,
        \"title\": \"Demo WMS Service\",
        \"abstract\": \"WMS service for the demo workspace\",
        \"max_width\": 4096,
        \"max_height\": 4096
    }")

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to enable WMS: $response"
        exit 1
    fi

    log_success "WMS enabled"
}

# Step 7: Enable WFS
enable_wfs() {
    local public_str="false"
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        public_str="true"
    fi

    log_info "Enabling WFS service (public: $public_str)..."

    local response
    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/settings/wfs" "{
        \"enabled\": true,
        \"public\": $public_str,
        \"title\": \"Demo WFS Service\",
        \"abstract\": \"WFS service for the demo workspace\",
        \"max_features\": 10000
    }")

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to enable WFS: $response"
        exit 1
    fi

    log_success "WFS enabled"
}

validate_conformance_wfs_fixture() {
    [[ "$CONFORMANCE_MODE" == "true" ]] || return 0

    log_info "Validating WFS CITE namespace and canonical feature QNames..."
    local endpoint="${BASE_URL}/workspaces/${WORKSPACE_NAME}/wfs"
    local capabilities
    capabilities=$(curl -fsS "${endpoint}?SERVICE=WFS&REQUEST=GetCapabilities&VERSION=2.0.0")
    if ! grep -Fq 'xmlns:cite="http://cite.opengeospatial.org/gmlsf"' <<<"$capabilities"; then
        log_error "WFS capabilities do not declare the CITE namespace"
        exit 1
    fi
    if grep -Fq '<wfs:Name>app:' <<<"$capabilities"; then
        log_error "WFS capabilities contain feature types outside the configured CITE namespace"
        exit 1
    fi

    local schema
    schema=$(curl -fsS "${endpoint}?SERVICE=WFS&REQUEST=DescribeFeatureType&VERSION=2.0.0&TYPENAMES=RoadSegments")
    if ! grep -Fq 'targetNamespace="http://cite.opengeospatial.org/gmlsf"' <<<"$schema"; then
        log_error "DescribeFeatureType did not canonicalize a bare CITE feature type"
        exit 1
    fi

    local features
    features=$(curl -fsS "${endpoint}?SERVICE=WFS&REQUEST=GetFeature&VERSION=2.0.0&TYPENAMES=RoadSegments&COUNT=1")
    if ! grep -Fq '<cite:RoadSegments ' <<<"$features"; then
        log_error "GetFeature did not emit the canonical CITE feature QName"
        exit 1
    fi
    log_success "WFS CITE fixture validated"
}

# Step 8: Enable WCS.
enable_wcs() {
    local public_str="false"
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        public_str="true"
    fi

    log_info "Enabling WCS service (public: $public_str)..."

    local response
    local details=""
    if [[ "$CONFORMANCE_MODE" == "true" ]]; then
        details=',
        "extensions": ["xml-post", "range-subsetting", "scaling", "crs", "interpolation"],
        "allowed_subsetting_crs": ["http://www.opengis.net/def/crs/EPSG/0/4326"],
        "allowed_output_crs": ["http://www.opengis.net/def/crs/EPSG/0/4326", "http://www.opengis.net/def/crs/EPSG/0/3857"],
        "interpolation_methods": ["nearest-neighbor", "linear"],
        "output_formats": ["image/tiff", "application/gml+xml", "multipart/related"]'
    fi
    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/settings/wcs" "{
        \"enabled\": true,
        \"public\": $public_str,
        \"title\": \"Demo WCS Service\",
        \"abstract\": \"WCS service for the demo workspace\"$details
    }")

    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to enable WCS: $response"
        exit 1
    fi

    log_success "WCS enabled"
}

# Step 9: Enable the shared tile publication model for OGC API - Tiles and WMTS.
enable_tiles_and_wmts() {
    local public_str="false"
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        public_str="true"
    fi
    log_info "Enabling OGC API - Tiles and WMTS (public: $public_str)..."

    local response
    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/settings/ogc-tiles" "{
        \"enabled\": true,
        \"public\": $public_str,
        \"title\": \"Demo OGC API - Tiles\",
        \"abstract\": \"Tile service for the demo workspace\",
        \"versions\": [\"1.0.0\"],
        \"settings\": {
            \"tile_matrix_sets\": [\"WebMercatorQuad\", \"WorldCRS84Quad\"],
            \"vector_tiles\": {\"enabled\": true, \"formats\": [\"application/vnd.mapbox-vector-tile\"]},
            \"map_tiles\": {\"enabled\": true, \"formats\": [\"image/png\", \"image/jpeg\"]},
            \"cache_enabled\": false
        }
    }")
    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to enable OGC API - Tiles: $response"
        exit 1
    fi

    response=$(api_call PUT "/api/v1/workspaces/$WORKSPACE_NAME/settings/wmts" "{
        \"enabled\": true,
        \"public\": $public_str,
        \"title\": \"Demo WMTS Service\",
        \"abstract\": \"WMTS service for the demo workspace\",
        \"feature_info_enabled\": true,
        \"vector_tiles_enabled\": false,
        \"provider_name\": \"neoserver\"
    }")
    if echo "$response" | grep -q '"code":'; then
        log_error "Failed to enable WMTS: $response"
        exit 1
    fi
    log_success "OGC API - Tiles and WMTS enabled"
}

# Step 10: Create API key
create_api_key() {
    log_info "Creating viewer API key..."

    local response
    response=$(api_call GET "/api/v1/workspaces/$WORKSPACE_NAME/apikeys")
    if echo "$response" | jq -e '.api_keys[]? | select(.name=="demo-viewer" and .revoked==false)' >/dev/null; then
        log_warn "API key 'demo-viewer' already exists, skipping"
        return 0
    fi

    response=$(api_call POST "/api/v1/workspaces/$WORKSPACE_NAME/apikeys" "{
        \"name\": \"demo-viewer\",
        \"role_id\": \"viewer\",
        \"owner_name\": \"Demo Application\"
    }")

    if echo "$response" | grep -q '"code":'; then
        # Check if it's a duplicate key error
        if echo "$response" | grep -q 'already exists'; then
            log_warn "API key 'demo-viewer' already exists, skipping"
            return 0
        fi
        log_error "Failed to create API key: $response"
        return 1
    fi

    local api_key
    api_key=$(echo "$response" | jq -r '.key // empty')

    if [[ -n "$api_key" ]]; then
        log_success "API key created"
        echo ""
        echo -e "${GREEN}========================================${NC}"
        echo -e "${GREEN}  API Key (save this, shown only once):${NC}"
        echo -e "${GREEN}  $api_key${NC}"
        echo -e "${GREEN}========================================${NC}"
        echo ""
    fi
}

# Main execution
main() {
    echo ""
    echo "============================================"
    echo "  neoserver Demo Workspace Setup"
    echo "============================================"
    echo ""

    wait_for_server
    echo ""

    # Create workspace
    create_workspace

    # Create PostGIS service
    create_service

    # Give it a moment to connect
    sleep 1

    # Discover and publish layers
    log_info "Discovering available layers..."
    local layers
    layers=$(discover_layers)

    if [[ -n "$layers" ]]; then
        echo "$layers" | while read -r layer; do
            if [[ -n "$layer" ]]; then
                local public_id
                case "$layer" in
                    cite.*) public_id="cite:${layer#cite.}" ;;
                    public.*) public_id="${layer#public.}" ;;
                    *) public_id="$layer" ;;
                esac

                # The WMS CITE data precondition identifies the standard
                # fixture layers by their exact cite:* titles. Keep those
                # canonical and humanize titles only for ordinary demo data.
                local title
                if [[ "$public_id" == cite:* ]]; then
                    title="$public_id"
                else
                    title=$(echo "$public_id" | sed 's/_/ /g' | awk '{for(i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) tolower(substr($i,2));}1')
                fi

                publish_layer "$layer" "$public_id" "$title"
            fi
        done

        # Configure time dimension for cite:Autos layer (required for CITE WMS compliance)
        configure_autos_time_dimension
        configure_lakes_elevation_dimension
        if [[ "$CONFORMANCE_MODE" == "true" ]]; then
            configure_assurance_style
        fi
    else
        log_warn "No layers discovered. Make sure PostGIS has data loaded."
    fi

    publish_wcs_fixtures

    echo ""

    # Enable OGC services
    enable_ogcapi
    enable_wms
    enable_wfs
    enable_wcs
    if [[ "$CONFORMANCE_MODE" == "true" ]]; then
        enable_tiles_and_wmts
    fi
    validate_conformance_wfs_fixture

    echo ""

    # Create API key
    create_api_key

    echo ""
    echo "============================================"
    echo "  Setup Complete!"
    echo "============================================"
    echo ""
    if [[ "$PUBLIC_ACCESS" == "true" ]]; then
        echo -e "${GREEN}Public access is ENABLED - no authentication required${NC}"
        echo ""
    else
        echo -e "${YELLOW}Public access is DISABLED - authentication required${NC}"
        echo "Use the API key above or a JWT token to access services."
        echo ""
    fi
    echo "Your workspace is now available at:"
    echo ""
    echo "  OGC API Features:"
    echo "    ${BASE_URL}/workspaces/${WORKSPACE_NAME}/ogc/"
    echo "    ${BASE_URL}/workspaces/${WORKSPACE_NAME}/ogc/collections"
    echo ""
    echo "  WMS GetCapabilities:"
    echo "    ${BASE_URL}/workspaces/${WORKSPACE_NAME}/wms?SERVICE=WMS&REQUEST=GetCapabilities"
    echo ""
    echo "  WFS GetCapabilities:"
    echo "    ${BASE_URL}/workspaces/${WORKSPACE_NAME}/wfs?SERVICE=WFS&REQUEST=GetCapabilities"
    echo ""
    echo "  WCS GetCapabilities:"
    echo "    ${BASE_URL}/workspaces/${WORKSPACE_NAME}/wcs?SERVICE=WCS&REQUEST=GetCapabilities&ACCEPTVERSIONS=2.1.0"
    echo ""
    if [[ "$CONFORMANCE_MODE" == "true" ]]; then
        echo "  OGC API - Tiles:"
        echo "    ${BASE_URL}/workspaces/${WORKSPACE_NAME}/ogc-tiles/"
        echo ""
        echo "  WMTS GetCapabilities:"
        echo "    ${BASE_URL}/workspaces/${WORKSPACE_NAME}/wmts?SERVICE=WMTS&REQUEST=GetCapabilities"
        echo ""
    fi
    echo "  Administration console:"
    echo "    ${BASE_URL}/admin/"
    echo ""
}

main
