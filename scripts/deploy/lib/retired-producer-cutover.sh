#!/usr/bin/env bash

retired_producer_container_names() {
  printf '%s\n' \
    hololive-youtube-producer \
    hololive-youtube-producer-a \
    hololive-youtube-producer-b \
    hololive-youtube-producer-c \
    hololive-youtube-producer-d
}

retired_producer_unit_names() {
  local service="${1:-}"
  printf '%s\n' \
    hololive-youtube-producer.service
  if [[ -n "$service" ]]; then
    printf '%s\n' "hololive-youtube-producer@${service}.service"
  fi
  printf '%s\n' \
    hololive-youtube-producer@youtube-collector-a.service \
    hololive-youtube-producer@youtube-collector-b.service \
    hololive-youtube-producer@youtube-collector-c.service \
    hololive-youtube-producer@youtube-collector-d.service \
    hololive-youtube-producer@youtube-producer-a.service \
    hololive-youtube-producer@youtube-producer-b.service \
    hololive-youtube-producer@youtube-producer-c.service \
    hololive-youtube-producer@youtube-producer-d.service
}

stop_retired_producer_containers() {
  local container_cli="${CONTAINER_CLI:-docker}"
  local container_name=""
  local container_id=""

  while IFS= read -r container_name; do
    [[ -n "$container_name" ]] || continue
    container_id="$("${container_cli}" ps -aq --filter "name=^${container_name}$" 2>/dev/null || true)"
    if [[ -z "$container_id" ]]; then
      continue
    fi
    echo "[CUTOVER] Stopping leftover producer container: ${container_name}"
    "${container_cli}" stop "$container_name" >/dev/null
  done < <(retired_producer_container_names)
}

stop_retired_producer_units() {
  local service="${1:-}"
  local unit=""
  command -v systemctl >/dev/null 2>&1 || return 0

  while IFS= read -r unit; do
    [[ -n "$unit" ]] || continue
    if ! systemctl cat "$unit" >/dev/null 2>&1; then
      continue
    fi
    echo "[CUTOVER] Disabling leftover producer unit: ${unit}"
    sudo -n systemctl disable --now "$unit" >/dev/null
  done < <(retired_producer_unit_names "$service" | awk 'NF && !seen[$0]++')
}

stop_retired_producer_runtime() {
  stop_retired_producer_containers
  stop_retired_producer_units "${1:-}"
}

retired_producer_unit_allowed() {
  local service="${1:-}"
  local candidate="${2:-}"
  local unit=""

  while IFS= read -r unit; do
    if [[ "$unit" == "$candidate" ]]; then
      return 0
    fi
  done < <(retired_producer_unit_names "$service" | awk 'NF && !seen[$0]++')
  return 1
}

retired_producer_container_allowed() {
  local candidate="${1:-}"
  local container_name=""

  while IFS= read -r container_name; do
    if [[ "$container_name" == "$candidate" ]]; then
      return 0
    fi
  done < <(retired_producer_container_names)
  return 1
}

write_retired_producer_runtime_state() {
  local service="${1:-}"
  local container_cli="${CONTAINER_CLI:-docker}"
  local unit=""
  local container_name=""

  while IFS= read -r unit; do
    [[ -n "$unit" ]] || continue
    systemctl cat "$unit" >/dev/null 2>&1 || continue
    if systemctl is-enabled --quiet "$unit" 2>/dev/null; then
      printf 'unit-enabled %s\n' "$unit"
    fi
    if systemctl is-active --quiet "$unit" 2>/dev/null; then
      printf 'unit-active %s\n' "$unit"
    fi
  done < <(retired_producer_unit_names "$service" | awk 'NF && !seen[$0]++')

  command -v "$container_cli" >/dev/null 2>&1 || return 0
  while IFS= read -r container_name; do
    [[ -n "$container_name" ]] || continue
    if [[ -n "$("$container_cli" ps -q --filter "name=^${container_name}$" 2>/dev/null || true)" ]]; then
      printf 'container-active %s\n' "$container_name"
    fi
  done < <(retired_producer_container_names)
}

