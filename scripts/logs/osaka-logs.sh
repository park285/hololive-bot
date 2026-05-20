#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
. "$REPO_ROOT/scripts/deploy/lib/compose-services.sh"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
SERVICE="${1:-all}"
SINCE="${SINCE:-30m}"
TAIL="${TAIL:-300}"
PATTERN="${PATTERN:-}"
FOLLOW="${FOLLOW:-0}"
FULL="${FULL:-0}"
SOURCE="${SOURCE:-docker}"
SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")

usage() {
  cat <<'USAGE'
Usage:
  osaka-logs.sh [youtube-producer|all]

Environment:
  SINCE=30m              docker log since window
  TAIL=300               fallback/file tail line count
  PATTERN='ERR|panic'    optional grep -E pattern
  FOLLOW=1               follow docker logs
  FULL=1                 print all available logs; ignores SINCE and TAIL
  SOURCE=docker|file     read docker logs or /home/ubuntu/hololive-bot/logs/*.log

Examples:
  osaka-logs.sh youtube-producer
  SINCE=2h PATTERN='pre-send claim|ERR' osaka-logs.sh youtube-producer
  FOLLOW=1 osaka-logs.sh all
  FULL=1 osaka-logs.sh all
  SOURCE=file TAIL=500 osaka-logs.sh all
USAGE
}

if [[ "$SERVICE" == "-h" || "$SERVICE" == "--help" || "$SERVICE" == "help" ]]; then
  usage
  exit 0
fi

services_output="$(compose_service_resolve_osaka_log_targets "$SERVICE")" || {
  echo "unknown service: $SERVICE" >&2
  echo "Available: $(compose_service_osaka_log_targets_text)" >&2
  usage >&2
  exit 2
}
mapfile -t services <<<"$services_output"

remote() {
  "${SSH_OSAKA[@]}" "$@"
}

for service in "${services[@]}"; do
  echo "== $service =="
  if [[ "$SOURCE" == "file" ]]; then
    logfile=$(compose_service_resolve_osaka_log_file "$service")
    if [[ "$FULL" == "1" ]]; then
      if [[ -n "$PATTERN" ]]; then
        remote "cd ~/hololive-bot && cat '$logfile' | grep -E '$PATTERN' || true"
      else
        remote "cd ~/hololive-bot && cat '$logfile'"
      fi
      echo
      continue
    fi
    if [[ -n "$PATTERN" ]]; then
      remote "cd ~/hololive-bot && tail -n '$TAIL' '$logfile' | grep -E '$PATTERN' || true"
    else
      remote "cd ~/hololive-bot && tail -n '$TAIL' '$logfile'"
    fi
  else
    container=$(compose_service_resolve_osaka_container "$service")
    follow_args=()
    range_args=(--since "$SINCE" --tail "$TAIL")
    if [[ "$FOLLOW" == "1" ]]; then
      follow_args=(-f)
    fi
    if [[ "$FULL" == "1" ]]; then
      range_args=()
    fi
    if [[ -n "$PATTERN" ]]; then
      remote "docker logs ${follow_args[*]} ${range_args[*]} '$container' 2>&1 | grep -E '$PATTERN' || true"
    else
      remote "docker logs ${follow_args[*]} ${range_args[*]} '$container' 2>&1"
    fi
  fi
  echo
done
