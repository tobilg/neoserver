#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STEPS=(test test-protocol-integration test-conformance test-conformance-derived)
FAILED=()

cd "$ROOT"
for step in "${STEPS[@]}"; do
    echo "Running assurance category: $step"
    if ! make "$step"; then
        FAILED+=("$step")
    fi
done

if [[ "${#FAILED[@]}" -gt 0 ]]; then
    echo "Failed assurance categories: ${FAILED[*]}" >&2
    exit 1
fi
echo "All assurance categories passed"
