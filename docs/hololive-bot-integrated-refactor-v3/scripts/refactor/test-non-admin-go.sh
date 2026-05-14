#!/usr/bin/env bash
set -euo pipefail

go test ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
