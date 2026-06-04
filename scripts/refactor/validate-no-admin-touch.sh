#!/usr/bin/env bash
set -euo pipefail

BASE_REF="${BASE_REF:-origin/main}"
HEAD_REF="${HEAD_REF:-HEAD}"

changed_paths_from_name_status() {
  awk -F '\t' '{ for (i = 2; i <= NF; i++) print $i }'
}

range_changed=""
if ! range_changed="$(git diff --name-status -M "${BASE_REF}...${HEAD_REF}" 2>&1)"; then
  echo "failed to compare admin scope against ${BASE_REF}...${HEAD_REF}" >&2
  echo "${range_changed}" >&2
  exit 2
fi

changed="$(
  {
    printf '%s\n' "${range_changed}" | changed_paths_from_name_status
    git diff --name-status -M --cached 2>/dev/null | changed_paths_from_name_status || true
    git diff --name-status -M 2>/dev/null | changed_paths_from_name_status || true
    git ls-files --others --exclude-standard 2>/dev/null || true
  } | sort -u
)"

admin_changed="$(echo "${changed}" | grep -E '^admin-dashboard/' || true)"
if [[ -z "${admin_changed}" ]]; then
  echo "ok: no admin-dashboard scope changes against ${BASE_REF}...${HEAD_REF}"
  exit 0
fi

printf '%s\n' "${admin_changed}"

echo "admin-dashboard files changed; running Go-only admin-dashboard quality gates" >&2
./scripts/ci/admin-dashboard-go-ci.sh

frontend_changed="$(echo "${admin_changed}" | grep -E '^admin-dashboard/frontend/' || true)"
if [[ -n "${frontend_changed}" ]]; then
  echo "frontend files changed; running frontend lint/build" >&2
  (cd admin-dashboard/frontend && npm ci --no-audit --no-fund && npm run lint && npm run build)
fi

echo "ok: admin-dashboard changes passed active quality gates"
