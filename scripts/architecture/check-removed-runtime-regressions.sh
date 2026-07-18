#!/usr/bin/env bash
# The check definitions are resolved through run_check's namerefs.
# shellcheck disable=SC2034
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

CHECKS=(references build_paths)

references_pattern='(settlement-go|CommandSettlementStatus|CommandSettlementPaid|CommandSettlementRegister|settlement_status|settlement_paid|settlement_register|SETTLEMENT_ROOM_ID|dispatcher-go|hololive-dispatcher-go|legacy-dispatcher-go|smoke-dispatcher|DISPATCHER_PORT|30020)'
references_flags=(--hidden)
references_paths=("${ROOT_DIR}")
references_globs=(
  '!.git/**'
  # 변경 이력은 제거된 runtime 명칭을 보존하는 역사 문서다.
  '!CHANGELOG.md'
  '!docs/history/settlement/**'
  '!docs/history/**'
  '!docs/superpowers/**'
  '!hololive/hololive-api/scripts/migrations/archive/settlement/**'
  '!hololive/hololive-api/scripts/migrations/manual/settlement_drop.sql'
  '!scripts/architecture/check-removed-runtime-regressions.sh'
  '!scripts/architecture/ci-notification-egress-gate.sh'
  '!scripts/deploy/test-compose-services.sh'
  '!scripts/deploy/lib/removed-runtimes.sh'
  '!scripts/deploy/test-removed-runtimes.sh'
  '!docs/current/architecture/repo-refactor-audit.md'
  '!hololive/hololive-shared/pkg/config/repo_security_contract_test.go'
  '!hololive/hololive-shared/pkg/config/internal/settings/repo_security_contract_test.go'
  '!.tasklists/**'
  '!repo_full_scope_diff_blueprint*.md'
  '!.worktrees/**'
  '!**/*.tar.gz'
  '!**/node_modules/**'
)
references_fail='removed runtime references detected'
references_ok='no removed runtime references detected'

# build_paths 는 삭제된 모듈의 "디렉토리 경로" 참조만 검사한다. 논리적 role 이름
# (llm-scheduler, admin-api)은 runtime_role_validation.go 가 정의하는 load-bearing
# 상수이므로 대상이 아니다. 경로 패턴은 그 이름들과 충돌하지 않는다.
build_paths_pattern='hololive/hololive-(kakao-bot-go|admin-api|llm-sched)'
build_paths_flags=(--no-messages)
build_paths_paths=(deploy scripts hololive admin-dashboard)
build_paths_globs=(
  '!**/*_test.go'
  '!scripts/deploy/lib/removed-runtimes.sh'
  '!scripts/architecture/check-removed-runtime-regressions.sh'
  '!hololive/hololive-api/scripts/test-bot-env-loader.sh'
)
build_paths_fail='removed runtime directory paths referenced in active build/deploy files'
build_paths_ok='no removed runtime directory paths in active build/deploy files'

run_check() {
  local check="$1"
  local -n pattern_ref="${check}_pattern"
  local -n flags_ref="${check}_flags"
  local -n paths_ref="${check}_paths"
  local -n globs_ref="${check}_globs"
  local -n fail_ref="${check}_fail"
  local -n ok_ref="${check}_ok"

  local glob_args=()
  local glob
  for glob in "${globs_ref[@]}"; do
    glob_args+=(--glob "${glob}")
  done

  local matches
  matches="$(rg -n "${flags_ref[@]}" "${pattern_ref}" "${paths_ref[@]}" "${glob_args[@]}" || true)"

  if [[ -n "${matches}" ]]; then
    echo "FAIL: ${fail_ref}" >&2
    echo "${matches}" >&2
    exit 1
  fi

  echo "OK: ${ok_ref}"
}

for check in "${CHECKS[@]}"; do
  run_check "${check}"
done
