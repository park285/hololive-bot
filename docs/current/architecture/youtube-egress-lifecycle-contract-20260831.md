# YouTube egress lifecycle contract

- 작성일: 2026-08-31 KST
- 적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`
- 적용 런타임: `hololive-alarm-worker`
- 상위 아키텍처: [`youtube-egress-lifecycle-transition-ownership-20260831.md`](youtube-egress-lifecycle-transition-ownership-20260831.md)
- Logical ledger: [`youtube-egress-logical-delivery-ledger-20260831.md`](youtube-egress-logical-delivery-ledger-20260831.md)
- Commit 판정: [`youtube-egress-lifecycle-commit-adjudication-20260831.md`](youtube-egress-lifecycle-commit-adjudication-20260831.md)
- 구현 선택 근거: [`youtube-egress-lifecycle-library-review-20260831.md`](youtube-egress-lifecycle-library-review-20260831.md)
- Ledger backfill 현재 상태(2026-09-28): 운영 완료. backfill 명령과 writer·cleanup의 completion marker 확인은 `DEC-20260926-hololive-retired-rollback-tooling`으로 지웠습니다. migration 227은 적용 시점, migration 229는 state 폐기 직전의 완료를 검사합니다. 아래 backfill·completion 조항은 설계 근거입니다.

## 규범 용어

`해야 한다`, `금지한다`, `오직`은 구현과 테스트가 지켜야 하는 규범입니다. `권장한다`는 동등한 안전 속성을 입증한 대안이 없을 때의 기본 선택입니다.

## 범위

이 계약은 다음을 다룹니다.

- `youtube_notification_outbox` pre-fanout intent
- `youtube_notification_delivery` claim, preparation, send, retry, terminal 처리
- 모든 kind의 logical delivery 중복 방지와 Community/Shorts canonical identity
- Post-level alarm claim과 per-room delivery success 결합
- Stale `SENDING` quarantine과 logical group reconciliation
- `FAILED` revive
- Child delivery 기반 outbox aggregate projection
- Logical `SENT|QUARANTINED` ledger와 fixed-high-water backfill
- `terminal_at` 기반 full-row cleanup

다음은 비범위입니다.

- Provider별 HTTP/SDK payload 계약
- Alarm-worker replica>1 승인
- Operator replay UI/API
- Event sourcing 또는 외부 workflow engine

## 모델과 식별자

### Physical delivery row

`youtube_notification_delivery.id`로 식별되는 DB row입니다. Claim, version fencing, outbox aggregate의 물리 단위입니다.

### Logical delivery

사용자 관점에서 같은 내용을 같은 방에 한 번 전달하는 단위입니다.

```text
Community/Shorts:
(kind, canonical_post_id, room_id)

