#!/usr/bin/env bash
# PGO 기본 적용 게이트: 핫패스 서비스 main package에 default.pgo가 존재하고
# 기본 빌드(-pgo=auto)가 실제로 -pgo 스탬프를 남기는지 검증한다.
# 새 서비스에 PGO를 기본 적용하면 PGO_REQUIRED_MAINS에 추가한다.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

PGO_REQUIRED_MAINS=(
  "hololive/hololive-api|./cmd/hololive-api"
  "hololive/hololive-alarm-worker|./cmd/alarm-worker"
)

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

for entry in "${PGO_REQUIRED_MAINS[@]}"; do
  module="${entry%%|*}"
  pkg="${entry##*|}"
  out="${tmp_dir}/$(basename "${pkg}")"
  echo "[pgo-gate] building ${module} ${pkg}"
  (cd "${module}" && CGO_ENABLED=0 go build -tags sonic -trimpath -o "${out}" "${pkg}")
  if ! go version -m "${out}" | grep -q -- '-pgo='; then
    echo "[pgo-gate] ${module} ${pkg} was built without PGO; expected ${pkg}/default.pgo (or GO_PGO_FILE)" >&2
    exit 1
  fi
done

echo "[pgo-gate] all required mains carry a -pgo build stamp"
