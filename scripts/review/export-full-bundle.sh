#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${1:-${ROOT_DIR}/artifacts/review}"
INCLUDE_UNTRACKED="${INCLUDE_UNTRACKED:-false}"
if [[ "${OUT_DIR}" != /* ]]; then
  OUT_DIR="${ROOT_DIR}/${OUT_DIR}"
fi
OUT_FILE="${OUT_DIR}/hololive-bot-review-bundle-full-${STAMP}.tar.gz"
TMP_DIR="$(mktemp -d)"
FILE_LIST="${TMP_DIR}/files.txt"
MANIFEST="${TMP_DIR}/BUNDLE_MANIFEST.txt"

BUNDLE_EXCLUDES=(
  ".git"
  ".worktrees"
  ".tasklists"
  ".runlogs"
  ".codex"
  ".claude"
  ".serena"
  ".gemini"
  "artifacts"
  "logs"
  "node_modules"
  "dist"
  "coverage"
  "*.tar.gz"
  "BUNDLE_MANIFEST.txt"
  ".idea"
  ".vscode"
  ".omc"
)

cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

mkdir -p "${OUT_DIR}"

should_exclude_path() {
  local path="$1"

  case "${path}" in
    .git|.git/*|\
    .worktrees|.worktrees/*|\
    .tasklists|.tasklists/*|\
    .runlogs|.runlogs/*|\
    .codex|.codex/*|\
    .claude|.claude/*|\
    .serena|.serena/*|\
    .gemini|.gemini/*|\
    artifacts|artifacts/*|\
    logs|logs/*|\
    node_modules|node_modules/*|\
    dist|dist/*|\
    coverage|coverage/*|\
    BUNDLE_MANIFEST.txt|\
    *.tar.gz|\
    .idea|.idea/*|\
    .vscode|.vscode/*|\
    .omc|.omc/*|\
    */.idea|*/.idea/*|\
    */.vscode|*/.vscode/*|\
    */.omc|*/.omc/*)
      return 0
      ;;
  esac

  return 1
}

compute_content_sha256() {
  (
    cd "${ROOT_DIR}"
    while IFS= read -r -d '' path; do
      sha256sum "${path}"
    done < "${FILE_LIST}"
  ) | sha256sum | awk '{print $1}'
}

append_git_paths() {
  while IFS= read -r -d '' path; do
    [[ -e "${path}" ]] || continue
    if should_exclude_path "${path}"; then
      continue
    fi
    printf '%s\0' "${path}" >> "${FILE_LIST}"
  done
}

(
  cd "${ROOT_DIR}"
  append_git_paths < <(git ls-files -z --cached)
  if [[ "${INCLUDE_UNTRACKED}" == "true" ]]; then
    append_git_paths < <(git ls-files -z --others --exclude-standard)
  fi
)

sort -zu "${FILE_LIST}" -o "${FILE_LIST}"
CONTENT_SHA256="$(compute_content_sha256)"

cat > "${MANIFEST}" <<MANIFEST
repo_root: ${ROOT_DIR}
mode: full
tracked_only: $([[ "${INCLUDE_UNTRACKED}" == "true" ]] && echo "false" || echo "true")
generated_at: $(date -u +%FT%TZ)
branch: $(git -C "${ROOT_DIR}" rev-parse --abbrev-ref HEAD)
commit: $(git -C "${ROOT_DIR}" rev-parse HEAD)
included_files: $(tr -cd '\0' < "${FILE_LIST}" | wc -c | tr -d ' ')
excluded_patterns: $(IFS=,; echo "${BUNDLE_EXCLUDES[*]}")
content_sha256: ${CONTENT_SHA256}
MANIFEST

(
  cd "${ROOT_DIR}"
  tar --null -T "${FILE_LIST}" \
    --transform 's,^,,' \
    --create -f "${TMP_DIR}/bundle.tar"
)

(
  cd "${TMP_DIR}"
  tar --append -f bundle.tar BUNDLE_MANIFEST.txt
  gzip -c bundle.tar > "${OUT_FILE}"
)

echo "${OUT_FILE}"