그 밖의 YouTube kind:
(kind, outbox.content_id, room_id)
```

Community/Shorts의 `canonical_post_id`는 claim, telemetry, sibling 조회가 같은 resolver를 사용해야 합니다. Payload와 `content_id` 해석을 package별로 복제해서는 안 됩니다.

현재 상태: 모든 소비자가 `contentid.ResolveDeliveryLogicalID`(room 없는 logical ID, `contentid.ResolveDeliveryKey`의 앞 단계) 하나를 씁니다. 시도 telemetry의 `post_id`는 전이 트랜잭션이 검증한 logical key이고, alarm-worker 발송 전 선택의 logical identity 검증·같은 방 SENT sibling 판정은 같은 resolver 결과를 쓰며 누락·파싱 실패·불일치는 오류(발송 전 retry 실패)로 드러냅니다. attempt started·발송 전 식별자 오류·`outbox_final_result` 감사 로그의 `post_id`는 `telemetry.PostIDLogValue`가 같은 resolver 값을 쓰고, invalid identity는 대체 ID 없이 `invalid:<kind>:<reason>` 라벨로 남깁니다. telemetry 버퍼 적재는 `post_id`를 `content_id`로 채우지 않고 빈 값을 오류로 거절합니다. NEW_VIDEO·LIVE_STREAM·MILESTONE은 payload `canonical_post_id`를 보지 않고 `content_id`만 씁니다. 과거의 `canonical_post_id → content_id → video_id/post_id` 폴백(감사 행 `holo-youtube-telemetry-post-id-fallback`)은 삭제했습니다. 삭제 전 운영 DB 읽기 전용 측정: COMMUNITY_POST 23건·NEW_SHORT 36건 모두 canonical_post_id가 있고 문자열 타입이었습니다(NEW_VIDEO 3건은 canonical_post_id가 없지만 content_id 계약이라 영향 없음).

같은 감사 행의 빈 `delivery_path` 대체도 제거했습니다. 기록 주체는 `CommunityShortsDeliveryPath`를 명시하며 버퍼 적재는 빈 경로를 batch 전체의 오류로 반환합니다. 방출 로그는 저장된 경로를 trim해서 쓰되 비어 있는 과거 값을 정상 경로로 꾸미지 않습니다. 삭제 전 guarded 운영 조회에서 telemetry 9행의 빈 `post_id`·빈 `delivery_path`는 각각 0건이었으므로 과거 row 호환 변환은 하지 않습니다. 기존 non-empty 값의 trim은 유지합니다.

`(outbox_id, room_id)` unique index는 physical duplicate만 막습니다. Community/Shorts에서는 서로 다른 outbox/content ID가 같은 canonical post를 표현할 수 있고, 모든 kind에서 cleanup 뒤 같은 content의 outbox ID가 달라질 수 있으므로 logical duplicate 방어를 대체하지 않습니다.

Canonical resolver는 Community/Shorts의 alias를 하나의 `canonical_post_id`로 정규화하고, 그 밖의 kind는 trim된 non-empty `content_id`를 사용합니다. Invalid payload/identity를 raw ID로 대체하지 않고 preparation 또는 backfill을 fail closed합니다.

### Logical delivery ledger

`youtube_notification_delivery_ledger`는 `(kind, logical_id, room_id)`별 terminal evidence의 정본입니다.

```text
SENT > QUARANTINED > absent
```

`QUARANTINED -> SENT`만 강화 전이로 허용하고 ledger row 삭제와 `SENT` downgrade를 금지합니다. Full outbox/delivery cleanup 뒤에도 ledger는 남습니다.

### Logical delivery group

Retained physical row 중 logical key가 같은 집합입니다. 그룹 안에는 정확히 하나의 deterministic retry owner가 있습니다.

```text
owner = 최소 (created_at, delivery_id)
```

Ledger `SENT/QUARANTINED`가 retained physical state보다 우선합니다. Ledger가 absent일 때 retained `SENDING`이 owner의 현재 `PENDING/FAILED`보다 우선합니다. Impossible mixed state는 우선순위로 숨기지 않고 invariant breach로 분류합니다.

### Provider operation

외부 provider 요청 한 번에 포함되는 정확한 logical delivery owner 집합입니다. 개별 메시지는 owner 한 건, grouped 메시지는 서로 다른 logical key의 owner 여러 건을 가질 수 있습니다.

Operation membership과 provider request는 최초 `BeginSending` 전에 `youtube_notification_send_request`에 저장하며 이후 변경할 수 없습니다. 최종 body·route·방·member IDs·dedupe keys·base ID·generation을 고정하고 retry/restart에서 다시 렌더링하지 않습니다. frozen 그룹의 모든 멤버가 due·freshness·lock 조건을 만족해야 claim 대상입니다. batch 상한보다 큰 그룹은 부분 전송하지 않으며 다른 eligible 작업을 막지 않습니다.

기존 미고정 행은 과거 전송 가능성을 현재 본문으로 추정하지 않습니다. migration 249는 `request_snapshot_allowed=false`로 기존 행을 구분했습니다. 2026-10-02 운영 조회에서 이 표시를 가진 67행이 모두 `SENT`·`FAILED`(2026-05-17, revive 신선도 창 밖)였으므로 dispatcher의 구분 검사(`ErrLegacyRequestEvidence`)는 지웠습니다. migration 256이 열을 지웠습니다. 표시가 `false`인데 아직 끝나지 않은(`PENDING`·`SENDING`·`QUARANTINED`) 행이 있으면 이 migration은 열을 남기고 실패합니다.

Iris 202는 durable admission만 증명합니다. `CompleteSent`는 고정 요청을 멤버보다 먼저 잠그고 세대·고정 멤버가 이번 발송과 같을 때만, SENT 전이와 같은 트랜잭션에서 고정 요청에 Iris requestId(`handoff_request_id`)와 첫 handoff 확인 시각(`handoff_next_check_at` = SENT 전이 시각 + min(5s, `youtube_delivery` executor `attempt_timeout`))을 기록합니다(migration 336·339). 요청이 다르거나 requestId가 비면 전이하지 않고 SENDING으로 남깁니다. `youtube-delivery-handoff-reconciler`가 `handoffcheck` 판정으로 지평까지 `/reply-status`를 확인합니다. `failed`는 세대 < 2이고 시도 예산(attempt + 1 < `max_retries`)이 남으면 논리 그룹의 SENT 행 전원(고정 멤버 밖 follower 포함)을 `retry_backoff_ms` 뒤 PENDING으로 되돌리고 요청 세대를 올려 같은 본문을 다음 세대 ID로 다시 보내며, 아니면 FAILED입니다. 결과 불명·404·지평 미확인은 QUARANTINED입니다. 세 경우 모두 그 owner를 출처로 한 SENT ledger만 같은 트랜잭션에서 지우고(격리는 같은 출처의 QUARANTINED ledger), 바뀐 delivery의 부모 outbox 집계도 그 트랜잭션 끝에서 canonical 집계 문장으로 다시 계산합니다. 정산의 잠금 순서는 고정 요청 → 영향 부모 outbox(id 순서, 그룹 잠금과 같은 후보 조건으로 미리 읽음) → 논리 그룹 delivery → ledger이고, 집계는 이미 잠근 부모에 다시 잠그지 않고 실행합니다. 정산 도중 같은 논리 키의 follower·outbox가 들어와 부모 집합이 늘면 새 부모를 잠그지 않고 아무것도 바꾸지 않은 `membership_changed` fence 미스로 끝내며 확인을 닫지 않으므로, soft claim 뒤 다음 확인이 늘어난 집합으로 정산합니다. 집계가 실패하면 정산 전체가 rollback되고 soft claim 뒤 다음 확인이 다시 정산합니다. 집계는 outbox `sent_at`을 지우지 않으며, 아래 post 단위 tracking 표시도 되돌리지 않습니다. 발송 경로는 202 admission만 확인하고 handoff를 기다리지 않으므로, handoff 결과는 이 reconciler만 정산합니다.

그룹 조회의 전역 행 한도는 `요청 수 × (LogicalGroupLimit + 2)`이며, 이 한도에 닿으면 창 밖 형제의 유무를 증명하지 못합니다. 다른 키로 풀리는 별칭 후보도 이 창을 차지하므로, 반환된 행의 키별 개수가 작거나 owner가 잠겼다는 사실만으로 완전성을 판단하지 않습니다. claim 기반 그룹 해석과 stale sweep은 불완전한 창을 원장 조회 전에 오류로 거부합니다. handoff 정산은 부모 집합 증가 미스를 먼저 판정한 뒤, 불완전한 창의 멤버 전원을 membership 거부로 처리해 행·원장·세대 변경 없이 확인만 닫습니다. 빈 요청은 완전한 빈 집합입니다.

### Tracking requirement

Community/Shorts의 post-level tracking은 room별 delivery와 수명이 다릅니다. 발송 전에 post 단위 상태를 선점하지 않으며, 같은 post의 room delivery는 room별 claim·send fence와 logical group gate로만 직렬화합니다.

- 성공 전이(`CompleteSent`)는 같은 트랜잭션에서 `MarkAlarmSentBatch`로 owner logical key의 tracking 행과 기존 `youtube_community_shorts_alarm_states` 행에 발송 시각을 기록합니다. 알람 상태 행이 없으면 tracking 행으로부터 만들고, 이미 SENT면 더 이른 시각만 남깁니다. tracking 행과 알람 상태 행이 모두 없으면 post 단위 표시는 남지 않습니다. 운영에서는 API canonicalwrite가 outbox와 같은 트랜잭션에서 tracking 행을 만듭니다.
- 정상 경로 트랜잭션은 기록 뒤 post 단위 표시를 다시 확인하지 않습니다. commit 응답 유실 판정이 `Applied`로 끝난 뒤의 read-back에서 post마다 알람 상태나 tracking 중 하나가 SENT인지 확인하고, 둘 다 아니면 `AtomicityBreachError`와 함께 `Indeterminate`(atomicity breach)로 분류합니다. 이미 commit된 뒤이므로 rollback하지 않습니다.
- Community/Shorts가 아닌 delivery에는 post-level 기록이 없습니다.
- 알람 상태의 `delivery_status`는 `alarm_sent_at`만으로 `DETECTED`·`SENT`를 정합니다. 구 워커의 발송 전 선점 열 `authorized_at`과 `ENQUEUED` 상태는 migration 331이 지웠고, 남은 `ENQUEUED` 행은 같은 규칙으로 `DETECTED`·`SENT`로 맞췄습니다.

여러 room이 같은 post를 발송할 수 있습니다. 첫 성공이 post 발송 시각을 정하고 후속 성공은 그 시각을 유지하며 delivery success를 rollback하지 않습니다. 한 room의 결과 불명은 같은 post의 다른 room 발송을 미루지 않습니다.

## 상태와 저장 필드

### Outbox 상태

```text
PENDING
SENT
FAILED
```

| Phase | 의미 | Writer |
|---|---|---|
| pre-fanout | subscriber lookup과 delivery materialization 대기 | `OutboxFanoutService` |
| post-fanout | child delivery 집계 | aggregate projector |

`terminal_at`은 outbox가 현재 terminal 상태에 들어간 시각입니다. `PENDING`이면 NULL이고 `SENT/FAILED`이면 non-NULL입니다. Idempotent terminal projection은 최초 값을 보존하고, `FAILED -> SENT`는 새 terminal transition 시각으로 갱신하며, revive로 `PENDING`이 되면 NULL로 지웁니다.

### Delivery 상태

```text
PENDING
SENDING
SENT
FAILED
QUARANTINED
```

| 상태 | 규범 의미 |
|---|---|
| `PENDING` | Provider 호출 전이거나 logical owner의 known-not-delivered retry 대기입니다. `locked_at`이 있으면 `locked_by` 소유자가 그 `row_version` 세대의 claim을 보유합니다. |
| `SENDING` | Exact provider operation이 durable하게 commit됐고 provider 호출이 시작될 수 있습니다. Provider 수신을 증명하지는 않습니다. |
| `SENT` | Logical delivery가 충족됐음을 local transaction으로 확정했습니다. Provider success 또는 same-logical `SENT` evidence가 근거입니다. |
| `FAILED` | Logical owner의 retry가 소진됐거나 permanent known failure입니다. Group revive만 허용합니다. |
| `QUARANTINED` | Logical delivery 처리 여부가 불명확합니다. Automatic resend와 일반 revive를 금지합니다. |

### `sent_at`

Physical row가 logical delivery 충족 상태로 local commit된 시각입니다.

- Provider success finalization 시각
- Same-logical `SENT` evidence reconciliation 시각

항상 provider response timestamp라는 뜻은 아닙니다. Transport latency는 telemetry evidence를 사용합니다.

### `attempt_count`

Logical retry owner의 실패/unknown 시도 수입니다. Follower projection은 owner의 retry budget을 소비하지 않습니다.

증가하지 않는 경우:

- claim
- batch duplicate follower defer
- `PENDING` owner follower defer
- equivalent `SENDING` defer
- mirrored `FAILED`/`QUARANTINED` propagation
- `BeginSend`
- provider success
- same-logical sent reconciliation
- grouped fallback 자체

Owner에서 1 증가하는 경우:

- retryable/permanent preparation failure
- known-not-delivered retryable/permanent send failure
- owner stale `SENDING -> QUARANTINED`

Group revive는 owner와 follower attempt를 0으로 재설정합니다.

### `row_version`

```sql
row_version bigint NOT NULL DEFAULT 0 CHECK (row_version >= 0)
```

Claim과 모든 physical row mutation은 version을 1 증가시킵니다. Version은 인덱싱하지 않습니다. 그래서 `row_version`이 같으면 `status`·`attempt_count`·`locked_at`·`locked_by`도 같습니다.

### `locked_by`

```sql
locked_by text NULL
```

Claim한 워커 인스턴스 ID(`youtube-dispatcher:<hostname>:<pid>`)입니다. Claim이 기록하고 `SENDING` 전이가 유지하며, 잠금을 비우는 전이는 `locked_at`과 함께 비웁니다. 컨테이너 이름이 고정이라 재시작한 프로세스가 같은 ID를 가질 수 있으므로 claim 세대는 `row_version`이 구분하고 `locked_by`만으로 fence하지 않습니다. 이 열을 모르는 이전 워커가 남긴 행은 NULL이거나 이전 값이며, 전이 fence는 다시 읽은 값과 비교하므로 그대로 처리합니다.

### 시간원

claim·만료·신선도 판정과 `locked_at`·`sent_at`·`next_attempt_at`·`terminal_at` 기록은 모두 PostgreSQL 시계가 정합니다. 각 SQL 문장은 `clock AS MATERIALIZED (SELECT clock_timestamp() AS at)`에서 시계를 한 번 채취하고, 조건에서는 `(SELECT at FROM clock)` 스칼라 하위 질의로 참조해 PENDING·SENDING 부분 인덱스 조건으로 쓰이게 합니다. Go는 시각 대신 `lease_ms`·`freshness_ms`·지연(`delay_ms`)과 DB에서 읽은 하한만 넘깁니다.

- 전이 문장(`transition_apply_rows.sql`)은 대상 행을 잠근 뒤 시계를 채취하고, 그 값을 전이 시각으로 돌려줍니다. 같은 트랜잭션의 ledger 관측 시각·tracking 발송 표시·시도 telemetry는 이 값을 씁니다. fanout 실패 문장(`fanout_fail.sql`)도 claim fence를 통과한 outbox 행을 잠근 뒤 시계를 채취합니다.
- 집계 문장(`delivery_repository_aggregate_sync.sql`)은 같은 트랜잭션의 앞 문장이 부모 outbox를 ID 순서로 잠근 뒤 실행되므로, 새로 기록하는 outbox `sent_at`·`terminal_at`도 부모 잠금 뒤의 DB 시계 하나로 정합니다.
- 후보 문장(claim·stale SENDING·만료 PENDING·revive)은 행을 잠그기 전에 시계를 채취합니다. 만료 판정은 실제 잠금 시점보다 보수적이고, claim이 기록한 lease도 그만큼 일찍 끝납니다. 후보 문장은 판정 시계를 `checked_at`으로 돌려주고 Go의 그룹 재확인은 이 값만 씁니다.
- `next_attempt_at`은 `GREATEST(clock + delay, floor)`입니다. floor는 owner `next_attempt_at` 또는 in-flight `locked_at + LockTimeout`처럼 DB 행에서 온 값만 씁니다.

보존 삭제 cutoff(`CleanupTerminalOutboxes`, `CleanupOrphanRequests`)는 상태 전이·소유권 판정이 아닌 일 단위 보존 창이라 Go 시각을 씁니다.

## 핵심 불변식

### I-001. PostgreSQL source of truth

Current state, attempt, due, lock, version은 PostgreSQL이 정본입니다. Process memory state는 정본이 될 수 없습니다.

### I-002. Single policy evaluation

Retry 소진, preparation/send failure, revive는 lifecycle policy가 한 번만 계산합니다. SQL `CASE`와 repository가 `MaxRetries`를 다시 평가해서는 안 됩니다.

### I-003. Fenced mutation

Claim 이후 mutation은 `id + expected status + expected locked_by + expected row_version`을 같은 문장에서 잠근 행과 비교해야 합니다. `attempt_count`와 `locked_at` 등가는 `row_version` 등가에 함의되므로 따로 비교하지 않습니다. 한 command의 대상은 전원이 fence를 통과할 때만 함께 전이하고, 미스는 대상별 원인을 담은 `sendfence.MissError`로 돌려줍니다.

### I-004. One logical retry owner

같은 logical group에서 provider send와 attempt 증가 권한은 deterministic owner 하나에만 있습니다. Follower가 별도 attempt budget으로 owner를 추월해서는 안 됩니다.

### I-005. In-batch coalescing

같은 batch의 동일 logical key는 owner/leader 하나만 provider candidate가 됩니다. Follower는 attempt 없이 defer하거나 owner state를 mirror합니다.

### I-006. Durable logical gate

모든 발송 candidate는 retained logical group을 먼저 잠그고 조회 창의 완전성과 claim fence를 확인한 뒤 ledger를 읽어 logical group resolution을 수행해야 합니다.

### I-007. External-effect ordering

Provider call은 operation-level `PENDING -> SENDING` commit이 확인된 뒤에만 시작할 수 있습니다.

### I-008. Outcome unknown hold

Provider 처리 여부가 불명확하면 즉시 state write, claim release, fallback, resend를 하지 않습니다. Operation member는 `SENDING`에 남고 stale sweeper가 group을 quarantine합니다.

### I-009. Monotonic fulfillment reconciliation

Same-logical ledger `SENT` evidence가 생기면 `PENDING`, `FAILED`, `QUARANTINED` follower를 provider 호출 없이 `SENT`로 reconcile할 수 있습니다. 이는 resend가 아니라 logical fulfillment의 단조 수렴입니다.

일반 발송 경로에는 `SENT -> *` 전이가 없습니다. 예외는 아직 handoff 확인 중인 고정 요청의 fenced 재발급·소진·격리 정산이며, 위 handoff 규약을 따릅니다. `QUARANTINED -> PENDING/FAILED`와 `FAILED -> PENDING` 일반 전이는 금지합니다.

### I-010. Tracking is idempotent terminal state

Post tracking이 이미 durable sent이면 후속 room delivery success를 수용하고 post 발송 시각은 가장 이른 값을 유지합니다. 이미 sent인 post를 delivery failure로 오판해서는 안 됩니다.

### I-011. Operation atomicity

Provider operation의 `BeginSending`, grouped success, grouped known failure는 owner member 전체에 all-or-none입니다. Mixed pre/post state는 atomicity breach입니다.

### I-012. Aggregate writer ownership

Child가 존재하는 outbox status와 `terminal_at`은 canonical aggregate 문장만 계산합니다. 일반 projector와 handoff 실패 정산의 트랜잭션 내 집계가 같은 문장을 사용합니다.

### I-013. Commit ambiguity is first-class

DB commit 오류를 rollback 증거로 간주하지 않습니다. Primary exact read-back으로 `Applied`, `Conflict`, `Missing`, `Indeterminate`를 판정합니다.

### I-014. No dual write

Legacy writer와 새 transition writer가 같은 row를 동시에 변경하는 기간을 만들지 않습니다.

Compatibility/backfill 구간에도 poller가 delivery/outbox lifecycle field를 직접 `PENDING/SENT`로 바꾸지 않습니다. High-water 이후 terminal event는 compatibility alarm-worker가 ledger와 같은 transaction에 기록해야 합니다.

### I-015. Safety before automatic liveness

Outcome unknown, logical unresolved, commit indeterminate에서는 자동 resend와 unsafe overwrite보다 중복 방지와 stale-write 방지를 우선합니다.

### I-016. Terminal evidence atomicity

Provider success의 delivery/tracking과 ledger `SENT`, stale outcome unknown의 delivery group과 ledger `QUARANTINED`는 각각 같은 transaction입니다. Aggregate projection은 이 transaction 밖의 별도 recoverable projection이며 transition commit 판정에 포함하지 않습니다. Handoff 실패 정산(재발급·소진·격리)만 예외로, SENT였던 부모를 되돌리는 집계를 정산 transaction 안에서 함께 commit하거나 rollback합니다.

## Token 계약

### Claim fence

Claim 결과 행의 `(id, row_version, locked_by)`입니다. `PENDING + locked_at` row만 나타냅니다. `PrepareClaimed`, `BeginSending`, `ApplyPreparedFailure`는 같은 transaction에서 logical group을 `FOR UPDATE`로 다시 읽은 뒤 caller claim 행마다 다음을 확인하고, 하나라도 어긋나면 아무 행도 바꾸지 않고 `sendfence.MissError`를 돌려줍니다.

| 관측 | 미스 원인 |
|---|---|
| 다시 읽은 행이 없음 | `missing` |
| `status <> PENDING` | `status_changed` |
| `locked_by`가 caller claim과 다름 | `owner_changed` |
| `row_version`이 caller claim과 다름 | `version_changed` |

조건이 맞은 다른 대상은 `peer_missed`로 함께 보고합니다. Fence 미스는 `ApplyConflict`(대상 전원이 `missing`이면 `ApplyMissing`)와 오류를 함께 반환하므로 caller가 버릴 수 없습니다.

### Send fence

`BeginSending`이 만든 owner 행의 `(id, row_version, locked_by)`입니다. `SENDING + locked_at` row만 나타냅니다. 발송 뒤 정산(`CompleteSent`, 실패 전이, 요청 세대 전진, grouped fallback 고정)은 이 값으로 fence하며 lease 만료를 보지 않습니다. 외부 성공을 DB 정산 실패로 되돌리지 않기 위해서입니다.

## Claim protocol

대상 조건(`clock`은 문장 첫머리에서 한 번 채취한 DB 시계):

```text
status = PENDING
next_attempt_at <= clock
locked_at IS NULL OR locked_at < clock - LockTimeout
outbox.created_at >= clock - ClaimFreshnessWindow
```

Claim SQL:

```sql
SET locked_at = (SELECT at FROM clock),
    locked_by = $owner,
    row_version = row_version + 1
