# YouTube logical delivery ledger 계약

작성일: 2026-08-31 KST  
적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`  
상위 계약: [`youtube-egress-lifecycle-contract-20260831.md`](youtube-egress-lifecycle-contract-20260831.md)

## 목적

Full outbox/delivery row의 cleanup 수명과 logical delivery 중복 방지 수명을 분리합니다.

현재 cleanup은 terminal outbox를 삭제하며 기준 시각으로 `COALESCE(sent_at, created_at)`을 사용합니다. `SENT`는 `sent_at`이 있지만 `FAILED`에는 terminal 진입 시각이 없어 오래 대기한 outbox가 늦게 실패하면 즉시 정리될 수 있습니다. Outbox가 삭제되면 해당 delivery row는 더 이상 per-room sent/unknown evidence로 사용할 수 없습니다.

Full payload/outbox/delivery를 무기한 보존하는 대신, logical delivery의 confirmed terminal evidence만 compact ledger에 남깁니다.

## 결정

### L-001. Ledger가 logical terminal evidence의 정본이다

새 테이블을 추가합니다.

```text
youtube_notification_delivery_ledger
```

이 테이블은 다음 두 상태만 보존합니다.

```text
SENT
QUARANTINED
```

`FAILED`는 provider가 전달하지 않았다고 확인된 상태이므로 durable duplicate guard가 아닙니다. Retained physical row가 있는 동안 logical owner/follower retry budget을 통제하지만 cleanup 뒤 새 known-safe attempt를 영구 금지하지 않습니다.

### L-002. Logical identity는 kind별 canonical identity를 사용한다

Ledger primary identity:

```text
(kind, logical_id, room_id)
```

`logical_id`:

```text
Community/Shorts:
canonical_post_id

기타 kind:
outbox.content_id
```

Kind가 key 일부이므로 같은 YouTube content라도 UPCOMING, LIVE, NEW_VIDEO 등 서로 다른 product event를 구분합니다.

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

### L-004. Ledger write는 delivery transition과 같은 transaction이다

- Provider success: owner/follower `SENT`, tracking, ledger `SENT`를 같은 transaction에서 commit합니다.
- Stale outcome unknown: logical group `QUARANTINED`, ledger `QUARANTINED`를 같은 transaction에서 commit합니다.
- Same-logical sent reconciliation: follower `SENT`와 existing/new ledger `SENT` 확인을 같은 transaction에서 수행합니다.

Ledger write 실패를 best-effort로 무시하지 않습니다.

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

Retained physical `SENT/QUARANTINED` row는 migration compatibility와 audit evidence일 뿐, cutover 뒤 terminal logical state의 primary read path가 아닙니다.

### L-006. Ledger는 초기 범위에서 자동 cleanup하지 않는다

Full outbox/delivery보다 훨씬 작은 one-row-per-logical-delivery ledger를 장기 보존합니다.

자동 retention을 도입하려면 다음을 별도 결정으로 증명해야 합니다.

- Producer가 같은 logical content를 retention 이후 재생성하지 않음
- Provider dedupe 또는 별도 immutable receipt가 있음
- `QUARANTINED` operator reconciliation이 종료됨
- 삭제가 same-room duplicate risk를 만들지 않음

## Schema

제안 schema:

```sql
CREATE TABLE youtube_notification_delivery_ledger (
    kind text NOT NULL,
    logical_id text NOT NULL,
    room_id text NOT NULL,
    status text NOT NULL,
    first_recorded_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    sent_at timestamptz,
    quarantined_at timestamptz,
    source_delivery_id bigint,
    PRIMARY KEY (kind, logical_id, room_id),
    CONSTRAINT chk_youtube_notification_delivery_ledger_status
        CHECK (status IN ('SENT', 'QUARANTINED')),
    CONSTRAINT chk_youtube_notification_delivery_ledger_shape
        CHECK (
            (status = 'SENT' AND sent_at IS NOT NULL)
            OR
            (status = 'QUARANTINED' AND quarantined_at IS NOT NULL)
        )
);
```

`source_delivery_id`는 evidence reference이며 FK를 두지 않습니다. Full delivery cleanup 뒤에도 ledger가 독립적으로 남아야 하기 때문입니다.

초기 index는 primary key뿐입니다. Status/age scan index는 실제 운영 query evidence가 있을 때 추가합니다.

## Monotonic upsert

### `RecordSent`

```text
row 없음
    -> SENT insert

