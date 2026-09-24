#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scanner="${SCRIPT_DIR}/check-deprecated-deadline.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT

mkdir -p "${fixture}/internal" "${fixture}/cmd"
"${scanner}" 2026-09-24 > "${fixture}/owners.log"
for owner in hololive-shared/pkg hololive-api/internal hololive-api/cmd hololive-youtube-collector/internal hololive-youtube-collector/cmd hololive-alarm-worker/internal hololive-alarm-worker/cmd; do
    rg -Fq "/hololive/${owner}" "${fixture}/owners.log"
done

printf '// TODO(2026-09-25): fixture\n' > "${fixture}/internal/future.go"
printf '// remove_after="2026-09-23"\n' > "${fixture}/cmd/overdue.go"

if "${scanner}" 2026-09-24 "${fixture}/internal" "${fixture}/cmd" > "${fixture}/overdue.log" 2>&1; then
    echo "[FAIL] overdue marker in cmd was accepted" >&2
    exit 1
fi
rg -q 'overdue.go' "${fixture}/overdue.log"

rm "${fixture}/cmd/overdue.go"
printf '// TODO(2026-13-01): fixture\n' > "${fixture}/cmd/malformed.go"
if "${scanner}" 2026-09-24 "${fixture}/internal" "${fixture}/cmd" > "${fixture}/malformed.log" 2>&1; then
    echo "[FAIL] malformed marker in cmd was accepted" >&2
    exit 1
fi
rg -q 'malformed.go' "${fixture}/malformed.log"

rm "${fixture}/cmd/malformed.go"
"${scanner}" 2026-09-24 "${fixture}/internal" "${fixture}/cmd" > "${fixture}/future.log"
rg -q 'pending_markers=1' "${fixture}/future.log"

if "${scanner}" 2026-09-24 "${fixture}/missing" > "${fixture}/missing.log" 2>&1; then
    echo "[FAIL] missing scan root was accepted" >&2
    exit 1
fi

echo "[PASS] deprecated deadline fixture coverage"
