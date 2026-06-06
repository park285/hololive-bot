resolve_compose_cmd() {
  COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.prod.yml}"
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
  local resolved

  if ! resolved="$(compose_service_resolve_log_target "${key}")"; then
    echo "ERROR: unknown service: ${key}" >&2
    echo "Available: $(compose_service_log_targets_text)" >&2
    exit 1
  fi

  printf '%s\n' "${resolved}"
}
