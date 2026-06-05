#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${1:-${ROOT_DIR}/artifacts/review}"
ALLOWLIST_FILE="${2:-}"

if (( $# > 2 )); then
  echo "usage: export-source-bundle.sh [output_dir] [allowlist_file]" >&2
  exit 2
fi

if [[ "${OUT_DIR}" != /* ]]; then
  OUT_DIR="${ROOT_DIR}/${OUT_DIR}"
fi

OUT_FILE="${OUT_DIR}/hololive-bot-source-${STAMP}.tar.gz"
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
  "backups"
  "data"
  "logs"
  "runtime-config"
  ".env"
  ".env.*"
  "**/.env"
  "**/.env.*"
  "*.key"
  "*.key.*"
  "*.pem"
  "*.pem.*"
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

source "${ROOT_DIR}/scripts/architecture/lib/git_guard.sh"
require_git_checkout "${ROOT_DIR}"

mkdir -p "${OUT_DIR}"
: >"${FILE_LIST}"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

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
    backups|backups/*|\
    data|data/*|\
    logs|logs/*|\
    runtime-config|runtime-config/*|\
    .env|.env.*|\
    */.env|*/.env.*|\
    *.key|*.key.*|\
    *.pem|*.pem.*|\
    node_modules|node_modules/*|\
    */node_modules|*/node_modules/*|\
    dist|dist/*|\
    */dist|*/dist/*|\
    coverage|coverage/*|\
    */coverage|*/coverage/*|\
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

validate_bundle_path() {
  local path="$1"
  local part
  local -a parts

  [[ -n "${path}" ]] || fail "unsafe source bundle path: empty path"
  [[ "${path}" != /* ]] || fail "unsafe source bundle path: ${path}"
  [[ "${path}" != *$'\n'* ]] || fail "unsafe source bundle path: ${path}"
  [[ "${path}" != *$'\r'* ]] || fail "unsafe source bundle path: ${path}"

  IFS='/' read -r -a parts <<<"${path}"
  for part in "${parts[@]}"; do
    if [[ -z "${part}" || "${part}" == "." || "${part}" == ".." ]]; then
      fail "unsafe source bundle path: ${path}"
    fi
  done
}

append_candidate_path() {
  local path="$1"
  local source="${2:-tracked}"
  local abs_path

  path="${path#./}"
  validate_bundle_path "${path}"

  if should_exclude_path "${path}"; then
    return
  fi

  abs_path="${ROOT_DIR}/${path}"
  if [[ -L "${abs_path}" ]]; then
    fail "unsafe source bundle file type: ${path}"
  fi
  if [[ ! -e "${abs_path}" ]]; then
    if [[ "${source}" == "tracked" ]]; then
      return
    fi
    fail "source bundle file missing: ${path}"
  fi
  if [[ ! -f "${abs_path}" ]]; then
    fail "unsafe source bundle file type: ${path}"
  fi

  printf '%s\0' "${path}" >>"${FILE_LIST}"
}

append_tracked_paths() {
  local path
  while IFS= read -r -d '' path; do
    append_candidate_path "${path}" "tracked"
  done < <(git -C "${ROOT_DIR}" ls-files -z --cached)
}

append_allowlist_paths() {
  local path

  [[ -f "${ALLOWLIST_FILE}" ]] || fail "allowlist file not found: ${ALLOWLIST_FILE}"
  while IFS= read -r path || [[ -n "${path}" ]]; do
    [[ -n "${path}" ]] || continue
    [[ "${path}" != \#* ]] || continue
    append_candidate_path "${path}" "allowlist"
  done <"${ALLOWLIST_FILE}"
}

write_manifest() {
  local file_count
  local policy="tracked-only"
  local tracked_only="true"
  local allowlist_label="<none>"
  local path
  local digest

  if [[ -n "${ALLOWLIST_FILE}" ]]; then
    policy="tracked-plus-allowlist"
    tracked_only="false"
    allowlist_label="${ALLOWLIST_FILE}"
  fi

  file_count="$(tr -cd '\0' <"${FILE_LIST}" | wc -c | tr -d ' ')"

  {
    printf 'format: hololive-review-bundle-v1\n'
    printf 'mode: source\n'
    printf 'policy: %s\n' "${policy}"
    printf 'tracked_only: %s\n' "${tracked_only}"
    printf 'allowlist_file: %s\n' "${allowlist_label}"
    printf 'generated_at: %s\n' "$(date -u +%FT%TZ)"
    printf 'branch: %s\n' "$(git -C "${ROOT_DIR}" rev-parse --abbrev-ref HEAD)"
    printf 'commit: %s\n' "$(git -C "${ROOT_DIR}" rev-parse HEAD)"
    printf 'file_count: %s\n' "${file_count}"
    printf 'excluded_patterns: %s\n' "$(IFS=,; echo "${BUNDLE_EXCLUDES[*]}")"
    printf 'files:\n'

    (
      cd "${ROOT_DIR}"
      while IFS= read -r -d '' path; do
        digest="$(sha256sum -- "${path}" | awk '{print $1}')"
        printf '%s  %s\n' "${digest}" "${path}"
      done <"${FILE_LIST}"
    )
  } >"${MANIFEST}"
}

append_tracked_paths
if [[ -n "${ALLOWLIST_FILE}" ]]; then
  append_allowlist_paths
fi
sort -zu "${FILE_LIST}" -o "${FILE_LIST}"
write_manifest

(
  cd "${ROOT_DIR}"
  tar --null --files-from "${FILE_LIST}" --create --file "${TMP_DIR}/bundle.tar"
)

(
  cd "${TMP_DIR}"
  tar --append --file bundle.tar BUNDLE_MANIFEST.txt
  gzip -c bundle.tar >"${OUT_FILE}"
)

if [[ -n "${ALLOWLIST_FILE}" ]]; then
  "${SCRIPT_DIR}/verify-full-bundle.sh" "${OUT_FILE}" "${MANIFEST}"
else
  "${SCRIPT_DIR}/verify-full-bundle.sh" "${OUT_FILE}"
fi
echo "${OUT_FILE}"
