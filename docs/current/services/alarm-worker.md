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

- Alarm HTTP provider route registration for `/internal/alarm/*`
- YouTube alarm checking and scheduling loops
- Dispatch queue publish path
- Dispatch queue consume/render/send path, serialized by PostgreSQL `FOR UPDATE SKIP LOCKED` row claims and the single Compose instance
- Generic `notification_delivery_outbox` consume/send path for major event/member news notification rows
- Alarm state cache warming and mutation coordination where configured
- Pending `youtube_notification_outbox` claim/render/send under the `youtube_delivery` profile executor
- v1 YouTube outbox는 v3 ledger로 넘기지 않고 `youtube_delivery` dispatcher가 direct egress까지 소유 (`DEC-20260926-hololive-outbox-v3-convergence`로 `shadow|cutover` handoff 삭제)
- Birthday and anniversary celebration production. Birthday stream delivery audience is derived from sent deliveries of the matching birthday greeting event; it does not fall back to every alarm room.

방송·콘텐츠·축하·생일 방송·X 스페이스 알림에는 단일·묶음·길이와 무관하게 자동 전체보기 패딩을 넣지 않습니다. worker의 렌더러와 dispatch 설정에는 접기 옵션이 없습니다. `BOT_SEE_MORE_FOLD`는 API의 조회·보고서 표시를 제어하며, worker는 공통 설정 로딩에서 값 형식만 검증합니다. 사용자 template/채널 override의 직접 패딩, 기존 예약 본문과 pinned request의 본문·route·ID는 유지합니다.

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Alarm HTTP provider | internal HTTP JSON | `/internal/alarm/*` | `hololive-api` bot/admin clients |
| Alarm dispatch egress | PostgreSQL table | `alarm_dispatch_deliveries` | Iris/Kakao via alarm-worker egress |
| Notification delivery outbox | PostgreSQL table | `notification_delivery_outbox` | Iris/Kakao via alarm-worker egress |
| YouTube outbox dispatch | PostgreSQL table | `youtube_notification_outbox` | Iris/Kakao via alarm-worker egress |
| Alarm service state | in-process subscription service | `internal/service/alarm/subscriptions` | scheduler `AlarmState` target/cache-warm port and alarm HTTP route-specific ports use the same service instance |

Worker 설정은 `internal/config`, 실제 구독 서비스와 private cache는
`internal/service/alarm/subscriptions[/internal/alarmcache]`, dedup·queue·dispatch 저장은
`internal/service/alarm/{dedup,queue,dispatchoutbox}`가 소유합니다. Alarm dispatch runner·SQL은
`internal/egress/alarmdispatch`, 실제 YouTube formatter는 `internal/egress/youtubedispatch/format`입니다.
Shared에는 envelope/DTO 계약과 HTTP client/handler 및 실제 공용 DB·시간·target-minute primitives가 남습니다.

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
- Kakao command parsing, owned by the `hololive-api` bot plane
- LLM summary generation, owned by the `hololive-api` LLM plane

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

Runtime은 scheduler·egress·celebration·birthday stream·X Spaces runner를 같은 취소 및 종료 대기 경계에서 관리합니다. 부모가 살아 있을 때 자식의 자체 timeout은 runtime 오류로 전달합니다. 종료 기한을 넘겨도 HTTP cleanup은 호출하며, 작업 대기 실패와 cleanup 오류를 함께 반환합니다.

부모 종료에 따른 순수 context 오류만 정상 종료로 처리합니다. 취소와 실제 오류가 함께 반환되면 오류 채널 또는 ERROR 로그에 원인을 남기며, 종료 중 소비되지 않는 오류 채널 때문에 작업이 멈추지 않도록 합니다.

Alarm-worker는 Karing template을 보내지 않습니다(`DEC-20260926-hololive-karing-egress-disposition`). Karing 전역 전송 슬롯과 chunk planner는 Karing 경로와 함께 삭제했습니다. Markdown 발송의 불명확한 handoff 결과는 SENDING(YouTube outbox) 또는 quarantine(alarm dispatch)으로 보존합니다.

