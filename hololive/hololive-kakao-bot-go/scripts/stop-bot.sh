#!/bin/bash
# Hololive KakaoTalk Bot (Go) 종료 스크립트 v1.0

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

PID_FILE=".bot.pid"

echo "[STOP] Stopping Hololive KakaoTalk Bot (Go)..."

if [ ! -f "$PID_FILE" ]; then
  echo "[INFO] No PID file found, searching for process..."

  PIDS=$(pgrep -f "bin/bot" 2>/dev/null | while read pid; do
    dir=$(readlink -f /proc/$pid/cwd 2>/dev/null || echo "")
    if [ "$dir" = "$PROJECT_ROOT" ]; then
      echo "$pid"
    fi
  done)

  if [ -z "$PIDS" ]; then
    echo "[OK] No bot process found (already stopped)"
    exit 0
  else
    echo "[WARN] Found running process: $PIDS"
    echo "Attempting to stop..."
  fi
else
  PIDS=$(cat "$PID_FILE")

  if ! ps -p "$PIDS" > /dev/null 2>&1; then
    echo "[WARN] Process $PIDS not running (stale PID file)"
    rm -f "$PID_FILE"
    echo "[OK] Cleaned up PID file"
    exit 0
  fi
fi

echo "Found bot process: $PIDS"

echo "Sending SIGTERM for graceful shutdown..."
for pid in $PIDS; do
  kill "$pid" 2>/dev/null || true
done

# 최대 15초 대기
for i in {1..15}; do
  sleep 1
  STILL_RUNNING=0
  for pid in $PIDS; do
    if ps -p "$pid" > /dev/null 2>&1; then
      STILL_RUNNING=1
    fi
  done

  if [ "$STILL_RUNNING" -eq 0 ]; then
    echo "[OK] Bot stopped gracefully"
    rm -f "$PID_FILE"
    exit 0
  fi
  echo "Waiting for shutdown... ($i/15)"
done

# === 3. 강제 종료 (SIGKILL) ===
echo "[WARN] Graceful shutdown timeout, forcing kill..."
for pid in $PIDS; do
  kill -9 "$pid" 2>/dev/null || true
done
sleep 1

STILL_RUNNING=0
for pid in $PIDS; do
  if ps -p "$pid" > /dev/null 2>&1; then
    STILL_RUNNING=1
    echo "[ERROR] Failed to stop PID: $pid"
  fi
done

if [ "$STILL_RUNNING" -eq 0 ]; then
  echo "[OK] Bot force killed"
  rm -f "$PID_FILE"
  exit 0
else
  echo "[ERROR] Some processes could not be stopped"
  exit 1
fi
