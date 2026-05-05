#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
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
  osaka-logs.sh [youtube-scraper|stream-ingester|all]

Environment:
  SINCE=30m              docker log since window
  TAIL=300               fallback/file tail line count
  PATTERN='ERR|panic'    optional grep -E pattern
  FOLLOW=1               follow docker logs
  FULL=1                 print all available logs; ignores SINCE and TAIL
  SOURCE=docker|file     read docker logs or /home/ubuntu/hololive-bot/logs/*.log

Examples:
  osaka-logs.sh youtube-scraper
  SINCE=2h PATTERN='pre-send claim|ERR' osaka-logs.sh youtube-scraper
  FOLLOW=1 osaka-logs.sh all
  FULL=1 osaka-logs.sh all
  SOURCE=file TAIL=500 osaka-logs.sh all
USAGE
}

case "$SERVICE" in
  youtube|scraper|youtube-scraper) services=(youtube-scraper) ;;
  stream|ingester|stream-ingester) services=(stream-ingester) ;;
  all) services=(youtube-scraper stream-ingester) ;;
  -h|--help|help) usage; exit 0 ;;
  *) echo "unknown service: $SERVICE" >&2; usage >&2; exit 2 ;;
esac

container_for() {
  case "$1" in
    youtube-scraper) printf '%s\n' hololive-youtube-scraper ;;
    stream-ingester) printf '%s\n' hololive-stream-ingester ;;
  esac
}

file_for() {
  case "$1" in
    youtube-scraper) printf '%s\n' logs/youtube-scraper.log ;;
    stream-ingester) printf '%s\n' logs/stream-ingester.log ;;
  esac
}

remote() {
  "${SSH_OSAKA[@]}" "$@"
}

for service in "${services[@]}"; do
  echo "== $service =="
  if [[ "$SOURCE" == "file" ]]; then
    logfile=$(file_for "$service")
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
    container=$(container_for "$service")
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
