#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:9000}"

echo "== health"
curl -fsS "$BASE_URL/health" | cat
echo

echo "== landing"
curl -fsS "$BASE_URL/ogc/" | jq -r '.title' >/dev/null

echo "== conformance"
curl -fsS "$BASE_URL/ogc/conformance" | jq -r '.conformsTo[]' >/dev/null

echo "== collections"
curl -fsS "$BASE_URL/ogc/collections" | jq -r '.collections[].id' | sort

echo "== items (places)"
curl -fsS "$BASE_URL/ogc/collections/public.places/items?limit=10" | jq -r '.type' | grep -q FeatureCollection

echo "== ui"
curl -fsSI "$BASE_URL/ui" | grep -qi "content-type: text/html"

# WFS Tests (only run if WFS is enabled)
if curl -fsS "$BASE_URL/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetCapabilities" 2>/dev/null | grep -q "WFS_Capabilities"; then
    echo "== WFS GetCapabilities"
    curl -fsS "$BASE_URL/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetCapabilities" | grep -q "WFS_Capabilities"

    echo "== WFS DescribeFeatureType"
    curl -fsS "$BASE_URL/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeFeatureType" | grep -q "schema"

    echo "== WFS ListStoredQueries"
    curl -fsS "$BASE_URL/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=ListStoredQueries" | grep -q "StoredQuery"

    echo "== WFS DescribeStoredQueries"
    curl -fsS "$BASE_URL/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeStoredQueries" | grep -q "StoredQueryDescription"

    # Get first collection ID for GetFeature test
    FIRST_COLLECTION=$(curl -fsS "$BASE_URL/ogc/collections" | jq -r '.collections[0].id')
    if [ -n "$FIRST_COLLECTION" ] && [ "$FIRST_COLLECTION" != "null" ]; then
        echo "== WFS GetFeature (GML)"
        curl -fsS "$BASE_URL/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=$FIRST_COLLECTION&COUNT=1" | grep -q "FeatureCollection"

        echo "== WFS GetFeature (GeoJSON)"
        curl -fsS "$BASE_URL/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=$FIRST_COLLECTION&COUNT=1&OUTPUTFORMAT=application/json" | grep -q "FeatureCollection"
    fi
else
    echo "== WFS (skipped - not enabled)"
fi

echo "OK"


