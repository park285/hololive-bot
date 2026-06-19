#!/usr/bin/env bash
# pre-push-gate: push 전 필수 품질 게이트.
# ~/.git-hooks/pre-push 에서 위임 호출됨.
# 이전 GitHub Actions CI (Verify, Architecture Gates, Dependency Hygiene,
# Frontend Quality) 를 로컬 게이트로 대체.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

# hook이 주입한 GIT_DIR 등이 남으면 linked worktree나 tmp 레포 대상 git 호출이
# 본 레포를 조작하므로 게이트 진입 시 일괄 해제한다.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX

echo "════════════════════════════════════════"
echo "  pre-push quality gate"
echo "════════════════════════════════════════"

if git rev-parse --verify origin/main >/dev/null 2>&1; then
  changed_files="$(git diff --name-only origin/main...HEAD 2>/dev/null || true)"
else
  changed_files="$(git diff --name-only HEAD~1..HEAD 2>/dev/null || true)"
fi

if [[ "${FULL_PRE_PUSH:-false}" == "true" ]]; then
  PRE_PUSH_MODE="${PRE_PUSH_MODE:-full}"
else
  PRE_PUSH_MODE="${PRE_PUSH_MODE:-fast}"
fi

case "${PRE_PUSH_MODE}" in
  fast)
    local_ci_go_scope="changed"
    race_default="true"
    dependency_hygiene_default="false"
    ;;
  full)
    local_ci_go_scope="all"
    race_default="true"
    dependency_hygiene_default="true"
    ;;
  *)
    echo "unsupported PRE_PUSH_MODE=${PRE_PUSH_MODE}; expected fast or full" >&2
    exit 1
    ;;
esac

# security.yml 을 dispatch-only 로 내리면서 주기 보안 스캔이 사라져, push 시점 govulncheck 가
# 유일한 의존성 취약점 방어선이 됐다. fast push 라도 코드 변경이 섞이면 강제하고 docs 전용
# push 만 면제한다. offline push 는 RUN_DEPENDENCY_HYGIENE=false 로 우회한다.
# grep -qv 회피: ugrep 는 quiet+invert 조합 exit 코드가 GNU grep 과 달라 필터 결과로 판정한다.
non_doc_changes="$(grep -vE '^docs/|\.md$' <<<"${changed_files}" || true)"
if [[ "${dependency_hygiene_default}" == "false" && -n "${non_doc_changes}" ]]; then
  dependency_hygiene_default="true"
fi

run_perf_budget_gate() {
  local collect_args=(--policy perf-budget.yaml --candidate artifacts/perf/pr --gate pr)
  if [[ -n "${PERF_GATE_COUNT:-}" ]]; then
    collect_args+=(--count "${PERF_GATE_COUNT}")
  fi
  collect_args+=(--benchtime "${PERF_GATE_BENCHTIME:-100ms}")

  echo "[pre-push] perf budget gate (warn)"
  ./scripts/perf/check-bench-regression.sh collect "${collect_args[@]}"
  ./scripts/perf/check-bench-regression.sh \
    --policy perf-budget.yaml \
    --baseline artifacts/perf/baseline/main \
    --candidate artifacts/perf/pr \
    --gate pr
}

if [[ "${PERF_GATE_ONLY:-false}" == "true" ]]; then
  run_perf_budget_gate
  exit 0
fi

# 구 ci.yml secret-free-gate 의 로컬 미커버 항목을 이전(gofmt 는 local-ci.sh 가 이미 커버).
echo "[pre-push] workflow boundary / gate ownership"
bash scripts/ci/check-workflow-secrets.sh
bash scripts/ci/check-workflow-secrets_test.sh
echo "[pre-push] shell syntax sweep"
while IFS= read -r script; do
  bash -n "${script}"
done < <(find scripts -type f -name '*.sh' | sort)

admin_touch_guardrail="${RUN_ADMIN_TOUCH_GUARDRAIL:-true}"
if echo "$changed_files" | grep -q '^admin-dashboard/' && [[ -z "${RUN_ADMIN_TOUCH_GUARDRAIL+x}" ]]; then
  admin_touch_guardrail=false
fi

resolved_local_ci_go_scope="${LOCAL_CI_GO_SCOPE:-${local_ci_go_scope}}"
echo "[pre-push] mode=${PRE_PUSH_MODE} local_ci_go_scope=${resolved_local_ci_go_scope}"

LOCAL_CI_GO_SCOPE="${resolved_local_ci_go_scope}" \
RUN_ADMIN_TOUCH_GUARDRAIL="${admin_touch_guardrail}" \
RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-${dependency_hygiene_default}}" \
STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}" \
RUN_NILAWAY="${RUN_NILAWAY:-true}" \
RUN_RACE_TESTS="${RUN_RACE_TESTS:-${race_default}}" \
  ./scripts/ci/local-ci.sh

run_perf_budget_gate

if echo "$changed_files" | grep -qE '^admin-dashboard/(frontend|backend)/'; then
  echo "[pre-push] admin-dashboard frontend 품질 게이트"
  (cd admin-dashboard/frontend && npm ci && npm run generate:api && npm test && npm run lint && npm run build)
fi

echo "════════════════════════════════════════"
echo "  pre-push quality gate passed"
echo "════════════════════════════════════════"
