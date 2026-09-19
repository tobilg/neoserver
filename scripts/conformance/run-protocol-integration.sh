#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE=(docker compose -p "${CONFORMANCE_PROJECT_NAME:-neoserver-native-$$}" -f "$ROOT/docker-compose.conformance.yml")
RESULT_ROOT="$ROOT/test-results/protocol-integration"
BASE_URL="http://localhost:${CONFORMANCE_HTTP_PORT:-19000}"
KEEP_ENVIRONMENT="${KEEP_CONFORMANCE_ENVIRONMENT:-false}"

cleanup() {
    if [[ "$KEEP_ENVIRONMENT" != "true" ]]; then
        "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

main() {
    export CONFORMANCE_SERVER_URLBASE="$BASE_URL"
    export GOCACHE="${GOCACHE:-/tmp/neoserver-gocache}"
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    mkdir -p "$RESULT_ROOT"
    "${COMPOSE[@]}" build setup
    if [[ "${CONFORMANCE_SKIP_BUILD:-false}" == "true" ]]; then
        : "${CONFORMANCE_TEST_IMAGE:?Set the exact prebuilt candidate image}"
        "${COMPOSE[@]}" up --no-build -d server
    else
        "${COMPOSE[@]}" up --build -d server
    fi
    docker inspect --format '{{.Image}}' "$("${COMPOSE[@]}" ps -q server)" > "$RESULT_ROOT/candidate-image-id.txt"
    "${COMPOSE[@]}" run -T --rm --no-deps setup
    cd "$ROOT"
    go run ./testing/protocol/cmd/protocoltest --base-url "$BASE_URL" --results "$RESULT_ROOT"
}

main "$@"
