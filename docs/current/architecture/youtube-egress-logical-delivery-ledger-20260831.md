# YouTube logical delivery ledger 계약

작성일: 2026-08-31 KST  
적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
상위 계약: [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md)

## 목적

Full outbox/delivery row의 cleanup 수명과 logical delivery 중복 방지 수명을 분리합니다.

현재 cleanup은 terminal outbox를 삭제하며 기준 시각으로 `COALESCE(sent_at, created_at)`을 사용합니다. `FAILED`에는 terminal 진입 시각이 없으므로 오래 대기한 outbox가 늦게 실패하면 즉시 삭제될 수 있습니다. Outbox가 삭제되면 cascade로 delivery row도 사라져 same-room fulfillment와 outcome-unknown evidence를 다시 확인할 수 없습니다.

Full payload/outbox/delivery를 무기한 보존하지 않고, one-row-per-logical-delivery terminal evidence와 backfill completion state만 장기 보존합니다.

## 결정

### L-001. Ledger가 logical terminal evidence의 정본이다

`youtube_notification_delivery_ledger`는 다음 두 상태만 보존합니다.

```text
SENT
QUARANTINED
```

`FAILED`는 provider가 전달하지 않았다고 확인된 상태이므로 durable duplicate guard가 아닙니다. Retained physical row가 있는 동안 owner/follower retry budget을 통제하지만, cleanup 뒤 새 known-safe attempt를 영구 금지하지 않습니다.

### L-002. Logical identity는 kind별 canonical identity를 사용한다

Ledger primary identity:

```text
(kind, logical_id, room_id)
```

`logical_id`:

```text
COMMUNITY_POST, NEW_SHORT:
canonical_post_id

NEW_VIDEO, LIVE_STREAM, MILESTONE:
outbox.content_id
```

Kind가 key 일부이므로 같은 YouTube content라도 서로 다른 product event를 구분합니다. Canonical resolver는 provider 호출 전에 payload와 identity를 검증하며 오류를 raw `content_id` fallback으로 바꾸지 않습니다.

### L-003. Ledger state는 단조 증가한다

허용:

```text
없음 -> QUARANTINED
없음 -> SENT
QUARANTINED -> SENT
SENT -> SENT idempotent
QUARANTINED -> QUARANTINED idempotent
```

금지:

```text
SENT -> QUARANTINED
SENT -> 없음
QUARANTINED -> 없음
```

`SENT`가 `QUARANTINED`보다 강한 evidence입니다.

### L-004. Terminal envelope는 하나의 transaction이다

- Provider success: owner/follower `SENT`, 필요한 tracking mutation, ledger `SENT`를 같은 transaction에서 commit합니다.
- Stale outcome unknown: logical group `QUARANTINED`, ledger `QUARANTINED`를 같은 transaction에서 commit합니다.
- Same-logical sent reconciliation: follower `SENT`와 ledger `SENT` 재확인을 같은 transaction에서 수행합니다.
- Ledger write 또는 read-back mismatch를 best-effort로 무시하지 않습니다.

Outbox aggregate와 `terminal_at` projection은 이 transaction의 일부가 아닙니다. Terminal envelope가 touched outbox ID를 반환하면 immediate projector가 별도 transaction으로 갱신하고, 실패 시 background projector가 수렴시킵니다. Aggregate 실패는 provider 성공 transaction을 rollback하거나 provider 재호출을 유발하지 않습니다.

### L-005. Preparation은 ledger를 먼저 읽는다

Logical group resolution 순서:

```text
1. Ledger SENT
   -> LogicalFulfilled

2. Ledger QUARANTINED
   -> LogicalUnresolved

3. Retained physical SENDING/PENDING/FAILED group
   -> in-flight 또는 deterministic owner
```

Retained physical `SENT/QUARANTINED` row는 migration compatibility와 audit evidence일 뿐, 완료 marker 이후 terminal logical state의 primary read path가 아닙니다.

### L-006. Ledger는 초기 범위에서 자동 cleanup하지 않는다

자동 retention을 도입하려면 별도 결정에서 다음을 모두 증명해야 합니다.

