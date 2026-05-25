#!/usr/bin/env bash
# pre-push-gate: push 전 필수 품질 게이트.
# ~/.git-hooks/pre-push 에서 위임 호출됨.
# 이전 GitHub Actions CI (Verify, Architecture Gates, Dependency Hygiene,
# Rust Quality, Frontend Quality) 를 로컬 게이트로 대체.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

echo "════════════════════════════════════════"
echo "  pre-push quality gate"
echo "════════════════════════════════════════"

if git rev-parse --verify origin/main >/dev/null 2>&1; then
  changed_files="$(git diff --name-only origin/main..HEAD 2>/dev/null || true)"
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
    ;;
  full)
    local_ci_go_scope="all"
    ;;
  *)
    echo "unsupported PRE_PUSH_MODE=${PRE_PUSH_MODE}; expected fast or full" >&2
    exit 1
    ;;
esac

admin_touch_guardrail="${RUN_ADMIN_TOUCH_GUARDRAIL:-true}"
if echo "$changed_files" | grep -q '^admin-dashboard/' && [[ -z "${RUN_ADMIN_TOUCH_GUARDRAIL+x}" ]]; then
  admin_touch_guardrail=false
fi

resolved_local_ci_go_scope="${LOCAL_CI_GO_SCOPE:-${local_ci_go_scope}}"
echo "[pre-push] mode=${PRE_PUSH_MODE} local_ci_go_scope=${resolved_local_ci_go_scope}"

LOCAL_CI_GO_SCOPE="${resolved_local_ci_go_scope}" \
RUN_ADMIN_TOUCH_GUARDRAIL="${admin_touch_guardrail}" \
RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-false}" \
STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}" \
RUN_RACE_TESTS="${RUN_RACE_TESTS:-false}" \
  ./scripts/ci/local-ci.sh

if echo "$changed_files" | grep -q '^admin-dashboard/backend/'; then
  echo "[pre-push] admin-dashboard backend Rust 품질 게이트"
  cargo fmt --check --manifest-path admin-dashboard/backend/Cargo.toml
  cargo clippy --manifest-path admin-dashboard/backend/Cargo.toml -- -D warnings
  cargo test --manifest-path admin-dashboard/backend/Cargo.toml
  if command -v cargo-deny >/dev/null 2>&1; then
    (cd admin-dashboard/backend && cargo deny check --config deny.toml)
  else
    echo "[pre-push] cargo-deny 미설치 — 라이선스/보안 감사 스킵 (cargo install cargo-deny)" >&2
  fi
fi

if echo "$changed_files" | grep -qE '^admin-dashboard/(frontend|backend)/'; then
  echo "[pre-push] admin-dashboard frontend 품질 게이트"
  (cd admin-dashboard/frontend && npm ci && npm run generate:api && npm test && npm run lint && npm run build)
fi

echo "════════════════════════════════════════"
echo "  pre-push quality gate passed"
echo "════════════════════════════════════════"
