#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="${repo_root}/testing/minio/docker-compose.yml"
minio_port="${NEOSRV_MINIO_PORT:-19000}"
# Never target an operator's default Compose project or its persistent data.
fixture_project="neoserver-s3-lease-$$"

cleanup() {
  docker compose -p "${fixture_project}" -f "${compose_file}" down --volumes --remove-orphans
}
trap cleanup EXIT

docker compose -p "${fixture_project}" -f "${compose_file}" up --detach --wait

export AWS_ACCESS_KEY_ID="neoserver-test"
export AWS_SECRET_ACCESS_KEY="neoserver-test-secret"
export AWS_REGION="us-east-1"
export AWS_EC2_METADATA_DISABLED="true"
export NEOSRV_MINIO_ENDPOINT="http://127.0.0.1:${minio_port}"

cd "${repo_root}"
go test ./internal/tilecache -run '^TestMinIO' -count=1 -v
