#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE=(docker compose -p "${CONFORMANCE_PROJECT_NAME:-neoserver-derived-$$}" -f "$ROOT/docker-compose.conformance.yml")
COMPOSE_ALL=("${COMPOSE[@]}" --profile wcs20-derived --profile wfs20-derived)
RESULT_ROOT=""
KEEP_ENVIRONMENT="${KEEP_CONFORMANCE_ENVIRONMENT:-false}"


cleanup() {
    if [[ "$KEEP_ENVIRONMENT" != "true" ]]; then
        "${COMPOSE_ALL[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

run_profile() {
    local key="$1" derived_image_ref teamengine
    case "$key" in
        wcs20/interpolation)
            derived_image_ref="neoserver/ets-wcs20-interpolation-derived:wcs20-1.21-uri-xpath-v1"
            teamengine="teamengine-wcs20-derived"
            ;;
        wfs20/core202)
            derived_image_ref="neoserver/ets-wfs202-derived:wfs20-1.42-lock-response-v1"
            teamengine="teamengine-wfs20-derived"
            ;;
        *) return 2 ;;
    esac
    RESULT_ROOT="$ROOT/test-results/conformance-derived/$key"
    local suite profile suite_code base_image patch_set patch_path patch_sha args_json
    IFS=$'\t' read -r suite profile suite_code base_image patch_set patch_path patch_sha args_json < <(
        go run ./testing/officialets/cmd/etsmanifest --server http://server:9000 derived "$key"
    )
    "${COMPOSE_ALL[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    rm -rf "$RESULT_ROOT"
    mkdir -p "$RESULT_ROOT"
    "${COMPOSE[@]}" build setup ets-controller "$teamengine"
    local derived_image_id
    derived_image_id="$(docker image inspect --format '{{.Id}}' "$derived_image_ref" 2>/dev/null || true)"
    if [[ -z "$derived_image_id" ]]; then
        echo "Unable to resolve the built official-derived image ID" >&2
        return 1
    fi
    if [[ "${CONFORMANCE_SKIP_BUILD:-false}" == "true" ]]; then
        : "${CONFORMANCE_TEST_IMAGE:?Set the exact prebuilt candidate image}"
        "${COMPOSE[@]}" up --no-build -d server
    else
        "${COMPOSE[@]}" up --build -d server
    fi
    export NEOSERVER_IMAGE_ID
    NEOSERVER_IMAGE_ID="$(docker inspect --format '{{.Image}}' "$("${COMPOSE[@]}" ps -q server)")"
    if [[ -f "$ROOT/testing/conformance/fixtures/$suite.sql" ]]; then
        "${COMPOSE[@]}" exec -T db psql -U postgres -d postgis -v ON_ERROR_STOP=1 < "$ROOT/testing/conformance/fixtures/$suite.sql"
    fi
    "${COMPOSE[@]}" run -T --rm --no-deps -e "CONFORMANCE_SUITE=$suite" setup
    "${COMPOSE[@]}" --profile "$suite-derived" up -d "$teamengine"

    if ! "${COMPOSE[@]}" run -T --rm --no-deps \
        -e "TEAMENGINE_URL=http://$teamengine:8080/teamengine" \
        -e "ETS_SUITE=$suite-$profile" \
        -e "ETS_SUITE_CODE=$suite_code" \
        -e "ETS_ARGS_JSON=$args_json" \
        -e "ETS_RESULT_DIR=/results/conformance-derived/$key" \
        -e "ETS_EVIDENCE_KIND=official-derived" \
        -e "ETS_IMAGE=$base_image" \
        -e "ETS_BASE_IMAGE=$base_image" \
        -e "ETS_DERIVED_IMAGE_ID=$derived_image_id" \
        -e "ETS_PATCH_SET=$patch_set" \
        -e "ETS_PATCH_SHA256=$patch_sha" \
        ets-controller; then
        "${COMPOSE[@]}" logs --no-color server "$teamengine" > "$RESULT_ROOT/container.log" 2>&1 || true
        # See run-official.sh: the suite's own error log and request records.
        "${COMPOSE[@]}" cp "$teamengine:/root/te_base/users" "$RESULT_ROOT/teamengine-sessions" >/dev/null 2>&1 || true
        return 1
    fi
    python3 "$ROOT/scripts/conformance/check-coverage.py" "$suite/$profile" "$RESULT_ROOT"
}

main() {
    local requested="${1:-all}"
    case "$requested" in
        all|wcs20-interpolation|wfs20-core202) ;;
        *) echo "Usage: $0 {all|wcs20-interpolation|wfs20-core202}" >&2; exit 2 ;;
    esac
    cd "$ROOT"
    export NEOSERVER_COMMIT
    NEOSERVER_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
    if [[ -n "$(git status --porcelain --untracked-files=normal 2>/dev/null)" ]]; then
        NEOSERVER_COMMIT+="-dirty"
    fi
    # Invoke each profile in a subshell so set -e still stops setup failures;
    # capture the status without an `if run_profile` disabling errexit inside it.
    local failed=0 status key
    for key in wcs20/interpolation wfs20/core202; do
        [[ "$requested" == "all" || "$requested" == "${key/\//-}" ]] || continue
        set +e
        (set -e; run_profile "$key")
        status=$?
        set -e
        [[ "$status" -eq 0 ]] || failed=1
    done
    exit "$failed"
}

main "$@"
