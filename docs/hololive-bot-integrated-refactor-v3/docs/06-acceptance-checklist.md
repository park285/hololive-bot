# 06. Acceptance Checklist

## Scope

```text
[ ] hololive-admin-api 수정 없음
[ ] admin-dashboard 수정 없음
```

## Logging foundation

```text
[ ] shared-go/pkg/logging에 Event/Runtime/Component/RequestID/JobID/DurationMS/ErrorAttrs 추가
[ ] RunOperation 추가
[ ] sanitize handler가 nested group도 처리
[ ] token/password/authorization/cookie/api_key/client_secret 마스킹
```

## HTTP

```text
[ ] X-Request-ID가 response header에 남음
[ ] request.Context()에 request_id가 들어감
[ ] HTTP log event가 http.request.completed로 통일됨
```

## Bot

```text
[ ] command received 로그에 raw message 없음
[ ] self sender skip 로그에 payload 없음
[ ] unknown command 로그에 msg 원문 없음
[ ] command execute failed 로그가 중복되지 않음
[ ] command success/failure에 duration_ms 있음
```

## Alarm worker

```text
[ ] loop iteration마다 job_id 있음
[ ] platform check 실패와 dispatch 실패가 분리됨
[ ] sent/skipped/failed count가 summary로 남음
```

## Dispatcher

```text
[ ] render/send/mark_sending/mark_dispatched 실패가 분리됨
[ ] retry scheduled와 DLQ moved가 분리됨
[ ] quarantine 이벤트가 별도임
```

## LLM

```text
[ ] prompt 원문 로그 없음
[ ] provider response 원문 로그 없음
[ ] prompt_len/prompt_sha256_8/model/provider 남음
```

## Ingestion

```text
[ ] runtime=stream-ingester/youtube-scraper 구분
[ ] youtube-scraper는 outbox production까지만 기록
[ ] alarm-worker가 final egress 기록
```

## Remote logs

```text
[ ] /logs/youtube-scraper.log가 remote/osaka/youtube-scraper.log를 가리킴
[ ] /logs/stream-ingester.log가 remote/osaka/stream-ingester.log를 가리킴
[ ] systemd timer로 자동 동기화됨
[ ] 동기화 실패 시 docker-tail fallback 가능
```