- Producer와 수동 replay가 retention보다 오래된 logical content를 재생성하지 않습니다.
- Provider dedupe 또는 별도 immutable receipt가 있습니다.
- `QUARANTINED` operator reconciliation이 종료되었습니다.
- 삭제가 same-room duplicate risk를 만들지 않습니다.

## Schema

Additive schema migration은 column/table/constraint만 생성합니다. Migration transaction 안에서 기존 row를 무제한 update하지 않습니다. 아래 SQL은 목표 final shape이며, existing delivery의 `NOT NULL` 적용은 migration 규약대로 nullable constant-default column 추가, `NOT VALID` null/nonnegative check, `VALIDATE CONSTRAINT`, `SET NOT NULL` 순서로 구현합니다. 모든 statement는 partial-file replay에 안전한 idempotent guard를 가집니다.

```sql
ALTER TABLE youtube_notification_delivery
    ADD COLUMN row_version bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_youtube_notification_delivery_row_version
        CHECK (row_version >= 0);

ALTER TABLE youtube_notification_outbox
    ADD COLUMN terminal_at timestamptz;

CREATE TABLE youtube_notification_delivery_ledger (
    kind text NOT NULL,
    logical_id varchar(50) NOT NULL,
    room_id varchar(100) NOT NULL,
    status text NOT NULL,
    first_recorded_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    sent_at timestamptz,
    quarantined_at timestamptz,
    source_delivery_id bigint,
    PRIMARY KEY (kind, logical_id, room_id),
    CONSTRAINT chk_youtube_notification_delivery_ledger_kind_vocab
        CHECK (kind IN (
            'NEW_VIDEO',
            'NEW_SHORT',
            'LIVE_STREAM',
            'COMMUNITY_POST',
            'MILESTONE'
        )),
    CONSTRAINT chk_youtube_notification_delivery_ledger_identity
        CHECK (
            length(btrim(logical_id)) > 0
            AND length(btrim(room_id)) > 0
            AND logical_id = btrim(logical_id)
            AND room_id = btrim(room_id)
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_status
        CHECK (status IN ('SENT', 'QUARANTINED')),
    CONSTRAINT chk_youtube_notification_delivery_ledger_shape
        CHECK (
            (
                status = 'SENT'
                AND sent_at IS NOT NULL
            )
            OR
            (
                status = 'QUARANTINED'
                AND sent_at IS NULL
                AND quarantined_at IS NOT NULL
            )
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_time_order
        CHECK (
            updated_at >= first_recorded_at
            AND (sent_at IS NULL OR sent_at >= first_recorded_at)
            AND (quarantined_at IS NULL OR quarantined_at >= first_recorded_at)
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_source
        CHECK (source_delivery_id IS NULL OR source_delivery_id > 0)
);

CREATE TABLE youtube_notification_delivery_ledger_state (
    singleton boolean PRIMARY KEY DEFAULT true,
    schema_version integer NOT NULL,
    delivery_high_water_id bigint NOT NULL,
    outbox_high_water_id bigint NOT NULL,
    delivery_cursor_id bigint NOT NULL DEFAULT 0,
    delivery_verify_cursor_id bigint NOT NULL DEFAULT 0,
    outbox_cursor_id bigint NOT NULL DEFAULT 0,
    legacy_coverage_start_at timestamptz,
    coverage_verified_at timestamptz,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL,
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_singleton
        CHECK (singleton),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_version
        CHECK (schema_version > 0),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_cursors
        CHECK (
            delivery_high_water_id >= 0
            AND outbox_high_water_id >= 0
            AND delivery_cursor_id BETWEEN 0 AND delivery_high_water_id
            AND delivery_verify_cursor_id BETWEEN 0 AND delivery_high_water_id
            AND outbox_cursor_id BETWEEN 0 AND outbox_high_water_id
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_time_order
        CHECK (
            updated_at >= started_at
            AND (
                (legacy_coverage_start_at IS NULL AND coverage_verified_at IS NULL)
                OR (
                    legacy_coverage_start_at IS NOT NULL
                    AND coverage_verified_at IS NOT NULL
                    AND legacy_coverage_start_at <= coverage_verified_at
                    AND coverage_verified_at <= updated_at
                )
            )
            AND (
                completed_at IS NULL
                OR (
                    completed_at >= coverage_verified_at
                    AND completed_at <= updated_at
                )
            )
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_completion
        CHECK (
            completed_at IS NULL
            OR (
                delivery_cursor_id = delivery_high_water_id
                AND delivery_verify_cursor_id = delivery_high_water_id
                AND outbox_cursor_id = outbox_high_water_id
                AND legacy_coverage_start_at IS NOT NULL
                AND coverage_verified_at IS NOT NULL
            )
        )
);
```

