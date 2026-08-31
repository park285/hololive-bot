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
  'TRIVY_VERSION: "0.74.0"' \
  'TRIVY_LINUX_AMD64_SHA256: 2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a' \
  'DOCKER_COMPOSE_VERSION: v2.39.4' \
  'DOCKER_COMPOSE_LINUX_X86_64_SHA256: 7af95166a730b87e172d4fc9aefea8725d3c6c7327d59149267b452114ddb7d4' \
  'https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-linux-x86_64' \
  'https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz' \
  'sha256sum --check -'; do
  grep -Fq "$required" "$workflow" || fail "security workflow is missing: $required"
done

[[ "$(grep -Fc 'DOCKER_COMPOSE_VERSION: v2.39.4' "$workflow")" -eq 2 ]] ||
  fail "both recurring scan jobs must install the exact Docker Compose release"

if grep -Fq 'uses: aquasecurity/' "$workflow"; then
  fail "security workflow must not use actions blocked by the repository allowlist"
fi

[[ "$(grep -Fc "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'" "$workflow")" -eq 4 ]] ||
  fail "all four final image install/setup/build/scan steps must be restricted to scheduled or explicit dispatch runs"

echo "recurring npm and final-image security scan contract passed"
