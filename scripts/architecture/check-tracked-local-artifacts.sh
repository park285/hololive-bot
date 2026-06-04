#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

source "${SCRIPT_DIR}/lib/git_guard.sh"
require_git_checkout "${ROOT_DIR}"

violations=()
forbidden_patterns=(
  '^logs/'
  '^data/'
  '^backups/'
  '^\.review-bundles/'
  '^runtime-config/'
  '^\.env$'
  '^\.env\.local$'
  '^\.env\..*\.local$'
  '^\.env\.(osaka|seoul|main-ap|prod|production|staging)$'
  '^.*\.key$'
  '^.*\.pem$'
  '^.*\.tar\.gz$'
)

is_allowed_exception() {
  local path="$1"
  case "${path}" in
    artifacts/architecture/go-workspace-import-graph.txt|\
    logs/.gitkeep|\
    runtime-config/.gitkeep|\
    runtime-config/README.md|\
    runtime-config/*.example)
      return 0
      ;;
  esac
  return 1
}

while IFS= read -r path; do
  [[ -z "${path}" ]] && continue
  if is_allowed_exception "${path}"; then
    continue
  fi
  if [[ -e "${ROOT_DIR}/${path}" ]]; then
    missing_suffix=""
  else
    missing_suffix=" (missing from working tree)"
  fi
  for pattern in "${forbidden_patterns[@]}"; do
    if [[ "${path}" =~ ${pattern} ]]; then
      violations+=("${path}${missing_suffix}")
      continue 2
    fi
  done
  case "${path}" in
    .worktrees/*|\
    .tasklists/*|\
    .runlogs/*|\
    .codex/*|\
    .claude/*|\
    .serena/*|\
    .gemini/*|\
    BUNDLE_MANIFEST.txt|\
    *.zip|\
    *.tar|\
    *.tar.gz|\
    *.patch|\
    *.diff|\
    *_artifact.*|\
    *.orig|\
    *.rej|\
    .idea/*|\
    .vscode/*|\
    .omc/*|\
    */.idea/*|\
    */.vscode/*|\
    */.omc/*)
      violations+=("${path}${missing_suffix}")
      ;;
  esac
done < <(
  {
    git -C "${ROOT_DIR}" ls-files -s | sed -E $'s/^[0-9]+ [0-9a-f]+ [0-9]+\t//'
    git -C "${ROOT_DIR}" diff --cached --name-only --no-renames
  } | sort -u
)

if (( ${#violations[@]} > 0 )); then
  echo "FAIL: tracked local artifacts detected" >&2
  for path in "${violations[@]}"; do
    echo " - ${path}" >&2
  done
  exit 1
fi

mnt_hits="$(git -C "${ROOT_DIR}" grep -n '/mnt/data' -- 'docs/**/*.md' '*.md' 2>/dev/null || true)"
if [[ -n "${mnt_hits}" ]]; then
  echo "FAIL: tracked docs contain /mnt/data references" >&2
  echo "${mnt_hits}" >&2
  exit 1
fi

echo "OK: no tracked local artifacts"
