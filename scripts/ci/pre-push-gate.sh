#!/usr/bin/env bash
# pre-push-gate: push 전 필수 품질 게이트.
# ~/.git-hooks/pre-push 에서 위임 호출됨.
# 이전 GitHub Actions CI (Verify, Architecture Gates, Dependency Hygiene,
# Frontend Quality) 를 로컬 게이트로 대체.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init
release_checked=false
route_resolved=false
route_exact=false
cd "${ROOT_DIR}"

# 필요한 보안 patch toolchain을 확보하되, go.mod/go.work 정본은 local-ci의
# ensure_go_mod_toolchains가 관리한다.
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.1+auto}"

# hook이 주입한 GIT_DIR 등이 남으면 linked worktree나 tmp 레포 대상 git 호출이
# 본 레포를 조작하므로 게이트 진입 시 일괄 해제한다.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX

# 게이트(특히 NilAway)가 호스트 램을 전역 고갈시킨 2026-07-04 OOM의 재발 방지.
if (( $# > 0 )); then
  echo "usage: $0" >&2
  exit 2
fi

if [[ -z "${PRE_PUSH_GATE_SCOPED:-}" ]] \
  && command -v systemd-run >/dev/null 2>&1 \
  && systemd-run --user --scope --quiet -p MemoryHigh=1G true >/dev/null 2>&1; then
  echo "[pre-push] memory scope: MemoryHigh=${PRE_PUSH_MEMORY_HIGH:-24G} MemoryMax=${PRE_PUSH_MEMORY_MAX:-32G}"
  export PRE_PUSH_GATE_SCOPED=1
  # 부모 cgroup을 벗어나는 scope에도 종료 상한을 두어 기동 fixture를 회수한다.
  exec systemd-run --user --scope --quiet \
    -p RuntimeMaxSec=2h \
    -p KillMode=control-group \
    -p "MemoryHigh=${PRE_PUSH_MEMORY_HIGH:-24G}" \
    -p "MemoryMax=${PRE_PUSH_MEMORY_MAX:-32G}" \
    "${SCRIPT_DIR}/pre-push-gate.sh"
fi

resolve_route() {
  if [[ "${route_resolved}" == "true" ]]; then
    return 0
  fi

  # 정확한 push 범위(BASE_SHA..HEAD_SHA)가 있을 때만 docs-only skip과 테스트 생략을 연다.
  # 범위가 없으면(새 branch·삭제·수동 실행) origin/main 기준 변경 집합은 라우팅에만 쓴다.
  if [[ -n "${BASE_SHA:-}" && -n "${HEAD_SHA:-}" ]]; then
    route_range="${BASE_SHA}..${HEAD_SHA}"
    if ! changed_files="$(git diff --name-only "${route_range}")"; then
      echo "failed to resolve exact pushed range ${route_range}" >&2
      return 1
    fi
    route_exact=true
  elif git rev-parse --verify origin/main >/dev/null 2>&1; then
    route_range="origin/main...HEAD"
    changed_files="$(git diff --name-only "${route_range}" 2>/dev/null || true)"
  else
    route_range="HEAD~1..HEAD"
    changed_files="$(git diff --name-only "${route_range}" 2>/dev/null || true)"
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
      ;;
    full)
      local_ci_go_scope="all"
      race_default="true"
      ;;
    *)
      echo "unsupported PRE_PUSH_MODE=${PRE_PUSH_MODE}; expected fast or full" >&2
      return 1
      ;;
  esac

  resolved_local_ci_go_scope="${LOCAL_CI_GO_SCOPE:-${local_ci_go_scope}}"
  route_resolved=true
}

is_docs_only_route() {
  local non_doc_changes docs_code_changes
  resolve_route
  [[ "${route_exact}" == "true" ]] || return 1
  # grep -qv 회피: ugrep 는 quiet+invert 조합 exit 코드가 GNU grep 과 달라 필터 결과로 판정한다.
  non_doc_changes="$(grep -vE '^(docs/|.*\.md$)' <<<"${changed_files}" || true)"
  docs_code_changes="$(grep -E '^docs/.*\.(go|sh|sql)$' <<<"${changed_files}" || true)"
  [[ "${PRE_PUSH_MODE}" != "full" ]] && [[ -n "${changed_files}" ]] \
    && [[ -z "${non_doc_changes}" ]] && [[ -z "${docs_code_changes}" ]]
}

