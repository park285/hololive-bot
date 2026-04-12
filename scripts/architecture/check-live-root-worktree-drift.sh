#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKTREE_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REPO_CANONICAL_ROOT="$(cd "$(git -C "${WORKTREE_ROOT}" rev-parse --path-format=absolute --git-common-dir)/.." && pwd)"
LIVE_DIR="${REPO_CANONICAL_ROOT}/hololive"
WORKTREES_DIR="${REPO_CANONICAL_ROOT}/.worktrees"

diff_targets=(
  "hololive-shared/pkg/config"
  "hololive-shared/pkg/service/youtube/poller"
  "hololive-shared/pkg/service/youtube/scraper"
  "hololive-stream-ingester/internal/runtime"
)

if [[ ! -d "${LIVE_DIR}" ]]; then
  echo "ERROR: live root hololive tree not found: ${LIVE_DIR}" >&2
  exit 1
fi

shopt -s nullglob
review_worktree_dirs=("${WORKTREES_DIR}"/review-guide-*/hololive)
shopt -u nullglob

if [[ "${#review_worktree_dirs[@]}" -eq 0 ]]; then
  echo "OK: no review-guide worktrees found under ${WORKTREES_DIR}"
  exit 0
fi

checked=0
drift=0

for review_dir in "${review_worktree_dirs[@]}"; do
  [[ -d "${review_dir}" ]] || continue
  checked=$((checked + 1))
  worktree_name="$(basename "$(dirname "${review_dir}")")"

  for rel in "${diff_targets[@]}"; do
    if ! diff_output="$(diff -qr "${LIVE_DIR}/${rel}" "${review_dir}/${rel}" 2>&1)"; then
      echo "ERROR: live root/worktree drift detected for ${worktree_name} under ${rel}" >&2
      printf '%s\n' "${diff_output}" >&2
      drift=1
    fi
  done
done

if [[ "${drift}" -ne 0 ]]; then
  exit 1
fi

echo "OK: checked ${checked} review-guide worktree(s); critical paths match live root"