Alarm dispatch payload/전달 문맥 복원 실패와 event 누락은 PostgreSQL worker fence 및 rows-affected 검증으로 DLQ 전이가 확인된 뒤 해당 delivery의 dedup 키를 해제합니다. 전이 실패·정상 전달·retry·결과 불명에서는 이 해제를 수행하지 않습니다.

Event 조회나 복원·거절 정리 실패로 배치를 반환하지 못하면, 확정된 DLQ를 제외한 미발송 lease를 기존 `ReleaseLeased`로 반환합니다. 부분 발송 입력은 반환하지 않으며 attempt·send-unit·미발송 dedup 키를 유지합니다. 정리는 요청 취소와 독립된 최대 5초로 제한하고, 정리 실패도 원래 오류와 함께 반환합니다. 이미 terminal이거나 다른 worker가 소유한 row는 DB fence가 보호합니다.

## 발행·복구 계약

발행 결과는 입력 ordinal별 receipt로 확인합니다. 저장된 active delivery와 SENT는 수용된 발행이며, payload collision과 DLQ·QUARANTINED·CANCELLED는 성공으로 표시하지 않습니다. 방별 수용 결과만 dedup·tier에 반영하므로 한 방의 성공이 다른 방의 복구를 막지 않습니다. commit 응답이 불명확하면 안정적인 event key·payload hash·delivery key를 한 번, 최대 5초 동안 확인하고 확인되지 않은 항목은 성공 처리하지 않습니다.

선정된 upcoming 알람은 `alarm_upcoming_candidates`에 최초 category·본문 입력·방을 저장하며 평가 checkpoint와 함께 commit합니다. 보장 시작점은 이 durable staging commit입니다. 기존 75초 조회 lookback 이후에도 미발행 후보를 복구하되 예정 시작 시각 이후에는 분 전 알람을 만료시킵니다. 확인된 일정 변경·방송 종료·최초공개·구독 해제는 사유를 남겨 종료하며, 단순 조회 목록 누락만으로 취소하지 않습니다. 최신 provider 응답은 과거 canonical 보강과 분리하고 canonical 종료는 선정 이후의 실제 상태·일정 관측 시각(`status_observed_at`, `schedule_observed_at`)만 사용합니다. 확정된 최초공개는 불변 분류이므로 시각과 무관하게 live 후보에서 제외합니다. 예정 시각도 저장되는 `last_seen_at`은 이 최신성의 증거가 아닙니다. 과거 행의 관측 시각은 추정하지 않습니다. Claim 대기 뒤 발행 직전에도 미발행 후보의 기한을 재검사하며, 이미 수용된 delivery의 현행 재시도 정책은 별도로 유지합니다. 제목 변경은 기존 event key/hash의 collision 거절 계약을 유지합니다.

DB subscriber fallback은 이번 조회 결과만 반환하고 positive set과 빈 구독 marker를 쓰지 않습니다. 늦은 조회가 구독 mutation을 덮어쓰지 않도록 cache 갱신은 기존 mutation·명시적 rebuild가 담당합니다. eviction 뒤에는 해당 채널을 DB에서 다시 확인합니다.

범용 delivery의 발송 attempt는 `notification_delivery.executor.attempt_timeout`(기본 10초)을 따르며 더 짧은 부모 deadline을 적용합니다. dispatcher는 profile 값을 기본값으로 바꾸지 않고, 잘못된 설정이면 기동에 실패합니다. 실행 가능한 슬롯 수만큼 방별 첫 due 항목을 claim하고, 실행 중인 방과 다른 owner가 처리 중인 방의 후행 항목은 claim하지 않습니다. 따라서 슬롯·방별 순서를 기다리는 항목의 60초 lease를 미리 소비하지 않습니다. `OUTCOME_UNKNOWN`·handoff 불명·호출 이후 timeout/cancel은 worker/status fence로 QUARANTINED 전이를 시도합니다. 저장 실패 시 SENDING 증거를 유지하여 stale sweep이 처리하며, 확정 성공 후 DB 반영 실패도 일반 미발송 실패로 바꾸지 않습니다. batch가 없어도 기존 tick에서 due maintenance를 실행합니다.

