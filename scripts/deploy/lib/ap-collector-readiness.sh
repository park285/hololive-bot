#!/usr/bin/env bash

collector_readiness_json_field() {
  local payload="${1-}"
  local field="${2:?field}"
  local value="${3:?value}"
  local pattern
  pattern="(^|[,{[:space:]])\\\"${field}\\\"[[:space:]]*:[[:space:]]*${value}([,}]|[[:space:]])"
  [[ "$payload" =~ $pattern ]]
}

collector_readiness_validate() {
  local payload="${1-}"
  local json_string='("([^"\\]|\\.)*")'
  local json_number='-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?'
  local json_value="${json_string}|true|false|null|${json_number}"
  local json_pair="\"[A-Za-z_][A-Za-z0-9_]*\":[[:space:]]*(${json_value})"
  local json_object="^[[:space:]]*[{]${json_pair}(,[[:space:]]*${json_pair})*[}][[:space:]]*$"

  [[ "$payload" != *$'\n'* ]] || return 1
  [[ "$payload" =~ $json_object ]] || return 1
  collector_readiness_json_field "$payload" status '"ready"' || return 1
  collector_readiness_json_field "$payload" helper '"ok"' || return 1
  collector_readiness_json_field "$payload" first_success true || return 1
  collector_readiness_json_field "$payload" handoff_status '"PROCESSED"' || return 1
  collector_readiness_json_field "$payload" pending_queue '(null|-?[0-9]+)' || return 1
}

collector_readiness_poll() {
  local attempts="${1:?attempts}"
  local delay_seconds="${2:?delay_seconds}"
  local ready=""
  local attempt
  shift 2
  (( $# > 0 )) || return 2

  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if ready="$("$@" 2>/dev/null)" && collector_readiness_validate "$ready"; then
      printf '%s\n' "$ready"
      return 0
    fi
    if (( attempt < attempts )); then
      sleep "$delay_seconds"
    fi
  done
  printf '%s\n' "$ready"
  return 1
}