existing QUARANTINED
    -> SENT, sent_at 설정, quarantined_at 보존

existing SENT
    -> sent_at 최초 값 보존, updated_at만 필요 시 갱신
```

`sent_at`은 최초 confirmed fulfillment 시각을 보존합니다.

### `RecordQuarantined`

```text
row 없음
    -> QUARANTINED insert

existing QUARANTINED
    -> 최초 quarantined_at 보존

existing SENT
    -> no-op, SENT 유지
```

Concurrent `RecordSent`와 `RecordQuarantined`가 경합해도 최종 상태는 `SENT`여야 합니다. SQL upsert와 integration test가 이를 보장해야 합니다.

## Outbox terminal timestamp

Full-row cleanup에는 outbox terminal 진입 시각이 별도로 필요합니다.

```sql
ALTER TABLE youtube_notification_outbox
ADD COLUMN terminal_at timestamptz;
```

Semantics:

```text
PENDING
    terminal_at = NULL

PENDING -> SENT/FAILED
    terminal_at = transition time

FAILED -> SENT
    terminal_at = new transition time

FAILED -> PENDING revive
    terminal_at = NULL

Terminal 상태가 그대로 유지되는 idempotent aggregate sync
    terminal_at 최초 값 보존
```

Cleanup은 `COALESCE(sent_at, created_at)`이 아니라 `terminal_at < cutoff`를 사용합니다.

## Migration과 compatibility

### Schema migration

하나의 additive migration에서 다음을 수행합니다.

1. Delivery `row_version` 추가
2. Outbox `terminal_at` 추가
3. Logical ledger table 생성
4. Existing terminal outbox backfill

Conservative backfill:

```text
SENT outbox:
terminal_at = COALESCE(sent_at, migration_at)

FAILED outbox:
terminal_at = migration_at

PENDING outbox:
terminal_at = NULL
```

Existing FAILED에 `created_at`을 사용하지 않습니다. Migration 직후 즉시 cleanup되는 것을 막기 위해 migration 시각을 사용합니다.

### Compatibility writer

Full lifecycle cutover 전 compatibility binary가 다음을 수행해야 합니다.

- Legacy outbox direct/aggregate writer도 `terminal_at`을 유지
- Legacy success transaction도 ledger `SENT` 기록
- Legacy stale quarantine transaction도 ledger `QUARANTINED` 기록
- Cleanup은 ledger backfill 완료 전 Community/Shorts terminal outbox를 삭제하지 않음

Compatibility ledger write는 delivery/tracking과 같은 DB transaction이어야 합니다. 별도 best-effort transaction은 허용하지 않습니다.

### Historical backfill

Canonical resolver를 사용하는 bounded Go backfill command를 만듭니다.

Input:

- Existing Community/Shorts delivery `SENT`
- Existing Community/Shorts delivery `QUARANTINED`
- 필요한 경우 기타 kind terminal rows

Rules:

- Same key에서 `SENT`가 `QUARANTINED`보다 우선
- Existing ledger와 idempotent upsert
- Invalid payload/canonical identity는 skip하지 않고 failure report
- Cursor 기반 bounded batch
- Resume 가능
- Raw room/post ID를 일반 log에 출력하지 않음

### Backfill completion gate

Cutover 전에 다음이 모두 충족되어야 합니다.

1. Backfill command 완료
2. Invalid identity 0건 또는 승인된 repair 목록
3. Source distinct logical terminal key 수와 ledger coverage 일치
4. Random sample과 deterministic fixture 대조
5. Compatibility writer가 새 terminal event를 ledger에 기록 중
6. Cleanup freeze 해제 승인

Backfill 완료를 process memory flag로만 관리하지 않습니다. Migration/version 또는 durable operations metadata에 완료 marker를 남깁니다.

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
```

Batch API:

```go
type LogicalLedger interface {
    Load(context.Context, []LogicalDeliveryKey) (map[LogicalDeliveryKey]LogicalLedgerEntry, error)
}
```

Per-row N+1 lookup을 기본 경로로 사용하지 않습니다.

## Transaction contract

### Success

`CompleteSent` transaction:

1. Owner/follower Send fence와 pre-state 확인
2. Delivery group `SENT`
3. Tracking requirement 충족
4. Ledger `RecordSent`
5. Latency classification
6. Commit

Commit read-back은 owner/follower/tracking뿐 아니라 ledger `SENT`도 확인합니다.

### Quarantine