`source_delivery_id`에는 FK를 두지 않습니다. Full delivery cleanup 뒤에도 ledger가 독립적으로 남아야 합니다. 초기 index는 두 table의 primary key뿐이며, status/age scan index는 실제 query evidence가 있을 때만 추가합니다.

`schema_version`은 binary가 지원하는 정확한 ledger contract version입니다. 알 수 없는 version이나 `completed_at IS NULL`이면 writer/cleanup cutover를 거부합니다.

## Backfill state write contract

State row도 단조 상태입니다.

```text
singleton insert       한 번만 허용
schema_version         immutable
delivery/outbox high-water  immutable
started_at             immutable
cursors                commit된 batch 끝 ID로만 증가
coverage timestamps    absent -> verified values
completed_at           absent -> completion time
```

Command는 state row를 `SELECT ... FOR UPDATE`하고 expected schema/high-water/current cursor를 검사한 뒤 data write와 cursor advance를 같은 transaction에 commit합니다. Cursor regression, high-water 재설정, completion clear/overwrite는 conflict입니다. 새 backfill generation이 필요하면 기존 row를 덮어쓰지 않고 별도 schema/version 결정과 migration을 먼저 만듭니다.

## Monotonic upsert

호출자는 `observed_at`, `source_delivery_id`, canonical key를 transaction 안에서 제공합니다. Backfill은 source terminal evidence 시각을 `first_recorded_at`으로 사용하고, 시각이 없으면 backfill 시작 시각을 보수적으로 사용합니다.

### `RecordSent`

```sql
INSERT INTO youtube_notification_delivery_ledger AS current (
    kind,
    logical_id,
    room_id,
    status,
    first_recorded_at,
    updated_at,
    sent_at,
    quarantined_at,
    source_delivery_id
) VALUES ($1, $2, $3, 'SENT', $4, $4, $4, NULL, $5)
ON CONFLICT (kind, logical_id, room_id) DO UPDATE
SET status = 'SENT',
    first_recorded_at = LEAST(
        current.first_recorded_at,
        EXCLUDED.first_recorded_at
    ),
    updated_at = GREATEST(current.updated_at, EXCLUDED.updated_at),
    sent_at = CASE
        WHEN current.status = 'SENT'
            THEN LEAST(current.sent_at, EXCLUDED.sent_at)
        ELSE EXCLUDED.sent_at
    END,
    quarantined_at = current.quarantined_at,
    source_delivery_id = CASE
        WHEN current.status = 'QUARANTINED' THEN EXCLUDED.source_delivery_id
        WHEN EXCLUDED.sent_at < current.sent_at THEN EXCLUDED.source_delivery_id
        ELSE COALESCE(current.source_delivery_id, EXCLUDED.source_delivery_id)
    END
RETURNING status, sent_at, quarantined_at, source_delivery_id;
```

Existing `SENT`에는 가장 이른 confirmed `sent_at`을 보존하고, `QUARANTINED`는 `SENT`로 승격합니다. 과거 evidence의 처리 순서가 결과를 바꾸지 않습니다.

### `RecordQuarantined`

