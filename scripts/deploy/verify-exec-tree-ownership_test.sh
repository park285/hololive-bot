#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERIFY="${ROOT_DIR}/scripts/deploy/verify-exec-tree-ownership.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

kapu_file="${TMP_DIR}/compose.sh"
printf '#!/bin/sh\n' > "${kapu_file}"
if "${VERIFY}" "${kapu_file}" >/dev/null 2>&1; then
  record_fail "non-root-owned file must be rejected (03e6dca8)"
else
  pass "non-root-owned file rejected"
fi

ww_file="${TMP_DIR}/world-writable"
printf 'x' > "${ww_file}"
chmod 0666 "${ww_file}"
if "${VERIFY}" "${ww_file}" >/dev/null 2>&1; then
  record_fail "writable file must be rejected"
else
  pass "writable file rejected"
fi

root_safe=""
for cand in /usr/bin/env /bin/true /usr/bin/true /bin/sh; do
  [[ -e "${cand}" ]] || continue
  cand_perms="$(printf '%04d' "$((10#$(stat -c '%a' "${cand}")))")"
  if [[ "$(stat -c '%u' "${cand}")" -eq 0 ]] \
     && (( ( ${cand_perms:3:1} & 2 ) == 0 )) \
     && (( ( ${cand_perms:2:1} & 2 ) == 0 )); then
    root_safe="${cand}"
    break
  fi
done
if [[ -z "${root_safe}" ]]; then
  echo "[SKIP] no root-owned reference file available for the pass case"
elif "${VERIFY}" "${root_safe}" >/dev/null 2>&1; then
  pass "root-owned non-writable file accepted (${root_safe})"
else
  record_fail "root-owned non-writable file wrongly rejected (${root_safe})"
fi

symlink_file="${TMP_DIR}/root-safe-link"
ln -s "${root_safe:-/bin/sh}" "${symlink_file}"
if "${VERIFY}" "${symlink_file}" >/dev/null 2>&1; then
  record_fail "symlinked exec path must be rejected"
else
  pass "symlinked exec path rejected"
fi

mockbin="${TMP_DIR}/mockbin"
mkdir -p "${mockbin}"
cat > "${mockbin}/stat" <<'EOF'
#!/usr/bin/env bash
format=""
path=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -c)
      format="$2"
      shift 2
      ;;
    *)
      path="$1"
      shift
      ;;
  esac
done
case "${format}" in
  %u)
    case "${path}" in
      */unsafe-parent) echo 1000 ;;
      *) echo 0 ;;
    esac
    ;;
  %g) echo 0 ;;
  %a) echo 0755 ;;
  *)
    echo "unsupported stat format: ${format}" >&2
    exit 2
    ;;
esac
EOF
chmod +x "${mockbin}/stat"
mock_root="${TMP_DIR}/unsafe-parent"
mkdir -p "${mock_root}"
mock_file="${mock_root}/compose.sh"
printf '#!/bin/sh\n' > "${mock_file}"
if PATH="${mockbin}:${PATH}" "${VERIFY}" "${mock_file}" >/dev/null 2>&1; then
  record_fail "non-root-owned parent directory must be rejected (4d57f81c)"
else
  pass "non-root-owned parent directory rejected"
fi

mock_safe_root="${TMP_DIR}/root-owned/current"
mkdir -p "${mock_safe_root}"
mock_safe_file="${mock_safe_root}/compose.sh"
printf '#!/bin/sh\n' > "${mock_safe_file}"
if PATH="${mockbin}:${PATH}" "${VERIFY}" "${mock_safe_file}" >/dev/null 2>&1; then
  pass "root-owned parent chain accepted"
else
  record_fail "root-owned parent chain should be accepted"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all exec-tree ownership checks passed (03e6dca8)"
