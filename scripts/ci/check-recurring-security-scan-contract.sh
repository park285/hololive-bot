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
cut -d'|' -f3 scripts/ci/final-image-scan-manifest.txt >"$tmp_dir/manifest-images"
cmp -s "$tmp_dir/manifest-images" "$tmp_dir/final-images" ||
  fail "final image manifest does not cover the exact current production Compose corpus"

if awk -F'|' '
  NF != 3 || $1 == "" || $2 != "linux/arm64" || $3 == "" { exit 1 }
  $3 ~ /@sha256:/ && $1 != "remote" { exit 1 }
  $3 !~ /@sha256:/ && $1 != "local" { exit 1 }
' scripts/ci/final-image-scan-manifest.txt; then
  :
else
  fail "final image manifest must scan arm64 local builds locally and exact arm64 external images remotely"
fi

LIVE_LOGS_PATH=/tmp LIVE_DB_BACKUP_PATH=/tmp \
  docker compose --env-file deploy/compose/build-only.env.sample \
    -f deploy/compose/docker-compose.prod.yml build --print >"$tmp_dir/production-bake.json"
grep -Fq '"type=provenance,mode=max"' "$tmp_dir/production-bake.json" ||
  fail "production builds must retain maximum provenance attestations"
grep -Fq '"type=sbom"' "$tmp_dir/production-bake.json" ||
  fail "production builds must retain SBOM attestations"

LIVE_LOGS_PATH=/tmp LIVE_DB_BACKUP_PATH=/tmp \
  docker compose --env-file deploy/compose/build-only.env.sample \
    -f deploy/compose/docker-compose.prod.yml \
    -f deploy/compose/docker-compose.security-scan.yml build --print >"$tmp_dir/security-scan-bake.json"
if grep -Fq '"attest"' "$tmp_dir/security-scan-bake.json"; then
  fail "disposable local-image scan builds must not request unsupported attestations"
fi

workflow=.github/workflows/security.yml
for required in \
  'bash scripts/ci/check-recurring-security-scan-contract.sh' \
  'bash scripts/ci/run-npm-audit.sh' \
  './build-all.sh --no-bump --build-only --security-scan --skip-local-ci' \
  'bash scripts/ci/run-final-image-scan.sh' \
  'DOCKER_DEFAULT_PLATFORM: linux/arm64' \
  'TRIVY_VERSION: "0.74.0"' \
  'TRIVY_LINUX_AMD64_SHA256: 2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a' \
  'DOCKER_COMPOSE_VERSION: v2.39.4' \
  'DOCKER_COMPOSE_LINUX_X86_64_SHA256: 7af95166a730b87e172d4fc9aefea8725d3c6c7327d59149267b452114ddb7d4' \
  'https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-linux-x86_64' \
  'https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz' \
  'sha256sum --check -'; do
  grep -Fq "$required" "$workflow" || fail "security workflow is missing: $required"
done

grep -Fq 'corepack "$npm_package_manager" audit --package-lock-only --audit-level=high' scripts/ci/run-npm-audit.sh ||
  fail "npm audit must use the exact integrity-bound packageManager"

scanner=scripts/ci/run-final-image-scan.sh
bash scripts/ci/python-runner.sh -- scripts/ci/check-trivyignore-contract.py --root "$root_dir"
bash scripts/ci/check-trivyignore-contract_test.sh

check_exception() {
  local key="$1" image="$2" file="$3" expected_count="$4" evidence="$5"
  local ignore="scripts/ci/$file" id_count duplicate_ids

  [[ -f "$ignore" && ! -L "$ignore" ]] ||
    fail "$key vulnerability exception must be a regular file"
  grep -Fq 'Owner: hololive-bot.' "$ignore" || fail "$key exception has no owner"
  grep -Fq "$evidence" "$ignore" || fail "$key exception has no reachability evidence"
  id_count="$(grep -Ec '^[[:space:]]+- id: CVE-[0-9-]+$' "$ignore")"
  [[ "$id_count" -eq "$expected_count" ]] ||
    fail "$key exception must contain exactly $expected_count vulnerabilities"
  duplicate_ids="$(sed -n 's/^[[:space:]]*- id: \(CVE-[0-9-]*\)$/\1/p' "$ignore" | LC_ALL=C sort | uniq -d)"
  [[ -z "$duplicate_ids" ]] || fail "$key exception contains duplicate vulnerability IDs"
  [[ "$(grep -Fc 'expired_at: 2026-09-14' "$ignore")" -eq "$expected_count" ]] ||
    fail "$key exception entries must all have the bounded expiry"
  [[ "$(date -u -d '2026-09-14' +%s)" -gt "$(date -u +%s)" ]] ||
    fail "$key vulnerability exception has expired"
  grep -Fq "${key}_exception_target='remote|linux/arm64|$image'" "$scanner" ||
    fail "$key exception must be bound to the exact linux/arm64 scanned image"
  grep -Fq "ignore_file=\"\$root_dir/scripts/ci/$file\"" "$scanner" ||
    fail "$key exception file is not selected by the exact-image branch"
}