```sql
INSERT INTO youtube_notification_delivery_ledger AS current (
    kind,
    logical_id,
    room_id,
    status,
    first_recorded_at,
    updated_at,
    sent_at,
    quarantined_at,
    source_delivery_id
) VALUES ($1, $2, $3, 'QUARANTINED', $4, $4, NULL, $4, $5)
ON CONFLICT (kind, logical_id, room_id) DO UPDATE
SET status = current.status,
    first_recorded_at = CASE
        WHEN current.status = 'QUARANTINED'
            THEN LEAST(current.first_recorded_at, EXCLUDED.first_recorded_at)
        ELSE current.first_recorded_at
    END,
    updated_at = CASE
        WHEN current.status = 'QUARANTINED'
            THEN GREATEST(current.updated_at, EXCLUDED.updated_at)
        ELSE current.updated_at
    END,
    sent_at = current.sent_at,
    quarantined_at = CASE
        WHEN current.status = 'QUARANTINED'
            THEN LEAST(current.quarantined_at, EXCLUDED.quarantined_at)
        ELSE current.quarantined_at
    END,
    source_delivery_id = CASE
        WHEN current.status = 'QUARANTINED'
            THEN COALESCE(current.source_delivery_id, EXCLUDED.source_delivery_id)
        ELSE current.source_delivery_id
    END
RETURNING status, sent_at, quarantined_at, source_delivery_id;
```

Existing `SENT`는 그대로 반환합니다. 호출자는 반환 상태가 `SENT`이면 quarantine commit을 계속하지 않고 fulfilled reconciliation으로 전환합니다. 두 upsert의 어느 것이 먼저 실행되어도 최종 상태는 `SENT`입니다.

## Outbox `terminal_at`

`terminal_at`은 현재 outbox 상태가 마지막으로 terminal 상태에 진입한 시각입니다.

```text
PENDING
    terminal_at = NULL

PENDING -> SENT/FAILED
    terminal_at = transition time

FAILED -> SENT
    terminal_at = new transition time

FAILED -> PENDING revive
    terminal_at = NULL

동일 terminal 상태의 idempotent aggregate sync
    existing terminal_at 보존
```

Aggregate projector가 child 상태에서 outbox terminal 상태를 계산할 때 상태 변경과 `terminal_at`을 같은 transaction에서 갱신합니다. Cleanup은 `COALESCE(sent_at, created_at)`이 아니라 `terminal_at < fixed_cutoff`를 사용합니다.

## Migration과 rollout

### 1. 사전 조건

- Canonical identity resolver를 cross-runtime public pure package로 먼저 승격합니다.
- Alarm worker, poller batch repository, tracking code가 같은 resolver와 kind vocabulary를 사용합니다.
- Invalid payload/identity는 provider 호출 전에 fail closed입니다.
- Poller batch repository의 direct delivery/outbox terminal·rearm writer를 제거하고, compatibility alarm-worker만 terminal transition을 쓰게 합니다.
- Compatibility binary와 locally built backfill binary를 배포 artifact에 함께 포함합니다. Remote host에서 build하지 않습니다.

### 2. Additive schema와 compatibility writer

승인된 maintenance window의 순서는 다음과 같습니다.

1. Alarm-worker와 poller batch repository를 실행하는 runtime을 포함해 inventoried lifecycle writer를 모두 중지합니다.
2. Column/table/constraint만 포함하는 additive migration을 적용합니다.
3. Poller direct lifecycle mutation이 제거된 runtime과 cleanup을 frozen 상태로 둔 compatibility alarm-worker를 배포합니다.
4. Source/outbox producer와 compatibility writer를 순서대로 시작합니다.
5. Compatibility writer가 모든 kind에서 새 success/quarantine ledger와 outbox `terminal_at`을 유지하는지 확인합니다.

Legacy와 새 writer를 동시에 실행하지 않습니다. Schema migration과 restart는 production 승인 대상이며, 이 설계 문서 자체는 실행 권한을 부여하지 않습니다.

Compatibility writer 규칙:

- Logical identity를 provider 호출 전에 해석합니다.
- Legacy success도 delivery/tracking/ledger `SENT`를 같은 transaction에 기록합니다.
- Legacy stale quarantine도 delivery/ledger `QUARANTINED`를 같은 transaction에 기록합니다.
- 모든 outbox aggregate writer가 `terminal_at` semantics를 지킵니다.
- Poller는 source observation과 outbox create만 수행하고 existing delivery/outbox를 `PENDING/SENT`로 직접 바꾸지 않습니다.
- Group-safe worker revive가 cutover되기 전까지 제거된 poller rearm의 liveness를 일시 중단하며 tracking을 room success로 추정하지 않습니다.
- Ledger/state completion 전에는 terminal cleanup을 실행하지 않습니다.