```

반환된 exact version·`locked_by`만 Claim fence입니다. Statement 결과를 받지 못하면 caller는 claim을 소유하지 않습니다. `ReadySnapshot`은 같은 predicate와 같은 방식의 시계로 claim 가능 행을 셉니다.

## Logical group resolution

### Candidate loading

Current claimed row의 logical key를 만든 뒤 retained physical row를 bounded batch query로 잠그고, 조회 창의 완전성·키별 행 한도·claim fence를 확인한 다음 ledger를 batch load합니다.

- 모든 kind의 후보는 kind·room 등가와 `btrim(content_id)` 또는 payload의 `canonical_post_id` 후보 등가로 조회합니다. Canonical resolver가 최종 논리 키를 확정하며, 다른 키로 풀리는 SQL 후보도 전역 조회 한도를 소비합니다.
- Current batch member는 한 번에 resolve합니다. Per-row N+1 query를 기본 구현으로 사용하지 않습니다.
- Overflow는 fail-closed preparation error이며 provider를 호출하지 않습니다.
- Ledger schema version과 backfill completion marker가 지원되지 않거나 미완료이면 새 writer는 시작하지 않습니다.

### Impossible mixed states

다음은 정상 priority로 수용하지 않고 `LogicalInvariantBreach`입니다.

```text
SENT 없이 QUARANTINED와 SENDING 동시 존재
SENT 없이 SENDING 두 건 이상 존재
동일 physical row가 서로 다른 canonical logical key로 해석됨
같은 operation에 동일 logical key owner 중복
ledger QUARANTINED와 retained SENDING 동시 존재
cutover 뒤 retained physical SENT/QUARANTINED에 대응하는 ledger evidence 누락
```

Invariant breach에서는 provider를 호출하거나 follower를 임의 상태로 mirror하지 않습니다. Critical metric과 audit evidence를 남기고 operator 조사에 맡깁니다.

Ledger `SENT` evidence가 있으면 confirmed fulfillment가 unknown/in-flight를 해소하므로 `LogicalFulfilled`가 우선합니다.

### Resolution priority

Impossible mixed state가 없을 때 다음 순서로 판정합니다.

```text
1. Ledger SENT
   -> LogicalFulfilled

