# Hololive Bot Integrated Refactor Plan v3

이 키트는 `park285/hololive-bot`의 Go runtime 리팩토링과 로그/관측성 개선을 하나로 합친 작업 세트입니다.

## 범위

포함:
- `shared-go`
- `hololive/hololive-shared`
- `hololive/hololive-kakao-bot-go`
- `hololive/hololive-alarm-worker`
- `hololive/hololive-dispatcher-go`
- `hololive/hololive-llm-sched`
- `hololive/hololive-stream-ingester`

제외:
- `hololive/hololive-admin-api`
- `admin-dashboard`

## 핵심 원칙

1. 로그 개선은 전체 리팩토링의 하위 작업입니다. 로그만 고치지 않습니다.
2. runtime ownership을 지킵니다.
   - bot: Kakao/Iris ingress + command routing
   - alarm-worker: alarm checker/scheduler + proactive egress
   - dispatcher-go: legacy queue consumer
   - llm-scheduler: major event/member news/LLM summary + notification intent
   - stream-ingester: photo sync + ingestion-adjacent runtime
   - youtube-scraper: YouTube scraping/polling + outbox production
3. admin-api는 수정하지 않습니다.
4. 기존 `/logs` 파일 기반 운영 방식을 유지합니다.
5. Osaka 등 원격 서버의 로그도 메인 서버 `/logs`에서 보이게 합니다.

## 권장 PR 순서

1. PR-01 `shared-go/pkg/logging` 관측성 기반 확장
2. PR-02 HTTP request context 전파 및 HTTP 로그 표준화
3. PR-03 bot ingress/command flow 리팩토링
4. PR-04 bot lifecycle/dependency boundary 정리
5. PR-05 alarm-worker scheduler loop 리팩토링
6. PR-06 alarm-worker notification egress/outbox boundary 정리
7. PR-07 dispatcher-go delivery flow 리팩토링
8. PR-08 llm-scheduler summary/intent flow 리팩토링
9. PR-09 stream-ingester/youtube-scraper ingestion flow 리팩토링
10. PR-10 메인 서버 `/logs` remote mirror 운영 적용
11. PR-11 guardrail/test/architecture gate 보강

## 바로 시작하기

```bash
# PR-01
find code/shared-go/pkg/logging -type f -name '*.go.txt' -exec sh -c '
  for src do
    dst="${src#code/}"
    dst="${dst%.txt}"
    mkdir -p "$(dirname "$dst")"
    cp "$src" "$dst"
  done
' sh {} +
gofmt -w shared-go/pkg/logging
go test ./shared-go/pkg/logging

# PR-10, 메인 서버에서 원격 로그를 /logs로 mirror
cp scripts/logs/remote-sync-main-logs.sh scripts/logs/remote-sync-main-logs.sh
chmod +x scripts/logs/remote-sync-main-logs.sh
LOG_ROOT=/logs ./scripts/logs/remote-sync-main-logs.sh once osaka
```

자세한 작업 문서는 `docs/`와 `llm-tasks/`를 보세요.
