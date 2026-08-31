#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
cd "$root_dir"

fail() {
  echo "recurring security scan contract failed: $*" >&2
  exit 1
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

git ls-files '*package-lock.json' | LC_ALL=C sort -u >"$tmp_dir/npm-locks"
cmp -s scripts/ci/npm-audit-manifest.txt "$tmp_dir/npm-locks" ||
  fail "npm audit manifest does not cover the exact tracked lockfile corpus"

{
  LIVE_LOGS_PATH=/tmp LIVE_DB_BACKUP_PATH=/tmp \
    docker compose --env-file deploy/compose/build-only.env.sample \
      -f deploy/compose/docker-compose.prod.yml \
      -f deploy/compose/docker-compose.live-compat.yml config --images
  docker compose --env-file deploy/compose/build-only.env.sample \
    -f deploy/compose/docker-compose.standby.yml config --images
} | LC_ALL=C sort -u >"$tmp_dir/final-images"
cmp -s scripts/ci/final-image-scan-manifest.txt "$tmp_dir/final-images" ||
  fail "final image manifest does not cover the exact current production Compose corpus"

workflow=.github/workflows/security.yml
for required in \
  'bash scripts/ci/check-recurring-security-scan-contract.sh' \
  'bash scripts/ci/run-npm-audit.sh' \
  './build-all.sh --no-bump --build-only --skip-local-ci' \
  'bash scripts/ci/run-final-image-scan.sh' \
  'aquasecurity/setup-trivy@3fb12ec12f41e471780db15c232d5dd185dcb514' \
  'version: v0.74.0'; do
  grep -Fq "$required" "$workflow" || fail "security workflow is missing: $required"
done

[[ "$(grep -Fc "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'" "$workflow")" -eq 3 ]] ||
  fail "all three final image setup/build/scan steps must be restricted to scheduled or explicit dispatch runs"

echo "recurring npm and final-image security scan contract passed"
