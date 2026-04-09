#!/usr/bin/env bash
# Docker Compose 로그 보조 명령 단일 진입점
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SCRIPT_PATH="${BASH_SOURCE[0]}"

declare -Ag SERVICE_MAP=(
  [bot]="hololive-bot"
  [hololive-bot]="hololive-bot"
  [dispatcher]="dispatcher-go"
  [dispatcher-go]="dispatcher-go"
  [ingester]="stream-ingester"
  [stream-ingester]="stream-ingester"
  [llm]="llm-scheduler"
  [llm-scheduler]="llm-scheduler"
)

usage() {
  cat <<'EOF'
Usage: ./scripts/logs/logs.sh <command> [args]

Commands:
  query <service> [--since 1h] [--limit 1000] [--grep pattern] [--quiet]
  tail <service> [--since 1h] [--tail 200]
  backfill <service> [--since 24h] [--limit 5000] [--output path] [--stdout]
  dump
  stream <start|stop|status|daemon>
  prune
  canary [options]
  canary-cron
  help
EOF
}

resolve_compose_cmd() {
  COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
  CONTAINER_CLI="${CONTAINER_CLI:-docker}"

  case "${CONTAINER_CLI}" in
    docker|podman) ;;
    *)
      echo "ERROR: unsupported CONTAINER_CLI: ${CONTAINER_CLI}" >&2
      exit 1
      ;;
  esac

  if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
    echo "ERROR: container CLI not found: ${CONTAINER_CLI}" >&2
    exit 1
  fi

  COMPOSE_CMD=("${CONTAINER_CLI}" "compose")
  COMPOSE_MODE="${CONTAINER_CLI} compose"

  if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=("podman-compose")
    COMPOSE_MODE="podman-compose"
  elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
    if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
      COMPOSE_CMD=("podman-compose")
      COMPOSE_MODE="podman-compose"
    else
      echo "ERROR: '${CONTAINER_CLI} compose' is unavailable" >&2
      exit 1
    fi
  fi
}

resolve_service() {
  local key="$1"
  local resolved="${SERVICE_MAP[${key}]:-}"
  if [[ -z "${resolved}" ]]; then
    echo "ERROR: unknown service: ${key}" >&2
    echo "Available: ${!SERVICE_MAP[*]}" >&2
    exit 1
  fi
  printf '%s\n' "${resolved}"
}

compose_query_output() {
  local service_key="$1"
  local since="$2"
  local limit="$3"
  local grep_pattern="$4"
  local quiet="$5"
  local service_name
  local output

  resolve_compose_cmd
  service_name="$(resolve_service "${service_key}")"

  if [[ "${quiet}" != "true" ]]; then
    echo "query: service=${service_name} since=${since} limit=${limit} mode=${COMPOSE_MODE}" >&2
  fi

  output="$("${COMPOSE_CMD[@]}" -f "${COMPOSE_FILE}" logs \
    --no-color \
    --no-log-prefix \
    --timestamps \
    --since "${since}" \
    --tail "${limit}" \
    "${service_name}" 2>/dev/null || true)"

  if [[ -n "${grep_pattern}" ]]; then
    printf '%s\n' "${output}" | grep -E -- "${grep_pattern}" || true
  else
    printf '%s\n' "${output}"
  fi
}

cmd_query() {
  local service=""
  local since="1h"
  local limit="1000"
  local grep_pattern=""
  local quiet="false"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help)
        usage
        return 0
        ;;
      --since)
        since="${2:-}"
        shift 2
        ;;
      --limit)
        limit="${2:-}"
        shift 2
        ;;
      --grep)
        grep_pattern="${2:-}"
        shift 2
        ;;
      --quiet)
        quiet="true"
        shift
        ;;
      *)
        if [[ -z "${service}" ]]; then
          service="$1"
          shift
        else
          echo "ERROR: unknown arg: $1" >&2
          exit 1
        fi
        ;;
    esac
  done

  if [[ -z "${service}" ]]; then
    echo "ERROR: service is required" >&2
    usage
    exit 1
  fi

  compose_query_output "${service}" "${since}" "${limit}" "${grep_pattern}" "${quiet}"
}

