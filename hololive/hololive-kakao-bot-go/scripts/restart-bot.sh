#!/bin/bash
# Hololive KakaoTalk Bot (Go) 재시작 스크립트 v1.0

cd "$(dirname "$0")/.." || exit 1

echo "[RESTART] Restarting Hololive KakaoTalk Bot (Go)..."

# 종료
./scripts/stop-bot.sh

# 빌드 옵션
if [ "$1" == "--build" ] || [ "$1" == "-b" ]; then
  echo "[BUILD] Building..."
  CGO_ENABLED=0 go build -tags go_json -o bin/bot ./cmd/bot || exit 1
fi

# 시작
./scripts/start-bot.sh
