# 02. Architecture Refactor Plan

## 구조 리팩토링의 핵심

로그 개선을 하려면 먼저 작업 경계가 명확해야 합니다. 그래서 다음 계층을 강제로 구분합니다.

```text
runtime bootstrap
  -> dependency assembly
  -> router/scheduler/worker construction
  -> runtime lifecycle

domain/application service
  -> 실제 기능 흐름 조율

repository/client
  -> DB, Valkey, external API 접근

transport
  -> HTTP, Iris, queue, outbox 입출력
```

## 공통 리팩토링 규칙

### 1. 하위 계층에서 중복 로그 금지

하위 계층은 error에 context를 담아서 반환하고, 최종 boundary에서 로그를 남깁니다.

예외:
- long-running loop의 heartbeat/summary
- external boundary에서 retry/DLQ 상태가 바뀌는 경우
- 보안상 즉시 확인해야 하는 설정 누락

### 2. 작업 단위는 `RunOperation`으로 감싼다

다음은 반드시 `RunOperation`으로 감쌉니다.

- HTTP request boundary
- command execution
- scheduler loop iteration
- queue batch processing
- provider API request
- outbox claim/write/finalize
- LLM provider call

### 3. 이벤트명은 domain.action.result

예:

```text
bot.command.execute.succeeded
alarm.scheduler.loop.iteration.failed
dispatch.group.send.failed
llm.provider.request.succeeded
youtube.outbox.write.failed
```

### 4. 원문 로그 금지

금지:
- Kakao user message raw
- LLM prompt raw
- LLM response raw
- YouTube/Holodex raw response
- queue payload full JSON
- token/API key/cookie/header
- DB password/connection string

대신:
- length
- hash prefix
- count
- id
- status
- duration
