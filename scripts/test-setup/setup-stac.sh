#!/usr/bin/env bash
set -euo pipefail
STAC_FIXTURES="${STAC_FIXTURES:-/stac-fixtures}"
STAC_BASE="${NEOSERVER_URL:-http://localhost:9000}/api/v1/workspaces/demo"
: "${NEOSERVER_TOKEN:?A disposable fixture administrator token is required}"
request() {
    curl -fsS -H "Authorization: Bearer $NEOSERVER_TOKEN" -H 'Content-Type: application/json' "$@"
}
request -X PUT "$STAC_BASE/settings/stac" -d '{"enabled":true,"public":true,"title":"STAC qualification"}' >/dev/null
collection=$(jq '{document:.,public:true,revision:0,item_count:0}' "$STAC_FIXTURES/collection.json")
request -X POST "$STAC_BASE/stac/collections" -d "$collection" >/dev/null
job=$(request -X POST "$STAC_BASE/stac/imports?collection_id=conformance-scenes" --data-binary "@$STAC_FIXTURES/items.ndjson" | jq -er '.id')
request -X POST "$STAC_BASE/stac/imports/$job/publish" -d '{"upsert":false}' >/dev/null
echo 'Published the 300-Item STAC conformance fixture.'
