#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="${root}/scripts/perf/check-youtube-plane-budget.sh"
fixture="$(mktemp -d)"
trap 'rm -rf -- "${fixture}"' EXIT
mkdir -p "${fixture}/bin"

cat >"${fixture}/bin/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "${TESTCONTAINERS_SESSION_ID}" == youtube-budget-* && "${TESTCONTAINERS_SESSION_ID}" != outer-session ]]
printf '%s' "${TESTCONTAINERS_SESSION_ID}" >"${CASE_DIR}/session"
if [[ "${CASE_MODE}" != missing-endpoint && "${CASE_MODE}" != external ]]; then
  echo '  Resolved Docker Host: unix:///fixture-engine'
fi
if [[ "${CASE_MODE}" == ambiguous-endpoint ]]; then
  echo '  Resolved Docker Host: unix:///another-engine'
fi
echo 'BenchmarkPublishConsumeCommunityObservation-2 20 100 ns/op 100 B/op 1 allocs/op'
[[ "${CASE_MODE}" != benchmark-failure ]] || exit 7
STUB

cat >"${fixture}/bin/docker" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "${CASE_MODE}" != external ]]
[[ "$*" == "--host unix:///fixture-engine ps --all --quiet --filter label=org.testcontainers.sessionId=$(cat "${CASE_DIR}/session")" ]]
count=0
[[ ! -f "${CASE_DIR}/queries" ]] || count="$(cat "${CASE_DIR}/queries")"
count=$((count + 1))
printf '%s' "${count}" >"${CASE_DIR}/queries"
if [[ "${CASE_MODE}" == query-failure ]]; then
  echo 'fixture Docker query unavailable' >&2
  exit 9
fi
# 첫 조회에는 소유 컨테이너가 남아 있고, 두 번째 조회에서 회수가 끝납니다.
if (( count == 1 )); then
  printf '%064d\n' 1
fi
STUB

cat >"${fixture}/bin/timeout" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == --kill-after=2s && "$2" == 30s && "$3" == bash ]]
[[ "${CASE_MODE}" != timeout ]] || exit 124
exec /usr/bin/timeout "$@"
STUB
chmod +x "${fixture}/bin/"*

run_case() {
  local mode="$1" expected="$2" status=0 external=""
  local directory="${fixture}/${mode}"
  mkdir -p "${directory}"
  [[ "${mode}" != external ]] || external='postgres://fixture.invalid/disposable'
  PATH="${fixture}/bin:${PATH}" CASE_DIR="${directory}" CASE_MODE="${mode}" \
    DOCKER_CONTEXT=unrelated-context TESTCONTAINERS_SESSION_ID=outer-session \
    TEST_DATABASE_URL="${external}" TMPDIR="${directory}" \
    bash "${checker}" >"${directory}/out" 2>&1 || status=$?
  if [[ "${status}" != "${expected}" ]]; then
    cat "${directory}/out" >&2
    echo "${mode}: status=${status}, expected=${expected}" >&2
    exit 1
  fi
  if (( expected == 0 )); then
    [[ "$(cat "${directory}/out")" == *'Projected fleet cycle:'* ]]
  else
    [[ "$(cat "${directory}/out")" != *'Projected fleet cycle:'* ]]
  fi
  if [[ "${mode}" == success || "${mode}" == benchmark-failure ]]; then
    [[ "$(cat "${directory}/queries")" == 2 ]]
  fi
  if [[ "${mode}" == external || "${mode}" == missing-endpoint || "${mode}" == ambiguous-endpoint ]]; then
    [[ ! -e "${directory}/queries" ]]
  fi
  [[ -z "$(find "${directory}" -mindepth 1 -maxdepth 1 -type d -name 'tmp.*' -print)" ]]
  echo "ok: ${mode}"
}

run_case success 0
run_case benchmark-failure 7
run_case query-failure 1
run_case timeout 1
run_case missing-endpoint 1
run_case ambiguous-endpoint 1
run_case external 0
