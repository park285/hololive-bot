# Service: alarm-worker

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-alarm-worker` |
| Binary | `alarm-worker` |
| Compose service | `hololive-alarm-worker` |
| Port | `30007` |
| Health endpoint | `https://127.0.0.1:30007/health` over H3 |
| Ready endpoint | `https://127.0.0.1:30007/ready`; diagnostic `https://127.0.0.1:30007/internal/ready` with `X-API-Key` |

## Role

Alarm checker/scheduler, alarm HTTP provider, alarm dispatch queue publishing/consumption, generic notification delivery outbox consumption, YouTube outbox dispatch, proactive notification egress를 담당합니다.

## Owns

- Alarm HTTP provider route registration for `/internal/alarm/*` during the staged provider migration
- Alarm checking and scheduling loops
- Dispatch queue publish path
- Dispatch queue consume/render/send path, serialized by PostgreSQL `FOR UPDATE SKIP LOCKED` row claims and the single Compose instance
- Generic `notification_delivery_outbox` consume/send path for major event/member news notification rows
- Alarm state cache warming and mutation coordination where configured
- Pending `youtube_notification_outbox` claim/render/send under the `youtube_delivery` profile executor
- v1 YouTube outbox의 optional `shadow|cutover` v3 handoff; claim owner는 기존 dispatcher로 유지
- Birthday and anniversary celebration production. Birthday stream delivery audience is derived from sent deliveries of the matching birthday greeting event; it does not fall back to every alarm room.

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Alarm HTTP provider | internal HTTP JSON | `/internal/alarm/*` | `bot`, `admin-api` facade |
| Alarm dispatch egress | PostgreSQL table | `alarm_dispatch_deliveries` | Iris/Kakao via alarm-worker egress |
| Notification delivery outbox | PostgreSQL table | `notification_delivery_outbox` | Iris/Kakao via alarm-worker egress |
| YouTube outbox dispatch | PostgreSQL table | `youtube_notification_outbox` | Iris/Kakao via alarm-worker egress |
| Alarm service state | in-process domain service | `domain.AlarmCRUD` | local scheduler/checker and alarm HTTP provider |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | alarm/member/channel state and notification delivery outbox | alarm evaluation, alarm HTTP CRUD/query, or generic notification delivery fails |
| PostgreSQL YouTube outbox | claim, render, per-room delivery, and final send state | YouTube notification dispatch pauses |
| Valkey | queue, cache, Pub/Sub | dispatch publishing and config updates fail |
| Settings Pub/Sub | config update handling | runtime settings may become stale |

## Must not own

- YouTube collection, owned by `youtube-collector`
- YouTube canonical persist and notification intent, owned by `hololive-api` YouTube plane
- Kakao command parsing, owned by `bot`
- LLM summary generation, owned by `llm-scheduler`

## Startup requirements

- PostgreSQL and Valkey availability
- `NOTIFICATION_SCHEDULER_ROLE=worker` in the current deployment; the production validator accepts `worker|off`, and Compose pins `worker` so the single instance always runs the alarm checker/scheduler
- A single running instance: proactive egress exclusivity comes from PostgreSQL `FOR UPDATE SKIP LOCKED` row claims plus the Compose `container_name`/fixed host port, not from a Valkey lease
- `STACK_WORKER_PROFILE_FILE=/run/hololive-bot/worker-profiles/alarm-worker.json`
- production profile executors `alarm_dispatch`, `notification_delivery`, and `youtube_delivery` enabled
- `YOUTUBE_OUTBOX_V3_HANDOFF_MODE=off` until an approved shadow/cutover procedure is executed
- Alarm timing/config env
- `BIRTHDAY_STREAM_RUNNER_ENABLED=true` only after the birthday stream template is present and full-roster producer discovery has been verified

## Shutdown behavior

- Stop the alarm HTTP listener gracefully.
- Stop scheduler/checker loops gracefully.
- Stop dispatch queue and YouTube outbox consumers during shutdown.

Runtime은 scheduler·egress·celebration·birthday stream·설정 subscriber를 같은 취소 및 종료 대기 경계에서 관리합니다. 부모가 살아 있을 때 자식의 자체 timeout은 runtime 오류로 전달합니다. 종료 기한을 넘겨도 HTTP와 alarm service cleanup은 호출하며, 작업 대기 실패와 cleanup 오류를 함께 반환합니다. Subscriber의 연결 오류는 기존 log-only 정책을 유지합니다.

부모 종료에 따른 순수 context 오류만 정상 종료로 처리합니다. 취소와 실제 오류가 함께 반환되면 오류 채널 또는 ERROR 로그에 원인을 남기며, 종료 중 소비되지 않는 오류 채널 때문에 작업이 멈추지 않도록 합니다.

Karing은 전역 전송 슬롯 획득을 `BeginSending` 전에 끝내며, admission과 실제 sender·handoff polling은 각각 기존 `DeliverySendTimeout` 한도를 사용합니다. 부모의 전체 기한은 계속 적용됩니다. admission 실패는 sender 호출이 없다는 증거로 기존 retryable 전이를 사용하고, 실제 호출 뒤의 불명확한 결과는 SENDING을 보존합니다.

Alarm dispatch payload/전달 문맥 복원 실패와 event 누락은 PostgreSQL worker fence 및 rows-affected 검증으로 DLQ 전이가 확인된 뒤 해당 delivery의 dedup 키를 해제합니다. 전이 실패·정상 전달·retry·결과 불명에서는 이 해제를 수행하지 않습니다.

Event 조회나 복원·거절 정리 실패로 배치를 반환하지 못하면, 확정된 DLQ를 제외한 미발송 lease를 기존 `ReleaseLeased`로 반환합니다. 부분 발송 입력은 반환하지 않으며 attempt·send-unit·미발송 dedup 키를 유지합니다. 정리는 요청 취소와 독립된 최대 5초로 제한하고, 정리 실패도 원래 오류와 함께 반환합니다. 이미 terminal이거나 다른 worker가 소유한 row는 DB fence가 보호합니다.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker`
- Health: `https://127.0.0.1:30007/health`
- Ready: `https://127.0.0.1:30007/ready`; authenticated `/diagnostics/workers` reports profile match, executors, and real queue snapshots.
- Queue: `alarm_dispatch_deliveries`; Valkey `alarm:dispatch:wakeup` is not backlog authority.
- Metrics: `hololive_youtube_outbox_v3_handoff_total`, `hololive_delivery_outbox_v3_handoff_total`, alarm-dispatch backlog/retention metrics

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/alarm-worker.md`
- YouTube egress lifecycle ownership decision (planned): `../architecture/youtube-egress-lifecycle-transition-ownership-20260831.md`
- YouTube egress lifecycle normative contract (planned): `../architecture/youtube-egress-lifecycle-contract-20260831.md`
- YouTube egress logical delivery ledger and backfill gate: `../architecture/youtube-egress-logical-delivery-ledger-20260831.md`
- YouTube egress DB commit adjudication: `../architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md`
- YouTube egress direct-versus-library review: `../architecture/youtube-egress-lifecycle-library-review-20260831.md`
- YouTube egress lifecycle implementation plan: `../plans/2026-08-31-youtube-egress-lifecycle-implementation.md`