2. Ledger QUARANTINED
   -> LogicalUnresolved

3. Ledger absent + retained SENDING 1건
   -> LogicalInFlight

4. Ledger absent + PENDING/FAILED 중 최소 (created_at,id) owner 선택
   owner=PENDING -> LogicalActive
   owner=FAILED  -> LogicalFailed
```

### Resolution actions

#### `LogicalFulfilled`

- Provider를 호출하지 않습니다.
- Ledger `SENT`를 transaction 안에서 다시 확인하고 current/follower `PENDING`, `FAILED`, `QUARANTINED`를 `SENT`로 reconcile합니다.
- Attempt는 유지합니다.
- `sent_at`은 ledger `SENT`의 `sent_at`을 그대로 씁니다.
- Tracking을 새로 추정하지 않습니다.

#### `LogicalUnresolved`

- Provider를 호출하지 않습니다.
- Ledger `QUARANTINED`를 transaction 안에서 다시 확인하고 current/follower `PENDING`, `FAILED`를 `QUARANTINED`로 mirror합니다.
- Follower attempt는 유지합니다.
- Existing quarantined owner attempt는 다시 증가시키지 않습니다.

#### `LogicalInFlight`

- Provider를 호출하지 않습니다.
- Current/follower `PENDING`을 attempt 없이 defer합니다.
- Due는 `GREATEST(clock + RetryBackoff, inFlight.locked_at + LockTimeout)`입니다.

#### `LogicalActive`

- Deterministic owner만 preparation/send를 계속합니다.
- Follower `PENDING`은 attempt 없이 `GREATEST(clock + RetryBackoff, owner.next_attempt_at)`로 defer합니다.
- Owner가 current batch에 없더라도 follower가 owner를 대체하지 않습니다.

#### `LogicalFailed`

- Provider를 호출하지 않습니다.
- Follower `PENDING`을 `FAILED`로 mirror합니다.
- Follower attempt는 유지합니다.
- Revive는 group 단위로만 수행합니다.

### Batch leader

같은 batch의 logical key가 같은 row는 resolution 전에 `(created_at,id)`로 정렬합니다. Deterministic owner가 batch 안에 있으면 그 row만 candidate입니다. Owner가 batch 밖에 있으면 batch row 전체가 owner state를 따릅니다.

### Replica 경계

현재 replica=1과 synchronous dispatch round를 전제로 합니다. Replica>1에서는 computed owner 조회와 claim 사이 race를 막는 persisted key/group-affinity가 추가로 필요합니다. 이 계약은 scale-out 승인이 아닙니다.

## Preparation 결과

```text
ReadyToSend
LogicalFulfilled
LogicalUnresolved
LogicalInFlight
LogicalOwnerPendingElsewhere
LogicalFailed
LogicalInvariantBreach
PreparationRetryableFailure
PreparationPermanentFailure
```

### `ReadyToSend`

Owner의 payload, rendering, route, stable client request ID, immutable request가 확정됐습니다.

### Preparation failure

```text
nextAttempt = owner.attempt_count + 1
nextAttempt < MaxRetries  -> owner PENDING retry + follower due alignment
nextAttempt >= MaxRetries -> owner FAILED + follower FAILED mirror
```

Permanent failure는 즉시 owner/group `FAILED`입니다. Follower attempt는 증가하지 않습니다.

## Provider operation

```go
type PreparedSendOperation struct {
    OperationID     string
    ClientRequestID string
    Owners          []PreparedOwner
    LedgerKeys      []LogicalDeliveryKey
    Tracking        []TrackingRequirement
    Request         ImmutableSendRequest
}
```

- Owner logical key는 operation 안에서 중복될 수 없습니다.
- Tracking requirement는 canonical post identity로 deduplicate합니다.
- Grouped message member는 서로 다른 logical key여야 합니다.

### `BeginSending`

Owner member 전체가 Claim fence를 만족하고 claim lease가 살아 있을 때만 전체를 `SENDING`으로 변경합니다.

```sql
SET status = 'SENDING',
    locked_at = clock,  -- 대상 행을 잠근 뒤 채취한 DB 시계
    row_version = row_version + 1
