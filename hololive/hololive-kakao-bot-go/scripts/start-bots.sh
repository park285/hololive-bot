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

# Start bot (integrated: webhook + alarm + YouTube scheduler)
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

if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
  echo "[ERROR] Container CLI not found: ${CONTAINER_CLI}"
  echo "Set CONTAINER_CLI=docker or CONTAINER_CLI=podman"
  exit 1
fi

# Defaults (can be overridden by env or .env)
MIN_COUNT=${CORE_MEMBER_HASH_SOFT_MIN_COUNT:-50}
TIMEOUT_SEC=${CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS:-45}

# Load .env if present
if [[ -f ./.env ]]; then
  set -a; . ./.env; set +a
fi

# Start unified bot
./scripts/start-bot.sh

# Wait for readiness: prefer ready flag, fallback to HLEN threshold
start_ts=$(date +%s)
while true; do
  # Prefer ready flag
  if "${CONTAINER_CLI}" exec holo-valkey valkey-cli EXISTS hololive:members:ready 2>/dev/null | grep -q "^1$"; then
    echo "[READY] hololive:members:ready flag detected"; break
  fi
  # Fallback: HLEN threshold
  count=$("${CONTAINER_CLI}" exec holo-valkey valkey-cli HLEN hololive:members 2>/dev/null | tr -d '') || count=0
  if [[ "$count" =~ ^[0-9]+$ ]] && [ "$count" -ge "$MIN_COUNT" ]; then
    echo "[READY] hololive:members count >= $MIN_COUNT (=$count)"; break
  fi
  now=$(date +%s); elapsed=$((now - start_ts))
  if [ $elapsed -ge $TIMEOUT_SEC ]; then
    echo "[WARN] Readiness not reached in ${TIMEOUT_SEC}s (flag missing, count=$count). Proceeding anyway."
    break
  fi
  sleep 1
done

echo "[OK] Bot started (webhook + alarm + YouTube scheduler)"
