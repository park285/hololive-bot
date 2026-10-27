# Contract: trigger

## Summary

운영자가 major event·member news 정기 알림을 같은 주·월 안에서 한 번 더 적재하는 운영 CLI 계약입니다. HTTP 경로는 없습니다. CLI는 실행 중인 `hololive-api` container 안에서 같은 설정과 자원 조립으로 llm plane scheduler만 만들어 정기 실행과 같은 코드 경로를 호출합니다.

## Contract ID

- `trigger.manual`

## Provider

- Service: `hololive-api` (llm plane scheduler)
- Entry point: `./bin/hololive-api --rerun-notification=<kind>` (`cmd/hololive-api`)
- Implementation: `hololive/hololive-api/internal/planes/llm/runtime` (`LLMSchedulerRuntime.RerunNotification`), `hololive/hololive-api/internal/app` (`RerunNotification`)

## Consumers

- 운영자 수동 실행(runbook `runbooks/hololive-api.md`의 LLM plane 장애 절차)

## Invocation

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api \
  ./bin/hololive-api --rerun-notification=majorevent-weekly
```

| kind | 호출하는 scheduler |
|---|---|
| `majorevent-weekly` | major event 주간 알림 |
| `majorevent-monthly` | major event 월간 알림 |
| `membernews-weekly` | member news 주간 digest |

## Result

stdout에 `notification.Result` JSON 한 줄을 씁니다. 실제 delivery/handoff 완료를 뜻하지 않습니다.

```json
{"status":"queued","queued":2,"skipped":0,"failed":0}
{"status":"skipped","queued":0,"skipped":3,"failed":0}
{"status":"partial","queued":2,"skipped":1,"failed":1}
```

- `queued`: 방별 durable intent 적재가 수락된 수입니다. 기존 intent에 대한 멱등 no-op도 포함하며 현재 행이 PENDING인지, 이미 발송됐는지는 증명하지 않습니다.
- `skipped`: 구독 멤버가 없어 처리하지 않은 방의 수입니다. 방·이벤트가 없거나 membernews 실행 락을 얻지 못한 경우도 `status=skipped`이며 방 집계는 0일 수 있습니다.
- `failed`: 적재 처리 실패 또는 실행 취소로 시작하지 못한 방의 수입니다. 실패한 DB 호출만으로 부수효과가 없었다고 추정하지 않습니다.
- 일부 방 적재가 성공하고 일부 실패하면 `partial`을 그대로 보고합니다. 전체 실행을 자동 재시도하지 않습니다. Major event의 완료 표시는 모든 방 적재가 성공한 경우에만 진행합니다.

## Exit codes

| Exit | Meaning | Operator behavior |
|---:|---|---|
| 0 | 오류 없이 끝남. 일부 방 실패는 `status=partial`과 `failed`로 보임 | 결과 JSON 확인 |
| 1 | 설정·자원 조립 실패, scheduler 미구성, 알 수 없는 kind, 전부 실패, 5분 시한 초과. stderr에 원인 | 원인 수정 뒤 재실행 판단. 시한 초과는 일부 적재가 이미 됐을 수 있으므로 outbox를 먼저 확인 |
| 2 | 같은 주·월 major event 알림이 이미 실행 중(`ErrNotificationInProgress`) | 실행 중인 작업이 끝난 뒤 다시 판단 |

## Timeout and retry policy

- CLI 한 번의 상한은 5분입니다. signal로 취소하지 않고 시한으로만 끊습니다(방별 적재가 중간에 끊기면 일부 방만 적재되기 때문).
- Retry: 자동 재시도는 없습니다. partial 적재와 시한 초과도 마찬가지입니다.
- Idempotency: 정기 실행과 같은 실행 lock과 outbox `(kind, content_id)` uniqueness/rearm 계약을 그대로 씁니다. 같은 주·월 key로 다시 실행하면 이미 적재된 방은 멱등하게 `queued`로 셉니다.
- Digest execution is cooperatively canceled after at most 14 minutes, before the existing 15-minute lock TTL; the CLI's 5-minute deadline wins. Lock release remains bounded to 5 seconds independently of caller cancellation.
- Cancellation is not joining or fencing: an external call that ignores cancellation can outlive the TTL.

## Compatibility policy

- 이전의 llm plane `/internal/trigger/*` HTTP 경로와 admin `/api/holo/majorevent/*trigger`·`/api/holo/settings/llm` run-now는 삭제했습니다. 운영 재실행은 이 CLI 하나입니다.
- 새 kind를 추가하면 이 문서, `CONTRACT_MAP.md`, `CONTRACT_MANIFEST.txt`, `RerunNotification`을 함께 갱신합니다.

## Tests

- CLI exit mapping: `hololive/hololive-api/cmd/hololive-api/main_test.go` (`TestRunNotificationRerunMapsOutcomes`)
- Kind dispatch guard: `hololive/hololive-api/internal/planes/llm/runtime/rerun_notification_test.go`
- Budget/cancel-then-join: `hololive/hololive-api/internal/planes/llm/internal/schedulerkit/digest_lifetime_test.go`
