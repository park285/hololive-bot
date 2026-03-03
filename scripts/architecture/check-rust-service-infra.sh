#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RUST_DIR="${ROOT_DIR}/hololive/hololive-rs"
ALLOWLIST_FILE="${1:-${ROOT_DIR}/docs/architecture/rust-service-infra-allowlist.txt}"

if [[ ! -d "${RUST_DIR}" ]]; then
  echo "error: rust workspace not found: ${RUST_DIR}" >&2
  exit 1
fi

if [[ ! -f "${ALLOWLIST_FILE}" ]]; then
  echo "error: allowlist not found: ${ALLOWLIST_FILE}" >&2
  exit 1
fi

found_file="$(mktemp)"
allowed_file="$(mktemp)"
cleanup() {
  rm -f "${found_file}" "${allowed_file}"
}
trap cleanup EXIT

while IFS= read -r cargo_toml; do
  crate_name="$(
    awk -F'=' '
      $1 ~ /^name[[:space:]]*$/ {
        gsub(/[[:space:]"]/,"",$2);
        print $2;
        exit
      }
    ' "${cargo_toml}"
  )"
  if [[ -z "${crate_name}" ]]; then
    continue
  fi

  while IFS= read -r dep_line; do
    dep_name="$(echo "${dep_line}" | sed -E 's/[[:space:]]*=.*$//' | tr -d '[:space:]')"
    if [[ -n "${dep_name}" ]]; then
      printf '%s -> %s\n' "${crate_name}" "${dep_name}"
    fi
  done < <(grep -E '^[[:space:]]*[a-zA-Z0-9_-]+-infra[[:space:]]*=' "${cargo_toml}" || true)
done < <(find "${RUST_DIR}/crates" -path '*/service/Cargo.toml' -type f | sort) | sort -u > "${found_file}"

awk '
  /^[[:space:]]*$/ { next }
  /^[[:space:]]*#/ { next }
  { sub(/[[:space:]]+$/, "", $0); print }
' "${ALLOWLIST_FILE}" | sort -u > "${allowed_file}"

new_violations="$(comm -23 "${found_file}" "${allowed_file}" || true)"
resolved_allowlist="$(comm -13 "${found_file}" "${allowed_file}" || true)"

if [[ -n "${new_violations}" ]]; then
  echo "FAIL: new rust service->infra dependency detected" >&2
  echo "${new_violations}" >&2
  echo
  echo "Update allowlist only when intentionally accepted:"
  echo "  ${ALLOWLIST_FILE}"
  exit 1
fi

echo "OK: no new rust service->infra dependencies"
echo "Current allowlisted edges:"
cat "${found_file}"

if [[ -n "${resolved_allowlist}" ]]; then
  echo
  echo "Info: remove stale allowlist entries:"
  echo "${resolved_allowlist}"
fi
