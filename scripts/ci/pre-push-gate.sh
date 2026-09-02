#!/usr/bin/env bash
# pre-push-gate: push 전 필수 품질 게이트.
# ~/.git-hooks/pre-push 에서 위임 호출됨.
# 이전 GitHub Actions CI (Verify, Architecture Gates, Dependency Hygiene,
# Frontend Quality) 를 로컬 게이트로 대체.
# phase 배정과 자기 테스트 실행 조건은 iris-stack docs/contracts/pre-push-gate-phases-v1.md 를 따른다.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init
PROFILE_ID="hololive-bot-v1"
release_checked=false
route_resolved=false
route_exact=false
cd "${ROOT_DIR}"

phase="${1:-}"
if (( $# > 1 )); then
  echo "usage: $0 [--phase=fingerprint|--phase=reusable|--phase=freshness|--phase=ambient]" >&2
  exit 2
fi
case "${phase}" in
  ""|--phase=fingerprint|--phase=reusable|--phase=freshness|--phase=ambient) ;;
  *)
    echo "usage: $0 [--phase=fingerprint|--phase=reusable|--phase=freshness|--phase=ambient]" >&2
    exit 2
    ;;
esac

# 필요한 보안 patch toolchain을 확보하되, go.mod/go.work 정본은 local-ci의
# ensure_go_mod_toolchains가 관리한다.
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.0+auto}"

# hook이 주입한 GIT_DIR 등이 남으면 linked worktree나 tmp 레포 대상 git 호출이
# 본 레포를 조작하므로 게이트 진입 시 일괄 해제한다.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX

# 게이트(특히 NilAway)가 호스트 램을 전역 고갈시킨 2026-07-04 OOM의 재발 방지.
if [[ "${phase}" != "--phase=fingerprint" && -z "${PRE_PUSH_GATE_SCOPED:-}" ]] \
  && command -v systemd-run >/dev/null 2>&1 \
  && systemd-run --user --scope --quiet -p MemoryHigh=1G true >/dev/null 2>&1; then
  echo "[pre-push] memory scope: MemoryHigh=${PRE_PUSH_MEMORY_HIGH:-24G} MemoryMax=${PRE_PUSH_MEMORY_MAX:-32G}"
  export PRE_PUSH_GATE_SCOPED=1
  exec systemd-run --user --scope --quiet \
    -p "MemoryHigh=${PRE_PUSH_MEMORY_HIGH:-24G}" \
    -p "MemoryMax=${PRE_PUSH_MEMORY_MAX:-32G}" \
    "${SCRIPT_DIR}/pre-push-gate.sh" "$@"
fi

