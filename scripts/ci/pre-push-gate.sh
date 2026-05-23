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

RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-false}" \
STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}" \
RUN_RACE_TESTS="${RUN_RACE_TESTS:-false}" \
  ./scripts/ci/local-ci.sh

push_range="$(git rev-list origin/main..HEAD 2>/dev/null | tail -1)..HEAD"
if [[ "$push_range" == "..HEAD" ]]; then
  push_range="HEAD~1..HEAD"
fi

changed_files="$(git diff --name-only "$push_range" 2>/dev/null || true)"

if echo "$changed_files" | grep -q '^admin-dashboard/backend/'; then
  echo "[pre-push] admin-dashboard backend Rust 품질 게이트"
  if command -v cargo >/dev/null 2>&1; then
    cargo fmt --check --manifest-path admin-dashboard/backend/Cargo.toml
    cargo clippy --manifest-path admin-dashboard/backend/Cargo.toml -- -D warnings
    cargo test --manifest-path admin-dashboard/backend/Cargo.toml
  else
    echo "[pre-push] cargo 없음 — Rust 게이트 스킵" >&2
  fi
fi

if echo "$changed_files" | grep -qE '^admin-dashboard/(frontend|backend)/'; then
  echo "[pre-push] admin-dashboard frontend 품질 게이트"
  if command -v npm >/dev/null 2>&1 && [[ -f admin-dashboard/frontend/package.json ]]; then
    (cd admin-dashboard/frontend && npm ci && npm run generate:api && npm test && npm run lint && npm run build)
  else
    echo "[pre-push] npm 없거나 frontend 디렉토리 없음 — frontend 게이트 스킵" >&2
  fi
fi

echo "════════════════════════════════════════"
echo "  pre-push quality gate passed"
echo "════════════════════════════════════════"
