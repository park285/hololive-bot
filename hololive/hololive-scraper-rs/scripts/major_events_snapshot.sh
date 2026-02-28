#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${ROOT_DIR}/.." && pwd)"

COMPOSE_FILE="${COMPOSE_FILE:-${MONOREPO_ROOT}/docker-compose.prod.yml}"
DB_SERVICE="${DB_SERVICE:-holo-postgres}"
DB_NAME="${DB_NAME:-hololive}"
DB_USER="${DB_USER:-${POSTGRES_ADMIN_USER:-postgres_admin}}"
LABEL_RAW="${1:-manual}"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"

case "${CONTAINER_CLI}" in
  docker|podman) ;;
  *)
    echo "[snapshot] unsupported CONTAINER_CLI: ${CONTAINER_CLI}" >&2
    echo "[snapshot] allowed values: docker, podman" >&2
    exit 1
    ;;
esac

if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
  echo "[snapshot] container CLI not found: ${CONTAINER_CLI}" >&2
  echo "[snapshot] set CONTAINER_CLI=docker or CONTAINER_CLI=podman" >&2
  exit 1
fi

COMPOSE_CMD=("${CONTAINER_CLI}" "compose")
COMPOSE_MODE="${CONTAINER_CLI} compose"
# podman 선택 시 docker-compose provider 의존을 피하기 위해 podman-compose 우선
if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
  COMPOSE_CMD=("podman-compose")
  COMPOSE_MODE="podman-compose"
elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
  if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=("podman-compose")
    COMPOSE_MODE="podman-compose"
  else
    echo "[snapshot] '${CONTAINER_CLI} compose' is unavailable" >&2
    exit 1
  fi
fi

if [[ ! -f "${COMPOSE_FILE}" ]]; then
  echo "[snapshot] compose file not found: ${COMPOSE_FILE}" >&2
  exit 1
fi

SAFE_LABEL="$(printf '%s' "${LABEL_RAW}" | tr -cs 'A-Za-z0-9._-' '-' | sed 's/^-*//;s/-*$//')"
if [[ -z "${SAFE_LABEL}" ]]; then
  SAFE_LABEL="manual"
fi

STAMP="$(date -u +%Y%m%d_%H%M%S)"
REPORT_DIR="${ROOT_DIR}/docs/reports/dual_run_snapshots"
mkdir -p "${REPORT_DIR}"

PREFIX="major_events_snapshot_${SAFE_LABEL}_${STAMP}"
SNAPSHOT_PATH="${REPORT_DIR}/${PREFIX}.tsv"
META_PATH="${REPORT_DIR}/${PREFIX}.json"
TMP_ROWS="$(mktemp "${TMPDIR:-/tmp}/${PREFIX}.rows.XXXXXX")"

cleanup() {
  rm -f "${TMP_ROWS}"
}
trap cleanup EXIT

echo "[snapshot] collecting major_events snapshot"
echo "[snapshot] compose_file=${COMPOSE_FILE} service=${DB_SERVICE} db=${DB_NAME} user=${DB_USER}"
echo "[snapshot] compose_mode=${COMPOSE_MODE}"

"${COMPOSE_CMD[@]}" -f "${COMPOSE_FILE}" exec -T "${DB_SERVICE}" \
  psql -U "${DB_USER}" -d "${DB_NAME}" -At -F $'\t' -c "
SELECT
  COALESCE(external_id, ''),
  COALESCE(\"type\", ''),
  COALESCE(event_start_date::text, ''),
  COALESCE(event_end_date::text, ''),
  COALESCE(status, ''),
  COALESCE(updated_at::text, '')
FROM major_events
ORDER BY external_id ASC, \"type\" ASC;
" > "${TMP_ROWS}"

{
  printf 'external_id\ttype\tevent_start_date\tevent_end_date\tstatus\tupdated_at\n'
  cat "${TMP_ROWS}"
} > "${SNAPSHOT_PATH}"

ROW_COUNT="$(wc -l < "${TMP_ROWS}" | tr -d '[:space:]')"
CHECKSUM="$(sha256sum "${SNAPSHOT_PATH}" | awk '{print $1}')"
GENERATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

python3 - <<'PY' "${META_PATH}" "${SNAPSHOT_PATH}" "${GENERATED_AT}" "${ROW_COUNT}" "${CHECKSUM}" "${SAFE_LABEL}" "${DB_SERVICE}" "${DB_NAME}" "${DB_USER}"
import json
import sys

(
    meta_path,
    snapshot_path,
    generated_at,
    row_count,
    checksum,
    label,
    db_service,
    db_name,
    db_user,
) = sys.argv[1:10]

payload = {
    "generated_at": generated_at,
    "label": label,
    "snapshot_path": snapshot_path,
    "row_count": int(row_count),
    "sha256": checksum,
    "db": {
        "service": db_service,
        "name": db_name,
        "user": db_user,
    },
    "columns": [
        "external_id",
        "type",
        "event_start_date",
        "event_end_date",
        "status",
        "updated_at",
    ],
}

with open(meta_path, "w", encoding="utf-8") as fp:
    json.dump(payload, fp, ensure_ascii=False, indent=2)
PY

echo "[snapshot] snapshot_tsv=${SNAPSHOT_PATH}"
echo "[snapshot] snapshot_meta=${META_PATH}"
echo "[snapshot] row_count=${ROW_COUNT} sha256=${CHECKSUM}"
