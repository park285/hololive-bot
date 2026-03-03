#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TEMPLATE_FILE="${ROOT_DIR}/docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md"

release_version="${RELEASE_VERSION:-}"
release_at="${RELEASE_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
author="${RELEASE_AUTHOR:-$(whoami)}"
pr_link="${RELEASE_PR_LINK:-}"
ci_evidence_link="${RELEASE_CI_EVIDENCE_LINK:-}"
ci_artifact_url="${RELEASE_CI_ARTIFACT_URL:-}"
output_file=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      release_version="${2:-}"
      shift 2
      ;;
    --released-at)
      release_at="${2:-}"
      shift 2
      ;;
    --author)
      author="${2:-}"
      shift 2
      ;;
    --pr-link)
      pr_link="${2:-}"
      shift 2
      ;;
    --ci-evidence-link)
      ci_evidence_link="${2:-}"
      shift 2
      ;;
    --ci-artifact-url)
      ci_artifact_url="${2:-}"
      shift 2
      ;;
    --output)
      output_file="${2:-}"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ ! -f "${TEMPLATE_FILE}" ]]; then
  echo "template not found: ${TEMPLATE_FILE}" >&2
  exit 1
fi

if [[ -z "${release_version}" ]]; then
  echo "release version is required (--version or RELEASE_VERSION)" >&2
  exit 1
fi

rendered="$(cat "${TEMPLATE_FILE}")"
rendered="${rendered//\{\{RELEASE_VERSION\}\}/${release_version}}"
rendered="${rendered//\{\{RELEASE_AT\}\}/${release_at}}"
rendered="${rendered//\{\{AUTHOR\}\}/${author}}"
rendered="${rendered//\{\{PR_LINK\}\}/${pr_link}}"
rendered="${rendered//\{\{CI_EVIDENCE_LINK\}\}/${ci_evidence_link}}"
rendered="${rendered//\{\{CI_ARTIFACT_URL\}\}/${ci_artifact_url}}"

if [[ -n "${output_file}" ]]; then
  printf '%s\n' "${rendered}" > "${output_file}"
  echo "rendered release note: ${output_file}"
  exit 0
fi

printf '%s\n' "${rendered}"
