#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

touch "${tmp}/pgpass"
chmod 600 "${tmp}/pgpass"
cat >"${tmp}/psql" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >"${FAKE_PSQL_ARGS_FILE}"
printf '%s\n' "${FAKE_PSQL_COUNTS}"
SH
chmod +x "${tmp}/psql"

run_preflight() {
  env -u PGPASSWORD PATH="${tmp}:${PATH}" PGSERVICE=test-service PGPASSFILE="${tmp}/pgpass" \
    FAKE_PSQL_ARGS_FILE="${tmp}/args" FAKE_PSQL_COUNTS="$1" \
    "${root}/preflight-durable-runtime-rollback.sh" --ingress-quiesced
}

if env -u PGPASSWORD PATH="${tmp}:${PATH}" PGSERVICE=test-service PGPASSFILE="${tmp}/pgpass" \
  FAKE_PSQL_COUNTS='0|0|0|0' "${root}/preflight-durable-runtime-rollback.sh" >"${tmp}/out" 2>&1; then
  echo "rollback preflight accepted a missing ingress-quiesced assertion" >&2
  exit 1
fi
grep -q '^usage:' "${tmp}/out"

if run_preflight '1|0|0|1' >"${tmp}/out" 2>&1; then
  echo "rollback preflight accepted an active inbox" >&2
  exit 1
fi
grep -q 'durable rollback blocked: inbox=1 commands=0 outbox=0 heads=1' "${tmp}/out"

if run_preflight '0|0|2|0' >"${tmp}/out" 2>&1; then
  echo "rollback preflight accepted an active outbox" >&2
  exit 1
fi
grep -q 'roll forward' "${tmp}/out"

run_preflight '0|0|0|0' >"${tmp}/out"
grep -q 'all durable backlogs are empty' "${tmp}/out"
[[ "$(cat "${tmp}/args")" == "-w -X -v ON_ERROR_STOP=1 -At -F |" ]]

echo "ok: durable runtime rollback preflight fails closed on ingress and backlog state"
