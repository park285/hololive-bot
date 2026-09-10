#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUDGET_FILE="${ROOT_DIR}/scripts/perf/perf-budget.yaml"

read_budget() {
  local key="$1"
  awk -v key="${key}:" '$1 == key { print $2 }' "${BUDGET_FILE}"
}

iterations="$(read_budget iterations)"
collector_members="$(read_budget modeled_collector_members)"
observation_kinds="$(read_budget modeled_observation_kinds)"
max_projected_fleet_cycle_ns="$(read_budget max_projected_fleet_cycle_ns)"
[[ "${iterations}" =~ ^[1-9][0-9]*$ ]]
[[ "${collector_members}" =~ ^[1-9][0-9]*$ ]]
[[ "${observation_kinds}" =~ ^[1-9][0-9]*$ ]]
[[ "${max_projected_fleet_cycle_ns}" =~ ^[1-9][0-9]*$ ]]
modeled_observations=$((collector_members * observation_kinds))

# 이 go test 호출의 PostgreSQL/Ryuk만 추적합니다. 다른 시험과 세션을 공유하지 않습니다.
session_dir="$(mktemp -d)"
session_id="youtube-budget-${session_dir##*/}"
trap 'rmdir -- "${session_dir}"' EXIT
benchmark_status=0
output="$(
  cd "${ROOT_DIR}"
  TESTCONTAINERS_SESSION_ID="${session_id}" go test -run '^$' \
    -bench '^BenchmarkPublishConsumeCommunityObservation$' \
    -benchtime="${iterations}x" \
    -count=1 \
    -benchmem \
    ./hololive/hololive-shared/pkg/service/youtube/sourceobservation 2>&1
)" || benchmark_status=$?
printf '%s\n' "${output}"

# Ryuk는 프로세스 종료 뒤 비동기로 회수합니다. 남은 interface 제거가 다음 Chromium
# 검사를 ERR_NETWORK_CHANGED로 중단했으므로 소유 세션의 회수까지 완료 경계에 포함합니다.
# 외부 disposable DB 모드는 dbtest의 기존 소유권 검증을 사용하며 컨테이너를 만들지 않습니다.
if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
  docker_host="$(sed -nE 's/^[[:space:]]*Resolved Docker Host: (.+)$/\1/p' <<<"${output}" | sort -u)"
  if [[ -z "${docker_host}" || "${docker_host}" == *$'\n'* ]]; then
    echo 'cannot verify benchmark container cleanup: Docker endpoint is missing or ambiguous' >&2
    (( benchmark_status == 0 )) || exit "${benchmark_status}"
    exit 1
  fi
  # SDK가 실제 사용한 endpoint와 고유 label로만 조회합니다. CLI context나 다른 세션은 소비하지 않습니다.
  if ! timeout --kill-after=2s 30s bash -c '
    set -euo pipefail
    while :; do
      remaining="$(docker --host "$1" ps --all --quiet --filter "label=org.testcontainers.sessionId=$2")"
      [[ -n "${remaining}" ]] || exit 0
      sleep 0.2
    done
  ' benchmark-cleanup "${docker_host}" "${session_id}"; then
    echo "benchmark container cleanup failed or exceeded 30 seconds: session=${session_id}" >&2
    exit 1
  fi
  echo "Benchmark container cleanup complete: session=${session_id}"
fi
(( benchmark_status == 0 )) || exit "${benchmark_status}"

measured_ns_per_op="$(
  awk '/^BenchmarkPublishConsumeCommunityObservation-/ {
    for (i = 1; i <= NF; i++) {
      if ($i == "ns/op") {
        print $(i - 1)
        exit
      }
    }
  }' <<<"${output}"
)"
[[ "${measured_ns_per_op}" =~ ^[0-9]+([.][0-9]+)?$ ]]

awk \
  -v measured="${measured_ns_per_op}" \
  -v observations="${modeled_observations}" \
  -v budget="${max_projected_fleet_cycle_ns}" \
  'BEGIN {
  projected = measured * observations
  if (projected > budget) {
    printf "YouTube plane performance budget exceeded: %.0f projected ns/cycle > %d ns/cycle\n", projected, budget > "/dev/stderr"
    exit 1
  }
  printf "Projected fleet cycle: %.0f ns (%d observations), budget: %d ns\n", projected, observations, budget
}'
