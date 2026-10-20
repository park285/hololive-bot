# 3c-2 리뷰 결과(R3C2, 미반영)

대상: hololive-bot-apsm-3c 미커밋 변경(3c-2), 기준 36fa889b4.

**결론: blocker는 없습니다.** 3c-2는 설계 §3.5·§3.6과 맞고, 정확성 결함은 찾지 못했습니다. 아래 minor 2건과 nit 1건만 있습니다.

## Findings

**1. [minor] 계약 문서가 Go 시각 경로를 하나 빠뜨림** — `docs/current/architecture/youtube-egress-lifecycle-contract-20260831.md:206,212`
- **문제:** 이번 패치가 새로 쓴 "시간원" 절은 `locked_at`·`sent_at`·`next_attempt_at`·`terminal_at` 기록이 "모두 PostgreSQL 시계"이고, 예외는 `CleanupTerminalOutboxes`·`CleanupOrphanRequests`뿐이라고 적었습니다. 그런데 outbox 집계 projector는 지금도 Go 시각으로 outbox `sent_at`·`terminal_at`을 씁니다.
- **근거:** `store/delivery_repository.go:91`의 `yttimestamp.Normalize(r.now())`가 `delivery_repository_aggregate_sync.sql`의 `$7`(`sent_at = … THEN $7`, `terminal_at = … THEN $7`)로 들어갑니다. CHANGELOG의 "fanout outbox … `terminal_at` 기록을 … `clock_timestamp()`로"도 같은 범위를 과장합니다. 그래서 자식 delivery의 `sent_at`(DB 시계)과 부모 outbox의 `sent_at`(Go 시각)이 서로 다른 시계에서 옵니다.
- **영향:** fence나 판정에 쓰이지는 않아 정확성 문제는 아닙니다.
- **제안:** 설계 표가 집계 projector를 범위에 넣지 않았으므로, 문서와 CHANGELOG에 "집계 projector의 outbox `sent_at`·`terminal_at`은 Go 시각" 예외를 적습니다. 아니면 그 SQL을 `clock_timestamp()`로 바꿉니다.

**2. [minor] 재시도 due 시험이 하한만 확인함** — `store/transition_clock_test.go:66-68` (그리고 `:115`)
- **문제:** 설계는 `next_attempt_at - checked_at = retryAfter`를 요구했는데, 시험은 `require.False(t, nextAttemptAt.Before(releasedAt.Add(retryAfter)))`처럼 하한만 봅니다.
- **영향:** 단위를 잘못 바꾸는 회귀(예: `INTERVAL '1 second'`로 3000초)나 지연이 덧붙는 회귀도 통과합니다. "owner due already passed" 하위 시험(`:115`)도 같은 구조입니다. delivery 재시도 경로에는 상한을 거는 다른 시험이 없습니다. fanout에는 `retryWindowEnd`가 있습니다.
- **제안:** `<-done` 직후 `SELECT clock_timestamp()`로 상한 시각을 읽고 `!nextAttemptAt.After(upper.Add(retryAfter))`를 추가합니다. follower 하위 시험도 `before + backoff ~ after + backoff` 구간으로 묶습니다.

**3. [nit] `fanout_fail.sql`은 행을 잠그기 전에 시계를 채취함** — `store/queries/fanout_fail.sql:4-10`
- **문제:** `ApplyFanoutFailure`는 같은 트랜잭션에서 outbox를 먼저 잠그지 않습니다. 그래서 `clock` CTE가 UPDATE의 행 잠금 대기보다 먼저 평가됩니다. 잠금 경합이 생기면 `next_attempt_at`과 `terminal_at`이 대기 시간만큼 앞당겨집니다.
- **근거:** delivery 전이 SQL은 `clock … FROM (SELECT count(id) FROM locked)`로 잠근 뒤에 시계를 채취하고, 시험(`TestRetryDueCountsFromDatabaseClockAfterLockWait`)도 이 성질을 확인합니다. fanout 실패 경로만 이 원칙에서 빠져 있습니다. 같은 fanout이라도 materialize 경로는 `fanout_load_outbox.sql`의 `FOR UPDATE` 뒤라 문제없습니다.
- **영향:** 경합 상대는 lease가 끝난 뒤의 cleanup·재claim처럼 짧은 문장뿐이라, 앞당겨지는 폭은 [INFERENCE] ms 단위입니다.
- **제안:** `WITH target AS MATERIALIZED (SELECT id FROM youtube_notification_outbox WHERE id=$6 FOR UPDATE), clock AS MATERIALIZED (SELECT clock_timestamp() AS at FROM (SELECT count(id) FROM target) t)` 형태로 잠근 뒤 시계를 채취합니다.