check_exception \
  nginx \
  'nginx:1.31.4-alpine-slim@sha256:1870de6d59aafee152589b64404556d2535922cdd998e6dac1c4888c938ed8f9' \
  trivyignore-nginx.yaml 1 'no TLS or QUIC listener'
check_exception \
  postgres \
  'postgres:18.6-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2' \
  trivyignore-postgres.yaml 23 'root-only gosu branch is unreachable'
check_exception \
  deunhealth \
  'qmcgaw/deunhealth@sha256:db1e4fcd3aceeb0da34a83f7a8a5432df586e6d0388ddb6ad8dd7b479e4aa25d' \
  trivyignore-deunhealth.yaml 28 'Exact source 37e5bd4 was reviewed'
check_exception \
  socket_proxy \
  'wollomatic/socket-proxy:1.12.3@sha256:74e770f5ed3cfc9ecb6350e177d2aa55873568c85bc953079834e68607dbf71b' \
  trivyignore-socket-proxy.yaml 8 'Exact release source bcb95c8 passed govulncheck'

[[ "$(grep -Fc -- '--ignorefile' "$scanner")" -eq 1 ]] ||
  fail "final-image scanning must have exactly one bounded ignore path"
[[ "$(grep -Fc 'ignore_file="$root_dir/scripts/ci/trivyignore-' "$scanner")" -eq 4 ]] ||
  fail "final-image scanning must have exactly four exact-image exception branches"
grep -Fq 'scan_args+=(--image-src remote --platform "$platform")' "$scanner" ||
  fail "exact external images must be scanned from the registry for their declared platform"
grep -Fq "actual_platform=\"\$(docker image inspect --format '{{.Os}}/{{.Architecture}}' \"\$image\")\"" "$scanner" ||
  fail "locally built images must be verified as the declared production platform"

[[ "$(grep -Fc 'DOCKER_COMPOSE_VERSION: v2.39.4' "$workflow")" -eq 2 ]] ||
  fail "both recurring scan jobs must install the exact Docker Compose release"
# Python 부트스트랩은 저장소 composite action 이 소유하고, subdirectory checkout(path: hololive-bot)을
# 쓰는 두 job 은 그 action 을 checkout 경로로 호출한다. action 사본 parity 와 핀 값은 iris-stack 이 본다.
python_runtime_action=.github/actions/python-runtime/action.yml
[[ "$(grep -Fc 'uses: ./hololive-bot/.github/actions/python-runtime' "$workflow")" -eq 2 ]] ||
  fail "both recurring scan jobs must bootstrap Python through the repository python-runtime action"
[[ "$(grep -Fc '          working-directory: hololive-bot' "$workflow")" -eq 2 ]] ||
  fail "both recurring scan jobs must point the python-runtime action at the hololive-bot checkout"
if grep -Fq -e 'actions/setup-python@' -e 'uv==0.12.7' -e 'python-runner.sh --print-interpreter' "$workflow"; then
  fail "recurring scan jobs must not inline the Python bootstrap"
fi
grep -Fq 'python-version-file: ${{ inputs.working-directory }}/.python-version' "$python_runtime_action" ||
  fail "python-runtime action must install the exact Python runtime from the checkout .python-version"
grep -Fq 'uv==0.12.7' "$python_runtime_action" ||
  fail "python-runtime action must install the exact uv release"
grep -Fq 'interpreter="$(bash scripts/ci/python-runner.sh --print-interpreter)"' "$python_runtime_action" ||
  fail "python-runtime action must initialize the exact Python interpreter"
grep -Fq "CI_PYTHON_BIN=%s" "$python_runtime_action" ||
  fail "python-runtime action must export the exact Python interpreter"
grep -Fq "CI_PYTHON_RUNTIME_ROOT=%s" "$python_runtime_action" ||
  fail "python-runtime action must export the Python runtime root"

if grep -Fq 'uses: aquasecurity/' "$workflow"; then
  fail "security workflow must not use actions blocked by the repository allowlist"
fi

[[ "$(grep -Fc "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'" "$workflow")" -eq 4 ]] ||
  fail "all four final image install/setup/build/scan steps must be restricted to scheduled or explicit dispatch runs"

echo "recurring npm and final-image security scan contract passed"
