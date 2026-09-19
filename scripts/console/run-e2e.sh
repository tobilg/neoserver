#!/usr/bin/env bash
set -euo pipefail

# Brings up a disposable neoserver (with Keycloak) and runs the console's
# Playwright suite against it. The console is served by the Go binary from the
# embedded build, so this exercises the real deployment shape rather than the
# Vite dev server.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE=(docker compose -p "${CONSOLE_PROJECT_NAME:-neoserver-console-e2e}" -f "$ROOT/docker-compose.console-e2e.yml")
PORT="${CONSOLE_HTTP_PORT:-19100}"
BASE_URL="http://localhost:${PORT}"
KEEP_ENVIRONMENT="${KEEP_CONSOLE_ENVIRONMENT:-false}"

cleanup() {
    if [[ "$KEEP_ENVIRONMENT" != "true" ]]; then
        "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

wait_for_ready() {
    for _ in $(seq 1 90); do
        if curl -fsS "${BASE_URL}/health" >/dev/null 2>&1; then
            return 0
        fi
        sleep 2
    done
    echo "error: server did not become healthy at ${BASE_URL}" >&2
    "${COMPOSE[@]}" logs --tail 80 server >&2 || true
    exit 1
}

main() {
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    if [[ "${CONSOLE_SKIP_BUILD:-false}" == "true" ]]; then
        # Opt in only when the local image already contains the current code.
        "${COMPOSE[@]}" up --no-build -d server
    else
        "${COMPOSE[@]}" up --build -d server
    fi
    wait_for_ready

    # The bootstrap token is the only credential that exists before the console
    # provisions anything; global-setup uses it to create the fixtures.
    local token
    token="$("${COMPOSE[@]}" exec -T server cat /state/token | tr -d '\r\n')"
    if [[ -z "$token" ]]; then
        echo "error: bootstrap token was empty" >&2
        exit 1
    fi

    cd "$ROOT/web/admin"
    CONSOLE_URL="${BASE_URL}/admin" \
    CONSOLE_API_URL="${BASE_URL}" \
    CONSOLE_BOOTSTRAP_TOKEN="$token" \
    CONSOLE_KEYCLOAK_URL="http://keycloak:8080" \
    CONSOLE_LOCAL_KEYCLOAK=true \
        npx playwright test "$@"
}

main "$@"
