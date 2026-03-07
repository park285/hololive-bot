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
