#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ARCHIVE_PATH="${1:?usage: verify-full-bundle.sh <bundle.tar.gz>}"
TMP_DIR="$(mktemp -d)"
ACTUAL_FILES="$(mktemp)"
EXPECTED_FILES="$(mktemp)"
cleanup() {
  rm -rf "${TMP_DIR}"
  rm -f "${ACTUAL_FILES}" "${EXPECTED_FILES}"
}
trap cleanup EXIT

source "${ROOT_DIR}/scripts/architecture/lib/git_guard.sh"
require_git_checkout "${ROOT_DIR}"

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

compute_content_sha256() {
  local base_dir="$1"
  local file_list="$2"

  (
    cd "${base_dir}"
    while IFS= read -r path; do
      [[ -n "${path}" ]] || continue
      sha256sum "${path}"
    done < "${file_list}"
  ) | sha256sum | awk '{print $1}'
}

tar -xzf "${ARCHIVE_PATH}" -C "${TMP_DIR}"
ROOT="${TMP_DIR}"

is_excluded_path() {
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

while IFS= read -r path; do
  [[ "${path}" == "BUNDLE_MANIFEST.txt" ]] && continue
  printf '%s\n' "${path}" >> "${ACTUAL_FILES}"
  if is_excluded_path "${path}"; then
    echo "FAIL: excluded path found in bundle: ${path}" >&2
    exit 1
  fi
done < <(cd "${ROOT}" && find . -mindepth 1 -type f -printf '%P\n' | sort)
if [[ ! -f "${ROOT}/BUNDLE_MANIFEST.txt" ]]; then
  echo "FAIL: bundle manifest missing" >&2
  exit 1
fi

manifest_mode="$(sed -n 's/^mode:[[:space:]]*//p' "${ROOT}/BUNDLE_MANIFEST.txt")"
manifest_tracked_only="$(sed -n 's/^tracked_only:[[:space:]]*//p' "${ROOT}/BUNDLE_MANIFEST.txt")"
manifest_branch="$(sed -n 's/^branch:[[:space:]]*//p' "${ROOT}/BUNDLE_MANIFEST.txt")"
manifest_commit="$(sed -n 's/^commit:[[:space:]]*//p' "${ROOT}/BUNDLE_MANIFEST.txt")"
manifest_included_files="$(sed -n 's/^included_files:[[:space:]]*//p' "${ROOT}/BUNDLE_MANIFEST.txt")"
manifest_excluded_patterns="$(sed -n 's/^excluded_patterns:[[:space:]]*//p' "${ROOT}/BUNDLE_MANIFEST.txt")"
manifest_content_sha256="$(sed -n 's/^content_sha256:[[:space:]]*//p' "${ROOT}/BUNDLE_MANIFEST.txt")"

[[ -n "${manifest_mode}" ]] || { echo "FAIL: bundle manifest schema drift: mode missing" >&2; exit 1; }
[[ -n "${manifest_tracked_only}" ]] || { echo "FAIL: bundle manifest schema drift: tracked_only missing" >&2; exit 1; }
[[ -n "${manifest_branch}" ]] || { echo "FAIL: bundle manifest schema drift: branch missing" >&2; exit 1; }
[[ -n "${manifest_commit}" ]] || { echo "FAIL: bundle manifest schema drift: commit missing" >&2; exit 1; }
[[ -n "${manifest_included_files}" ]] || { echo "FAIL: bundle manifest schema drift: included_files missing" >&2; exit 1; }
[[ -n "${manifest_excluded_patterns}" ]] || { echo "FAIL: bundle manifest schema drift: excluded_patterns missing" >&2; exit 1; }
[[ -n "${manifest_content_sha256}" ]] || { echo "FAIL: bundle manifest schema drift: content_sha256 missing" >&2; exit 1; }

if [[ "${manifest_mode}" != "full" ]]; then
  echo "FAIL: unsupported bundle mode: ${manifest_mode}" >&2
  exit 1
fi

expected_excluded_patterns="$(IFS=,; echo "${BUNDLE_EXCLUDES[*]}")"
if [[ "${manifest_excluded_patterns}" != "${expected_excluded_patterns}" ]]; then
  echo "FAIL: bundle manifest excluded_patterns drift" >&2
  exit 1
fi

if [[ "${manifest_commit}" != "$(git -C "${ROOT_DIR}" rev-parse HEAD)" ]]; then
  echo "FAIL: bundle manifest commit does not match current checkout" >&2
  exit 1
fi

if [[ "${manifest_branch}" != "$(git -C "${ROOT_DIR}" rev-parse --abbrev-ref HEAD)" ]]; then
  echo "FAIL: bundle manifest branch does not match current checkout" >&2
  exit 1
fi

append_git_paths() {
  while IFS= read -r -d '' path; do
    [[ -e "${ROOT_DIR}/${path}" ]] || continue
    if is_excluded_path "${path}"; then
      continue
    fi
    printf '%s\n' "${path}" >> "${EXPECTED_FILES}"
  done
}

append_git_paths < <(git -C "${ROOT_DIR}" ls-files -z --cached)
if [[ "${manifest_tracked_only}" == "false" ]]; then
  append_git_paths < <(git -C "${ROOT_DIR}" ls-files -z --others --exclude-standard)
elif [[ "${manifest_tracked_only}" != "true" ]]; then
  echo "FAIL: invalid tracked_only value: ${manifest_tracked_only}" >&2
  exit 1
fi

sort -u "${EXPECTED_FILES}" -o "${EXPECTED_FILES}"
sort -u "${ACTUAL_FILES}" -o "${ACTUAL_FILES}"

actual_count="$(wc -l < "${ACTUAL_FILES}" | tr -d '[:space:]')"
if [[ "${manifest_included_files}" != "${actual_count}" ]]; then
  echo "FAIL: bundle manifest included_files mismatch: expected ${manifest_included_files}, got ${actual_count}" >&2
  exit 1
fi

if ! cmp -s "${EXPECTED_FILES}" "${ACTUAL_FILES}"; then
  echo "FAIL: bundle contents differ from current checkout export policy" >&2
  echo "--- expected files" >&2
  cat "${EXPECTED_FILES}" >&2
  echo "--- actual files" >&2
  cat "${ACTUAL_FILES}" >&2
  exit 1
fi

expected_content_sha256="$(compute_content_sha256 "${ROOT_DIR}" "${EXPECTED_FILES}")"
actual_content_sha256="$(compute_content_sha256 "${ROOT}" "${ACTUAL_FILES}")"
if [[ "${manifest_content_sha256}" != "${expected_content_sha256}" ]]; then
  echo "FAIL: bundle manifest content_sha256 does not match current checkout" >&2
  exit 1
fi

if [[ "${manifest_content_sha256}" != "${actual_content_sha256}" ]]; then
  echo "FAIL: bundle file contents differ from manifest content hash" >&2
  exit 1
fi

echo "OK: full bundle matches in-repo export policy"
