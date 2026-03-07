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