### 3. Fixed-high-water state 초기화

Compatibility writer가 live이고 cleanup이 frozen이며 writer audit가 alarm-worker 밖 lifecycle mutation 0건을 확인한 상태에서 one-shot command가 한 transaction으로 state row를 생성합니다.

```text
delivery_high_water_id = MAX(youtube_notification_delivery.id), 없으면 0
outbox_high_water_id   = MAX(youtube_notification_outbox.id), 없으면 0
delivery_cursor_id     = 0
delivery_verify_cursor_id = 0
outbox_cursor_id       = 0
started_at             = database clock
completed_at           = NULL
```

State row가 이미 있으면 high-water를 다시 잡지 않고 기존 cursor에서 재개합니다. High-water 이후 생성되거나 terminal로 전이되는 row는 compatibility writer가 ledger/`terminal_at`을 유지합니다.

### 4. Bounded resumable backfill

Dedicated Go command는 alarm-worker image에 로컬 build된 binary로 포함하고, remote에서는 image entrypoint override를 사용하는 no-build one-shot으로 실행합니다. Backfill은 production data write 승인 대상입니다.

Delivery pass:

- `0 < id <= delivery_high_water_id`를 ID 오름차순 bounded batch로 모두 스캔합니다.
- 모든 kind의 `SENT/QUARANTINED` row에 canonical resolver를 적용합니다.
- `SENT`가 `QUARANTINED`보다 강한 monotonic upsert를 사용합니다.
- Invalid kind/payload/logical identity를 skip하지 않고 batch를 실패시킵니다.
- Ledger writes와 `delivery_cursor_id` 증가는 같은 transaction입니다.
- Raw logical ID와 room ID를 일반 log에 남기지 않습니다.

Outbox pass:

- `0 < id <= outbox_high_water_id`를 ID 오름차순 bounded batch로 스캔합니다.
- `PENDING`은 `terminal_at = NULL`을 유지합니다.
- `SENT`는 `outbox.sent_at`과 `MAX(child.sent_at)` 중 가장 늦은 non-null 시각을 사용하고, 둘 다 없으면 `backfill_started_at`을 사용합니다.
- `FAILED`는 정확한 과거 transition 시각이 없으므로 backfill 시작 시각을 사용합니다.
- Update와 `outbox_cursor_id` 증가는 같은 transaction입니다.

Process 종료, timeout, connection loss 뒤에도 state row의 cursor에서 재개합니다. Cursor는 성공적으로 commit한 마지막 스캔 ID만 가리킵니다.

### 5. 완전성 검증

Delivery pass 뒤 fixed range 전체를 다시 bounded scan하여 canonical source key와 ledger를 anti-join합니다.

```text
source SENT        -> ledger SENT 필수
source QUARANTINED -> ledger QUARANTINED 또는 SENT 필수
```

각 검증 batch가 mismatch 0건일 때만 같은 transaction에서 `delivery_verify_cursor_id`를 전진시킵니다. Invalid identity와 mismatch는 완료를 차단합니다. Distinct count equality와 random sample은 진단 자료일 뿐 completion 증거가 아닙니다.

### 6. 삭제된 historical evidence coverage

Backfill 시작 전에 이미 cleanup된 delivery는 현재 table에서 복구할 수 없습니다. 다음 값을 별도 evidence로 산출합니다.

```text
legacy_coverage_start_at:
    ledger 또는 retained physical terminal row가 완전하다고 검증된 가장 이른 logical-event 경계

replay_floor_at(path):
    각 producer, revive, repair, 수동 replay 경로가 다시 만들 수 있는 가장 이른 logical-event 경계
```

모든 경로에 대해 다음이 독립된 source-retention/config/code evidence로 증명되어야 합니다.

```text
legacy_coverage_start_at <= replay_floor_at(path)
```

경로 하나라도 unbounded, unknown, 또는 운영 관행에만 의존하면 `legacy_coverage_start_at`, `coverage_verified_at`, `completed_at`을 기록하지 않습니다. 이 경우 compatibility writer와 cleanup freeze를 유지하고 lifecycle writer/cleanup cutover를 차단합니다. 삭제된 row를 count equality, 추정, provider success 가정으로 성공 처리하지 않습니다.

