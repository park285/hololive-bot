#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

# 삭제된 모듈의 "디렉토리 경로" 참조만 검사한다. 논리적 role 이름(llm-scheduler,
# admin-api)은 runtime_role_validation.go 가 정의하는 load-bearing 상수이므로
# 대상이 아니다. 경로 패턴은 그 이름들과 충돌하지 않는다.
retired='hololive/hololive-(kakao-bot-go|admin-api|llm-sched)'

matches="$(
  rg -n --no-messages "${retired}" \
    deploy scripts hololive admin-dashboard \
    -g '!**/*_test.go' \
    -g '!**/*_test.sh' \
    -g '!**/test-*.sh' \
    -g '!scripts/deploy/lib/removed-runtimes.sh' \
    -g '!scripts/architecture/check-removed-runtime-references.sh' \
    -g '!scripts/architecture/check-removed-runtime-build-paths.sh' \
    || true
)"

if [[ -n "${matches}" ]]; then
  echo "FAIL: removed runtime directory paths referenced in active build/deploy files" >&2
  echo "${matches}" >&2
  exit 1
fi

echo "OK: no removed runtime directory paths in active build/deploy files"

stale_main_matches="$(
  rg -n --fixed-strings './cmd/bot' \
    hololive/hololive-api/scripts \
    -g '*.sh' \
    -g '!test-*.sh' \
    || true
)"

if [[ -n "${stale_main_matches}" ]]; then
  echo "FAIL: removed hololive-api ./cmd/bot main package referenced by active helper" >&2
  echo "${stale_main_matches}" >&2
  exit 1
fi

echo "OK: hololive-api helpers use the unified main package"

stale_log_matches="$(
  rg -n --fixed-strings 'bot.log' \
    hololive/hololive-api/scripts \
    docs/current/runbooks \
    -g '*.sh' \
    -g '*.md' \
    -g '!test-*.sh' \
    || true
)"

if [[ -n "${stale_log_matches}" ]]; then
  echo "FAIL: removed bot.log filename referenced by active hololive-api helper or current runbook" >&2
  echo "${stale_log_matches}" >&2
  exit 1
fi

echo "OK: hololive-api helpers and current runbooks use the unified file logging contract"
