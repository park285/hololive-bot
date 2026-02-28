#!/bin/bash
# Restart Ingestion + Core (order preserved)
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

./scripts/stop-bots.sh || true
./scripts/start-bots.sh "$@"
