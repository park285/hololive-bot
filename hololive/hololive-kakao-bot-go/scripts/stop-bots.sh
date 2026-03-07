#!/bin/bash
# Copyright (c) 2025 Kapu
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

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