`QuarantineLogicalGroup` transaction:

1. Stale owner `SENDING` 확인
2. Owner/follower `QUARANTINED`
3. Ledger `RecordQuarantined`
4. Touched outbox aggregate
5. Commit

Ledger가 이미 `SENT`이면 quarantine를 commit하지 않고 fulfilled reconciliation 경로로 전환합니다.

### Reconcile fulfilled

Ledger `SENT`를 근거로 follower를 `SENT`로 수렴시킬 때 ledger 자체를 변경할 필요는 없지만 transaction 안에서 key/state를 다시 확인합니다.

## Cleanup after ledger cutover

Full outbox/delivery cleanup은 다음 조건을 모두 확인합니다.

- Outbox terminal state
- `terminal_at < now - CleanupAfter`
- Active outbox lock 없음
- Child `PENDING/SENDING` 없음
- Child terminal `SENT/QUARANTINED` logical key가 ledger에 동일하거나 더 강한 evidence로 존재
- Failed logical owner를 삭제할 때 same-logical retained `PENDING/SENDING` sibling 없음

Candidate selection, ledger verification, sibling guard, delete는 bounded transaction에서 수행합니다.

Cleanup commit response loss는 candidate ID와 expected `terminal_at`을 기준으로 read-back합니다. Retry 시 cutoff를 새로 앞당기지 않습니다.

## Failure behavior

### Ledger read failure

Fail closed입니다. Provider를 호출하지 않습니다. Claimed PENDING owner/follower는 attempt를 소비하지 않는 defer 또는 lock expiry 경로로 돌아갑니다.

### Ledger write failure

Delivery success/quarantine transaction 전체를 rollback합니다.

- Provider success 뒤 rollback: DB-only finalization adjudication을 수행하고 provider를 재호출하지 않습니다.
- Quarantine write rollback: stale owner는 다음 sweeper에서 재시도할 수 있습니다.

### Backfill incomplete

New lifecycle cutover와 Community/Shorts cleanup을 금지합니다.

## Observability

```text
youtube_delivery_ledger_read_total{result}
youtube_delivery_ledger_write_total{target,result}
youtube_delivery_ledger_state_total{status}
youtube_delivery_ledger_backfill_total{result}
youtube_delivery_ledger_backfill_lag
youtube_delivery_cleanup_guard_total{reason}
youtube_delivery_terminal_at_missing_total{status}
```

Raw logical ID와 room ID를 metric label에 넣지 않습니다.

## Tests

### Schema/store

```text
TestLedgerSentInsert
TestLedgerQuarantinedInsert
TestLedgerSentWinsConcurrentQuarantine
TestLedgerQuarantineCannotDowngradeSent
TestLedgerPreservesFirstSentAt
TestLedgerSourceDeliverySurvivesDeliveryCleanup
```

### Transaction

```text
TestCompleteSentCommitsLedgerTrackingAndGroupAtomically
TestCompleteSentRollbackLeavesNoPartialLedger
TestQuarantineCommitsLedgerAndGroupAtomically
TestQuarantineSeesConcurrentSentAndReconcilesFulfilled
TestCommitReadBackIncludesLedger
```

### Backfill

```text
TestBackfillSentWinsQuarantined
TestBackfillResumesFromCursor
TestBackfillRejectsInvalidCanonicalIdentity
TestBackfillCoverageMatchesSourceKeys
TestCutoverRejectsIncompleteBackfill
```

### Cleanup

```text
TestCleanupUsesTerminalAtNotCreatedAt
TestLateFailedOutboxRetainedForCleanupAfter
TestCleanupRequiresSentOrQuarantinedLedgerEvidence
TestCleanupRetainsFailedOwnerWithActiveSibling
TestCleanupResponseLossUsesOriginalCutoff
```

## 완료 조건

1. Logical terminal read path는 ledger가 정본입니다.
2. `SENT`가 concurrent `QUARANTINED`보다 항상 우선합니다.
3. Success/quarantine와 ledger write가 같은 transaction입니다.
4. Existing terminal evidence가 canonical resolver로 backfill됩니다.
5. Backfill completion marker 없이 cutover/cleanup이 진행되지 않습니다.
6. Outbox cleanup은 `terminal_at`을 사용합니다.
7. Full delivery cleanup 뒤에도 sent/unknown dedupe evidence가 남습니다.
8. Ledger는 초기 범위에서 자동 삭제되지 않습니다.
