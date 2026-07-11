# shellcheck shell=bash

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

canary_log_file_for_service() {
  local service="$1"

  case "${service}" in
    alarm-worker|hololive-alarm-worker)
      printf '%s\n' "${REPO_ROOT}/logs/alarm-worker.log"
      ;;
    youtube-producer-c)
      printf '%s\n' "${REPO_ROOT}/logs/youtube-producer-c.log"
      ;;
    youtube-producer)
      printf '%s\n' "${REPO_ROOT}/logs/youtube-producer.log"
      ;;
    *)
      return 1
      ;;
  esac
}

canary_since_cutoff() {
  local since="$1"
  local value=""
  local unit=""

  if [[ "${since}" =~ ^([0-9]+)([smhd])$ ]]; then
    value="${BASH_REMATCH[1]}"
    unit="${BASH_REMATCH[2]}"
    case "${unit}" in
      s) unit="seconds" ;;
      m) unit="minutes" ;;
      h) unit="hours" ;;
      d) unit="days" ;;
    esac
    date -d "-${value} ${unit}" '+%Y-%m-%dT%H:%M:%S%:z' 2>/dev/null || true
  fi
}

canary_file_query_output() {
  local service="$1"
  local since="$2"
  local limit="$3"
  local grep_pattern="$4"
  local log_file=""
  local cutoff=""

  log_file="$(canary_log_file_for_service "${service}" 2>/dev/null || true)"
  [[ -n "${log_file}" && -r "${log_file}" ]] || return 0

  cutoff="$(canary_since_cutoff "${since}")"
  if [[ -n "${cutoff}" ]]; then
    awk -v cutoff="${cutoff}" '$1 >= cutoff { print }' "${log_file}" | tail -n "${limit}" | grep -E -- "${grep_pattern}" || true
  else
    tail -n "${limit}" "${log_file}" | grep -E -- "${grep_pattern}" || true
  fi
}

cmd_canary() {
  local service="alarm-worker"
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
  local enqueue_count=0
  local enqueue_outbox_claimed=0
  local enqueue_outbox_enqueued=0
  local enqueue_no_subscribers=0
  local enqueue_failures=0
  local enqueue_target_rooms=0
  local dispatch_count=0
  local dispatch_claimed=0
  local dispatch_sent=0
  local dispatch_failed=0
  local dispatch_outbox_touched=0
  local dispatch_aggregate_failures=0

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

  local grep_pattern="Outbox per-room (enqueue completed|dispatch completed)"

  raw_logs="$(compose_query_output \
    "${service}" \
    "${since}" \
    "${limit}" \
    "${grep_pattern}" \
    "true")"
  if [[ -z "${raw_logs}" ]]; then
    raw_logs="$(canary_file_query_output "${service}" "${since}" "${limit}" "${grep_pattern}")"
  fi

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

  while IFS='=' read -r key value; do
    if [[ ! "${value}" =~ ^[0-9]+$ ]]; then
      echo "ERROR: invalid canary summary value: ${key}=${value}" >&2
      return 1
    fi
    case "${key}" in
      enqueue_count) enqueue_count="${value}" ;;
      enqueue_outbox_claimed) enqueue_outbox_claimed="${value}" ;;
      enqueue_outbox_enqueued) enqueue_outbox_enqueued="${value}" ;;
      enqueue_no_subscribers) enqueue_no_subscribers="${value}" ;;
      enqueue_failures) enqueue_failures="${value}" ;;
      enqueue_target_rooms) enqueue_target_rooms="${value}" ;;
      dispatch_count) dispatch_count="${value}" ;;
      dispatch_claimed) dispatch_claimed="${value}" ;;
      dispatch_sent) dispatch_sent="${value}" ;;
      dispatch_failed) dispatch_failed="${value}" ;;
      dispatch_outbox_touched) dispatch_outbox_touched="${value}" ;;
      dispatch_aggregate_failures) dispatch_aggregate_failures="${value}" ;;
      *)
        echo "ERROR: unexpected canary summary key: ${key}" >&2
        return 1
        ;;
    esac
  done <<<"${summary}"

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
  local service="${OUTBOX_CANARY_SERVICE:-alarm-worker}"
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