### 7. Completion marker

다음을 모두 만족한 transaction만 state row를 `completed_at = database clock`으로 갱신합니다.

1. Delivery/backfill/verify/outbox cursor가 각각 fixed high-water에 도달했습니다.
2. Canonical anti-join mismatch가 0건입니다.
3. Invalid identity가 0건입니다.
4. 모든 terminal kind가 범위에 포함되었습니다.
5. Historical coverage inequality가 모든 replay path에 대해 증명되었습니다.
6. Compatibility writer가 high-water 이후 terminal event를 기록 중입니다.
7. Alarm-worker 밖 direct lifecycle writer가 0건입니다.

Application startup은 지원하는 `schema_version`과 `completed_at`을 함께 확인합니다. Process memory flag나 수동 checklist만으로 cutover하지 않습니다.

## Read contract

```go
type LogicalLedgerState uint8

const (
    LedgerAbsent LogicalLedgerState = iota
    LedgerSent
    LedgerQuarantined
)

type LogicalLedgerEntry struct {
    Key              LogicalDeliveryKey
    State            LogicalLedgerState
    FirstRecordedAt  time.Time
    UpdatedAt        time.Time
    SentAt           *time.Time
    QuarantinedAt    *time.Time
    SourceDeliveryID *int64
}

type LogicalLedger interface {
    Load(context.Context, []LogicalDeliveryKey) (map[LogicalDeliveryKey]LogicalLedgerEntry, error)
}
```

Batch API를 사용하며 per-row N+1 lookup을 기본 경로로 사용하지 않습니다.

## Transaction contract

### Success

`CompleteSent` transaction:

1. Owner/follower send fence와 pre-state를 확인합니다.
2. Delivery group을 `SENT`로 전이합니다.
3. Tracking requirement를 충족합니다.
4. Ledger `RecordSent`를 적용합니다.
5. Commit합니다.
6. Touched outbox ID를 별도 aggregate projector에 전달합니다.

Commit read-back은 owner/follower/tracking뿐 아니라 ledger `SENT`도 확인합니다. Aggregate는 이 adjudication envelope에 포함하지 않습니다.

### Quarantine

`QuarantineLogicalGroup` transaction:

1. Stale owner `SENDING`을 확인합니다.
2. Owner/follower를 `QUARANTINED`로 전이합니다.
3. Ledger `RecordQuarantined`를 적용합니다.
4. Commit합니다.
5. Touched outbox ID를 별도 aggregate projector에 전달합니다.

Ledger가 이미 `SENT`이면 quarantine를 commit하지 않고 fulfilled reconciliation 경로로 전환합니다.

### Reconcile fulfilled

Ledger `SENT`를 근거로 follower를 `SENT`로 수렴시킬 때 transaction 안에서 같은 key/state를 다시 확인합니다. Physical source row가 cleanup되어 없어도 ledger가 충분한 evidence입니다.

## Cleanup after ledger cutover

Full outbox/delivery cleanup은 다음 조건을 모두 확인합니다.

- 지원하는 ledger schema version과 non-null completion marker
- Outbox terminal state와 non-null `terminal_at`
- `terminal_at < fixed_cutoff`
- Active outbox lock 없음
- Child `PENDING/SENDING` 없음
- Child `SENT`마다 same-key ledger `SENT`
- Child `QUARANTINED`마다 same-key ledger `QUARANTINED` 또는 `SENT`
- Failed logical owner를 삭제할 때 same-logical retained `PENDING/SENDING` sibling 없음

Candidate selection, ledger verification, sibling guard, delete는 bounded transaction에서 수행합니다. Cleanup commit response loss는 candidate ID와 expected `terminal_at`을 기준으로 read-back합니다. Retry 시 최초 fixed cutoff를 새 현재 시각으로 앞당기지 않습니다.

## Failure behavior

### Ledger read failure

Fail closed입니다. Provider를 호출하지 않습니다. Claimed `PENDING` owner/follower는 attempt를 소비하지 않는 defer 또는 lock expiry 경로로 돌아갑니다.