resolve_route() {
  if [[ "${route_resolved}" == "true" ]]; then
    return 0
  fi

  # 정확한 push 범위(BASE_SHA..HEAD_SHA)가 있을 때만 docs-only skip과 자기 테스트 생략을 연다.
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

  admin_touch_guardrail="${RUN_ADMIN_TOUCH_GUARDRAIL:-true}"
  if echo "$changed_files" | grep -q '^admin-dashboard/' && [[ -z "${RUN_ADMIN_TOUCH_GUARDRAIL+x}" ]]; then
    admin_touch_guardrail=false
  fi
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

self_test_inputs_changed() {
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

# 자기 테스트는 테스트 자신·검사기·fixture·공용 lib 이 push 범위에서 바뀔 때만 실행한다.
# 범위를 못 얻었거나 PRE_PUSH_MODE=full 이면 전량 실행한다(fail-closed). 선언된 입력이
# 저장소에 없으면 입력 목록 산출 실패로 보고 경고 뒤 실행한다.
run_self_test() {
  local test="$1" input inputs_valid=true
  shift
  resolve_route
  for input in "${test}" "$@"; do
    if [[ ! -e "${input}" ]]; then
      echo "[pre-push] self-test input missing: ${input} (declared for ${test})" >&2
      inputs_valid=false
    fi
  done
  if [[ "${PRE_PUSH_MODE}" != "full" && "${route_exact}" == "true" && "${inputs_valid}" == "true" ]] \
    && ! self_test_inputs_changed "${test}" "$@"; then
    echo "[pre-push] self-test skipped (inputs unchanged): ${test}"
    return 0
  fi
  echo "[pre-push] self-test: ${test}"
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

hash_stream() {
  sha256sum | awk '{print $1}'
}

# go.work 의 ../ 모듈(형제 checkout)은 local-ci Go 검사 결과를 결정하므로, reusable receipt 가
# 형제 변경을 넘어 재사용되지 않도록 tree·working-tree diff·untracked 목록 해시를 identity 에 넣는다.
sibling_checkout_identity() {
  local module dir
  while IFS= read -r module; do
    [[ "${module}" == ../* ]] || continue
    dir="${ROOT_DIR}/${module}"
    if ! git -C "${dir}" rev-parse --verify --quiet 'HEAD^{tree}' >/dev/null 2>&1; then
      echo "pre-push fingerprint unavailable: sibling checkout ${module} is not a git checkout" >&2
      return 1
    fi
    printf 'sibling=%s tree=%s\n' "${module}" "$(git -C "${dir}" rev-parse --verify 'HEAD^{tree}')"
    printf 'sibling=%s diff=%s\n' "${module}" "$(git -C "${dir}" diff --no-ext-diff --binary HEAD | hash_stream)"
    printf 'sibling=%s untracked=%s\n' "${module}" "$(git -C "${dir}" ls-files --others --exclude-standard | hash_stream)"
  done < <(go work edit -json | "${CI_PYTHON_BIN}" -c '
import json, sys
for use in json.load(sys.stdin).get("Use", []):
    print(use["DiskPath"])
')
}

run_fingerprint() {
  local required head tree effective_base route_fingerprint toolchain_fingerprint
  local profile_inputs_fingerprint status_output
  local -a profile_inputs=()

  for required in git sha256sum awk sort go systemd-run; do
    if ! command -v "${required}" >/dev/null 2>&1; then
      echo "pre-push fingerprint unavailable: missing ${required}" >&2
      return 1
    fi
  done

  if ! status_output="$(git status --porcelain=v1 --untracked-files=all)"; then
    echo "pre-push fingerprint unavailable: cannot inspect repository status" >&2
    return 1
  fi
  if [[ -n "${status_output}" ]]; then
    echo "pre-push fingerprint unavailable: repository is not clean" >&2
    return 1
  fi

  head="$(git rev-parse --verify 'HEAD^{commit}')"
  tree="$(git rev-parse --verify 'HEAD^{tree}')"
  effective_base="${BASE_SHA:-}"
  if [[ -n "${effective_base}" && ! "${effective_base}" =~ ^[0-9a-f]{40}$|^[0-9a-f]{64}$ ]]; then
    echo "pre-push fingerprint unavailable: invalid BASE_SHA" >&2
    return 1
  fi
  resolve_route

  route_fingerprint="$({
    printf 'head=%s\n' "${head}"
    printf 'tree=%s\n' "${tree}"
    printf 'head_sha=%s\n' "${HEAD_SHA:-${head}}"
    printf 'base_sha=%s\n' "${effective_base}"
    printf 'range=%s\n' "${route_range}"
    printf 'mode=%s\n' "${PRE_PUSH_MODE}"
    printf 'local_ref=%s\n' "${PRE_PUSH_LOCAL_REF:-}"
    printf 'remote_ref=%s\n' "${PRE_PUSH_REMOTE_REF:-}"
    printf 'go_scope=%s\n' "${resolved_local_ci_go_scope}"
    printf 'race=%s\n' "${RUN_RACE_TESTS:-${race_default}}"
    printf 'admin_touch_guardrail=%s\n' "${admin_touch_guardrail}"
    printf '%s\n' "${changed_files}" | hash_stream
  } | hash_stream)"

  toolchain_fingerprint="$({
    go version
    go env GOVERSION GOOS GOARCH GOTOOLCHAIN
    systemd-run --version
    uv --version
    "${CI_PYTHON_BIN}" -I -S -c 'import platform; print(platform.python_version())'
    git hash-object -- .python-version scripts/ci/python-runner.sh \
      scripts/ci/python-runtime.sh scripts/ci/go-tooling.sh scripts/ci/local-ci.sh
    printf 'GOTOOLCHAIN=%s\n' "${GOTOOLCHAIN}"
    printf 'MemoryHigh=%s\n' "${PRE_PUSH_MEMORY_HIGH:-24G}"
    printf 'MemoryMax=%s\n' "${PRE_PUSH_MEMORY_MAX:-32G}"
    sibling_checkout_identity
  } | hash_stream)"

  mapfile -t profile_inputs < <(git ls-files -- \
    '.python-version' \
    '.github/workflows/*.yml' \
    '.golangci.yml' \
    'go.mod' 'go.sum' \
    'admin-dashboard/backend/go.mod' 'admin-dashboard/backend/go.sum' \
    'scripts/check-release-version.sh' \
    'scripts/architecture/check-structure-budget.py' \
    'scripts/architecture/check-structure-budget_test.py' \
    'scripts/architecture/structure-budget-policy.json' \
    'scripts/architecture/check-function-budget.py' \
    'scripts/architecture/check-function-budget.sh' \
    'scripts/architecture/check-file-loc.sh' \
    'docs/architecture/file-loc-thresholds.txt' \
    'scripts/ci/*.sh' \
    'scripts/ci/pre-push-gate-profile-v1.json' | LC_ALL=C sort -u)
  if (( ${#profile_inputs[@]} == 0 )); then
    echo "pre-push fingerprint unavailable: no profile inputs" >&2
    return 1
  fi
  profile_inputs_fingerprint="$({
    printf 'profile_id=%s\n' "${PROFILE_ID}"
    git hash-object -- "${profile_inputs[@]}"
  } | hash_stream)"

  printf '{"schema_version":1,"profile_id":"%s","effective_base_sha":"%s","route_fingerprint":"%s","toolchain_fingerprint":"%s","profile_inputs_fingerprint":"%s"}\n' \
    "${PROFILE_ID}" "${effective_base}" "${route_fingerprint}" "${toolchain_fingerprint}" "${profile_inputs_fingerprint}"
}

# reusable: 커밋 내용(과 fingerprint 의 toolchain identity)으로 결정되는 검사 전부.
run_reusable_phase() {
  run_release_check
  resolve_route
  if is_docs_only_route; then
    echo "[pre-push] docs-only change detected; skipping reusable Go gate"
    return 0
  fi

  echo "[pre-push] workflow boundary / gate ownership"
  bash scripts/ci/check-workflow-secrets.sh
  run_self_test scripts/ci/check-workflow-secrets_test.sh \
    scripts/ci/check-workflow-secrets.sh scripts/ci/workflow-gate-profile \
    scripts/ci/python-runtime.sh .github/workflows
  "${CI_PYTHON_BIN}" scripts/ci/check-workflow-ci-owner.py
  run_self_test scripts/ci/check-workflow-ci-owner_test.py \
    scripts/ci/check-workflow-ci-owner.py scripts/ci/workflow-ci-owner scripts/ci/workflow-gate-profile \
    scripts/ci/public-pr-go-gate.sh scripts/ci/public-pr-frontend-gate.sh .github/workflows
  bash scripts/ci/check-recurring-security-scan-contract.sh
  if [[ "${PRE_PUSH_PROFILE_CONTRACT_TEST_ACTIVE:-false}" != "true" ]]; then
    export PRE_PUSH_PROFILE_CONTRACT_TEST_ACTIVE=true
    run_self_test scripts/ci/pre-push-gate-profile-v1_test.sh \
      scripts/ci/pre-push-gate.sh scripts/ci/pre-push-gate-profile-v1.json \
      scripts/ci/python-runner.sh scripts/ci/python-runtime.sh scripts/ci/go-tooling.sh \
      scripts/ci/go-workspace-modules.sh scripts/ci/local-ci.sh .python-version
    unset PRE_PUSH_PROFILE_CONTRACT_TEST_ACTIVE
  fi

  echo "[pre-push] shell syntax sweep"
  bash scripts/ci/shell-syntax-sweep.sh

  echo "[pre-push] gate self-tests (run when the checker, test, fixture, or shared lib changed)"
  run_self_test scripts/ci/test-local-ci-packages.sh \
    scripts/ci/local-ci-packages.sh scripts/ci/local-ci-files.sh scripts/ci/local-ci.sh \
    deploy/compose/docker-compose.prod.yml
  run_self_test scripts/ci/local-ci-gofix_test.sh scripts/ci/local-ci-gofix.sh scripts/ci/local-ci-files.sh
  run_self_test scripts/ci/go-work-sync-drift_test.sh \
    scripts/ci/go-work-sync-drift.sh scripts/ci/admin-dashboard-go-ci.sh
  run_self_test scripts/ci/nilaway-inputs_test.sh \
    scripts/ci/nilaway-inputs.sh scripts/ci/local-ci.sh scripts/ci/admin-dashboard-go-ci.sh
  run_self_test scripts/ci/race-parallel-guard_test.sh scripts/ci/local-ci.sh
  run_self_test scripts/refactor/grep-sensitive-logs_test.sh scripts/refactor/grep-sensitive-logs.sh
  run_self_test scripts/refactor/test-validate-no-admin-touch.sh \
    scripts/refactor/validate-no-admin-touch.sh scripts/ci/admin-dashboard-go-ci.sh
  run_self_test scripts/ci/check-pgo-default_test.sh \
    scripts/ci/check-pgo-default.sh scripts/ci/pgo-off-policy.tsv scripts/build/build-youtube-collector-go.sh
  run_self_test scripts/ci/check-go-test-json_test.py scripts/ci/check-go-test-json.py
  run_self_test scripts/ci/check-youtube-collector-hardening-contract_test.sh \
    scripts/ci/check-youtube-collector-hardening-contract.sh scripts/ci/youtube-collector-hardening-contract.tsv
  run_self_test scripts/build/build-youtube-collector-go_test.sh \
    scripts/build/build-youtube-collector-go.sh scripts/build/check-youtube-collector-go-artifact.sh \
    scripts/ci/public-pr-go-gate.sh scripts/ci/python-runtime.sh hololive/hololive-youtube-collector/Makefile
  run_self_test scripts/ci/check-production-go-workspace_test.sh \
    scripts/ci/check-production-go-workspace.sh deploy/compose \
    hololive/hololive-alarm-worker/Dockerfile hololive/hololive-api/Dockerfile \
    hololive/hololive-youtube-collector/Dockerfile hololive/hololive-alarm-worker/go.mod \
    hololive/hololive-api/go.mod hololive/hololive-dbtest/go.mod hololive/hololive-shared/go.mod \
    hololive/hololive-youtube-collector/go.mod
  run_self_test scripts/deploy/check-ap-rsync-manifest_test.sh \
    scripts/deploy/check-ap-rsync-manifest.sh scripts/deploy/ap-rsync-files.txt
  run_self_test scripts/ci/check-postgres-capacity_test.sh \
    scripts/ci/check-postgres-capacity.sh scripts/ci/postgres-capacity-policy.tsv \
    deploy/compose/docker-compose.prod.yml
  run_self_test scripts/deploy/test-postgres-capacity-entrypoints.sh \
    scripts/ci/check-postgres-capacity.sh scripts/deploy/compose-redeploy-service.sh \
    scripts/deploy/compose.sh scripts/deploy/lib/postgres-capacity.sh deploy/compose/docker-compose.prod.yml
  run_self_test hololive/hololive-api/scripts/migrations/preflight-114-restore_test.sh \
    hololive/hololive-api/scripts/migrations/preflight-114-restore.sh
  run_self_test hololive/hololive-api/scripts/migrations/preflight-durable-runtime-rollback_test.sh \
    hololive/hololive-api/scripts/migrations/preflight-durable-runtime-rollback.sh
  run_self_test hololive/hololive-api/scripts/migrations/manual/repair_message_contract_074_082_test.sh \
    hololive/hololive-api/scripts/migrations/manual/repair_message_contract_074_082.sh
  run_self_test scripts/logs/daily-rollup-logs_test.sh scripts/logs/daily-rollup-logs.sh
  run_self_test scripts/logs/test-stream-mirror-retention.sh scripts/logs/lib/stream.sh
  run_self_test scripts/deploy/verify-exec-tree-ownership_test.sh scripts/deploy/verify-exec-tree-ownership.sh
  run_self_test scripts/deploy/systemd-compose-up_test.sh \
    scripts/deploy/systemd-compose-up.sh scripts/deploy/systemd-compose-down.sh scripts/deploy/compose.sh \
    scripts/deploy/sync-opt-current.sh scripts/systemd deploy/compose/docker-compose.live-compat.yml \
    deploy/compose/docker-compose.youtube-collector-disabled.yml
  run_self_test scripts/deploy/lib/public-bind-mounts_test.sh scripts/deploy/lib/public-bind-mounts.sh deploy/nginx
  run_self_test scripts/deploy/test-compose-security-defaults.sh \
    deploy/compose deploy/nginx scripts/deploy/lib/public-bind-mounts.sh scripts/ci/python-runtime.sh
  run_self_test scripts/runtime/set-iris-base-url_test.sh scripts/runtime/set-iris-base-url.sh
  run_self_test scripts/runtime/pg-hotpath-explain-snapshot_test.sh \
    scripts/runtime/pg-hotpath-explain-snapshot.sh scripts/runtime/lib \
    hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/queries \
    hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries
  run_self_test scripts/deploy/ap-host-native-deploy_test.sh \
    scripts/deploy/ap-host-native-deploy.sh scripts/deploy/ap-host-native-rollback.sh \
    scripts/deploy/ap-host-native-deploy_contract_checks.inc.sh scripts/deploy/ap-completion-check.sh \
    scripts/deploy/lib scripts/logs/ap-host-native-status.sh
  run_self_test scripts/deploy/ap-completion-check_test.sh \
    scripts/deploy/ap-completion-check.sh scripts/deploy/ap-hosts scripts/deploy/lib deploy/compose

  echo "[pre-push] mode=${PRE_PUSH_MODE} local_ci_go_scope=${resolved_local_ci_go_scope}"
  LOCAL_CI_GO_SCOPE="${resolved_local_ci_go_scope}" \
  BASE_REF="${BASE_SHA:-origin/main}" \
  RUN_ADMIN_TOUCH_GUARDRAIL="${admin_touch_guardrail}" \
  STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}" \
  RUN_NILAWAY="${RUN_NILAWAY:-true}" \
  RUN_RACE_TESTS="${RUN_RACE_TESTS:-${race_default}}" \
    ./scripts/ci/local-ci.sh

  if echo "$changed_files" | grep -qE '^admin-dashboard/(frontend|backend)/'; then
    echo "[pre-push] admin-dashboard frontend 품질 게이트"
    (cd admin-dashboard/frontend && corepack npm ci && corepack npm run generate:api && corepack npm test && corepack npm run lint && corepack npm run build)
  fi

  if [[ "${PRE_PUSH_MODE}" == "full" ]] || echo "$changed_files" | grep -q '^hololive/hololive-youtube-collector/'; then
    echo "[pre-push] youtube-collector YouTube.js helper 품질 게이트"
    bash scripts/ci/public-pr-collector-helper-gate.sh
  fi
}

# freshness: 시간이 지나면 부패하는 advisory 데이터. 주기 security workflow 는 merge 전 검증을
# 대신하지 않으므로 docs-only push 만 면제한다(DEC-20260711 offline fail-closed 유지).
run_freshness_phase() {
  local module govulncheck_bin
  resolve_route
  if is_docs_only_route; then
    echo "[pre-push] docs-only change detected; skipping freshness Go gate"
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

# ambient: 형제 checkout(go.work 의 ../ 모듈) 상태를 소비하는 검사. receipt 없이 항상 실행된다.
run_ambient_phase() {
  resolve_route
  if is_docs_only_route; then
    echo "[pre-push] docs-only change detected; skipping ambient Go gate"
    return 0
  fi

  bash scripts/ci/test-go-workspace-modules.sh
}

case "${phase}" in
  --phase=fingerprint)
    run_fingerprint
    ;;
  --phase=reusable)
    run_reusable_phase
    ;;
  --phase=freshness)
    run_freshness_phase
    ;;
  --phase=ambient)
    run_ambient_phase
    ;;
  "")
    echo "════════════════════════════════════════"
    echo "  pre-push quality gate"
    echo "════════════════════════════════════════"
    run_reusable_phase
    run_freshness_phase
    run_ambient_phase
    echo "════════════════════════════════════════"
    echo "  pre-push quality gate passed"
    echo "════════════════════════════════════════"
    ;;
esac
