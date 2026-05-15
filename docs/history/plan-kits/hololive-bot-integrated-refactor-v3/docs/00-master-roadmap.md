# 00. Master Roadmap

## 이 문서의 위치

이 문서는 전체 리팩토링의 상위 계획입니다. 로그 개선은 별도 작업이 아니라 전체 구조 리팩토링 안에 포함됩니다.

## 최종 목표

현재 `hololive-bot`은 여러 Go runtime으로 분리되어 있습니다. 이 상태에서 가장 큰 위험은 다음입니다.

1. runtime별 책임이 코드 안에서 다시 섞이는 것
2. 같은 실패가 여러 계층에서 중복 로깅되는 것
3. 원문 메시지, prompt, API 응답, token류가 로그에 남는 것
4. scheduler/dispatcher/outbox 작업의 단위 추적이 어려운 것
5. `youtube-scraper`, `stream-ingester`가 다른 서버에 있어 메인 서버에서 로그 확인이 불편한 것

따라서 이번 작업은 다음을 동시에 달성해야 합니다.

- 구조 리팩토링
- 로그 이벤트 표준화
- context/job id 전파
- 원격 runtime 로그를 메인 서버 `/logs`로 통합
- admin-api 제외
- 테스트와 guardrail 추가

## 전체 PR 구성

### PR-01. shared logging foundation

`shared-go/pkg/logging`에 다음 기능을 추가합니다.

- `event` 필드
- `runtime`, `component`
- `request_id`, `job_id`
- `duration_ms`
- error 표준 필드
- `RunOperation`
- sanitize 강화

### PR-02. HTTP request context

`hololive-shared/pkg/server/middleware`에서 request id를 `gin.Context`뿐 아니라 `request.Context()`에도 넣습니다.

### PR-03. bot command flow

`hololive-kakao-bot-go/internal/bot`에서 다음 구조로 분리합니다.

```text
MessageIngress
  - 메시지 처리 가능 여부 판단
  - self sender skip
  - ACL skip
  - command parse
  - command received event

CommandRouter
  - command normalize
  - command execute started/succeeded/failed
  - unknown command 처리

BotMessageHandler
  - panic recovery
  - sync/async 분기
  - 사용자 오류 응답 전송
```

### PR-04. bot lifecycle

시작/종료/의존성 준비 로그를 event 기반으로 바꾸고, `Valkey`, `Iris`, `Postgres` 상태를 따로 남깁니다.

### PR-05. alarm-worker scheduler

현재 platform loop를 다음 구조로 관측합니다.

```text
loop tick
  -> target minutes sync
  -> platform check
  -> notification dispatch
  -> summary
```

### PR-06. alarm-worker egress/outbox

`alarm-worker`가 proactive egress owner이므로, final send/outbox claim/delivery state는 여기서 기록합니다.

### PR-07. dispatcher-go

legacy dispatcher라도 render/send/ack/retry/dlq/quarantine 실패를 분리합니다.

### PR-08. llm-scheduler

LLM prompt 원문을 남기지 않고, prompt length/hash/model/provider/result count만 남깁니다.

### PR-09. ingestion/youtube-scraper

`stream-ingester`와 `youtube-scraper`가 같은 module을 쓰더라도 runtime role은 로그 필드로 반드시 구분합니다.

### PR-10. remote /logs mirror

다른 서버에서 실행 중인 `youtube-scraper`, `stream-ingester` 로그를 메인 서버 `/logs`에 mirror합니다.

### PR-11. guardrails

- admin 수정 금지
- 민감 로그 grep
- non-admin Go test
- architecture gate
