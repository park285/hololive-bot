#!/bin/bash
# Stop Ingestion bot gracefully
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

stop_one() {
  local name="$1" pidfile="$2"
  if [[ -f "$pidfile" ]]; then
    local pid=$(cat "$pidfile" 2>/dev/null || echo "")
    if [[ -n "$pid" ]] && ps -p "$pid" >/dev/null 2>&1; then
      echo "[STOP] $name (PID $pid)"
      kill "$pid" || true
      for i in {1..10}; do
        if ps -p "$pid" >/dev/null 2>&1; then sleep 1; else break; fi
      done
      if ps -p "$pid" >/dev/null 2>&1; then
        echo "[WARN] $name not exiting, sending SIGKILL"
        kill -9 "$pid" || true
      fi
    else
      echo "[INFO] $name not running or stale PID"
    fi
    rm -f "$pidfile"
  else
    echo "[INFO] $name PID file not found ($pidfile)"
  fi
}

stop_one "Bot" ".bot.pid"

echo "[OK] Stop sequence completed"
