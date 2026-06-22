#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT_DIR}/scripts/runtime/set-iris-base-url.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

secret_url="https://user:secret@iris.internal.example/path?token=abc"
stdout="$(
  RUNTIME_CONFIG_DIR="${TMP_DIR}/runtime-config" \
    IRIS_TRANSPORT=h3 \
    "${SCRIPT}" "${secret_url}"
)"

for leaked in "${secret_url}" "user:secret" "token=abc"; do
  if [[ "${stdout}" == *"${leaked}"* ]]; then
    echo "set-iris-base-url stdout leaked ${leaked}" >&2
    exit 1
  fi
done

target="${TMP_DIR}/runtime-config/iris_base_url"
mode="$(stat -c '%a' "${target}")"
case "${mode}" in
  600|400) ;;
  *)
    echo "iris_base_url mode = ${mode}, want owner-only permissions (af53b4ef)" >&2
    exit 1
    ;;
esac

if [[ -r "${target}" && "$(cat "${target}")" != "${secret_url}" ]]; then
  echo "iris_base_url content mismatch" >&2
  exit 1
fi

echo "ok: set-iris-base-url writes owner-only runtime URL file without leaking the URL"
