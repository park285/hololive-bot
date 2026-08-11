#!/bin/dash

set -eu
set -f

MODE="${1:-}"
ORIGINAL_COMMAND="${SSH_ORIGINAL_COMMAND:-}"
CURRENT_USER="$(/usr/bin/id -un)"

die() {
  printf '[postgres-failover-ssh-dispatch] %s\n' "$1" >&2
  exit 126
}

is_token() {
  printf '%s\n' "$1" | /usr/bin/grep -Eq '^[A-Za-z0-9._:-]{8,128}$'
}

is_host() {
  printf '%s\n' "$1" | /usr/bin/grep -Eq '^[A-Za-z0-9._:-]+$'
}

is_port() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "${#1}" -le 5 ] && [ "$1" -gt 0 ] && [ "$1" -le 65535 ]
}

is_service() {
  printf '%s\n' "$1" | /usr/bin/grep -Eq '^svc:[a-z0-9][a-z0-9-]{0,62}$'
}

case "${ORIGINAL_COMMAND}" in
  ''|*"
"*|*""*|*"	"*) die "missing or malformed original command" ;;
esac

saved_ifs="${IFS}"
IFS=' '
# glob을 끄고 줄바꿈·탭을 거부한 뒤 고정 명령의 공백 구분 필드만 의도적으로 분리합니다.
# shellcheck disable=SC2086
set -- ${ORIGINAL_COMMAND}
IFS="${saved_ifs}"

case "${MODE}" in
  fence)
    [ "$#" -eq 10 ] \
      && [ "$1" = /usr/bin/sudo ] \
      && [ "$2" = -n ] \
      && [ "$3" = /usr/bin/env ] \
      && [ "$4" = bash ] \
      && [ "$5" = /usr/local/libexec/hololive-postgres-failover/postgres-primary-fence.sh ] \
      || die "unexpected fence command"
    is_token "$6" || die "invalid fence request id"
    is_host "$7" || die "invalid old primary host"
    is_host "$8" || die "invalid new primary host"
    is_port "$9" || die "invalid new primary port"
    is_service "${10}" || die "invalid Tailscale Service"
    [ "${CURRENT_USER}" = hololive-pg-fence ] || die "unexpected fence account"
    exec /usr/bin/sudo -n /usr/bin/env bash \
      /usr/local/libexec/hololive-postgres-failover/postgres-primary-fence.sh \
      "$6" "$7" "$8" "$9" "${10}"
    ;;
  route)
    [ "$#" -eq 13 ] \
      && [ "$1" = /usr/bin/sudo ] \
      && [ "$2" = -n ] \
      && [ "$3" = /usr/bin/env ] \
      && [ "$4" = bash ] \
      && [ "$5" = /usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh ] \
      && [ "$6" = --config ] \
      && [ "$7" = /etc/hololive-postgres-failover/route.env ] \
      || die "unexpected route command"
    is_host "$8" || die "invalid old primary host"
    is_port "$9" || die "invalid old primary port"
    is_host "${10}" || die "invalid new primary host"
    is_port "${11}" || die "invalid new primary port"
    is_token "${12}" || die "invalid fence token"
    is_service "${13}" || die "invalid Tailscale Service"
    [ "${CURRENT_USER}" = hololive-pg-route ] || die "unexpected route account"
    exec /usr/bin/sudo -n /usr/bin/env bash \
      /usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh \
      --config /etc/hololive-postgres-failover/route.env \
      "$8" "$9" "${10}" "${11}" "${12}" "${13}"
    ;;
  *)
    die "unknown dispatch mode"
    ;;
esac