WHERE id = $id
  AND status = 'PENDING'
  AND row_version = $claim_version
  AND locked_by IS NOT DISTINCT FROM $claim_owner
  AND locked_at + $lease_ms * INTERVAL '1 millisecond' > clock
```

Lease 조건(`lease_expired`)은 정지했던 소유자가 lease 밖에서 외부 부수효과를 시작하지 않게 합니다. `locked_by`는 유지합니다. 한 owner라도 미스면 전체 rollback하고 provider를 호출하지 않습니다. 새 `locked_at`은 문장이 돌려준 전이 시각이며, 정산 단계의 시도 시작 시각으로도 쓰입니다.

Commit response loss는 provider call 전에 primary exact read-back으로 판정합니다. Exact post-state만 provider call 권한을 만듭니다.

### Send lease budget

```text
SendingFinalizeGrace = max(2 * PollInterval, 5 seconds)
LockTimeout >= DeliverySendTimeout + SendingFinalizeGrace
```

Provider call 전 remaining lease budget이 부족하면 provider를 호출하지 않고 known-not-delivered retryable outcome으로 처리합니다.

## Provider outcome taxonomy

```go
type ProviderOutcomeKind uint8

const (
    ProviderDelivered ProviderOutcomeKind = iota + 1
    ProviderKnownNotDeliveredRetryable
    ProviderKnownNotDeliveredPermanent
    ProviderOutcomeUnknown
)
```

Stable request ID의 존재만으로 retry-safe가 되지는 않습니다. Provider의 request rejection, dedupe, result-query 계약이 증거여야 합니다. 공통 `sendoutcome` 분류를 사용하되 각 파이프라인의 상태 전이 정책은 유지합니다.

`CLIENT_REQUEST_ID_FAILED`의 확정 pre-handoff 실패만 SDK r1/r2 재발급을 허용합니다. generation과 owner/group retry·telemetry는 한 transaction으로 저장하고 다음 정규 attempt에서 전송합니다. 기존 MaxRetries·freshness·시간 한도를 확장하지 않으며 unknown·payload mismatch·already exists·code 없는 409에는 재발급하지 않습니다. generation 소진은 terminal 처리하고 revive로 초기화하지 않습니다.

### Grouped fallback

- 멤버 이름 조회 오류와 grouped 포맷 오류는 `format_message` 준비 실패로 보존합니다. 포맷 실패를 개별 발송으로 바꾸지 않으며 provider를 호출하지 않습니다. 이름 조회가 성공했지만 표시명이 빈 경우의 예외는 alarm 계약을 따릅니다.
- Outcome unknown 및 client request admission 충돌(409)에서는 fallback을 금지합니다.
- Known-not-accepted + `fallback_allowed=true`에서만 individual fallback을 허용합니다.
- Fallback 자체는 attempt를 소비하지 않습니다.
- Individual fallback 전 lease budget을 다시 확인하며 singleton 최종 request도 발송 전에 저장합니다.

이 fallback은 workspace Failure paths 규칙의 예외 경로이므로 다음 항목을 함께 지킵니다(2026-10-02 보강).

| 항목 | 내용 |
|---|---|
| Trigger | grouped 전송이 Iris `ErrPermanent`로 거절된 경우만 해당합니다. 인증 실패, 409 admission 충돌, outcome unknown은 fallback하지 않습니다. |
| 한도 | group attempt 하나에 한 번이며, 개별 request 수는 group row 수와 같습니다. 개별 발송은 각 row의 per-room 전이를 따르고 fallback을 다시 하지 않습니다. |
| 종단 | 개별 request를 만들거나 저장하지 못하면 개별 발송을 시작하지 않습니다. 이때 group은 `SENDING`에 남고 stale sweeper가 quarantine합니다. |
| Telemetry | `hololive_youtube_outbox_grouped_send_fallback_total{result}`(`started`, `prepare_failed`, `freeze_failed`)와 Warn 로그 "Grouped delivery send failed, falling back to version-fenced individual deliveries"를 남깁니다. |
| Owner | `hololive-alarm-worker`의 `internal/egress/youtubedispatch`입니다. |
| 검토 조건 | `result="started"`가 90일 동안 0이면 이 경로를 지우고 grouped `ErrPermanent`를 group 전체의 permanent failure로 처리합니다. `prepare_failed`나 `freeze_failed`가 0이 아니면 결함으로 보고 원인을 조사합니다. |

Grouped 포맷 실패는 이 fallback의 trigger가 아닙니다. 묶음 메시지를 만들지 못하면(템플릿 결함이나 표시명 정본 조회 오류) 개별 발송으로 바꾸지 않고, group 전체를 재시도 가능한 `format_message` 실패로 전이합니다.

## Group transition semantics

### Owner retry

- Owner: `SENDING/PENDING leased -> PENDING`, attempt +1, `next_attempt_at = clock + max(RetryBackoff, RetryAfter)`, lock clear, version +1
- Follower `PENDING`: attempt 유지, owner와 같은 지연으로 due 설정, lock clear, version +1
- Terminal/in-flight mixed follower는 conflict 또는 invariant breach

### Owner terminal failure

- Owner: `FAILED`, attempt +1, `next_attempt_at` 유지, lock clear, version +1
- Follower `PENDING`: `FAILED`, attempt 유지, 자기 `next_attempt_at` 유지, lock clear, version +1
- Provider는 재호출하지 않습니다.

### Owner quarantine

- Stale owner: `QUARANTINED`, attempt +1, lock clear, version +1
- Follower `PENDING/FAILED`: `QUARANTINED`, attempt 유지, lock clear, version +1
- Existing `SENT` evidence가 발견되면 fulfilled reconciliation이 우선합니다.

### Owner success

Success transaction:

1. Operation owner 전체 exact `SENDING + SendFence` 확인
2. Owner를 `SENT`, `sent_at = clock`(전이 문장이 돌려준 DB 시계), lock clear, version +1
3. Same-logical follower `PENDING/FAILED/QUARANTINED`를 `SENT`로 reconcile, attempt 유지, version +1
4. Community/Shorts owner logical key마다 tracking과 post 단위 알람 상태에 발송 시각을 기록(이미 SENT면 더 이른 시각 유지)
5. Owner logical key마다 ledger `RecordSent`(관측 시각은 같은 전이 시각)
6. Latency classification과 tracking mutation commit

정상 경로 트랜잭션은 기록 뒤 post 단위 SENT 표시를 다시 확인하지 않습니다. commit 응답 유실 판정의 read-back에서 post 단위 SENT 표시(알람 상태 또는 tracking)가 없으면 atomicity breach(`Indeterminate`)로 분류합니다. Provider는 다시 호출하지 않습니다.

Transaction은 touched outbox ID를 반환합니다. Commit이 확인된 뒤 aggregate projector를 즉시 실행하고 background projector가 current child state에서 재수렴합니다. Aggregate 실패는 committed delivery/tracking/ledger를 rollback하지 않습니다.

### Already-fulfilled reconciliation

Provider call 전 ledger `SENT` evidence가 있으면 tracking mutation 없이 follower만 `SENT`로 reconcile합니다. Ledger key/state는 transaction 안에서 다시 확인합니다.

## Decision model

Policy는 nullable patch map이 아니라 sealed concrete decision을 반환합니다.

```go
type DecisionContext struct {
    RuleID           RuleID
    LogicalKeyHash   string
    OwnerID          int64
    ExpectedStatus   DeliveryStatus
    ExpectedVersion  int64
    ExpectedLockedBy *string
}
```

최소 decision:

```text
BeginSendDecision
RetryLogicalGroupDecision
FailLogicalGroupDecision
SentLogicalGroupDecision
FulfilledReconciliationDecision
UnresolvedPropagationDecision
DeferLogicalFollowerDecision
ReviveLogicalGroupDecision
```

Invalid field combination을 public struct literal로 만들 수 없어야 합니다.

## Transition store API

```go
type TransitionStore interface {
    ClaimPending(context.Context, ClaimRequest) ([]ClaimedDelivery, error)
    ResolveLogicalGroups(context.Context, []ClaimedDelivery) ([]LogicalGroupSnapshot, error)
    BeginSending(context.Context, PreparedSendOperation) (StartedSendOperation, ApplyResult, error)
    ReconcileFulfilled(context.Context, FulfilledCommand) (ApplyResult, error)
    PropagateUnresolved(context.Context, UnresolvedCommand) (ApplyResult, error)
    DeferFollowers(context.Context, []DeferCommand) ([]ApplyResult, error)
    ScheduleLogicalRetry(context.Context, LogicalRetryCommand) (ApplyResult, error)
    FailLogicalGroup(context.Context, LogicalFailCommand) (ApplyResult, error)
    CompleteSent(context.Context, SentOperation) (ApplyResult, error)
    QuarantineStaleSending(context.Context, time.Time, int) ([]LogicalGroupResult, error)
    ReviveFailedLogicalGroups(context.Context, ReviveRequest) (int64, error)
}
```

금지 API:

```text
UpdateStatus(id,status)
ApplyPatch(map[string]any)
Expected token을 생략한 writer
Repository가 MaxRetries를 받는 API
Raw failure string branch
ID-only SENDING -> SENT recovery
```

## Apply outcome과 commit 판정

```text
Applied
Conflict
Missing
Indeterminate
```

- `Applied`: commit 또는 exact post-state 확인
- `Conflict`: row는 존재하지만 expected state/token 불일치
- `Missing`: row 없음
- `Indeterminate`: exact pre/post/coherent conflict 판정 불가

Group member mixed state와 delivery/tracking/ledger mismatch는 일반 conflict가 아니라 atomicity breach입니다.

## Outcome unknown

허용 작업:

- warning audit
- bounded metric
- masked operation evidence

금지 작업:

- state write
- retry/failure finalization
- tracking/claim mutation
- grouped fallback
- external resend

Stale sweeper가 owner와 follower group을 quarantine하고 같은 transaction에서 ledger `QUARANTINED`를 기록합니다.

## Revive

Revive는 logical group 단위로 판정합니다.

허용 조건:

- Ledger row 없음
- deterministic owner가 `FAILED`
- 관련 outbox never-sent이며 freshness window 안
- active group lock 없음
- revive policy와 batch limit 충족
- freshness 만료 및 ID generation 소진 사유가 아님; 저장된 request/generation을 초기화하지 않음

적용:

- Owner/follower: `PENDING`, attempt 0, `next_attempt_at = clock`, lock/error clear, version +1
- Commit 뒤 touched outbox: 표준 aggregate projector

Commit `Indeterminate`에서는 stale selected ID set을 반복하지 않고 eligibility selection부터 다시 수행합니다.

## Outbox fanout

### `CompleteWithoutTargets`

Active outbox claim과 child 부재를 같은 transaction에서 확인한 뒤 `SENT`, `sent_at = terminal_at = clock`, lock clear를 적용합니다.

### `MaterializeFanout`

한 transaction에서:

1. Active outbox claim 확인
2. Target room canonicalize/dedupe
3. `(outbox_id,room_id)` idempotent insert, 첫 `next_attempt_at = clock`
4. Outbox lock clear
5. Status `PENDING` 유지

Commit response loss는 canonical child 전체와 lock clear를 primary read-back합니다. 일부 child만 존재하면 atomicity breach입니다.

### Post-fanout writer

Child가 하나라도 있으면 direct writer는 conflict를 반환하고 aggregate projector가 상태를 소유합니다.

## Aggregate projection

```text
PENDING 또는 SENDING child 존재
    -> outbox PENDING

