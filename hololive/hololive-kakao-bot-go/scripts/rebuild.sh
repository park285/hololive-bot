#!/bin/bash
# Hololive KakaoTalk Bot (Go) 재빌드 스크립트 v1.0

set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "[REBUILD] Rebuilding Hololive KakaoTalk Bot (Go)..."

# Clean build cache
echo "[CLEAN] Cleaning build cache..."
go clean -cache

# Build with optimizations
echo "[BUILD] Building optimized binary (static + stripped + netgo)..."
time CGO_ENABLED=0 go build -tags netgo,go_json -ldflags="-s -w" -o bin/bot ./cmd/bot

# Check binary
if [ -f "bin/bot" ]; then
  SIZE=$(du -h bin/bot | cut -f1)
  echo "[OK] Build successful"
  echo "   Binary: bin/bot ($SIZE)"
else
  echo "[ERROR] Build failed"
  exit 1
fi

# Restart option
if [ "$1" == "--restart" ] || [ "$1" == "-r" ]; then
  echo ""
  ./scripts/restart-bot.sh
fi
