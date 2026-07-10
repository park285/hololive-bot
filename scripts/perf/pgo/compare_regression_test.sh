#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
COMPARE="${SCRIPT_DIR}/compare-pgo.sh"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

cat >"${TMP_ROOT}/perf-budget.yaml" <<'EOF'
schemaVersion: 1
repo: fixture
benchmarks:
  BenchmarkImproves: { package: ./hololive/hololive-api/internal/planes/bot, class: critical, gate: pr }
  BenchmarkRegresses: { package: ./hololive/hololive-api/internal/planes/bot, class: critical, gate: pr }
settings:
  min_count: 2
EOF

cat >"${TMP_ROOT}/build.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${PGO_BUILD_MODE}" == "off" ]]; then
  head -c 100000 /dev/zero >"${PGO_BUILD_OUTPUT}"
else
  head -c 101000 /dev/zero >"${PGO_BUILD_OUTPUT}"
fi
EOF

cat >"${TMP_ROOT}/bench.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
off=1000
if [[ "${PGO_BENCH_NAME}" == "BenchmarkImproves" ]]; then
  on=900
else
  on=1200
fi
value="${off}"
[[ "${PGO_BENCH_MODE}" == "on" ]] && value="${on}"
printf '%s-12\t1000\t%s ns/op\n' "${PGO_BENCH_NAME}" "${value}" >"${PGO_BENCH_OUTPUT}"
EOF

cat >"${TMP_ROOT}/live.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat >"${PGO_LIVE_OUTPUT}" <<'JSON'
{"cpuPercentDelta": -5, "p95LatencyDelta": -2, "p99LatencyDelta": 0, "rssDelta": 1}
JSON
EOF
chmod +x "${TMP_ROOT}/build.sh" "${TMP_ROOT}/bench.sh" "${TMP_ROOT}/live.sh"

set +e
output="$(
  cd "${REPO_ROOT}" &&
  PGO_PERF_BUDGET="${TMP_ROOT}/perf-budget.yaml" \
    bash "${COMPARE}" \
      --service hololive-api \
      --main ./hololive/hololive-api/cmd/hololive-api \
      --profile "${TMP_ROOT}/candidate.pgo" \
      --workload fixture \
      --output-dir "${TMP_ROOT}/out" \
      --build-cmd "${TMP_ROOT}/build.sh" \
      --bench-cmd "${TMP_ROOT}/bench.sh" \
      --live-cmd "${TMP_ROOT}/live.sh" 2>&1
)"
status=$?
set -e

if [[ "${status}" -ne 2 || "${output}" != *"hot benchmark regression > 3%"* ]]; then
  printf 'not ok - mixed hot benchmark regression must reject\nstatus=%s\n%s\n' "${status}" "${output}" >&2
  exit 1
fi

worst="$(python3 - "${TMP_ROOT}/out/comparison.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1]))["hotBenchmarkPercentDelta"])
PY
)"
python3 -c "import sys; sys.exit(0 if abs(float(sys.argv[1]) + 20.0) < 1e-6 else 1)" "${worst}"
echo "ok - mixed hot benchmark regression is rejected using worst-case delta"
