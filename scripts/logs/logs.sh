#!/usr/bin/env bash
# Docker Compose 로그 보조 명령 단일 진입점
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SCRIPT_PATH="${BASH_SOURCE[0]}"

declare -Ag SERVICE_MAP=(
  [bot]="hololive-bot"
  [hololive-bot]="hololive-bot"
  [dispatcher]="dispatcher-go"
  [dispatcher-go]="dispatcher-go"
  [ingester]="stream-ingester"
  [stream-ingester]="stream-ingester"
  [llm]="llm-scheduler"
  [llm-scheduler]="llm-scheduler"
)

usage() {
  cat <<USAGE
Usage: ./scripts/logs/logs.sh <command> [args]

Commands:
  query <service> [--since 1h] [--limit 1000] [--grep pattern] [--quiet]
  tail <service> [--since 1h] [--tail 200]
  backfill <service> [--since 24h] [--limit 5000] [--output path] [--stdout]
  dump
  stream <start|stop|status|daemon>
  prune
  canary [options]
  canary-cron
  help
USAGE
}

source "${SCRIPT_DIR}/lib/compose.sh"
source "${SCRIPT_DIR}/lib/query.sh"
source "${SCRIPT_DIR}/lib/stream.sh"
source "${SCRIPT_DIR}/lib/canary.sh"

main() {
  local command="${1:-help}"

  case "${command}" in
    query)
      shift
      cmd_query "$@"
      ;;
    tail)
      shift
      cmd_tail "$@"
      ;;
    backfill)
      shift
      cmd_backfill "$@"
      ;;
    dump)
      shift
      cmd_dump "$@"
      ;;
    stream)
      shift
      cmd_stream "$@"
      ;;
    prune)
      shift
      cmd_prune "$@"
      ;;
    canary)
      shift
      cmd_canary "$@"
      ;;
    canary-cron)
      shift
      cmd_canary_cron "$@"
      ;;
    _stream-worker)
      shift
      ensure_mirror_enabled
      run_stream_service_worker "${1:-}"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      echo "ERROR: unknown command: ${command}" >&2
      usage
      exit 1
      ;;
  esac
}

main "$@"
