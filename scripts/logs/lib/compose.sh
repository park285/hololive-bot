# shellcheck shell=bash
resolve_service() {
  local key="$1"
  local resolved

  if ! resolved="$(compose_service_resolve_log_target "${key}")"; then
    echo "ERROR: unknown service: ${key}" >&2
    echo "Available: $(compose_service_log_targets_text)" >&2
    exit 1
  fi

  printf '%s\n' "${resolved}"
}