## 확인했고 문제없는 항목
- **(1) 시계 이중 채취·Go 시각 잔존:**
  - 모든 SQL이 MATERIALIZED `clock`을 한 번 만들고 `(SELECT … FROM clock)`로만 참조합니다.
  - CompleteSent는 apply가 돌려준 `transitioned_at` 하나를 ledger·`LoadAlarmSentMarksForDeliveryIDs`·telemetry에 함께 넘깁니다. Quarantine은 ledger `ObservedAt`과 telemetry에, 실패·세대 전진은 telemetry에 같은 방식으로 넘깁니다.
  - youtubedispatch에 남은 비시험 `time.Now`는 다음뿐입니다: 로그·감사·지표(`metrics_recorder_*`, `audit_logger`, `send_engine_send`), 3c-3 범위(`claim_manager_acquire.go:29`), 설계상 제외된 보존 cutoff(`claim_manager_release.go:28`, telemetry retention), 그리고 1번의 집계 projector.
- **(2) CROSS JOIN 회귀:** 시계는 CROSS JOIN 없이 모두 스칼라 하위 질의입니다. `TestClaimSQLReadsCandidatesWithoutScanningTerminalHistory`(custom·auto·force_generic 실행 계획)가 통과했습니다.
- **(3) 만료되지 않은 lease를 빼앗을 수 있는가:** 빼앗을 수 없습니다.
  - 후보 SQL의 clock은 실제 잠금 시점보다 늦을 수 없으므로, `locked_at < clock - lease`면 잠금 시점에도 만료입니다.
  - 다른 실행이 갱신한 행은 `FOR UPDATE` 재평가에서 같은 InitPlan 값으로 다시 판정됩니다.
  - Go 쪽 재확인(quarantine cutoff, expiry·revive active lock)도 `checked_at` 기준이라 같은 방향으로 보수적입니다.
  - claim이 기록하는 `locked_at`이 잠금 전 시계라 lease가 조금 짧아지지만, 소유자와 경쟁자가 같은 `locked_at`으로 판정하므로 안전합니다.
- **(4) 시각 열을 뺀 commit 판정:** 안전합니다.
  - 모든 전이가 버전을 +1 하므로, 같은 pre-version에서 같은 모양(status·attempt·locked_by·error)으로 이길 수 있는 다른 전이는 같은 의미의 전이(defer·revive·fulfilled)뿐입니다.
  - BeginSending post-state는 `locked_by=나`를 포함하므로 소유자 고유입니다.
  - BeginSending 판정이 Applied로 끝나는 경로에서도 `ownerAfter.LockedAt`은 tx 본문에서 받은 DB 값으로 채워집니다.
  - fanout 실패 판정은 시각 비교가 빠져 버전 없는 outbox에서 고유성이 약해졌습니다. 다만 그 경우도 같은 사유의 같은 실패 전이라 결과는 같습니다.
- **(5) follower due 변화:**
  - `LogicalOwnerPendingElsewhere`가 "owner due 그대로"에서 `GREATEST(clock+backoff, owner due)`로 바뀌었고, 계약 문서에 반영돼 있습니다.
  - follower 발송은 owner 그룹 전이가 대신하므로 외부 발송이 연쇄로 늦어지지 않습니다. 오히려 owner due가 이미 지났을 때 매 poll마다 즉시 다시 defer되던 반복이 사라집니다.
  - terminal follower의 `next_attempt_at` 유지는 revive가 clock으로 다시 쓰므로 영향이 없습니다.