cmd_tail() {
  local service=""
  local since="1h"
  local tail_lines="200"
  local service_name

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help)
        usage
        return 0
        ;;
      --since)
        since="${2:-}"
        shift 2
        ;;
      --tail)
        tail_lines="${2:-}"
        shift 2
        ;;
      *)
        if [[ -z "${service}" ]]; then
          service="$1"
          shift
        else
          echo "ERROR: unknown arg: $1" >&2
          exit 1
        fi
        ;;
    esac
  done

  if [[ -z "${service}" ]]; then
    echo "ERROR: service is required" >&2
    usage
    exit 1
  fi

  resolve_compose_cmd
  service_name="$(resolve_service "${service}")"
  echo "tail: service=${service_name} since=${since} tail=${tail_lines} mode=${COMPOSE_MODE}" >&2
  exec "${COMPOSE_CMD[@]}" -f "${COMPOSE_FILE}" logs \
    --follow \
    --no-color \
    --no-log-prefix \
    --timestamps \
    --since "${since}" \
    --tail "${tail_lines}" \
    "${service_name}"
}

cmd_backfill() {
  local service=""
  local since="24h"
  local limit="5000"
  local output=""
  local stdout_only="false"
  local snapshot_dir="${REPO_ROOT}/logs/backfill"
  local retention_days="${BACKFILL_RETENTION_DAYS:-7}"
  local enable_log_aux_files="${ENABLE_LOG_AUX_FILES:-0}"
  local tmp_file=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help)
        usage
        return 0
        ;;
      --since)
        since="${2:-}"
        shift 2
        ;;
      --limit)
        limit="${2:-}"
        shift 2
        ;;
      --output)
        output="${2:-}"
        shift 2
        ;;
      --stdout)
        stdout_only="true"
        shift
        ;;
      *)
        if [[ -z "${service}" ]]; then
          service="$1"
          shift
        else
          echo "ERROR: unknown arg: $1" >&2
          exit 1
        fi
        ;;
    esac
  done

  if [[ -z "${service}" ]]; then
    echo "ERROR: service is required" >&2
    usage
    exit 1
  fi

  if [[ "${stdout_only}" == "true" ]]; then
    compose_query_output "${service}" "${since}" "${limit}" "" "true"
    return 0
  fi

  if [[ "${enable_log_aux_files}" != "1" ]]; then
    echo "aux log files disabled: set ENABLE_LOG_AUX_FILES=1 to write snapshot files" >&2
    return 0
  fi

  mkdir -p "${snapshot_dir}"
  if [[ -z "${output}" ]]; then
    output="${snapshot_dir}/${service}-$(date +%Y%m%d-%H%M%S).log"
  fi

  tmp_file="$(mktemp "${snapshot_dir}/.${service}.tmp.XXXXXX")"
  trap 'rm -f "${tmp_file}"' EXIT INT TERM
  compose_query_output "${service}" "${since}" "${limit}" "" "true" > "${tmp_file}"
  mv "${tmp_file}" "${output}"
  trap - EXIT INT TERM

  find "${snapshot_dir}" -type f -name '*.log' -mtime +"${retention_days}" -delete >/dev/null 2>&1 || true
  echo "backfill saved: ${output}" >&2
}