validate_retired_producer_runtime_state() {
  local state_file="${1:?state file is required}"
  local service="${2:-}"
  local state=""
  local name=""
  local extra=""

  [[ -r "$state_file" ]] || {
    echo "retired producer runtime state is not readable: $state_file" >&2
    return 1
  }
  while read -r state name extra; do
    [[ -n "$state" ]] || continue
    if [[ -n "$extra" || -z "$name" ]]; then
      echo "invalid retired producer runtime state entry" >&2
      return 1
    fi
    case "$state" in
      unit-enabled|unit-active)
        retired_producer_unit_allowed "$service" "$name" || {
          echo "unapproved retired producer unit in state: $name" >&2
          return 1
        }
        ;;
      container-active)
        retired_producer_container_allowed "$name" || {
          echo "unapproved retired producer container in state: $name" >&2
          return 1
        }
        ;;
      *)
        echo "invalid retired producer runtime state type: $state" >&2
        return 1
        ;;
    esac
  done < "$state_file"
}

require_named_containers_inactive() {
  local container_cli="${CONTAINER_CLI:-docker}"
  local name=""

  command -v "$container_cli" >/dev/null 2>&1 || return 0
  for name in "$@"; do
    [[ -n "$name" ]] || continue
    if [[ -n "$("$container_cli" ps -q --filter "name=^${name}$" 2>/dev/null || true)" ]]; then
      echo "collector container still active: $name" >&2
      return 1
    fi
  done
}

stop_named_containers_and_require_inactive() {
  local container_cli="${CONTAINER_CLI:-docker}"
  local name=""

  command -v "$container_cli" >/dev/null 2>&1 || return 0
  for name in "$@"; do
    [[ -n "$name" ]] || continue
    if [[ -n "$("$container_cli" ps -q --filter "name=^${name}$" 2>/dev/null || true)" ]]; then
      echo "[CUTOVER] Stopping collector container: ${name}"
      "$container_cli" stop "$name" >/dev/null
    fi
  done
  require_named_containers_inactive "$@"
}

require_named_units_inactive() {
  local unit=""

  command -v systemctl >/dev/null 2>&1 || return 0
  for unit in "$@"; do
    [[ -n "$unit" ]] || continue
    systemctl cat "$unit" >/dev/null 2>&1 || continue
    if systemctl is-active --quiet "$unit" 2>/dev/null; then
      echo "collector unit still active: $unit" >&2
      return 1
    fi
  done
}

stop_named_units_and_require_inactive() {
  local unit=""

  command -v systemctl >/dev/null 2>&1 || return 0
  for unit in "$@"; do
    [[ -n "$unit" ]] || continue
    if ! systemctl cat "$unit" >/dev/null 2>&1; then
      continue
    fi
    echo "[CUTOVER] Stopping collector unit: ${unit}"
    sudo -n systemctl disable --now "$unit" >/dev/null
  done
  require_named_units_inactive "$@"
}

restore_retired_producer_runtime() {
  local state_file="${1:?state file is required}"
  local service="${2:-}"
  local container_cli="${CONTAINER_CLI:-docker}"
  local state=""
  local name=""

  validate_retired_producer_runtime_state "$state_file" "$service"
  while read -r state name; do
    [[ -n "$state" ]] || continue
    case "$state" in
      unit-enabled)
        echo "[CUTOVER] Re-enabling retired producer unit: ${name}"
        sudo -n systemctl enable "$name" >/dev/null
        ;;
      unit-active)
        echo "[CUTOVER] Restarting retired producer unit: ${name}"
        sudo -n systemctl start "$name"
        ;;
      container-active)
        echo "[CUTOVER] Restarting retired producer container: ${name}"
        "$container_cli" start "$name" >/dev/null
        ;;
    esac
  done < "$state_file"
  retired_producer_runtime_matches_state "$state_file" "$service"
}

retired_producer_runtime_matches_state() {
  local state_file="${1:?state file is required}"
  local service="${2:-}"
  local container_cli="${CONTAINER_CLI:-docker}"
  local state=""
  local name=""

  validate_retired_producer_runtime_state "$state_file" "$service"
  while read -r state name; do
    [[ -n "$state" ]] || continue
    case "$state" in
      unit-enabled) systemctl is-enabled --quiet "$name" ;;
      unit-active) systemctl is-active --quiet "$name" ;;
      container-active)
        [[ -n "$("$container_cli" ps -q --filter "name=^${name}$" 2>/dev/null || true)" ]]
        ;;
    esac
  done < "$state_file"
}