inputs_changed() {
  local input file
  for input in "$@"; do
    while IFS= read -r file; do
      [[ -n "${file}" ]] || continue
      case "${file}" in
        "${input}"|"${input}"/*) return 0 ;;
      esac
    done <<<"${changed_files}"
  done
  return 1
}

# 배포·운영 스크립트 테스트는 테스트 자신이나 대상 스크립트가 push 범위에서 바뀔 때만 실행한다.
# 범위를 못 얻었거나 PRE_PUSH_MODE=full 이면 전량 실행한다. 선언된 입력이 저장소에 없으면
# 경고 뒤 실행한다.
run_if_changed() {
  local test="$1" input inputs_valid=true
  shift
  resolve_route
  for input in "${test}" "$@"; do
    if [[ ! -e "${input}" ]]; then
      echo "[pre-push] test input missing: ${input} (declared for ${test})" >&2
      inputs_valid=false
    fi
  done
  if [[ "${PRE_PUSH_MODE}" != "full" && "${route_exact}" == "true" && "${inputs_valid}" == "true" ]] \
    && ! inputs_changed "${test}" "$@"; then
    echo "[pre-push] test skipped (inputs unchanged): ${test}"
    return 0
  fi
  echo "[pre-push] test: ${test}"
  case "${test}" in
    *.py) "${CI_PYTHON_BIN}" "${test}" ;;
    *) bash "${test}" ;;
  esac
}

run_release_check() {
  if [[ "${release_checked}" == "false" ]]; then
    bash scripts/check-release-version.sh
    release_checked=true
  fi
}

run_content_gates() {
  run_release_check
  resolve_route
  if is_docs_only_route; then
    echo "[pre-push] docs-only change detected; skipping Go gate"
    return 0
  fi

  run_if_changed scripts/deploy/admin-bind_test.sh scripts/deploy/lib/admin-bind.sh scripts/deploy/compose.sh \
    scripts/deploy/compose-redeploy-service.sh build-all.sh scripts/systemd/admin-dashboard-ingress.nft
  run_if_changed scripts/deploy/admin-network_test.sh deploy/compose/docker-compose.prod.yml deploy/compose/docker-compose.live-compat.yml
  run_if_changed scripts/ci/check-recurring-security-scan-contract.sh \
    scripts/ci/run-final-image-scan.sh scripts/ci/final-image-scan-policy.sh scripts/ci/final-image-scan-manifest.txt \
    scripts/ci/disabled-bake-attestations.jq scripts/ci/npm-audit-manifest.txt scripts/ci/go-tooling.sh \
    .github/workflows/security.yml deploy/compose
  echo "[pre-push] deploy/runtime script tests (run when the test or its target changed)"
  run_if_changed scripts/build/build-youtube-collector-go_test.sh \
    scripts/build/build-youtube-collector-go.sh scripts/build/check-youtube-collector-go-artifact.sh \
    scripts/ci/public-pr-go-gate.sh scripts/ci/python-runtime.sh hololive/hololive-youtube-collector/Makefile
  run_if_changed scripts/build/image-runtime-tree-permissions_test.sh \
    hololive/hololive-youtube-collector/Dockerfile.po-sandbox hololive/hololive-youtube-collector/Dockerfile \
    hololive/hololive-alarm-worker/Dockerfile
  run_if_changed scripts/deploy/lib/ap-prechange-config_test.sh \
    scripts/deploy/lib/ap-prechange-config.sh scripts/deploy/ap-deploy.sh scripts/deploy/ap-rollback.sh
  run_if_changed scripts/deploy/test-postgres-capacity-entrypoints.sh \
    scripts/ci/check-postgres-capacity.sh scripts/deploy/compose-redeploy-service.sh \
    scripts/deploy/compose.sh scripts/deploy/lib/postgres-capacity.sh deploy/compose/docker-compose.prod.yml
  run_if_changed hololive/hololive-api/scripts/migrations/preflight-durable-runtime-rollback_test.sh \
    hololive/hololive-api/scripts/migrations/preflight-durable-runtime-rollback.sh
  run_if_changed scripts/logs/daily-rollup-logs_test.sh scripts/logs/daily-rollup-logs.sh
  run_if_changed scripts/logs/test-stream-mirror-retention.sh scripts/logs/lib/stream.sh
  run_if_changed scripts/deploy/verify-exec-tree-ownership_test.sh scripts/deploy/verify-exec-tree-ownership.sh
  run_if_changed scripts/deploy/systemd-compose-up_test.sh \
    scripts/deploy/systemd-compose-up.sh scripts/deploy/systemd-compose-down.sh scripts/deploy/compose.sh \
    scripts/deploy/sync-opt-current.sh scripts/systemd deploy/compose/docker-compose.live-compat.yml \
    deploy/compose/docker-compose.youtube-collector-disabled.yml
  run_if_changed scripts/deploy/lib/public-bind-mounts_test.sh scripts/deploy/lib/public-bind-mounts.sh deploy/nginx
  run_if_changed scripts/deploy/x-spaces-overlay_test.sh \
    scripts/deploy/compose.sh scripts/deploy/lib deploy/compose/docker-compose.x-spaces.yml
  run_if_changed scripts/runtime/set-iris-base-url_test.sh scripts/runtime/set-iris-base-url.sh
  run_if_changed scripts/runtime/pg-hotpath-explain-snapshot_test.sh \
    scripts/runtime/pg-hotpath-explain-snapshot.sh scripts/runtime/lib \
    hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/queries \
    hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/queries
  run_if_changed scripts/deploy/ap-host-native-deploy_test.sh \
    scripts/deploy/ap-host-native-deploy.sh scripts/deploy/ap-host-native-rollback.sh \
    scripts/deploy/ap-host-native-deploy_contract_checks.inc.sh scripts/deploy/ap-completion-check.sh \
    scripts/deploy/lib scripts/logs/ap-host-native-status.sh
  run_if_changed scripts/deploy/ap-completion-check_test.sh \
    scripts/deploy/ap-completion-check.sh scripts/deploy/ap-hosts scripts/deploy/lib deploy/compose

  # Go 시험의 Ryuk 회수는 프로세스 종료 뒤에도 interface를 제거합니다. Chromium이
  # ERR_NETWORK_CHANGED로 중단됐으므로 독립적인 브라우저 검사를 그보다 먼저 완료합니다.
  if echo "$changed_files" | grep -qE '^(admin-dashboard/|deploy/compose/|scripts/ci/public-pr-frontend-gate.sh)'; then
    bash scripts/ci/public-pr-frontend-gate.sh
  fi

  echo "[pre-push] mode=${PRE_PUSH_MODE} local_ci_go_scope=${resolved_local_ci_go_scope}"
  LOCAL_CI_GO_SCOPE="${resolved_local_ci_go_scope}" \
  BASE_REF="${BASE_SHA:-origin/main}" \
  STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}" \
  RUN_NILAWAY="${RUN_NILAWAY:-true}" \
  RUN_RACE_TESTS="${RUN_RACE_TESTS:-${race_default}}" \
    ./scripts/ci/local-ci.sh

  if [[ "${PRE_PUSH_MODE}" == "full" ]] || echo "$changed_files" | grep -q '^hololive/hololive-youtube-collector/'; then
    echo "[pre-push] youtube-collector YouTube.js helper 품질 게이트"
    bash scripts/ci/public-pr-collector-helper-gate.sh
  fi
}

# 시간이 지나면 부패하는 advisory 데이터. 주기 security workflow 는 merge 전 검증을
# 대신하지 않으므로 docs-only push 만 면제한다(DEC-20260711 offline fail-closed 유지).
run_dependency_hygiene() {
  local module govulncheck_bin
  resolve_route
  if is_docs_only_route; then
    echo "[pre-push] docs-only change detected; skipping dependency hygiene"
    return 0
  fi

  # shellcheck source=go-tooling.sh
  source "${SCRIPT_DIR}/go-tooling.sh"
  # shellcheck source=go-workspace-modules.sh
  source "${SCRIPT_DIR}/go-workspace-modules.sh"
  govulncheck_bin="$(ensure_govulncheck)"
  for module in . "${GO_WORKSPACE_MODULES[@]}"; do
    echo "[pre-push] dependency hygiene: ${module}"
    (cd "${module}" && GOWORK=off go list -m -u -mod=readonly all >/dev/null && GOWORK=off "${govulncheck_bin}" ./...)
  done
}

echo "════════════════════════════════════════"
echo "  pre-push quality gate"
echo "════════════════════════════════════════"
run_content_gates
run_dependency_hygiene
echo "════════════════════════════════════════"
echo "  pre-push quality gate passed"
echo "════════════════════════════════════════"
