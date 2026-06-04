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
admin_changed="$(echo "$changed" | grep -E '^admin-dashboard/' | grep -v -E '^admin-dashboard/Dockerfile$' || true)"
if [[ -n "${admin_changed}" ]]; then
  echo "${admin_changed}"
  {
    echo "admin-dashboard files changed while the admin-touch guardrail is active."
    echo "intentional admin-dashboard work: commit it so pre-push swaps this tripwire for the admin quality gates"
    echo "(cargo fmt/clippy/test + frontend). otherwise remove the stray admin-dashboard changes,"
    echo "or set RUN_ADMIN_TOUCH_GUARDRAIL=false for a one-off bypass."
  } >&2
  exit 1
fi

echo "ok: no admin-dashboard scope changes against ${BASE_REF}...${HEAD_REF}"
