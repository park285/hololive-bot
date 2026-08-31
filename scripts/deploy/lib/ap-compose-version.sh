#!/usr/bin/env bash

ap_compose_read_semver() {
  local file="$1"
  local label="$2"
  local -a lines=()
  local semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

  [[ -f "$file" ]] || {
    echo "$label file not found: $file" >&2
    return 1
  }
  mapfile -t lines < "$file"
  [[ "${#lines[@]}" -eq 1 && -n "${lines[0]}" ]] || {
    echo "$label must contain exactly one non-empty line: $file" >&2
    return 1
  }
  [[ "${lines[0]}" =~ $semver_re ]] || {
    echo "$label must use strict MAJOR.MINOR.PATCH SemVer: ${lines[0]}" >&2
    return 1
  }
  printf '%s\n' "${lines[0]}"
}

ap_compose_release_version() {
  local repo_root="$1"

  ap_compose_read_semver \
    "$repo_root/hololive/hololive-api/VERSION" \
    hololive-api/VERSION
}

compose_alarm_worker_release_version() {
  local repo_root="$1"

  ap_compose_read_semver \
    "$repo_root/hololive/hololive-alarm-worker/VERSION" \
    hololive-alarm-worker/VERSION
}

compose_export_release_versions() {
  local repo_root="$1"
  local api_version alarm_worker_version

  api_version="$(ap_compose_release_version "$repo_root")" || return 1
  alarm_worker_version="$(compose_alarm_worker_release_version "$repo_root")" || return 1

  if [[ -n "${HOLO_API_VERSION:-}" && "$HOLO_API_VERSION" != "$api_version" ]]; then
    echo "HOLO_API_VERSION does not match hololive-api/VERSION" >&2
    return 1
  fi
  if [[ -n "${HOLO_ALARM_WORKER_VERSION:-}" && "$HOLO_ALARM_WORKER_VERSION" != "$alarm_worker_version" ]]; then
    echo "HOLO_ALARM_WORKER_VERSION does not match hololive-alarm-worker/VERSION" >&2
    return 1
  fi

  HOLO_API_VERSION="$api_version"
  HOLO_ALARM_WORKER_VERSION="$alarm_worker_version"
  export HOLO_API_VERSION HOLO_ALARM_WORKER_VERSION
}
