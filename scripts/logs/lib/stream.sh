ensure_mirror_enabled() {
  local enable_log_mirror="${ENABLE_LOG_MIRROR:-0}"
  if [[ "${enable_log_mirror}" != "1" ]]; then
    echo "log mirror disabled: set ENABLE_LOG_MIRROR=1 to enable" >&2
    exit 0
  fi
}

stream_mirror_positive_int() {
  local value="$1"
  local fallback="$2"

  if [[ "${value}" =~ ^[0-9]+$ ]] && [[ "${value}" -gt 0 ]]; then
    printf '%s\n' "${value}"
    return
  fi

  printf '%s\n' "${fallback}"
}

stream_mirror_non_negative_int() {
  local value="$1"
  local fallback="$2"

  if [[ "${value}" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "${value}"
    return
  fi

  printf '%s\n' "${fallback}"
}

stream_mirror_max_bytes() {
  stream_mirror_positive_int "${STREAM_MIRROR_MAX_BYTES:-10485760}" 10485760
}

stream_mirror_max_rotations() {
  stream_mirror_non_negative_int "${STREAM_MIRROR_MAX_ROTATIONS:-3}" 3
}

stream_mirror_rotate() {
  local log_file="$1"
  local rotations=""
  local i=0

  rotations="$(stream_mirror_max_rotations)"
  if [[ "${rotations}" -eq 0 ]]; then
    rm -f "${log_file}"
    return
  fi

  rm -f "${log_file}.${rotations}"
  for ((i = rotations - 1; i >= 1; i--)); do
    if [[ -e "${log_file}.${i}" ]]; then
      mv "${log_file}.${i}" "${log_file}.$((i + 1))"
    fi
  done

  if [[ -e "${log_file}" ]]; then
    mv "${log_file}" "${log_file}.1"
  fi
}

stream_mirror_append_file() {
  local log_file="$1"
  local chunk_file="$2"
  local max_bytes=""
  local current_size=0
  local chunk_size=0

  mkdir -p "$(dirname "${log_file}")"

  if [[ ! -s "${chunk_file}" ]]; then
    return
  fi

  max_bytes="$(stream_mirror_max_bytes)"
  chunk_size="$(stat -c%s "${chunk_file}" 2>/dev/null || echo 0)"

  if [[ "${chunk_size}" -gt "${max_bytes}" ]]; then
    stream_mirror_rotate "${log_file}"
    tail -c "${max_bytes}" "${chunk_file}" > "${log_file}"
    return
  fi

  if [[ -e "${log_file}" ]]; then
    current_size="$(stat -c%s "${log_file}" 2>/dev/null || echo 0)"
  fi

  if [[ "$((current_size + chunk_size))" -gt "${max_bytes}" ]]; then
    stream_mirror_rotate "${log_file}"
  fi

  cat "${chunk_file}" >> "${log_file}"
}

stream_mirror_append_line() {
  local log_file="$1"
  local line="$2"
  local chunk_file=""

  chunk_file="$(mktemp "$(dirname "${log_file}")/.stream-line.XXXXXX")"
  printf '%s\n' "${line}" > "${chunk_file}"
  stream_mirror_append_file "${log_file}" "${chunk_file}"
  rm -f "${chunk_file}"
}

run_stream_service_worker() {
  local svc="$1"
  local log_root="${REPO_ROOT}/logs"
  local mirror_dir="${log_root}/mirror"
  local pid_dir="${log_root}/runtime/pids"
  local log_file="${mirror_dir}/${svc}.log"
  local since="${STREAM_SINCE:-5m}"
  local chunk_file=""

  mkdir -p "${mirror_dir}" "${pid_dir}"

  while true; do
    chunk_file="$(mktemp "${mirror_dir}/.${svc}.stream.XXXXXX")"
    if ! "${SCRIPT_PATH}" tail "${svc}" --since "${since}" --tail 50 > "${chunk_file}" 2>&1; then
      stream_mirror_append_file "${log_file}" "${chunk_file}"
      rm -f "${chunk_file}"
      stream_mirror_append_line "${log_file}" "$(date '+%Y-%m-%d %H:%M:%S') ${svc}: log stream reconnect"
      sleep 1
    else
      stream_mirror_append_file "${log_file}" "${chunk_file}"
      rm -f "${chunk_file}"
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
  local services=(hololive-api youtube-producer)

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
