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

