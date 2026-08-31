#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
runner="$root_dir/scripts/ci/python-runner.sh"
checker="$root_dir/scripts/ci/check-trivyignore-contract.py"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  echo "Trivy exception contract test failed: $*" >&2
  exit 1
}

make_fixture() {
  local destination="$1"
  mkdir -p "$destination/scripts/ci" "$destination/deploy/compose" "$destination/deploy/nginx"
  cp "$root_dir"/scripts/ci/trivyignore-*.yaml "$destination/scripts/ci/"
  cp "$root_dir/deploy/compose/docker-compose.prod.yml" "$destination/deploy/compose/"
  cp "$root_dir/deploy/compose/docker-compose.live-compat.yml" "$destination/deploy/compose/"
  cp "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template" "$destination/deploy/nginx/"
}

expect_failure() {
  local label="$1" fixture="$2"
  local output
  if output="$("$runner" -- "$checker" --root "$fixture" 2>&1)"; then
    fail "$label: expected failure"
  fi
  [[ "$output" == *"exception tuple mismatch"* || "$output" == *"missing purls"* || "$output" == *"purls must not be empty"* || "$output" == *"exception requires"* ]] ||
    fail "$label: unexpected failure: $output"
}

"$runner" -- "$checker" --root "$root_dir" >/dev/null

fixture="$tmp_dir/missing-purl"
make_fixture "$fixture"
sed -i '0,/pkg:apk\/alpine\/libcrypto3@3.5.7-r0/{//d;}' "$fixture/scripts/ci/trivyignore-nginx.yaml"
expect_failure "missing per-entry purl" "$fixture"

fixture="$tmp_dir/widened-purl"
make_fixture "$fixture"
sed -i '0,/pkg:golang\/stdlib@v1.24.6/{s/v1.24.6/v1.25.5/;}' "$fixture/scripts/ci/trivyignore-postgres.yaml"
expect_failure "changed CVE purl" "$fixture"

fixture="$tmp_dir/changed-statement"
make_fixture "$fixture"
sed -i '0,/Owner: hololive-bot\./{s/Owner: hololive-bot\./Owner: nobody./;}' "$fixture/scripts/ci/trivyignore-socket-proxy.yaml"
expect_failure "changed per-entry statement" "$fixture"

fixture="$tmp_dir/root-postgres"
make_fixture "$fixture"
sed -i '0,/user: "999:999"/{s/user: "999:999"/user: "0:0"/;}' "$fixture/deploy/compose/docker-compose.prod.yml"
expect_failure "reachable root postgres entrypoint" "$fixture"

fixture="$tmp_dir/tls-ingress"
make_fixture "$fixture"
sed -i '0,/listen 127.0.0.1:30193;/{s/listen 127.0.0.1:30193;/listen 127.0.0.1:30193 ssl;/;}' "$fixture/deploy/nginx/admin-dashboard-ingress.conf.template"
expect_failure "reachable nginx TLS listener" "$fixture"

echo "exact Trivy exception tuple mutation tests passed"