### Ledger write 또는 commit read-back failure

Terminal envelope 전체를 rollback하거나 commit adjudication으로 판정합니다.

- Provider success 뒤 non-commit confirmed: 같은 operation의 DB-only finalization만 재시도하고 provider를 재호출하지 않습니다.
- Commit indeterminate 또는 ledger mismatch: `outcome_unknown`/atomicity breach로 보존하고 provider를 재호출하지 않습니다.
- Quarantine non-commit: stale owner는 다음 sweeper에서 같은 fenced transition을 재시도할 수 있습니다.

### Aggregate failure

Committed delivery/tracking/ledger를 되돌리지 않습니다. Immediate projector 실패를 기록하고 background projector가 touched outbox를 수렴시킵니다.

### Backfill incomplete

New lifecycle writer cutover와 모든 kind의 terminal cleanup을 금지합니다.

## Observability

```text
youtube_delivery_ledger_operation_total{operation,result}
youtube_delivery_ledger_state_total{status}
youtube_delivery_ledger_backfill_total{phase,result}
youtube_delivery_ledger_backfill_lag{phase}
youtube_delivery_cleanup_guard_total{reason,result}
youtube_delivery_terminal_at_missing_total{status}
```

Raw logical ID와 room ID를 metric label이나 일반 log에 넣지 않습니다.

## Tests

### Schema/store

```text
TestLedgerRejectsInvalidKindIdentityAndShape
TestLedgerSentInsert
TestLedgerQuarantinedInsert
TestLedgerSentWinsConcurrentQuarantine
TestLedgerQuarantineCannotDowngradeSent
TestLedgerPreservesFirstSentAt
TestLedgerSourceDeliverySurvivesDeliveryCleanup
TestLedgerStateRejectsPrematureCompletion
TestLedgerStateRejectsCursorRegressionAndHighWaterRewrite
```

### Transaction

```text
TestCompleteSentCommitsLedgerTrackingAndGroupAtomically
TestCompleteSentRollbackLeavesNoPartialLedger
TestQuarantineCommitsLedgerAndGroupAtomically
TestQuarantineSeesConcurrentSentAndReconcilesFulfilled
TestCommitReadBackIncludesLedger
TestAggregateFailureDoesNotRollBackTerminalEnvelope
```

### Backfill

```text
TestBackfillCapturesFixedHighWaterOnce
TestBackfillIncludesEveryTerminalKind
TestBackfillSentWinsQuarantined
TestBackfillResumesFromCommittedCursor
TestBackfillRejectsInvalidCanonicalIdentity
TestBackfillCompletionRequiresCanonicalAntiJoinZero
TestBackfillCompletionRejectsUnknownHistoricalCoverage
TestHighWaterConcurrentTerminalUsesCompatibilityWriter
```

### Cleanup

```text
TestCleanupUsesTerminalAtNotCreatedAt
TestLateFailedOutboxRetainedForCleanupAfter
TestCleanupRequiresCompletedLedgerState
TestCleanupRequiresSentOrQuarantinedLedgerEvidence
TestCleanupRetainsFailedOwnerWithActiveSibling
TestCleanupResponseLossUsesOriginalCutoff
```

## 완료 조건

1. Logical terminal read path는 ledger가 정본입니다.
2. `SENT`가 concurrent `QUARANTINED`보다 항상 우선합니다.
3. Success/quarantine와 ledger write가 같은 transaction입니다.
4. Aggregate projection은 terminal envelope와 분리되고 실패해도 provider를 재호출하지 않습니다.
5. 모든 terminal kind가 fixed-high-water와 durable cursor로 backfill됩니다.
6. Canonical anti-join mismatch와 invalid identity가 0건입니다.
7. Historical coverage가 모든 replay path보다 충분함을 증명하지 못하면 completion이 차단됩니다.
8. Completion marker 없이 writer/cleanup cutover가 진행되지 않습니다.
9. Outbox cleanup은 `terminal_at`과 fixed cutoff를 사용합니다.
10. Full delivery cleanup 뒤에도 sent/unknown dedupe evidence가 남습니다.
11. Ledger는 초기 범위에서 자동 삭제되지 않습니다.
