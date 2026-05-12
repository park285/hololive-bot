#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

source "${SCRIPT_DIR}/lib/git_guard.sh"
require_git_checkout "${ROOT_DIR}"

violations=()

while IFS= read -r path; do
  if [[ ! -e "${ROOT_DIR}/${path}" ]]; then
    continue
  fi
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
      violations+=("${path}")
      ;;
  esac
done < <(git -C "${ROOT_DIR}" ls-files)

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
