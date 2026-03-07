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

# Show status of Ingestion bot (with integrated alarm checker)
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"

case "${CONTAINER_CLI}" in
  docker|podman) ;;
  *)
    echo "[ERROR] Unsupported CONTAINER_CLI: ${CONTAINER_CLI}"
    echo "Allowed values: docker, podman"
    exit 1
    ;;
esac

status_one() {
  local name="$1" pidfile="$2"
  if [[ -f "$pidfile" ]]; then
    local pid=$(cat "$pidfile" 2>/dev/null || echo "")
    if [[ -n "$pid" ]] && ps -p "$pid" >/dev/null 2>&1; then
      echo "[RUNNING] $name (PID $pid)"
    else
      echo "[STOPPED] $name (stale PID file)"; rm -f "$pidfile" || true
    fi
  else
    echo "[STOPPED] $name"
  fi
}

status_one "Bot" ".bot.pid"

# Optional: member readiness (requires container `holo-valkey`)
if command -v "${CONTAINER_CLI}" >/dev/null 2>&1 && "${CONTAINER_CLI}" ps 2>/dev/null | grep -q "holo-valkey"; then
  flag=$("${CONTAINER_CLI}" exec holo-valkey valkey-cli EXISTS hololive:members:ready 2>/dev/null | tr -d '\r' || echo 0)
  count=$("${CONTAINER_CLI}" exec holo-valkey valkey-cli HLEN hololive:members 2>/dev/null | tr -d '\r' || echo 0)
  if [[ "$flag" == "1" ]]; then echo "[READY] hololive:members:ready set"; else echo "[READY] flag not set"; fi
  echo "[COUNT] hololive:members = $count"
fi
