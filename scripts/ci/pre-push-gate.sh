#!/usr/bin/env bash
# pre-push-gate: push 전 필수 품질 게이트.
# ~/.git-hooks/pre-push 에서 위임 호출됨.
# 이전 GitHub Actions CI (Verify, Architecture Gates, Dependency Hygiene,
# Frontend Quality) 를 로컬 게이트로 대체.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
PROFILE_ID="hololive-bot-v1"
release_checked=false
route_resolved=false
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
  if [[ -n "${BASE_SHA:-}" && -n "${HEAD_SHA:-}" ]]; then
    route_range="${BASE_SHA}..${HEAD_SHA}"
    if ! changed_files="$(git diff --name-only "${route_range}")"; then
      echo "failed to resolve exact pushed range ${route_range}" >&2
      return 1
    fi
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
      dependency_hygiene_default="false"
      ;;
    full)
      local_ci_go_scope="all"
      race_default="true"
      dependency_hygiene_default="true"
      ;;
    *)
      echo "unsupported PRE_PUSH_MODE=${PRE_PUSH_MODE}; expected fast or full" >&2
      return 1
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

  admin_touch_guardrail="${RUN_ADMIN_TOUCH_GUARDRAIL:-true}"
  if echo "$changed_files" | grep -q '^admin-dashboard/' && [[ -z "${RUN_ADMIN_TOUCH_GUARDRAIL+x}" ]]; then
    admin_touch_guardrail=false
  fi
  resolved_local_ci_go_scope="${LOCAL_CI_GO_SCOPE:-${local_ci_go_scope}}"
  route_resolved=true
}

run_release_check() {
  if [[ "${release_checked}" == "false" ]]; then
    bash "${ROOT_DIR}/scripts/check-release-version.sh"
    release_checked=true
  fi
}

hash_stream() {
  sha256sum | awk '{print $1}'
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
    printf 'dependency_hygiene=%s\n' "${RUN_DEPENDENCY_HYGIENE:-${dependency_hygiene_default}}"
    printf 'admin_touch_guardrail=%s\n' "${admin_touch_guardrail}"
    printf '%s\n' "${changed_files}" | hash_stream
  } | hash_stream)"

  toolchain_fingerprint="$({
    go version
    go env GOVERSION GOOS GOARCH GOTOOLCHAIN
    systemd-run --version
    git hash-object -- scripts/ci/go-tooling.sh scripts/ci/local-ci.sh
    printf 'GOTOOLCHAIN=%s\n' "${GOTOOLCHAIN}"
    printf 'MemoryHigh=%s\n' "${PRE_PUSH_MEMORY_HIGH:-24G}"
    printf 'MemoryMax=%s\n' "${PRE_PUSH_MEMORY_MAX:-32G}"
  } | hash_stream)"

  mapfile -t profile_inputs < <(git ls-files -- \
    '.github/workflows/*.yml' \
    '.golangci.yml' \
    'go.mod' 'go.sum' \
    'admin-dashboard/backend/go.mod' 'admin-dashboard/backend/go.sum' \
    'scripts/check-release-version.sh' \
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

run_reusable() {
  run_release_check
  echo "[pre-push] workflow boundary / gate ownership"
  bash scripts/ci/check-workflow-secrets.sh
  bash scripts/ci/check-workflow-secrets_test.sh
  if [[ "${PRE_PUSH_PROFILE_CONTRACT_TEST_ACTIVE:-false}" != "true" ]]; then
    PRE_PUSH_PROFILE_CONTRACT_TEST_ACTIVE=true \
      bash scripts/ci/pre-push-gate-profile-v1_test.sh
  fi
}

run_freshness() {
  :
}

run_ambient() {
  if [[ "${route_resolved}" == "false" ]]; then
    resolve_route
  fi

  # go.work sibling checkout를 소비하는 검사는 cache 대상에서 제외한다.
  bash scripts/ci/test-go-workspace-modules.sh
  bash scripts/deploy/check-ap-rsync-manifest.sh
  echo "[pre-push] security regression shell tests"
  bash scripts/logs/daily-rollup-logs_test.sh
  bash scripts/logs/test-stream-mirror-retention.sh
  bash scripts/deploy/verify-exec-tree-ownership_test.sh
  bash scripts/deploy/systemd-compose-up_test.sh
  bash scripts/deploy/lib/public-bind-mounts_test.sh
  bash scripts/deploy/test-compose-security-defaults.sh
  bash scripts/runtime/set-iris-base-url_test.sh
  bash scripts/runtime/pg-hotpath-explain-snapshot_test.sh
  bash scripts/deploy/ap-host-native-deploy_test.sh
  bash scripts/deploy/ap-completion-check_test.sh
  bash scripts/ci/race-parallel-guard_test.sh
  bash hololive/hololive-api/scripts/migrations/manual/repair_message_contract_074_082_test.sh
  echo "[pre-push] shell syntax sweep"
  bash scripts/ci/shell-syntax-sweep.sh

  echo "[pre-push] mode=${PRE_PUSH_MODE} local_ci_go_scope=${resolved_local_ci_go_scope}"
  LOCAL_CI_GO_SCOPE="${resolved_local_ci_go_scope}" \
  BASE_REF="${BASE_SHA:-origin/main}" \
  RUN_ADMIN_TOUCH_GUARDRAIL="${admin_touch_guardrail}" \
  RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-${dependency_hygiene_default}}" \
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

case "${phase}" in
  --phase=fingerprint)
    run_fingerprint
    ;;
  --phase=reusable)
    run_reusable
    ;;
  --phase=freshness)
    run_freshness
    ;;
  --phase=ambient)
    run_ambient
    ;;
  "")
    echo "════════════════════════════════════════"
    echo "  pre-push quality gate"
    echo "════════════════════════════════════════"
    run_release_check
    resolve_route
    run_reusable
    run_freshness
    run_ambient
    echo "════════════════════════════════════════"
    echo "  pre-push quality gate passed"
    echo "════════════════════════════════════════"
    ;;
esac