- **(6) scany와 비공개 embedded struct:** scany v2.1.4 `structref.go:37`이 Anonymous 필드를 순회하고, reflect는 non-pointer embedded struct의 exported 필드를 설정할 수 있습니다. dbtest의 quarantine·expiry·revive 경로가 통과했습니다.
- **(7) 타이밍 시험:**
  - lock-wait 시험의 `pg_stat_activity` 조건이 너무 일찍 참이 되더라도 단언은 성립하므로, 흔들리지는 않고 공허해질 수만 있습니다.
  - lease 경계 시험은 5초, ready-age는 0.5초 여유가 있습니다.
- **(8) 롤링 배포 공존:** migration이 없고 SQL은 binary마다 내장돼 있습니다. 구 binary는 자기 Go 시각으로만 자기 전이를 판정합니다. 3c-1 BeginSending의 lease 검사도 이미 DB clock이었습니다. 앱과 DB의 시계 차이는 lease 길이에만 반영되고 fence(버전)에는 영향이 없습니다.
- **기타:** 모든 SQL에서 인자 순서와 Go 호출의 대응이 맞습니다. `GREATEST`는 NULL floor를 무시합니다. `CleanupExpiredFanoutOutboxes(age)`는 `max(CleanupAfter, Freshness)` 의미를 유지합니다. `CLOCK_QUERY_POLICIES`는 `*.sql`로 등록됐습니다. 한국어 주석과 CHANGELOG도 갱신됐습니다.

## 실행한 검증
- `go vet ./internal/egress/youtubedispatch/... ./internal/app/workerapp/...`: 출력 없음.
- `go test -count=1`: store, lifecycle, preparation, youtubedispatch, workerapp 모두 ok. testcontainers를 쓰는 dbtest 포함, `-race` 없이 실행했습니다.
- golangci-lint(youtubedispatch): 0 issues. `gofmt -l`: 빈 출력.
- `check-sql-ownership.py`: passed.

## 확인하지 못한 범위
- `-race` 실행.
- `hololive-dbtest` 모듈의 schema golden 시험. migration이 없어 영향은 없을 것으로 봅니다 [INFERENCE].
- API가 쓰는 outbox `created_at`의 시계 출처. 신선도 비교의 한쪽 값이라 확인이 필요하지만, 이 비교 구조는 패치 이전부터 있었습니다.

---

# 3c-2 구현 보고(U3C, 원문 JSON)

