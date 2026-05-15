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
  local services=(bot stream-ingester llm-scheduler)

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
