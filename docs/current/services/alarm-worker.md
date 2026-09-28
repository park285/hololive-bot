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
- YouTube alarm checking and scheduling loops
- Dispatch queue publish path
- Dispatch queue consume/render/send path, serialized by PostgreSQL `FOR UPDATE SKIP LOCKED` row claims and the single Compose instance
- Generic `notification_delivery_outbox` consume/send path for major event/member news notification rows
- Alarm state cache warming and mutation coordination where configured
- Pending `youtube_notification_outbox` claim/render/send under the `youtube_delivery` profile executor
- v1 YouTube outbox는 v3 ledger로 넘기지 않고 `youtube_delivery` dispatcher가 direct egress까지 소유 (`DEC-20260926-hololive-outbox-v3-convergence`로 `shadow|cutover` handoff 삭제)
- Birthday and anniversary celebration production. Birthday stream delivery audience is derived from sent deliveries of the matching birthday greeting event; it does not fall back to every alarm room.

긴 여러 항목 알림(쇼츠·영상·커뮤니티 묶음, 여러 방송 알람)은 `BOT_SEE_MORE_FOLD`(기본 true)에 따라 공통 머리 문단 전체보기 정책으로 렌더합니다. 단일 알림·상태·오류는 제외하고, 사용자 지정 본문/채널 override 저장값과 펼친 가시 문자는 보존합니다. 설정은 bot·llm과 같은 `settings.LoadSeeMoreFold`를 사용하되 실제 렌더 경로에 명시적으로 전달합니다.

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
| Valkey | dispatch wakeup, cache, member epoch Pub/Sub | dispatch wakeup and cache coordination degrade |
| `settings.json` + `PUT /internal/alarm/settings` | alarm advance minutes restore at startup and runtime apply from `hololive-api` | a failed apply keeps the previous target minutes until re-apply or restart |

## Must not own

- YouTube collection, owned by `youtube-collector`
- YouTube canonical persist and notification intent, owned by `hololive-api` YouTube plane
- Kakao command parsing, owned by `bot`
- LLM summary generation, owned by `llm-scheduler`

## Startup requirements

`DEC-20260926-youtube-only-stream-providers`에 따라 Chzzk·Twitch client와 조회 루프, platform mapping 동기화는 제거했습니다. 관련 자격 증명과 `ALARM_TWITCH_ENABLED`를 소비하지 않습니다. 보관된 비유튜브 방송 envelope는 전송 전에 기존 실패 경로로 거부하며 YouTube URL로 바꾸지 않습니다. 기존 retry 상한과 DLQ 전이는 유지합니다. X Spaces·celebration·digest 알림과 YouTube 상태 소유권은 별도 계약대로 유지합니다.

- PostgreSQL and Valkey availability
- `NOTIFICATION_SCHEDULER_ROLE=worker` in the current deployment; the production validator accepts `worker|off`, and Compose pins `worker` so the single instance always runs the alarm checker/scheduler
- A single running instance: proactive egress exclusivity comes from PostgreSQL `FOR UPDATE SKIP LOCKED` row claims plus the Compose `container_name`/fixed host port, not from a Valkey lease
- `STACK_WORKER_PROFILE_FILE=/run/hololive-bot/worker-profiles/alarm-worker.json`
- production profile executors `alarm_dispatch`, `notification_delivery`, and `youtube_delivery` enabled; v1 `youtube_delivery` and v2 `notification_delivery` are canonical direct-egress pipelines, not v3 handoff sources (`DEC-20260926-hololive-outbox-v3-convergence`)
- no `YOUTUBE_OUTBOX_V3_HANDOFF_MODE` or `DELIVERY_OUTBOX_V3_HANDOFF_MODE` key in the runtime env; the retired guard rejects the key even with an empty value
- Alarm timing/config env
- `BIRTHDAY_STREAM_RUNNER_ENABLED=true` only after the birthday stream template is present and full-roster producer discovery has been verified

## Shutdown behavior

- Stop the alarm HTTP listener gracefully.
- Stop scheduler/checker loops gracefully.
- Stop dispatch queue and YouTube outbox consumers during shutdown.

Runtime은 scheduler·egress·celebration·birthday stream·설정 subscriber를 같은 취소 및 종료 대기 경계에서 관리합니다. 부모가 살아 있을 때 자식의 자체 timeout은 runtime 오류로 전달합니다. 종료 기한을 넘겨도 HTTP와 alarm service cleanup은 호출하며, 작업 대기 실패와 cleanup 오류를 함께 반환합니다. Subscriber의 연결 오류는 기존 log-only 정책을 유지합니다.

부모 종료에 따른 순수 context 오류만 정상 종료로 처리합니다. 취소와 실제 오류가 함께 반환되면 오류 채널 또는 ERROR 로그에 원인을 남기며, 종료 중 소비되지 않는 오류 채널 때문에 작업이 멈추지 않도록 합니다.

Alarm-worker는 Karing template을 보내지 않습니다(`DEC-20260926-hololive-karing-egress-disposition`). Karing 전역 전송 슬롯과 chunk planner는 Karing 경로와 함께 삭제했습니다. Markdown 발송의 불명확한 handoff 결과는 SENDING(YouTube outbox) 또는 quarantine(alarm dispatch)으로 보존합니다.

Alarm dispatch payload/전달 문맥 복원 실패와 event 누락은 PostgreSQL worker fence 및 rows-affected 검증으로 DLQ 전이가 확인된 뒤 해당 delivery의 dedup 키를 해제합니다. 전이 실패·정상 전달·retry·결과 불명에서는 이 해제를 수행하지 않습니다.

Event 조회나 복원·거절 정리 실패로 배치를 반환하지 못하면, 확정된 DLQ를 제외한 미발송 lease를 기존 `ReleaseLeased`로 반환합니다. 부분 발송 입력은 반환하지 않으며 attempt·send-unit·미발송 dedup 키를 유지합니다. 정리는 요청 취소와 독립된 최대 5초로 제한하고, 정리 실패도 원래 오류와 함께 반환합니다. 이미 terminal이거나 다른 worker가 소유한 row는 DB fence가 보호합니다.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker`
- Health: `https://127.0.0.1:30007/health`
- Ready: `https://127.0.0.1:30007/ready`; authenticated `/diagnostics/workers` reports profile match, executors, and real queue snapshots.
- Queue: `alarm_dispatch_deliveries`; Valkey `alarm:dispatch:wakeup` is not backlog authority.
- Metrics: alarm-dispatch backlog/retention metrics (the v3 handoff metrics were removed with the handoff)

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
