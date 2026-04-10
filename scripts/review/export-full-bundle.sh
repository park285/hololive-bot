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

cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

mkdir -p "${OUT_DIR}"

(
  cd "${ROOT_DIR}"
  while IFS= read -r -d '' path; do
    [[ -e "${path}" ]] || continue
    printf '%s\0' "${path}" >> "${FILE_LIST}"
  done < <(git ls-files -z --cached)
  if [[ "${INCLUDE_UNTRACKED}" == "true" ]]; then
    while IFS= read -r -d '' path; do
      [[ -e "${path}" ]] || continue
      printf '%s\0' "${path}" >> "${FILE_LIST}"
    done < <(git ls-files -z --others --exclude-standard)
  fi
)

cat > "${MANIFEST}" <<MANIFEST
repo_root: ${ROOT_DIR}
mode: full
tracked_only: $([[ "${INCLUDE_UNTRACKED}" == "true" ]] && echo "false" || echo "true")
generated_at: $(date -u +%FT%TZ)
branch: $(git -C "${ROOT_DIR}" rev-parse --abbrev-ref HEAD)
commit: $(git -C "${ROOT_DIR}" rev-parse HEAD)
included_files: $(tr -cd '\0' < "${FILE_LIST}" | wc -c | tr -d ' ')
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
