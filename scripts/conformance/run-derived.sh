#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE=(docker compose -p "${CONFORMANCE_PROJECT_NAME:-neoserver-derived-$$}" -f "$ROOT/docker-compose.conformance.yml")
COMPOSE_ALL=("${COMPOSE[@]}" --profile wcs20-derived)
RESULT_ROOT="$ROOT/test-results/conformance-derived/wcs20/interpolation"
KEEP_ENVIRONMENT="${KEEP_CONFORMANCE_ENVIRONMENT:-false}"
DERIVED_IMAGE_REF="neoserver/ets-wcs20-interpolation-derived:wcs20-1.21-uri-xpath-v1"

cleanup() {
    if [[ "$KEEP_ENVIRONMENT" != "true" ]]; then
        "${COMPOSE_ALL[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

main() {
    local requested="${1:-all}"
    if [[ "$requested" != "all" && "$requested" != "wcs20-interpolation" ]]; then
        echo "Usage: $0 {all|wcs20-interpolation}" >&2
        exit 2
    fi
    cd "$ROOT"
    export NEOSERVER_COMMIT
    NEOSERVER_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
    if [[ -n "$(git status --porcelain --untracked-files=normal 2>/dev/null)" ]]; then
        NEOSERVER_COMMIT+="-dirty"
    fi

    local suite profile suite_code base_image patch_set patch_path patch_sha args_json
    IFS=$'\t' read -r suite profile suite_code base_image patch_set patch_path patch_sha args_json < <(
        go run ./testing/officialets/cmd/etsmanifest --server http://server:9000 derived wcs20/interpolation
    )
    "${COMPOSE_ALL[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    rm -rf "$RESULT_ROOT"
    mkdir -p "$RESULT_ROOT"
    "${COMPOSE[@]}" build setup ets-controller teamengine-wcs20-derived
    local derived_image_id
    derived_image_id="$(docker image inspect --format '{{.Id}}' "$DERIVED_IMAGE_REF" 2>/dev/null || true)"
    if [[ -z "$derived_image_id" ]]; then
        echo "Unable to resolve the built official-derived image ID" >&2
        exit 1
    fi
    if [[ "${CONFORMANCE_SKIP_BUILD:-false}" == "true" ]]; then
        : "${CONFORMANCE_TEST_IMAGE:?Set the exact prebuilt candidate image}"
        "${COMPOSE[@]}" up --no-build -d server
    else
        "${COMPOSE[@]}" up --build -d server
    fi
    export NEOSERVER_IMAGE_ID
    NEOSERVER_IMAGE_ID="$(docker inspect --format '{{.Image}}' "$("${COMPOSE[@]}" ps -q server)")"
    "${COMPOSE[@]}" run -T --rm --no-deps setup
    "${COMPOSE[@]}" --profile wcs20-derived up -d teamengine-wcs20-derived

    if ! "${COMPOSE[@]}" run -T --rm --no-deps \
        -e "TEAMENGINE_URL=http://teamengine-wcs20-derived:8080/teamengine" \
        -e "ETS_SUITE=$suite-$profile" \
        -e "ETS_SUITE_CODE=$suite_code" \
        -e "ETS_ARGS_JSON=$args_json" \
        -e "ETS_RESULT_DIR=/results/conformance-derived/wcs20/interpolation" \
        -e "ETS_EVIDENCE_KIND=official-derived" \
        -e "ETS_IMAGE=$base_image" \
        -e "ETS_BASE_IMAGE=$base_image" \
        -e "ETS_DERIVED_IMAGE_ID=$derived_image_id" \
        -e "ETS_PATCH_SET=$patch_set" \
        -e "ETS_PATCH_SHA256=$patch_sha" \
        ets-controller; then
        "${COMPOSE[@]}" logs --no-color server teamengine-wcs20-derived > "$RESULT_ROOT/container.log" 2>&1 || true
        # See run-official.sh: the suite's own error log and request records.
        "${COMPOSE[@]}" cp teamengine-wcs20-derived:/root/te_base/users "$RESULT_ROOT/teamengine-sessions" >/dev/null 2>&1 || true
        exit 1
    fi
}

main "$@"
