# 03. Logging Contract

## 공통 필드

모든 주요 로그는 가능한 한 아래 필드를 사용합니다.

```text
event
runtime
component
operation
request_id
job_id
duration_ms
error_type
error_message
```

## Runtime 필드

```text
bot
alarm-worker
dispatcher-go
llm-scheduler
stream-ingester
youtube-scraper
```

## Component 예시

```text
http
command
lifecycle
scheduler
checker
notifier
queue
outbox
delivery
llm
ingestion
youtube
```

## Error 필드

```text
error_type
error_message
error_code
retryable
```

`stack`은 운영 기본 로그에는 넣지 않습니다. panic 또는 fatal 수준에서만 별도 고려합니다.

## 로그 레벨

```text
debug:
  루프 내부 상세, cache hit/miss, branch detail

info:
  runtime started/stopped
  command succeeded
  scheduler summary
  outbox write succeeded
  dispatch group sent

warn:
  retry scheduled
  DLQ moved
  ACL skip
  degraded dependency
  non-fatal external API failure

error:
  command execution failed
  provider request failed
  DB write failed
  outbox finalize failed
  runtime build failed
```

## 절대 금지 로그

```text
raw
payload
body
message
prompt
response
authorization
cookie
token
api_key
client_secret
password
```

단, 필드 이름이 `message_len`, `message_sha256_8`, `prompt_len`, `prompt_sha256_8`인 경우는 허용합니다.