cmd_dump() {
  local mirror_dir="${REPO_ROOT}/logs/mirror"
  local since="${DUMP_SINCE:-2h}"
  local limit="${DUMP_LIMIT:-10000}"
  local rotate_bytes=$((100 * 1024 * 1024))
  local retention_days="${DUMP_RETENTION_DAYS:-30}"
  local enable_log_mirror="${ENABLE_LOG_MIRROR:-0}"
  local dump_count=0
  local tmp_file=""
  local log_file=""
  local file_size=0
  local line_count=0
  local svc=""
  local services=(bot dispatcher-go stream-ingester llm-scheduler)

  if [[ "${enable_log_mirror}" != "1" ]]; then
    echo "log mirror disabled: set ENABLE_LOG_MIRROR=1 to enable" >&2
    return 0
  fi

  mkdir -p "${mirror_dir}"

  for svc in "${services[@]}"; do
    log_file="${mirror_dir}/${svc}.log"

    if [[ -f "${log_file}" ]]; then
      file_size="$(stat -c%s "${log_file}" 2>/dev/null || echo 0)"
      if [[ "${file_size}" -gt "${rotate_bytes}" ]]; then
        mv -f "${log_file}" "${log_file}.1"
        echo "$(date '+%Y-%m-%d %H:%M:%S') rotation: ${svc}.log -> ${svc}.log.1 (${file_size} bytes)" >&2
      fi
    fi

    tmp_file="$(mktemp)"
    compose_query_output "${svc}" "${since}" "${limit}" "" "true" > "${tmp_file}"
    line_count="$(wc -l < "${tmp_file}" | xargs)"
    if [[ "${line_count}" -gt 0 ]]; then
      cat "${tmp_file}" >> "${log_file}"
    fi
    rm -f "${tmp_file}"

    dump_count=$((dump_count + line_count))
    echo "$(date '+%Y-%m-%d %H:%M:%S') ${svc}: ${line_count} lines" >&2
  done

  find "${mirror_dir}" -name '*.log.1' -mtime +"${retention_days}" -delete >/dev/null 2>&1 || true
  echo "$(date '+%Y-%m-%d %H:%M:%S') dump complete: total ${dump_count} lines" >&2
}

ensure_mirror_enabled() {
  local enable_log_mirror="${ENABLE_LOG_MIRROR:-0}"
  if [[ "${enable_log_mirror}" != "1" ]]; then
    echo "log mirror disabled: set ENABLE_LOG_MIRROR=1 to enable" >&2
    exit 0
  fi
}

run_stream_service_worker() {
  local svc="$1"
  local log_root="${REPO_ROOT}/logs"
  local mirror_dir="${log_root}/mirror"
  local pid_dir="${log_root}/runtime/pids"
  local log_file="${mirror_dir}/${svc}.log"
  local since="${STREAM_SINCE:-5m}"

  mkdir -p "${mirror_dir}" "${pid_dir}"

  while true; do
    if ! "${SCRIPT_PATH}" tail "${svc}" --since "${since}" --tail 50 >> "${log_file}" 2>&1; then
      echo "$(date '+%Y-%m-%d %H:%M:%S') ${svc}: log stream reconnect" >> "${log_file}"
      sleep 1
    fi
    since="1m"
  done
}

