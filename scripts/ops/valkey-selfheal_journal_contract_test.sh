#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d /tmp/valkey-selfheal-journal-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT

MODE=--apply
NOW=100
JOURNAL_MAX_BYTES=32768
JOURNAL_FIELD_MAX_CHARS=2048
JOURNAL="${TMP_DIR}/journal.jsonl"
. "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-journal.sh"

fail() { echo "[FAIL] $*" >&2; exit 1; }

write_guard() {
  printf '%s %s\n' "$(stat -c '%s' "${JOURNAL}")" "$(sha256sum "${JOURNAL}" | awk '{print $1}')" >"${JOURNAL_GUARD}"
}

assert_bounded() {
  local path
  for path in "${JOURNAL}" "${JOURNAL}.previous"; do
    [ ! -e "${path}" ] || [ "$(stat -c '%s' "${path}")" -le "${JOURNAL_MAX_BYTES}" ] || fail "${path} exceeds the journal bound"
  done
}

oversized_active_is_not_rotated() {
  printf '%40000s' x >"${JOURNAL}"
  journal fixture || true
  assert_bounded
  [ ! -e "${JOURNAL}.previous" ] || fail "oversized active journal was published as previous"
  jq -e '.event == "journal_rejected" and .detail.reason == "active_oversize"' "${JOURNAL}" >/dev/null || fail "oversized active journal did not converge to a bounded marker"
}

corrupt_active_preserves_previous() {
  local previous_hash
  printf '%s\n' '{"event":"previous"}' >"${JOURNAL}.previous"
  previous_hash="$(sha256sum "${JOURNAL}.previous")"
  printf '%s\n' '{"event":"corrupt"}' >"${JOURNAL}"
  printf '1 %064d\n' 0 >"${JOURNAL_GUARD}"
  journal fixture || true
  assert_bounded
  [ "$(sha256sum "${JOURNAL}.previous")" = "${previous_hash}" ] || fail "corrupt active journal replaced previous evidence"
  jq -e '.event == "journal_rejected" and .detail.reason == "active_size_mismatch"' "${JOURNAL}" >/dev/null || fail "corrupt active journal did not fail closed"
}

oversized_previous_is_bounded() {
  printf '%s\n' '{"event":"active"}' >"${JOURNAL}"
  write_guard
  printf '%40000s' y >"${JOURNAL}.previous"
  journal fixture || true
  assert_bounded
  jq -e '.event == "journal_rejected" and .detail.reason == "previous_oversize"' "${JOURNAL}.previous" >/dev/null || fail "oversized previous journal did not converge to a bounded marker"
}

rotation_keeps_both_files_bounded() {
  {
    printf '{"event":"fixture","padding":"'
    printf '%32600s' ''
    printf '"}\n'
  } >"${JOURNAL}"
  write_guard
  journal fixture
  assert_bounded
  jq -e . "${JOURNAL}" "${JOURNAL}.previous" >/dev/null || fail "rotated journal is not JSONL"
}

foreign_temp_is_finite_and_unchanged() {
  local target before iteration count
  JOURNAL="${TMP_DIR}/foreign-journal.jsonl"
  JOURNAL_GUARD="${JOURNAL}.guard"
  target="${TMP_DIR}/foreign-target"
  printf 'foreign-owned\n' >"${target}"
  before="$(sha256sum "${target}")"
  ln -s "${target}" "${JOURNAL}.tmp"
  for ((iteration = 1; iteration <= 100; iteration++)); do journal fixture || true; done
  count="$(find "${TMP_DIR}" -maxdepth 1 -name 'foreign-journal.jsonl*.tmp' -o -name 'foreign-journal.jsonl*.tmp.*' | wc -l)"
  [ "${count}" -le 3 ] || fail "foreign journal temp exceeded the finite slot cap"
  [ "$(sha256sum "${target}")" = "${before}" ] || fail "foreign journal temp target changed"
}

signal_and_rename_failures_leave_finite_slots() {
  local fakebin="${TMP_DIR}/failure-bin" renamebin="${TMP_DIR}/rename-bin" iteration count killed_pid wait_step
  mkdir -p "${fakebin}"
  cat >"${fakebin}/sync" <<'EOF'
#!/usr/bin/env bash
: >"${FAKE_KILL_ENTER:?}"
while kill -0 "${PPID}" 2>/dev/null; do sleep 0.01; done
EOF
  chmod +x "${fakebin}/sync"
  JOURNAL="${TMP_DIR}/killed-journal.jsonl"
  JOURNAL_GUARD="${JOURNAL}.guard"
  PATH="${fakebin}:${PATH}" FAKE_KILL_ENTER="${TMP_DIR}/sync.enter" \
    bash -c 'set -uo pipefail; MODE=--apply; NOW=100; JOURNAL_MAX_BYTES=32768; JOURNAL_FIELD_MAX_CHARS=2048; JOURNAL="$1"; . "$2"; journal fixture' \
      journal-kill "${JOURNAL}" "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-journal.sh" >/dev/null 2>&1 &
  killed_pid="$!"
  for ((wait_step = 1; wait_step <= 500; wait_step++)); do [ ! -e "${TMP_DIR}/sync.enter" ] || break; sleep 0.01; done
  [ -e "${TMP_DIR}/sync.enter" ] || fail "SIGKILL fixture did not reach the synced temp slot"
  kill -KILL "${killed_pid}"
  wait "${killed_pid}" 2>/dev/null || true
  for ((iteration = 1; iteration <= 100; iteration++)); do journal fixture || true; done
  count="$(find "${TMP_DIR}" -maxdepth 1 -name 'killed-journal.jsonl*.tmp' -o -name 'killed-journal.jsonl*.tmp.*' | wc -l)"
  [ "${count}" -le 3 ] && [ -e "${JOURNAL}.tmp" ] || fail "SIGKILL did not remain within deterministic temp slots"

  mkdir -p "${renamebin}"
  cat >"${renamebin}/mv" <<'EOF'
#!/usr/bin/env bash
exit 72
EOF
  chmod +x "${renamebin}/mv"
  JOURNAL="${TMP_DIR}/rename-journal.jsonl"
  JOURNAL_GUARD="${JOURNAL}.guard"
  PATH="${renamebin}:${PATH}" journal fixture || true
  for ((iteration = 1; iteration <= 100; iteration++)); do journal fixture || true; done
  count="$(find "${TMP_DIR}" -maxdepth 1 -name 'rename-journal.jsonl*.tmp' -o -name 'rename-journal.jsonl*.tmp.*' | wc -l)"
  [ "${count}" -le 3 ] || fail "rename failures exceeded deterministic temp slots"
}

oversized_active_is_not_rotated
corrupt_active_preserves_previous
oversized_previous_is_bounded
rotation_keeps_both_files_bounded
foreign_temp_is_finite_and_unchanged
signal_and_rename_failures_leave_finite_slots
echo "ok: valkey self-heal journal contract tests passed"
