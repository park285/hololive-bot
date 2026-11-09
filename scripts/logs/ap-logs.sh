#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"

AP_HOST_ARG="${1:-}"
SERVICE="${2:-all}"
TAIL="${TAIL:-300}"
PATTERN="${PATTERN:-}"
FULL="${FULL:-0}"

usage() {
  cat <<'USAGE'
Usage:
  ap-logs.sh <ap-host> [youtube-collector|<ap-service>|all]

Reads the native AP file logs.

Environment:
  TAIL=300               tail line count
  PATTERN='ERR|panic'    optional grep -E pattern
  FULL=1                 print the whole log file; ignores TAIL

Examples:
  ap-logs.sh osaka youtube-collector
  PATTERN='pre-send claim|ERR' ap-logs.sh osaka youtube-collector
  FULL=1 ap-logs.sh osaka all
  TAIL=500 ap-logs.sh osaka all
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
  youtube-collector|all)
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
      echo "Available: youtube-collector ${AP_SERVICES[*]} all" >&2
      usage >&2
      exit 2
    fi
    ;;
esac

# native AP는 collector 로그를 /var/log/hololive-bot/<service>.log로 남긴다.
for service in "${services[@]}"; do
  echo "== $service =="
  logfile="/var/log/hololive-bot/${service}.log"
  if [[ "$FULL" == "1" ]]; then
    ap_remote_bash "$logfile" "$PATTERN" <<'REMOTE'
set -euo pipefail
logfile="$1"; pattern="$2"
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
if [[ -n "$pattern" ]]; then
  sudo -n tail -n "$tail_n" "$logfile" | grep -E "$pattern" || true
else
  sudo -n tail -n "$tail_n" "$logfile"
fi
REMOTE
  echo
done