active child 없음 + FAILED 또는 QUARANTINED child 존재
    -> outbox FAILED

active/failure child 없음 + SENT child 존재
    -> outbox SENT

child 없음
    -> outbox PENDING
```

하나의 atomic SQL statement로 유지합니다. Outbox `sent_at`은 NULL일 때만 설정합니다.

Logical group transition은 touched outbox ID 전체를 반환하고 projector가 모두 수렴시켜야 합니다.

`terminal_at` projection:

```text
next=PENDING              -> NULL
PENDING -> SENT/FAILED    -> 집계 문장의 clock
FAILED -> SENT            -> 집계 문장의 clock
terminal -> same terminal -> existing terminal_at 보존
```

Aggregate transaction과 delivery/tracking/ledger transaction은 분리합니다. Projector failure는 telemetry와 background convergence 대상이며 provider 또는 lifecycle command를 재실행하지 않습니다. 예외인 handoff 실패 정산은 같은 lock·sync 문장을 정산 transaction 끝에서 실행하므로 집계 실패가 정산 전체를 rollback하고, 다음 handoff 확인이 정산을 다시 시도합니다.

## 만료 대기 종료

부모 created_at이 기존 ClaimFreshnessWindow를 지난 PENDING은 bounded keyset sweep으로 검사합니다. logical ledger·owner·row_version을 검증하고 SENT/QUARANTINED·SENDING 증거를 우선합니다. known-unsent만 명시적 freshness 만료 사유로 FAILED 종료하며, unknown 증거를 만료로 지우지 않습니다. touched 부모는 표준 aggregate로 수렴하고 만료는 revive에서 제외합니다. ready snapshot은 실제 claim eligibility를 따르며 만료 대기 수는 기존 batch size 상한의 별도 지표로 집계합니다.

## Cleanup과 retention

Full outbox/delivery row와 compact terminal evidence의 retention을 분리합니다.

- Ledger schema version과 backfill completion marker 없이는 cleanup을 실행하지 않습니다.
- `terminal_at < fixed cutoff`인 terminal outbox만 후보입니다.
- Active outbox lock 또는 `PENDING/SENDING` child가 있으면 삭제하지 않습니다.
- `SENT/QUARANTINED` child마다 ledger에 동일하거나 더 강한 evidence가 있어야 합니다.
- Same-logical nonterminal row가 있으면 terminal owner/follower를 삭제하지 않습니다.
- Cleanup retry가 cutoff를 더 최신 시각으로 앞당기면 안 됩니다.
- Candidate selection, ledger verification, sibling guard, delete는 bounded transaction입니다.
- Candidate 부모를 잠근 cleanup은 경쟁 트랜잭션이 잠근 자식·same-logical 행을 기다리지 않고(`SKIP LOCKED`) 건너뛴 행을 관측 누락 증거로 돌려받습니다. 후보를 쥔 채 기다리면 같은 그룹을 잠그는 stale SENDING sweep과 교착합니다.
- Same-logical 조회 한도는 batch 전체가 나눠 쓰지 않고 요청 논리 키마다 `LogicalGroupScanLimit+1`행입니다. 키의 identity 조합(room·kind·identity 후보)이 찾은 행은 그 조합을 만든 키의 한도를 쓰고, 반환 행은 최대 `자식 수 + 요청 키 수 × (LogicalGroupScanLimit+1)`입니다. 한도에 닿은 키는 조합이 찾은 행이 다른 키로 풀리더라도 불완전 관측으로 봅니다.
- 자식이나 same-logical 행을 모두 잠가 관측하지 못한 후보(경쟁 잠금, 같은 논리 키 행의 `LogicalGroupScanLimit` 초과, 그 키의 조회 한도 도달)는 삭제하지 않고 `incomplete_logical_group` guard로 셉니다. 보존은 그 키에 속한 후보에만 적용되어, 과대·잠긴 그룹이 있어도 같은 batch에서 완전 관측한 다른 논리 키의 후보는 그대로 판정·삭제합니다. Cursor는 보존한 후보 뒤로 진행하고 다음 cleanup 주기가 처음부터 다시 검사하므로, 그룹이 한도 안으로 줄거나 경쟁 잠금이 풀리기 전까지 그 후보는 매 주기 보존됩니다. Ledger는 완전 관측한 논리 키만 조회합니다.
- Ledger는 초기 범위에서 자동 삭제하지 않습니다.
- 참조 delivery가 모두 정리된 request 본문만 기존 retention cutoff와 batch 상한으로 삭제합니다.

## 금지 전이

```text
SENT -> *
QUARANTINED -> PENDING/FAILED
FAILED -> PENDING outside group revive
PENDING unclaimed -> SENDING
SENDING -> SENDING re-claim
Follower의 독립 provider send/attempt 증가
```

허용되는 monotonic reconciliation:

```text
PENDING/FAILED/QUARANTINED -> SENT
단, exact same-logical SENT evidence가 있을 때만
```

## Configuration validation

```text
MaxRetries > 0
RetryBackoff > 0
LogicalGroupScanLimit > 0
LockTimeout > 0
DeliverySendTimeout > 0
LockTimeout >= DeliverySendTimeout + max(2*PollInterval, 5s)
ReviveFreshnessWindow > 0 when revive enabled
ClaimFreshnessWindow >= ReviveFreshnessWindow + ReviveInterval
CleanupAfter >= ClaimFreshnessWindow + ReviveFreshnessWindow + CleanupSafetyMargin
SupportedLedgerSchemaVersion = persisted schema_version
LedgerBackfillCompleted = true before writer/cleanup cutover
ExternalLifecycleWriterCount = 0 before high-water capture
```

Production invalid config는 startup에서 거부합니다.

## Observability

### Metric

```text
youtube_delivery_transition_total{rule,from,to,result}
youtube_delivery_logical_group_total{resolution,result}
youtube_delivery_logical_follower_total{action}
youtube_delivery_outcome_unknown_total{transport}
youtube_delivery_commit_adjudication_total{operation,result}
youtube_delivery_commit_indeterminate_total{operation,phase}
youtube_delivery_atomicity_breach_total{operation}
youtube_delivery_tracking_resolution_total{requirement,result}
youtube_delivery_quarantine_total{reason}
youtube_delivery_ledger_operation_total{operation,result}
youtube_delivery_ledger_backfill_total{phase,result}
youtube_delivery_cleanup_guard_total{reason,result}
youtube_outbox_aggregate_transition_total{from,to}
youtube_outbox_aggregate_lag_seconds
```

Raw IDs, logical key, room, error 문자열은 label에 넣지 않습니다.

### Structured log

```text
rule_id
logical_key_hash
logical_owner_id
follower_count
operation_id
client_request_id_hash
from_state
to_state
attempt_count
row_version
tracking_requirement
apply_outcome
provider_effect_started
provider_effect_confirmed
ledger_schema_version
ledger_backfill_completed
```

## Contract tests

### Logical group

```text
TestLogicalKeyUsesCanonicalPostAndRoom
TestSameBatchSelectsDeterministicOwner
TestFollowerCannotUseIndependentAttemptBudget
TestPendingOwnerDefersFollowerToOwnerDue
TestFailedOwnerMirrorsFollowerFailed
TestSentEvidenceReconcilesFailedAndQuarantinedFollowers
TestQuarantinedEvidenceBlocksProviderCall
TestSendingEvidenceDefersProviderCall
TestQuarantineAndSendingMixedStateIsBreach
TestMultipleSendingRowsIsBreach
TestProceedCacheHitStillResolvesLogicalGroup
TestLogicalGroupOverflowFailsClosed
TestInvalidLogicalIdentityFailsBeforeProviderCall
TestLedgerSentEvidenceReconcilesCleanedPhysicalRows
TestLedgerQuarantineEvidenceBlocksProviderCall
```

### Tracking

```text
TestCompleteSentMarksPostSentWithoutClaim
TestOutcomeUnknownInOneRoomDoesNotDeferOtherRoomOfSamePost
TestDispatcher_ProcessOnce_ConcurrentExecutionsSendCommunityShortsAlarmOnce
TestProcessOnce_RetryAfterCommunityShortsSendFailureSendsExactlyOnce
TestRepositoryDuplicateAlarmMarksFinalizeOnePostLevelSentState
TestRepositoryMarkAlarmSentBatchRepairsMissingAlarmStateForAlreadySentTracking
```

### CAS/operation

```text
TestClaimIncrementsVersion
TestBeginSendingAllOrNone
TestBeginCommitUnknownNeverCallsProviderBeforeReadBack
TestCompleteSentUpdatesOwnerFollowersAndTrackingAtomically
TestCompleteSentConfirmedNonCommitRetriesDBOnly
TestCompleteSentIndeterminateNeverResends
TestMixedGroupStateIsAtomicityBreach
TestSuccessEnvelopeCommitsDeliveryTrackingAndLedgerAtomically
TestQuarantineEnvelopeCommitsDeliveryAndLedgerAtomically
TestLedgerReadBackMismatchIsAtomicityBreach
```

### Outcome/crash

```text
TestOutcomeUnknownWritesNothingAndKeepsClaim
TestGroupedUnknownNeverFallsBack
TestCrashAfterSendingCommitQuarantinesLogicalGroup
TestCrashAfterProviderSuccessNeverGuessesTracking
TestAggregateReconcilesAfterDeliveryCommitCrash
TestAggregateFailureDoesNotRollBackTerminalEnvelope
```

### Revive/cleanup

```text
TestReviveResetsLogicalOwnerAndFollowers
TestReviveRejectsSentOrQuarantinedGroup
TestReviveRejectsLogicalKeyPresentInLedger
TestCleanupRetainsTerminalEvidenceWhileFollowerNonterminal
TestCleanupRequiresCompletedLedgerBackfill
TestCleanupRequiresLedgerEvidenceForEverySentOrQuarantinedDelivery
TestTerminalAtUsesLatestTerminalEvidence
TestCleanupAfterExceedsFreshnessEnvelope
```

### Backfill

```text
TestBackfillCapturesFixedDeliveryAndOutboxHighWater
TestBackfillResumeUsesDurableCursor
TestBackfillUpsertIsMonotonic
TestBackfillCompletionRequiresCanonicalAntiJoinZero
TestBackfillCompletionRejectsUnprovenHistoricalCoverage
```

## 완료 판정

1. Policy와 SQL이 retry를 중복 계산하지 않습니다.
2. Community/Shorts canonical logical key resolver가 하나입니다.
3. Logical group의 provider/attempt owner가 하나입니다.
4. Follower가 owner retry budget을 우회하지 않습니다.
5. Durable state와 impossible mixed state를 같은 규칙으로 해석합니다.
6. Post-level cache가 room-level logical resolution을 생략하지 않습니다.
7. Outcome unknown은 write, release, fallback, resend를 하지 않습니다.
8. Claim과 모든 mutation이 version을 증가시킵니다.
9. Begin/finalization operation이 all-or-none입니다.
10. Tracking exact token과 already-sent가 idempotent하게 수용됩니다.
11. Provider success 뒤 모든 DB 오류에서 provider 재호출은 0회입니다.
12. ID-only success recovery가 없습니다.
13. Child outbox status는 aggregate projector만 계산합니다.
14. Worker 전용 store/row model이 alarm-worker internal에 있습니다.
15. 모든 confirmed-success와 outcome-unknown terminal transition이 같은 transaction에서 logical ledger evidence를 남깁니다.
16. 고정 high-water backfill이 durable cursor로 재개되고 canonical anti-join 0건으로 완료됩니다.
17. 삭제된 과거 row까지 포함한 `legacy_coverage_start_at`이 모든 replay/freshness 경계보다 앞선다는 근거가 없으면 cutover를 차단합니다.
18. Cleanup은 `terminal_at`과 completed ledger gate를 사용하고, logical terminal evidence를 freshness envelope보다 오래 보존합니다.
19. Poller와 API를 포함한 모든 lifecycle 직접 writer가 제거되거나 새 transition owner로 전환됩니다.
20. Contract, logical-owner, tracking, ledger backfill, integration, crash, commit fault-injection, race test가 통과합니다.
21. Decision record를 `verified`로 올릴 evidence가 저장소에 남습니다.

## 공통 attempt 계측

`workercontract` Begin/End와 terminal counter는 실제 provider operation 한 번에 대응합니다. grouped 호출은 한 번이며 방별 delivery 결과와 구분합니다. claim·preparation·이미 충족된 행은 success를 만들지 않습니다. success/failed/timeout/canceled/panic/outcome_unknown은 provider 결과로 결정하며, DB finalization 실패는 provider 성공을 실패로 바꾸지 않습니다.
