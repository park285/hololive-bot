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

output="$(
  cd "${ROOT_DIR}"
  go test -run '^$' \
    -bench '^BenchmarkPublishConsumeCommunityObservation$' \
    -benchtime="${iterations}x" \
    -count=1 \
    -benchmem \
    ./hololive/hololive-shared/pkg/service/youtube/sourceobservation
)"
printf '%s\n' "${output}"

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