```json
{
  "status": "done",
  "summary": "3c-2 is finished. Lease expiry, freshness, due checks and the recorded locked_at, sent_at, next_attempt_at and terminal_at values for YouTube delivery and fanout outbox rows now all come from the PostgreSQL clock. Each SQL statement takes clock_timestamp() once in a `clock AS MATERIALIZED` CTE and reads it through scalar subqueries, so the index conditions still apply. The transition SQL takes the clock after locking the target rows and returns it as an 11th column. BeginSending uses that value for the owner's lease start, and CompleteSent uses it for tracking, ledger and telemetry; failure, advance-generation and quarantine use it for telemetry and ledger time. Candidate SQL (stale SENDING, expired PENDING, revive) takes lease_ms and freshness_ms and returns checked_at, and Go re-checks groups only against that value. Lifecycle mutations now carry a DueSpec instead of an absolute due time, and the resolver no longer reads a clock (it exposes DueFloor). DeferCommand takes a Delay, and sameTransitionState no longer compares time columns. ReadySnapshot and the fanout claim, revive, failure, no-target, child-insert and cleanup SQL use the same clock rule. CleanupExpiredFanoutOutboxes now takes an age instead of absolute cutoffs. The whole youtube store/queries directory is registered in CLOCK_QUERY_POLICIES as clock_timestamp-only.",
  "design_decision": "1) next_attempt_at follows S3B §3.5: GREATEST(clock + delay, floor), where floor comes only from DB rows (owner next_attempt_at, or in-flight locked_at + LockTimeout). Retry gives owner and followers a delay of max(backoff, retryAfter). Terminal failure keeps each row's own next_attempt_at, so followers no longer copy the owner's value (a behaviour change the design accepted). Revive uses delay 0. 2) Transition rows carry the time actions alongside the rest of the next state (due, lockFromClock, sentFromClock). Fulfilled reconciliation writes the ledger sent_at as an explicit value. 3) RowMutation.clearLock was always true, and the SQL cannot keep a lock anyway, so I removed it. StartedOperation.startedAt was only used by Valid(), so I removed it too. 4) The transition SQL's 11th column is read by a store-local scanTransitionObservation. sendfence.ScanObservation and the rest of the sendfence public API are unchanged. 5) Candidate SQL takes its clock before locking rows, so expiry decisions are conservative; this is documented in the SQL comments and the contract doc. 6) ReadySnapshot keeps its public signature (it still takes durations). 7) Out of scope as the design specifies: CleanupTerminalOutboxes and CleanupOrphanRequests retention cutoffs, and the 3c-3 claim_manager_acquire alarm-state claim.",
  "files_changed": [
    "CHANGELOG.md",
    "docs/current/architecture/youtube-egress-lifecycle-contract-20260831.md",
    "docs/current/architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md",
    "scripts/architecture/check-sql-ownership.py",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/claim_manager_gate.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/claim_manager_release.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_request.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/types.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/policy.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle/policy_test.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/preparation/resolver.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/preparation/resolver_test.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/ready_snapshot.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/send_request.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_begin.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_cleanup.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_complete.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_expiry.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_failure.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_fanout.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_prepare.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_quarantine.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_revive.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_store.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_types.go",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/queries/{transition_apply_rows,transition_claim_pending,delivery_ready_predicate,delivery_ready_snapshot,transition_stale_sending,transition_expired_pending,transition_revive_candidates,fanout_claim,fanout_revive_failed,fanout_cleanup_expired,fanout_fail,fanout_complete_no_targets,fanout_insert_children}.sql",
    "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/{transition_clock_test.go (new), ready_snapshot_test.go, claim_history_plan_test.go, fanout_claim_execution_test.go, fanout_claim_predicate_test.go, transition_status_literal_test.go}"
  ],
  "tests": {
    "new": [
      "TestBeginSendingLeaseStartsAtDatabaseClockAfterLockWait: on the HEAD 36fa889b4 code (extracted to a temporary directory) it FAILED (locked_at 18:35:58.070823 was earlier than lock release 18:35:58.392953); it passes after the change",
      "TestRetryDueCountsFromDatabaseClockAfterLockWait: on the HEAD code it FAILED (next_attempt_at 18:36:01.525544 was earlier than release 18:35:58.850095 + 3s); it passes after the change",
      "TestFollowerDeferDueRespectsOwnerFloor (when the owner's due time is later, the follower gets the owner's due; when it has already passed, the follower gets DB clock + backoff)",
      "TestClaimPendingLeaseUsesDatabaseClock (regression guard; also passes on the HEAD code)",
      "TestStaleSendingQuarantineUsesDatabaseClock (regression guard; also passes on the HEAD code)"
    ],
    "updated": "policy_test (asserts DueSpec; terminal failure keeps next_attempt_at), resolver_test (DueFloor), ready_snapshot_test (the exact-microsecond boundary test became DB-clock-relative boundary cases plus a claim/snapshot agreement check), claim_history_plan_test, fanout_claim_execution_test, transition_status_literal_test (new parameters), fanout_claim_predicate_test (LIMIT $4). The index-plan tests pass unchanged under the clock InitPlan."
  },
  "verification": [
    {
      "command": "gofmt -l ./internal (alarm-worker)",
      "result": "no output"
    },
    {
      "command": "/home/kapu/go/bin/golangci-lint run --allow-parallel-runners -c ../../.golangci.yml ./... (alarm-worker)",
      "result": "0 issues."
    },
    {
      "command": "go build ./... && go vet ./internal/egress/youtubedispatch/...",
      "result": "ok"
    },
    {
      "command": "go test -race -count=1 ./internal/egress/youtubedispatch/... ./internal/egress/sendfence/... ./internal/app/workerapp/...",
      "result": "all ok (youtubedispatch, format, lifecycle, preparation, store, sendfence, workerapp)"
    },
    {
      "command": "go test -count=1 -run 'Clock tests' ./internal/egress/youtubedispatch/store/ on the HEAD 36fa889b4 baseline tree",
      "result": "2 FAIL (BeginSending lease, retry due); the other 2 PASS"
    },
    {
      "command": "python3 scripts/architecture/check-sql-ownership.py",
      "result": "SQL ownership check passed"
    },
    {
      "command": "scripts/architecture/check-migration-manifest.sh",
      "result": "OK: migration manifest matches SQL files"
    }
  ],
  "contract_changes": "Inside hololive-alarm-worker: lifecycle.EvaluateFailure no longer takes `at`; RowMutation.NextAttemptAt()/ClearsLock() are replaced by Due() DueSpec; Decision.At() is removed. preparation.ResolveGroups no longer takes `at`; ResolverConfig.RetryBackoff is removed; Resolution.Due() is replaced by DueFloor(). store.DeferCommand.NextAttemptAt becomes Delay. CleanupExpiredFanoutOutboxes(ctx, pendingAge, limit) replaces the two absolute cutoffs. Parameters for ClaimPending/ReadySnapshot/fanout SQL changed to (status, lease_ms, freshness_ms, batch[, owner]). No DB schema or migration change, and sendfence is unchanged. Both contract docs are updated: the 'time normalization' section is replaced by a 'time source' section; the claim protocol, defer/retry/terminal/revive/fanout time rules, and the post-state identification tuple, with time columns excluded from comparison, are rewritten.",
  "risks": "1) Behaviour change: on terminal failure, a follower's next_attempt_at keeps its own value (design-accepted). 2) For a follower deferred behind an owner pending in another batch, the due used to be exactly owner.next_attempt_at; it is now GREATEST(clock + backoff, owner due), so if the owner's due has already passed the follower waits an extra backoff. 3) Candidate SQL takes its clock before locking, so the new lease ends earlier by the statement's duration (milliseconds; conservative). 4) [INFERENCE] Scanning checkedTransitionRow (an embedded unexported struct) through scany depends on the library version; it passed in the tests that use it (quarantine, expiry and revive paths). 5) The lock-wait tests rely on timing: they detect pg_stat_activity wait_event_type='Lock', then wait a further 300ms.",
  "blockers": "none",
  "commit_message": "refactor(worker): YouTube 발송 시각을 DB 시계로 통일\n\ndelivery·fanout outbox의 claim, lease 만료·신선도 판정과 locked_at·sent_at·\nnext_attempt_at·terminal_at 기록을 Go 시각 대신 SQL 문장이 한 번 채취한\nclock_timestamp()로 정한다.\n\n- 전이 SQL은 대상 행을 잠근 뒤 시계를 채취해 전이 시각을 돌려준다.\n  BeginSending lease 시작과 CompleteSent의 tracking·ledger·telemetry\n  발송 시각이 이 값을 쓴다.\n- next_attempt_at은 GREATEST(clock + delay, DB 유래 floor)로 계산한다.\n  lifecycle 결정은 절대 시각 대신 DueSpec을 돌려주고 resolver는 시계를\n  읽지 않는다. DeferCommand는 Delay를 받는다.\n- stale SENDING·만료 PENDING·revive 후보 SQL은 lease_ms·freshness_ms를\n  받고 판정 시계를 checked_at으로 돌려준다.\n- claim·ReadySnapshot·fanout SQL은 clock CTE를 스칼라 하위 질의로 참조해\n  부분 인덱스 조건을 유지한다.\n- commit 응답 유실 판정은 시각 열을 비교하지 않는다.\n- youtube delivery queries 디렉터리를 clock_timestamp 전용으로 등록한다.\n\n잠금 대기 뒤 lease·재시도 시각이 대기 이전으로 앞당겨지던 문제를 시험으로\n재현(기존 코드 FAIL)하고 수정했다. migration 없음."
}
```
