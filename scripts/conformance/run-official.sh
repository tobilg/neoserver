#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE=(docker compose -p "${CONFORMANCE_PROJECT_NAME:-neoserver-official-$$}" -f "$ROOT/docker-compose.conformance.yml")
COMPOSE_ALL=("${COMPOSE[@]}" --profile wms13 --profile wfs20 --profile wcs20 --profile wcs20-derived --profile wmts10 --profile ogcapi-features10 --profile ogcapi-tiles10)
RESULT_ROOT="$ROOT/test-results/conformance"
KEEP_ENVIRONMENT="${KEEP_CONFORMANCE_ENVIRONMENT:-false}"
CURRENT_TEAMENGINE=""

SUITES=(wms13 wfs20 wcs20 wmts10 ogcapi-features10 ogcapi-tiles10)

usage() {
    echo "Usage: $0 {all|wms13|wfs20|wcs20|wmts10|ogcapi-features10|ogcapi-tiles10}"
}

cleanup() {
    if [[ "$KEEP_ENVIRONMENT" != "true" ]]; then
        "${COMPOSE_ALL[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

manifest() {
    go run ./testing/officialets/cmd/etsmanifest "$@"
}

is_suite() {
    local requested="$1"
    local candidate
    for candidate in "${SUITES[@]}"; do
        [[ "$candidate" == "$requested" ]] && return 0
    done
    return 1
}

prepare_environment() {
    "${COMPOSE_ALL[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    mkdir -p "$RESULT_ROOT"
    "${COMPOSE[@]}" build setup ets-controller
    if [[ "${CONFORMANCE_SKIP_BUILD:-false}" == "true" ]]; then
        : "${CONFORMANCE_TEST_IMAGE:?Set the exact prebuilt candidate image}"
        "${COMPOSE[@]}" up --no-build -d server
    else
        "${COMPOSE[@]}" up --build -d server
    fi
    export NEOSERVER_IMAGE_ID
    NEOSERVER_IMAGE_ID="$(docker inspect --format '{{.Image}}' "$("${COMPOSE[@]}" ps -q server)")"
    "${COMPOSE[@]}" run -T --rm --no-deps setup
}

run_controller() {
    local suite="$1"
    local profile="$2"
    local suite_code="$3"
    local image="$4"
    local args_json="$5"
    local result_dir="/results/conformance/$suite/$profile"
    local teamengine_url="http://$CURRENT_TEAMENGINE:8080/teamengine"
    mkdir -p "$RESULT_ROOT/$suite"

    # Disable container stdin so a controller launched inside the manifest
    # read-loop cannot consume the remaining profile records.
    if ! "${COMPOSE[@]}" run -T --rm --no-deps \
        -e "TEAMENGINE_URL=$teamengine_url" \
        -e "ETS_SUITE=$suite-$profile" \
        -e "ETS_SUITE_CODE=$suite_code" \
        -e "ETS_ARGS_JSON=$args_json" \
        -e "ETS_RESULT_DIR=$result_dir" \
        -e "ETS_EVIDENCE_KIND=official" \
        -e "ETS_IMAGE=$image" \
        ets-controller; then
        "${COMPOSE[@]}" logs --no-color server "$CURRENT_TEAMENGINE" > "$RESULT_ROOT/$suite/container.log" 2>&1 || true
        # TEAM Engine keeps its error log and per-test request/response records
        # inside the container. Without them a failure that happens inside the
        # suite itself (a parser exception, an unreadable response) cannot be
        # told apart from a server defect.
        "${COMPOSE[@]}" cp "$CURRENT_TEAMENGINE:/root/te_base/users" "$RESULT_ROOT/$suite/teamengine-sessions" >/dev/null 2>&1 || true
        echo "Official $suite/$profile ETS failed; evidence is under $RESULT_ROOT/$suite" >&2
        return 1
    fi
}

execute_suite() {
    local suite="$1"
    local suite_failed=0
    local suite_code image
    IFS=$'\t' read -r suite_code image < <(manifest suite "$suite")

    echo "Preparing isolated official conformance environment for $suite"
    # A suite is selected from the fixed allowlist above, so removing only its
    # previous generated evidence cannot affect source files or another suite.
    rm -rf "$RESULT_ROOT/$suite"
    prepare_environment
    CURRENT_TEAMENGINE="teamengine-$suite"
    "${COMPOSE[@]}" --profile "$suite" up -d "$CURRENT_TEAMENGINE"

    local profile args_json found=0
    while IFS=$'\t' read -r profile args_json <&3; do
        [[ -z "$profile" ]] && continue
        found=1
        run_controller "$suite" "$profile" "$suite_code" "$image" "$args_json" || suite_failed=1
    done 3< <(manifest --server http://server:9000 profiles official "$suite")
    if [[ "$found" -eq 0 ]]; then
        echo "No official profiles are registered for $suite" >&2
        suite_failed=1
    fi

    if [[ "$KEEP_ENVIRONMENT" != "true" ]]; then
        "${COMPOSE_ALL[@]}" down -v --remove-orphans
    fi
    return "$suite_failed"
}

main() {
    local requested="${1:-}"
    if [[ -z "$requested" ]]; then
        usage
        exit 2
    fi
    cd "$ROOT"
    export NEOSERVER_COMMIT
    NEOSERVER_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
    if [[ -n "$(git status --porcelain --untracked-files=normal 2>/dev/null)" ]]; then
        NEOSERVER_COMMIT+="-dirty"
    fi

    local failed=0
    if [[ "$requested" == "all" ]]; then
        local suite
        for suite in "${SUITES[@]}"; do
            execute_suite "$suite" || failed=1
        done
    elif is_suite "$requested"; then
        execute_suite "$requested" || failed=1
    else
        usage
        exit 2
    fi
    exit "$failed"
}

main "$@"
