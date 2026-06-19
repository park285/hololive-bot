#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"

AP_HOST_ARG="${1:-}"
SERVICE="${2:-all}"
SINCE="${SINCE:-30m}"
TAIL="${TAIL:-300}"
PATTERN="${PATTERN:-}"
FOLLOW="${FOLLOW:-0}"
FULL="${FULL:-0}"
SOURCE="${SOURCE:-docker}"

usage() {
  cat <<'USAGE'
Usage:
  ap-logs.sh <ap-host> [youtube-producer|<ap-service>|all]

Environment:
  SINCE=30m              docker log since window
  TAIL=300               fallback/file tail line count
  PATTERN='ERR|panic'    optional grep -E pattern
  FOLLOW=1               follow docker logs
  FULL=1                 print all available logs; ignores SINCE and TAIL
  SOURCE=docker|file     read docker logs or /home/ubuntu/hololive-bot/logs/*.log

Examples:
  ap-logs.sh osaka youtube-producer
  SINCE=2h PATTERN='pre-send claim|ERR' ap-logs.sh seoul youtube-producer
  FOLLOW=1 ap-logs.sh osaka all
  FULL=1 ap-logs.sh seoul all
  SOURCE=file TAIL=500 ap-logs.sh osaka all
USAGE
}

if [[ "$AP_HOST_ARG" == "-h" || "$AP_HOST_ARG" == "--help" || "$AP_HOST_ARG" == "help" || -z "$AP_HOST_ARG" ]]; then
  usage
  [[ -n "$AP_HOST_ARG" ]] && exit 0
  exit 2
fi

ap_host_load "$REPO_ROOT" "$AP_HOST_ARG"

services=()
case "$SERVICE" in
  youtube-producer|all)
    services=("${AP_SERVICES[@]}")
    ;;
  *)
    for candidate in "${AP_SERVICES[@]}"; do
      if [[ "$candidate" == "$SERVICE" ]]; then
        services=("$SERVICE")
        break
      fi
    done
    if [[ ${#services[@]} -eq 0 ]]; then
      echo "unknown service for $AP_NAME: $SERVICE" >&2
      echo "Available: youtube-producer ${AP_SERVICES[*]} all" >&2
      usage >&2
      exit 2
    fi
    ;;
esac

ap_container_for_service() {
  local service="$1"
  local i
  for i in "${!AP_SERVICES[@]}"; do
    if [[ "${AP_SERVICES[$i]}" == "$service" ]]; then
      printf '%s\n' "${AP_CONTAINERS[$i]}"
      return 0
    fi
  done
  return 1
}

for service in "${services[@]}"; do
  echo "== $service =="
  if [[ "$SOURCE" == "file" ]]; then
    logfile="logs/youtube-producer.log"
    if [[ "$FULL" == "1" ]]; then
      ap_remote_bash "$logfile" "$PATTERN" <<'REMOTE'
set -euo pipefail
logfile="$1"; pattern="$2"
cd ~/hololive-bot
if [[ -n "$pattern" ]]; then
  sudo -n cat "$logfile" | grep -E "$pattern" || true
else
  sudo -n cat "$logfile"
fi
REMOTE
      echo
      continue
    fi
    ap_remote_bash "$logfile" "$PATTERN" "$TAIL" <<'REMOTE'
set -euo pipefail
logfile="$1"; pattern="$2"; tail_n="$3"
cd ~/hololive-bot
if [[ -n "$pattern" ]]; then
  sudo -n tail -n "$tail_n" "$logfile" | grep -E "$pattern" || true
else
  sudo -n tail -n "$tail_n" "$logfile"
fi
REMOTE
  else
    container=$(ap_container_for_service "$service")
    ap_remote_bash "$FOLLOW" "$FULL" "$SINCE" "$TAIL" "$PATTERN" "$container" <<'REMOTE'
set -euo pipefail
follow="$1"; full="$2"; since="$3"; tail_n="$4"; pattern="$5"; container="$6"
args=(logs)
if [[ "$follow" == "1" ]]; then args+=(-f); fi
if [[ "$full" != "1" ]]; then args+=(--since "$since" --tail "$tail_n"); fi
if [[ -n "$pattern" ]]; then
  docker "${args[@]}" "$container" 2>&1 | grep -E "$pattern" || true
else
  docker "${args[@]}" "$container" 2>&1
fi
REMOTE
  fi
  echo
done