cmd_stream() {
  local command="${1:-}"
  local log_root="${REPO_ROOT}/logs"
  local mirror_dir="${log_root}/mirror"
  local pid_dir="${log_root}/runtime/pids"
  local pid_file=""
  local worker_pid=""
  local name=""
  local pid=""
  local svc=""
  local services=(bot dispatcher-go stream-ingester llm-scheduler)

  case "${command}" in
    start)
      ensure_mirror_enabled
      mkdir -p "${mirror_dir}" "${pid_dir}"
      for svc in "${services[@]}"; do
        pid_file="${pid_dir}/${svc}.pid"
        if [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
          echo "${svc}: already running (PGID $(cat "${pid_file}"))" >&2
          continue
        fi
        setsid "${SCRIPT_PATH}" _stream-worker "${svc}" >/dev/null 2>&1 &
        worker_pid=$!
        echo "${worker_pid}" > "${pid_file}"
        echo "${svc}: started (PGID ${worker_pid}) -> ${mirror_dir}/${svc}.log" >&2
      done
      ;;
    stop)
      if [[ ! -d "${pid_dir}" ]]; then
        echo "no running workers" >&2
        return 0
      fi
      for pid_file in "${pid_dir}"/*.pid; do
        [[ -f "${pid_file}" ]] || continue
        name="$(basename "${pid_file}" .pid)"
        pid="$(cat "${pid_file}")"
        if kill -0 "${pid}" 2>/dev/null; then
          kill -- -"${pid}" 2>/dev/null || kill "${pid}" 2>/dev/null || true
          echo "${name}: stopped (PGID ${pid})" >&2
        fi
        rm -f "${pid_file}"
      done
      ;;
    status)
      if [[ ! -d "${pid_dir}" ]]; then
        echo "no worker running"
        return 0
      fi
      for svc in "${services[@]}"; do
        pid_file="${pid_dir}/${svc}.pid"
        if [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
          echo "${svc}: running (PGID $(cat "${pid_file}"))"
        else
          echo "${svc}: stopped"
        fi
      done
      ;;
    daemon)
      ensure_mirror_enabled
      mkdir -p "${mirror_dir}" "${pid_dir}"
      while true; do
        "${SCRIPT_PATH}" stream start >/dev/null 2>&1 || true
        sleep 10
      done
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

cmd_prune() {
  local log_root="${REPO_ROOT}/logs"
  local backfill_dir="${log_root}/backfill"
  local mirror_dir="${log_root}/mirror"
  local archive_dir="${log_root}/archive"
  local cron_dir="${log_root}/cron"
  local canary_dir="${log_root}/canary"
  local pid_dir="${log_root}/runtime/pids"
  local backfill_retention_days="${BACKFILL_RETENTION_DAYS:-7}"
  local aux_retention_days="${AUX_RETENTION_DAYS:-30}"
  local archive_retention_days="${ARCHIVE_RETENTION_DAYS:-${LOG_MAX_AGE_DAYS:-30}}"
  local pid_file=""
  local pid=""

  if [[ -d "${backfill_dir}" ]]; then
    find "${backfill_dir}" -type f -name '*.log' -mtime +"${backfill_retention_days}" -delete >/dev/null 2>&1 || true
  fi
  if [[ -d "${mirror_dir}" ]]; then
    find "${mirror_dir}" -type f -name '*.log.1' -mtime +"${aux_retention_days}" -delete >/dev/null 2>&1 || true
  fi
  if [[ -d "${archive_dir}" ]]; then
    find "${archive_dir}" -type f -name '*.gz' -mtime +"${archive_retention_days}" -delete >/dev/null 2>&1 || true
  fi
  if [[ -d "${cron_dir}" ]]; then
    find "${cron_dir}" -type f -name '*.log*' -mtime +"${aux_retention_days}" -delete >/dev/null 2>&1 || true
  fi
  if [[ -d "${canary_dir}" ]]; then
    find "${canary_dir}" -type f -name '*.log*' -mtime +"${aux_retention_days}" -delete >/dev/null 2>&1 || true
  fi

  if [[ -d "${pid_dir}" ]]; then
    for pid_file in "${pid_dir}"/*.pid; do
      [[ -f "${pid_file}" ]] || continue
      pid="$(cat "${pid_file}" 2>/dev/null || true)"
      if [[ -z "${pid}" ]] || ! kill -0 "${pid}" 2>/dev/null; then
        rm -f "${pid_file}"
      fi
    done
  fi

  echo "prune complete: backfill>${backfill_retention_days}d archive>${archive_retention_days}d aux>${aux_retention_days}d"
}

cmd_canary() {
  local service="stream-ingester"
  local since="30m"
  local limit="5000"
  local warn_failure_rate="0.10"
  local max_aggregate_failures="0"
  local max_enqueue_failures="0"
  local min_delivery_claimed="1"
  local raw_logs=""
  local summary=""
  local failure_rate="0.000000"
  local warn=0

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --since)
        since="${2:-}"
        shift 2
        ;;
      --limit)
        limit="${2:-}"
        shift 2
        ;;
      --service)
        service="${2:-}"
        shift 2
        ;;
      --warn-failure-rate)
        warn_failure_rate="${2:-}"
        shift 2
        ;;
      --max-aggregate-failures)
        max_aggregate_failures="${2:-}"
        shift 2
        ;;
      --max-enqueue-failures)
        max_enqueue_failures="${2:-}"
        shift 2
        ;;
      --min-delivery-claimed)
        min_delivery_claimed="${2:-}"
        shift 2
        ;;
      -h|--help)
        usage
        return 0
        ;;
      *)
        echo "ERROR: unknown arg: $1" >&2
        exit 1
        ;;
    esac
  done

  raw_logs="$(compose_query_output \
    "${service}" \
    "${since}" \
    "${limit}" \
    "Outbox per-room (enqueue completed|dispatch completed)" \
    "true")"

  if [[ -z "${raw_logs}" ]]; then
    echo "NO_DATA: no per-room logs found (service=${service}, since=${since})"
    return 2
  fi

  summary="$(printf '%s\n' "${raw_logs}" | awk '
function value(line, key,   n,i,a,p) {
  n=split(line, a, " ")
  for (i=1; i<=n; i++) {
    if (index(a[i], key "=") == 1) {
      p=index(a[i], "=")
      return substr(a[i], p+1) + 0
    }
  }
  return 0
}
{
  if (index($0, "Outbox per-room enqueue completed") > 0) {
    enqueue_count++
    enqueue_outbox_claimed += value($0, "outbox_claimed")
    enqueue_outbox_enqueued += value($0, "outbox_enqueued")
    enqueue_no_subscribers += value($0, "outbox_no_subscribers")
    enqueue_failures += value($0, "enqueue_failures")
    enqueue_target_rooms += value($0, "target_rooms")
  }
  if (index($0, "Outbox per-room dispatch completed") > 0) {
    dispatch_count++
    dispatch_claimed += value($0, "delivery_claimed")
    dispatch_sent += value($0, "delivery_sent")
    dispatch_failed += value($0, "delivery_failed")
    dispatch_outbox_touched += value($0, "outbox_touched")
    dispatch_aggregate_failures += value($0, "aggregate_failures")
  }
}
END {
  printf("enqueue_count=%d\n", enqueue_count)
  printf("enqueue_outbox_claimed=%d\n", enqueue_outbox_claimed)
  printf("enqueue_outbox_enqueued=%d\n", enqueue_outbox_enqueued)
  printf("enqueue_no_subscribers=%d\n", enqueue_no_subscribers)
  printf("enqueue_failures=%d\n", enqueue_failures)
  printf("enqueue_target_rooms=%d\n", enqueue_target_rooms)
  printf("dispatch_count=%d\n", dispatch_count)
  printf("dispatch_claimed=%d\n", dispatch_claimed)
  printf("dispatch_sent=%d\n", dispatch_sent)
  printf("dispatch_failed=%d\n", dispatch_failed)
  printf("dispatch_outbox_touched=%d\n", dispatch_outbox_touched)
  printf("dispatch_aggregate_failures=%d\n", dispatch_aggregate_failures)
}')"

  eval "${summary}"

  if [[ "${dispatch_claimed}" -gt 0 ]]; then
    failure_rate="$(awk -v f="${dispatch_failed}" -v c="${dispatch_claimed}" 'BEGIN { printf "%.6f", f/c }')"
  fi

  echo "=== Outbox Per-Room Canary Summary ==="
  echo "service=${service} since=${since} limit=${limit}"
  echo "enqueue_count=${enqueue_count} claimed=${enqueue_outbox_claimed} enqueued=${enqueue_outbox_enqueued} no_subscribers=${enqueue_no_subscribers} enqueue_failures=${enqueue_failures} target_rooms=${enqueue_target_rooms}"
  echo "dispatch_count=${dispatch_count} claimed=${dispatch_claimed} sent=${dispatch_sent} failed=${dispatch_failed} outbox_touched=${dispatch_outbox_touched} aggregate_failures=${dispatch_aggregate_failures}"
  echo "delivery_failure_rate=${failure_rate} (warn_threshold=${warn_failure_rate})"
  echo "thresholds: min_delivery_claimed=${min_delivery_claimed} max_aggregate_failures=${max_aggregate_failures} max_enqueue_failures=${max_enqueue_failures}"

  if [[ "${dispatch_count}" -eq 0 ]]; then
    echo "WARN: no dispatch summary logs found"
    warn=1
  fi
  if [[ "${dispatch_aggregate_failures}" -gt "${max_aggregate_failures}" ]]; then
    echo "WARN: aggregate_failures too high (${dispatch_aggregate_failures} > ${max_aggregate_failures})"
    warn=1
  fi
  if [[ "${enqueue_failures}" -gt "${max_enqueue_failures}" ]]; then
    echo "WARN: enqueue_failures too high (${enqueue_failures} > ${max_enqueue_failures})"
    warn=1
  fi
  if [[ "${dispatch_claimed}" -lt "${min_delivery_claimed}" ]]; then
    echo "WARN: insufficient delivery sample (${dispatch_claimed} < ${min_delivery_claimed})"
    warn=1
  fi
  if awk -v fr="${failure_rate}" -v th="${warn_failure_rate}" -v c="${dispatch_claimed}" -v mc="${min_delivery_claimed}" 'BEGIN { exit !((c >= mc) && (fr > th)) }'; then
    echo "WARN: delivery failure rate too high (${failure_rate} > ${warn_failure_rate}, claimed=${dispatch_claimed})"
    warn=1
  fi

  return "${warn}"
}

cmd_canary_cron() {
  local cron_dir="${REPO_ROOT}/logs/cron"
  local canary_dir="${REPO_ROOT}/logs/canary"
  local summary_log="${canary_dir}/outbox-per-room-canary.log"
  local enable_log_aux_files="${ENABLE_LOG_AUX_FILES:-0}"
  local since="${OUTBOX_CANARY_SINCE:-30m}"
  local limit="${OUTBOX_CANARY_LIMIT:-5000}"
  local warn_failure_rate="${OUTBOX_CANARY_WARN_FAILURE_RATE:-0.10}"
  local service="${OUTBOX_CANARY_SERVICE:-stream-ingester}"
  local max_aggregate_failures="${OUTBOX_CANARY_MAX_AGGREGATE_FAILURES:-0}"
  local max_enqueue_failures="${OUTBOX_CANARY_MAX_ENQUEUE_FAILURES:-0}"
  local min_delivery_claimed="${OUTBOX_CANARY_MIN_DELIVERY_CLAIMED:-10}"
  local allow_no_data="${OUTBOX_CANARY_ALLOW_NO_DATA:-true}"
  local result=""
  local exit_code=0

  if [[ "${enable_log_aux_files}" != "1" ]]; then
    echo "aux log files disabled: set ENABLE_LOG_AUX_FILES=1 to enable canary log files" >&2
    return 0
  fi

  mkdir -p "${cron_dir}" "${canary_dir}"

  set +e
  result="$("${SCRIPT_PATH}" canary \
    --service "${service}" \
    --since "${since}" \
    --limit "${limit}" \
    --warn-failure-rate "${warn_failure_rate}" \
    --max-aggregate-failures "${max_aggregate_failures}" \
    --max-enqueue-failures "${max_enqueue_failures}" \
    --min-delivery-claimed "${min_delivery_claimed}" 2>&1)"
  exit_code=$?
  set -e

  if [[ "${exit_code}" -eq 2 && "${allow_no_data}" == "true" ]]; then
    result="NO_DATA allowed: service=${service} since=${since}"
    exit_code=0
  fi

  printf '%s [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "outbox-per-room-canary" "${result}" >> "${summary_log}"

  if [[ "${exit_code}" -ne 0 ]]; then
    echo "WARN: outbox per-room canary check returned ${exit_code}" >&2
  fi

  return "${exit_code}"
}

main() {
  local command="${1:-help}"

  case "${command}" in
    query)
      shift
      cmd_query "$@"
      ;;
    tail)
      shift
      cmd_tail "$@"
      ;;
    backfill)
      shift
      cmd_backfill "$@"
      ;;
    dump)
      shift
      cmd_dump "$@"
      ;;
    stream)
      shift
      cmd_stream "$@"
      ;;
    prune)
      shift
      cmd_prune "$@"
      ;;
    canary)
      shift
      cmd_canary "$@"
      ;;
    canary-cron)
      shift
      cmd_canary_cron "$@"
      ;;
    _stream-worker)
      shift
      ensure_mirror_enabled
      run_stream_service_worker "${1:-}"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      echo "ERROR: unknown command: ${command}" >&2
      usage
      exit 1
      ;;
  esac
}

main "$@"