YouTube 전송은 `youtube_delivery.executor.attempt_timeout`을 실제 전송 예산으로 사용합니다. 서비스별 `delivery_send_timeout_ms`는 이 값과 같아야 하며, 불일치는 설정 오류로 거절합니다. 기존 두 설정 필드와 기본값은 유지합니다.

한 poll에서 같은 notification outbox ID는 한 번만 처리합니다. 처리한 ID 목록은 batch 크기 이내로 유지하고 다음 poll에 초기화하여 짧은 backoff나 재발급이 같은 poll에서 재시도 예산을 연속 소비하지 않게 합니다.

세 발송 경로는 최초 외부 발송 전에 최종 본문·경로·요청 ID 관계를 저장하고 재시도에서 재사용합니다. alarm과 YouTube의 묶음은 membership도 고정합니다. `CLIENT_REQUEST_ID_FAILED`의 확정 pre-handoff 실패만 SDK의 결정적 r1/r2 generation을 허용하며, 기존 attempt·시간 상한을 유지합니다. unknown·payload mismatch·already exists·code 없는 409·transport 오류에는 새 ID를 발급하지 않습니다.

YouTube의 freshness를 지난 known-unsent PENDING은 bounded sweep에서 ledger·logical owner·row version을 확인한 뒤 명시적 만료 사유로 FAILED 종료합니다. SENT/QUARANTINED 및 진행 중인 SENDING 증거를 우선하며, 만료로 unknown을 재발송 가능 상태로 바꾸지 않습니다. 부모 aggregate·terminal retention을 거쳐 정리하고 만료 행은 revive하지 않습니다.

### 운영 전환 제한

새 migration과 코드의 로컬 검증은 운영 적용 승인이 아닙니다. 실제 적용 전에는 기존 미고정 request 행의 상태·과거 전송 증거를 inventory로 확인해야 합니다. 과거 본문을 입증할 수 없는 in-flight/retry 행은 현재 템플릿으로 복원하지 않으며, 자동 재전송·일괄 초기화하지 않습니다. 이 inventory, 공유 DB migration/backfill, 배포·재시작은 별도 승인 범위입니다.

request snapshot/generation을 보존하지 못하는 구버전 바이너리로 단순 rollback하지 않습니다. 전환 문제가 생기면 egress를 멈추고 저장된 request·membership·generation·unknown 증거를 보존한 뒤 호환 코드 또는 forward fix로 복구합니다. 구독 조회·수집은 각 기존 소유 경계를 유지합니다.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker`
- Health: `https://127.0.0.1:30007/health`
- Ready: `https://127.0.0.1:30007/ready`; authenticated `/diagnostics/workers` reports profile match, executors, and real queue snapshots.
- Queue: `alarm_dispatch_deliveries`; Valkey `alarm:dispatch:wakeup` is not backlog authority.
- Metrics: alarm-dispatch backlog/retention metrics (the v3 handoff metrics were removed with the handoff)
- YouTube 공통 attempt counter는 실제 provider operation을 집계하며 grouped 호출 1회와 방별 delivery 결과를 구분합니다. 준비·claim·이미 충족된 행은 provider success로 세지 않습니다. provider 성공과 DB finalization 실패도 별도로 유지합니다.
- YouTube ready snapshot은 부모 created_at freshness·due·lock 조건을 실제 claim과 맞춥니다. `hololive_youtube_delivery_expired_pending_bounded`는 만료 대기 PENDING 수를 기존 batch size까지만 보여 주므로 전체 backlog 총수가 아닙니다.

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
