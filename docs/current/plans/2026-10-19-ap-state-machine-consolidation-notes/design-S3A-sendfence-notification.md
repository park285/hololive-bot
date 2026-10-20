# S3A 설계: 공통 fence 기반, notification-delivery 처리, alarm-dispatch fence 정리

작성: 2026-10-19 · 기준 코드: `hololive-bot-apsm-1` = origin/main `cb105cc97` (아래 경로는 `hololive/hololive-alarm-worker/` 기준. 다른 모듈은 전체 경로)
계획: `docs/current/plans/2026-10-19-ap-state-machine-consolidation.md` §2.1·§3·§4.1·§5·§6 3단계
관련 설계: S3B(youtube 수렴, `local://apsm-design-S3B.md`), S4(Iris 202 reconciler, `local://apsm-design-S4.md`)

## 0. 결론 요약

1. **공통 기반은 작게 둔다.** 새 패키지 `internal/egress/sendfence`(sendoutcome 옆)는 typed miss 오류, 같은 문장에서 돌려받은 행 관측값의 분류, SQL 규약(잠금 뒤 clock CTE 한 곳, 시각 대신 `*_ms` 정수)만 가진다. 테이블별 SQL·runner·재시도 정책은 공유하지 않는다. 범용 상태머신 엔진은 계획 §1에서 금지했다.
2. **notification-delivery는 권고안 (i)로 처리한다.** `notification_delivery_outbox`와 API producer는 그대로 두고, worker 소비자만 공통 fence 기반으로 옮긴다. 행을 `alarm_dispatch_deliveries`로 옮기는 (ii)는 현행 계약 `DEC-20260926-hololive-outbox-v3-convergence`와 `PROJECT_MAP.md:94-100`("does not merge the notification delivery and alarm dispatch ledgers")을 뒤집는다. 또 계획 §1이 4.2 결정 전까지 유지하라고 한 순서·재시도·rearm·보존 의미를 모두 바꾼다. 그래서 (ii)는 사용자 결정이 필요한 open question으로 분리했다.
3. **"kind·period_key·room 순서 제약을 send unit 그룹 키로 표현"은 코드와 맞지 않는다.** 실제 순서 제약은 room별 FIFO 하나(`outbox_ready_predicate.sql:4-12`)다. (kind, content_id=period_key:room)은 순서가 아니라 유일성 키(`idx_ndo_kind_content`)다. alarm-dispatch의 그룹 키는 "한 메시지로 묶을 멤버"를 뜻한다. room을 그룹 키로 쓰면 서로 다른 digest가 한 메시지로 합쳐진다. 그래서 순서는 그룹 키와 별도의 **serial lane**으로 표현해야 한다(§3.5).
4. **alarm-dispatch fence 정리(3b)의 내용**은 다음과 같다.
   - 모든 소유자 전이는 행 튜플 (status, locked_by, attempt_count)을 검사한다.
   - Go가 만든 `next_attempt_at`(RFC3339 왕복)을 없애고 `retry_after_ms`로 넘겨 SQL에서 계산한다.
   - `checkedAt` 전달(`BeginSendRequest` → `repository_transitions_0020_01.sql $4`)을 단일 문장 `send_request_begin.sql`로 바꾼다.
   - lease 단위를 초 절사(`wholeSeconds`)에서 ms로 바꾼다.
   - `ErrSendRequestFence`·`ErrSendRequestLeaseExpired`·`PartialTransitionError`는 `*sendfence.Miss` 하나로 바꾼다.
   - 발송 전 membership 미스는 격리하지 않고 반환한다. 단 멤버가 종료 상태면 격리한다.
5. migration은 3a·3b 모두 필요 없다. 다음 migration 번호는 `325`이다(manifest 끝 `324_youtube_live_orphaned_legacy_live_end.sql`). 병렬 설계(S2·S3B·S4)와 번호가 겹치므로 머지 순서로 확정한다.
6. PR 순서는 0단계(5-1·5-2·5-5) 머지 → **3a**(sendfence 도입 + notification 적용) → **3b**(alarm-dispatch fence 정리) → S3B youtube → S4 reconciler.

---

## 1. alarm-dispatch 현재 fence 전수 (dispatchoutbox + egress/alarmdispatch)

### 1.1 행 상태·컬럼 (schema snapshot `hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql:66-114, 162-182`)

- 상태 8종: `pending, retry, leased, sending, sent, dlq, quarantined, cancelled` (`:96`, Go 상수는 `internal/service/alarm/dispatchoutbox/model.go:12-21`).
- shape CHECK `chk_alarm_dispatch_deliveries_state_shape` (`:98`)
  - leased는 locked_by·locked_at·lock_expires_at NOT NULL이어야 한다.
  - sending은 여기에 sending_started_at까지 NOT NULL이어야 한다.
  - 종료 상태는 각 `*_at`이 NOT NULL이어야 한다.
  - 계획 §1에 따라 유지한다.
- send unit: `alarm_dispatch_send_units`(client_request_id UNIQUE, request_body·route·body_hash·delivery_ids·base_client_request_id·request_generation). `chk_alarm_send_request_shape`(`:179`)는 generation 0..2, delivery_ids 1..10을 강제하며 유지한다.

### 1.2 SQL별 CAS 조건표 (현재)

경로: `internal/service/alarm/dispatchoutbox/queries/`. 시계 열의 "NOW()"는 트랜잭션 시작 시각이다.

| SQL 파일 | Go 함수 (file:line) | 상태 조건 | 소유자 | 버전 | lease 만료 | 시계 | 시각 출처 | 미스 처리 |
|---|---|---|---|---|---|---|---|---|
| `repository_claim_0053_02.sql` (+`delivery_ready_predicate.sql`) | `PgxRepository.ClaimDue` `repository_claim.go:29-61` | ready: `pending|retry`, `next_attempt_at <= NOW()` (predicate:1-3) | — (설정 `locked_by=$2`) | — | lease 설정 `NOW()+$3 s` (:45) | NOW() | lease 초 = `wholeSeconds` 절사 (`repository_transitions.go:15-21`) | 해당 없음 |
| `repository_transitions_0020_01.sql` | `MarkSending` `repository_transitions.go:23-43`, `BeginSendRequest` `send_request.go:261` | `leased` (:4) | `locked_by=$3` (:4) | 없음 | `lock_expires_at > clock` (:21) | `COALESCE($4, clock_timestamp())` (:10) | **$4 = Go가 받아 되돌린 `checkedAt`** (`send_request.go:252,261`) | `expectRowsAffected` (`repository.go:65-71`) / Begin은 `ErrSendRequestFence` (`send_request.go:266-268`) |
| `repository_transitions_0040_02.sql` | `MarkSent` `repository_transitions.go:49-64` | `sending` (:10) | `locked_by=$2` (:11) | 없음 | 무시(설계, 주석 :45-48) | NOW() | — | `validatePostSendRowsAffected` → `PartialTransitionError` (`:254-270`) |
| `repository_transitions_0160_06.sql` | `RouteFailures` `:66-72` → `applyFailureUpdates` `:162-179` | `leased` (:11) | `locked_by=$2` (:12) | `input.attempt_count = attempt_count + 1` (:10) | `lock_expires_at > clock` (:29) | clock_timestamp (:17) | **`next_attempt_at` = Go 시각** (`consumer.go:318,346,355`, `alarm_dispatch_runner_failure.go:314,347`) | 미적용 ID → `PartialTransitionError` (`:181-201`) |
| `repository_transitions_0170_07.sql` | `RouteSendingFailures` `:80-86` | `leased|sending` (:12) | `locked_by=$2` (:13) | `+1` (:11) | 없음(사후) | NOW() (:4,6) | Go `next_attempt_at` | 같음 |
| `repository_transitions_0185_08.sql` | `RequeuePreSend` `:88-100` | `leased|sending` (:9) | `locked_by=$2` (:10) | **등가** `= attempt_count` (:8) | 없음 | NOW() | Go `next_attempt_at` (5s, `runner_failure.go:313-314`) | 같음 |
| `repository_transitions_0152_05.sql` | `ReleaseLeased` `:235-250` | `leased` (:12) | `locked_by=$2` (:13) | 없음 | 없음 | NOW() | — | `expectRowsAffected` |
| `repository_terminal_0019_01.sql` | `MoveToDLQ`/`Quarantine` `:219-233` → `terminalUpdates` `repository_terminal.go:9-33` | dlq·quarantined는 `leased|sending`, 그 외는 비종료 (`repository_terminal.go:35-48`) | `locked_by=$3` (:17) | 없음 | 없음 | NOW() (:8,14) | — | `expectRowsAffected` |
| `repository_maintenance_0010_01.sql` | `RecoverExpiredLeased` `repository_maintenance.go:12-23` | `leased`, 만료 (:4,51), unit 전원 만료일 때만 (:23-27) | — | — | `lock_expires_at < NOW()` | NOW() | — | 행 수만 |
| `repository_maintenance_0035_02.sql` | `QuarantineStaleSending` `:25-37` | `sending`, `sending_started_at < NOW()-threshold` | — | — | — | NOW() | threshold 초 절사 | 행 수만 |
| `send_request_lock.sql` | `lockSendRequest` `send_request.go:117-126` | unit `FOR UPDATE` | — | — | — | — | — | 오류 |
| `send_request_members.sql` | `fenceSendRequest` `send_request.go:128-171` | 멤버 전원 `FOR UPDATE`, Go가 `status==want && owner==worker` 판정 (:154) | Go 비교 | 없음 | `lock_expires_at > clock` (SQL :14), Go가 sending이면 무시 (:155) | clock_timestamp (:9) | **`checked_at`을 Go로 반환** (:149,170) | `ErrSendRequestFence`/`ErrSendRequestLeaseExpired` (:162-168) |
| `send_request_pin.sql` | `prepareSendRequest` `send_request.go:59-115` | `request_body IS NULL` (:2) | 사전 members fence | — | 사전 fence | — | — | **rows-affected 미검사** (`send_request.go:105-108`) |
| `send_request_reissue_delivery.sql` | `requeueReissuedDeliveries` `send_request.go:277-298` (행마다 1문장) | `sending` | `locked_by=$2` | `attempt_count + 1 = $3` | 없음 | NOW() | **Go `next_attempt_at` $4** | `ErrSendRequestFence` (:292-294) |
| `send_request_reissue.sql` | `ReissueSendRequest` `send_request.go:179-227` | unit 사전 lock·fence(`sending`) | 사전 | `client_request_id = expectedID` (:200) | — | — | — | **rows-affected 미검사** (:217-220). 세대 소진은 `(false, nil)` bool (:204-206) |

단위(send unit) fence(`send_request.go:76-78, 162-164, 200-202, 257-259`)
- 고정된 `request_delivery_ids` = 현재 멤버 ID 집합 = 호출 ids (Go `sameDeliveryIDs` :173-175)
- `request_body_hash != ""`
- `client_request_id == expectedID` (세대 대표)

Go 쪽 시각 비교·생성
- `failureUpdatesFromEnvelopes`는 `time.Now().UTC()`를 기본값으로 쓴다(`consumer.go:318`). `failureUpdateFromEnvelope`는 `NextVisibleAt`을 RFC3339로 파싱한다(`consumer.go:355-357`).
- `NextVisibleAt`은 `sameAttemptAlarmDispatchRetry`(`runner_failure.go:314`)와 `nextAlarmDispatchRetry`(`:347`)가 **봉투마다** `time.Now()`로 만든다. 이것이 결함 5-1의 원천이다.
- `Consumer.maybeRecover`(`consumer.go:181-208`)의 `c.now`는 로컬 주기 판정에만 쓰고 DB 시각과 비교하지 않는다. 유지한다.

외부 dedup
- Valkey claim key(`notified:claim:` 등)는 `consumer_claim_release.go:15-19`가 다룬다.
- fence가 아니라 생산 측 dedup marker다. 해제는 소유권이 확인된 DLQ 전이 뒤에만 한다(`consumer_envelope.go:121-138`, `runner_failure.go:82-96`). partial 미적용 ID는 해제에서 뺀다(`runner_failure.go:63-80`, `UnappliedDeliveryIDs` 인터페이스 `:211-223`).
- **이 순서는 유지한다.** sendfence.Miss가 같은 인터페이스를 구현한다.
- Iris 승인 dedup: client_request_id(`dispatch_group.go:107` `hololive-alarm:` + unit hash, 재발급 `:r1/:r2`는 `iris.ReissuedClientRequestID`, `send_request.go:208`).

라우팅(`internal/egress/alarmdispatch/alarm_dispatch_runner.go:258-272`)
- `ErrSendRequestLeaseExpired`이면 no-op이고 복구 경로가 맡는다(:260-263).
- **`ErrSendRequestFence`이면 `Quarantine`한다(:265-268). 미발송인데 격리로 굳는 경로다**(계획 §3 "복구 종료" 행과 결함 5-1의 후속 증상).

### 1.3 기타 소비자·작성자 (fence 정합 확인)

- admin `dispatchops`(`hololive/hololive-api/internal/planes/admin/internal/service/dispatchops/queries/requeue.sql:3-17`, `settle.sql:3-15`)는 `dlq|quarantined` 행만 다룬다.
  - serializable로 attempt_count와 오류 이력을 보존한다.
  - `updated_at`을 리비전으로 쓴다(`GREATEST(clock_timestamp(), updated_at+1µs)`).
  - worker 소유 행(leased·sending)과 겹치지 않으므로 변경하지 않는다.
  - 제약: **worker 전이는 계속 `updated_at`을 갱신해야 한다.**
- `record_alarm_dispatch_closeout`(SERIALIZABLE 강제)은 유지한다.
- retention SQL(`internal/egress/alarmdispatch/queries/alarm_dispatch_retention_delete_*.sql`)은 SQL `NOW()` 기준이고 Go 시각이 아니다. fence가 아니므로 범위 밖이다.

---

## 2. 4.1 목표 튜플로의 매핑과 변경 목록 (alarm-dispatch, PR 3b)

### 2.1 튜플

| 축 | 목표 | alarm-dispatch 적용 |
|---|---|---|
| 행 | (status, locked_by, attempt_count). row_version 없음 | 모든 소유자 전이 SQL에 세 조건을 둔다. 입력은 `(id, attempt_count 관측값)` |
| 단위 | 멤버 ID 배열 등가, 요청 본문 해시, generation | `array_agg(id ORDER BY id) = request_delivery_ids = $ids(정렬)`, `request_body_hash IS NOT NULL`, `client_request_id = $expected`(세대 1:1 대표) |
| 시간 | clock_timestamp 한 곳 | 잠금 CTE 뒤 `clock AS MATERIALIZED (SELECT clock_timestamp() …)` 한 번. lease·`next_attempt_at`·`*_at`이 모두 이 값을 쓴다. Go는 `lease_ms`·`retry_after_ms` 정수만 넘긴다 |
| 미스 | typed error, bool 버림 금지 | `*sendfence.Miss`. `ReissueSendRequest`의 `(bool, error)`는 `error` + `sendfence.ErrGenerationExhausted`로 바꾼다 |

버전 축 결정
- attempt_count를 유지한다. claim·release·만료 회수는 attempt_count를 바꾸지 않으므로(재시도 예산 의미 보존, 계획 §1), "claim마다 바뀌는 토큰"은 `locked_by`가 맡는다.
- 남는 ABA: 같은 worker ID가 같은 행을 같은 attempt로 재claim하는 경우다. runner가 단일 루프라 이전 claim 처리가 끝난 뒤에만 다음 DrainBatch가 돈다(`alarm_dispatch_runner_loop.go:16-20`, `alarm_dispatch_runner.go:158-172`). 그래서 도달하지 않는다.
- 이 불변식을 패키지 주석으로 명시하고 시험으로 고정한다(§6 3b 시험 7).

### 2.2 sendfence 패키지 (3a에서 도입, 3b에서 확장 사용)

`internal/egress/sendfence/fence.go` (신규, 한국어 주석)

```go
// Reason은 fence 미스의 관측 원인이다. 판정 순서는 missing → status → owner → version → lease → membership → request.
type Reason string
const (
    ReasonMissing           Reason = "missing"
    ReasonStatusChanged     Reason = "status_changed"
    ReasonOwnerChanged      Reason = "owner_changed"
    ReasonVersionChanged    Reason = "version_changed"
    ReasonLeaseExpired      Reason = "lease_expired"
    ReasonMembershipChanged Reason = "membership_changed"
    ReasonMemberTerminal    Reason = "member_terminal"   // 고정 멤버 중 종료 상태가 있어 다시 모일 수 없음
    ReasonRequestChanged    Reason = "request_changed"
)
// Observation은 전이 문장이 잠근 행에서 같은 clock으로 계산해 돌려준 조건값이다(왕복 추가 없음).
type Observation struct {
    ID int64; Status string
    StatusMatches, OwnerMatches, VersionMatches, LeaseValid, MembershipMatches, RequestMatches, Applied bool
}
type RowMiss struct { ID int64; Reason Reason }
type Miss struct { Action string; Expected, Applied int; Rows []RowMiss }
func (m *Miss) Error() string
func (m *Miss) UnappliedDeliveryIDs() []int64          // runner partialFailureRouting 호환
func (m *Miss) Has(reason Reason) bool
func Check(action string, want []int64, observed []Observation) error // 전원 Applied면 nil
var ErrGenerationExhausted = errors.New("send request generations exhausted")
```

- Observation 열 이름은 U0-2 초안(`hololive-bot-apsm-0-2/.../outbox_reissue_failed_request.sql:9-14`의 `status_matches, owner_matches, lease_valid, attempt_matches, request_matches`)과 맞춘다. version은 SQL에서 `attempt_matches`(alarm·notification) 또는 `row_version_matches`(youtube, S3B)로 쓰고 Go 필드는 `VersionMatches` 하나로 둔다.
- `LeaseValid`는 테이블 SQL이 계산한다. 컬럼은 `lock_expires_at`(alarm·notification) 또는 `locked_at+lease_ms`(youtube, S3B 요구 2)를 쓴다. 사후 전이(sending 정산)는 `TRUE`로 고정한다.
- 지표는 기존 이름을 재사용한다.
  - alarm-dispatch: `alarm_dispatch_pg_transition_partial_total`(`dispatchoutbox/metrics.go:96`)에 `action`·`reason` 라벨을 추가하고 `observePGTransitionPartial`(`:174`)을 `observePGFenceMiss(action, reason)`으로 바꾼다.
  - notification: 0-2가 추가하는 지표를 쓴다(§4).
- SQL 규약(문서는 패키지 주석에 둔다)
  - (a) 대상 전원을 ID 순 `FOR UPDATE`로 잠근다.
  - (b) 그 뒤 `clock` CTE를 한 번 둔다.
  - (c) 조건을 boolean 열로 계산한다.
  - (d) 전부 참인 행만 UPDATE한다.
  - (e) 대상별 관측값 + applied를 SELECT한다.
  - (f) `next_attempt_at = GREATEST(clock.now + retry_after_ms·1ms, floor)`를 쓴다. floor는 DB 유래 timestamptz만 허용한다(S3B 요구 3). "유지"는 SET에서 기존 값을 그대로 둔다.

### 2.3 바뀌는 SQL (3b)

| 파일 | 변경 |
|---|---|
| `delivery_ready_predicate.sql` | `NOW()` → `clock.now`(호출 SQL이 `clock` CTE 제공). notification `now_value` 선례(`notificationdelivery/queries/outbox_claim_ready.sql:1-3`)와 같은 방식 |
| `repository_claim_0053_02.sql` | 맨 앞에 `clock AS MATERIALIZED (SELECT clock_timestamp() AS now)`. lease = `clock.now + $3 * 1ms`(ms). `locked_at`·`updated_at` = clock.now. U0-1의 unit 전원 claim(`hololive-bot-apsm-0-1` diff: 내부 count·picked가 `status IN ('pending','retry')`)은 유지 |
| `delivery_ready_snapshot.sql` | `clock` CTE 추가(관측 SQL이며 predicate 공유) |
| `repository_transitions_0020_01.sql` | `$4` 제거. 입력 `(id, attempt_count)` jsonb. 관측 SELECT 형태. **독립 MarkSending 경로 삭제 후에는 파일 자체를 삭제**(§2.4) |
| 신규 `send_request_begin.sql` | unit 잠금 + 멤버 전원 잠금 + clock + unit/행 조건 계산 + 전원 참일 때 `sending` 전이 + 관측 반환. `send_request_members.sql`의 checkedAt 왕복과 `BeginSendRequest`의 3문장 트랜잭션을 대체 |
| `repository_transitions_0040_02.sql` (MarkSent) | 입력 `(id, attempt_count)`, `status='sending' AND locked_by AND attempt_count=`, clock CTE, 관측 반환. lease 무시는 유지(주석 `repository_transitions.go:45-48`) |
| `repository_transitions_0160_06.sql` | `next_attempt_at` = `clock.now + retry_after_ms`(입력 열 `retry_after_ms BIGINT`). 관측 반환 형태로 변경 |
| `repository_transitions_0170_07.sql` | locked/clock CTE 도입, `NOW()` 제거, `retry_after_ms`, 관측 반환 |
| `repository_transitions_0185_08.sql` | 같음(attempt 등가 유지) |
| `repository_transitions_0152_05.sql` | 입력 `(id, attempt_count)`, clock CTE, 관측 반환 |
| `repository_terminal_0019_01.sql` | `%s` 포맷 대신 상태별 고정 파일 2개(`repository_terminal_dlq.sql`, `repository_terminal_quarantine.sql`. cancelled·sent 분기는 호출처가 없으면 삭제. 3b에서 grep으로 확인). attempt 등가 추가, clock CTE, 관측 반환 |
| `repository_maintenance_0010_01.sql`, `0035_02.sql` | 단일 `clock` CTE. 만료 재검사(:51)는 유지. threshold·limit은 ms |
| `send_request_members.sql` | `checked_at` 반환 열 삭제. pin·load 경로에서는 관측값만 반환 |
| `send_request_reissue_delivery.sql` + `send_request_reissue.sql` | 단일 문장 `send_request_reissue.sql`로 합친다. 멤버 전원 jsonb 입력, `status='sending' AND locked_by AND attempt_count+1=`, unit `client_request_id=$expected`, `request_generation < 2`, clock 한 번으로 멤버 전원 같은 `next_attempt_at`. 소진 여부도 관측 열로 반환 |
| `send_request_pin.sql` | `RETURNING id`로 rows-affected를 검사(0행 = 동시 pin → Miss request_changed) |

`check-sql-ownership.py` `CLOCK_QUERY_POLICIES`(`scripts/architecture/check-sql-ownership.py:54-65`, 첫 일치 우선)
- `ALARM_DISPATCH_QUERIES`의 개별 3줄(:56-58)을 다음 glob으로 바꾼다. 모두 `clock_timestamp`이다.
  - `repository_claim_0053_02.sql`, `delivery_ready_*.sql`, `repository_transitions_*.sql`, `repository_terminal_*.sql`, `repository_maintenance_*.sql`, `send_request_*.sql`
- `repository_insert_*.sql`(생성 시 `next_attempt_at=NOW()` 기본)과 `upcoming_candidates_*`는 fence가 아니므로 정책 밖에 둔다.
- 3a에서 `NOTIFICATION_DELIVERY_QUERIES = "hololive/hololive-alarm-worker/internal/egress/notificationdelivery/queries"`를 추가한다.
  - `outbox_repository_0279_09.sql`(보존 후보)은 `statement_timestamp`이고 **이 줄을 먼저 둔다.**
  - 나머지 `outbox_*.sql`은 `clock_timestamp`이다.
- 체커 자기 시험은 추가하지 않는다(rules :22).

### 2.4 바뀌는 Go (3b)

`internal/service/alarm/dispatchoutbox`
- `model.go:130-137` `FailureUpdate.NextAttemptAt time.Time`을 `RetryAfter time.Duration`(json `retry_after_ms`, ms 정수)으로 바꾼다. `TerminalUpdate`에는 `AttemptCount`를 추가한다. `Record`는 그대로 둔다.
- `model.go:160-166` `DeliveryProgressWriter.MarkSending`을 삭제하고 `BeginSendRequest`를 `Repository` 인터페이스로 올린다.
  - `consumer.go:210-235`의 타입 단언 fallback은 운영에서 죽은 분기다. `PgxRepository`는 항상 구현한다(`send_request.go:230`).
  - fake는 `consumer_marksending_metrics_test.go:38-43,83-88`이다.
- `consumer.go:317-360`
  - `failureUpdatesFromEnvelopes`·`failureUpdateFromEnvelope`에서 `time.Now()`와 `NextVisibleAt` 파싱을 삭제한다.
  - `RetryAfter = time.Duration(envelope.Retry.RetryAfterMS) * time.Millisecond`로 계산한다.
  - attempt는 기존대로 둔다.
- `repository_transitions.go`
  - `wholeSeconds`(:13-21)를 삭제하고 `leaseMilliseconds`(1ms 미만 오류)로 바꾼다.
  - `expectRowsAffected`·`validatePostSendRowsAffected`·`partialFailureRoutingError`·`unappliedFailureIDs`(:181-217, :252-270, `repository.go:65-71`)를 삭제한다. 공통 `applyFenced(ctx, sql, input, action) error`가 관측 행을 `sendfence.Check`로 넘긴다.
- `repository.go:23-43` `PartialTransitionError`를 삭제하고 `sendfence.Miss`로 바꾼다. 사용처는 `consumer.go:287-291`(`errors.AsType[*sendfence.Miss]`)이다.
- `send_request.go`
  - `ErrSendRequestFence`·`ErrSendRequestLeaseExpired`(:36-40)를 삭제한다.
  - `fenceSendRequest`(:128-171)는 관측 반환만 하고 checkedAt을 없앤다.
  - `BeginSendRequest`(:230-275)는 단일 SQL Exec 후 Check한다.
  - `ReissueSendRequest`(:179-227)는 `(error)`를 반환한다. 소진은 `sendfence.ErrGenerationExhausted`다.
  - `requeueReissuedDeliveries`(:277-298)는 단일 문장 SQL로 대체하며 삭제한다.
  - `ErrSendRequestUnpinned`는 유지한다(미스가 아니라 정상 분기).
- `consumer_send_request.go:59-77`의 반환형도 맞춘다.
- `metrics.go`: §2.2 지표 라벨.

`internal/egress/alarmdispatch`
- `alarm_dispatch_runner.go:25-29` `RequestConsumer.ReissueSendRequest`를 `error` 반환으로 바꾼다.
- `alarm_dispatch_runner.go:258-272`: 발송 전 미스 라우팅을 §2.5 표로 바꾼다.
- `alarm_dispatch_runner_failure.go`
  - `:313-314`, `:346-347`의 `NextVisibleAt` 생성을 삭제한다(`RetryAfterMS`만).
  - `:352-366` `reissueFailedRequest`는 `errors.Is(err, sendfence.ErrGenerationExhausted)`이면 격리로 간다(현행 의미 유지).
- `hololive/hololive-shared/pkg/domain/alarm.go:249` `AlarmQueueRetryMetadata.NextVisibleAt`의 유일한 생산·소비자가 위 두 곳이다(`git grep NextVisibleAt`). 필드를 삭제한다. 저장 JSON(`alarm_dispatch_events.payload`)에는 retry가 들어가지 않는다(`publisher.go:289-296` 생성 시 Retry nil, `attachRecordMetadata`는 DB 열로 복원 `consumer_envelope.go:95-101`). 그래서 삭제해도 호환된다.

### 2.5 미스 라우팅 (발송 전·후 분리, 계획 §3 "복구 종료" 정합)

| 단계 | Reason | 처리 | 근거 |
|---|---|---|---|
| 발송 전 (Load/Pin/Begin) | lease_expired | 손대지 않는다. 만료 복구가 unit 전체를 retry로 돌린다 | 현행 `runner.go:260-263` 유지 |
| 발송 전 | owner/status/version_changed, missing | 손대지 않는다. 로그+지표. 이미 다른 소유자·회수 경로 소유다 | 소유하지 않은 행에 쓰기 금지 |
| 발송 전 | membership_changed (멤버가 pending·retry·타 소유 leased) | 소유한 행 `ReleaseLeased` → 다음 claim이 unit 전원을 다시 모은다 | **현행 Quarantine(`runner.go:265-268`)을 바꾼다.** 미발송 claim은 격리하지 않는다(계획 §3). U0-1 이후 정상 경로에서는 발생하지 않는다 |
| 발송 전 | member_terminal (고정 멤버 중 sent·dlq·quarantined·cancelled) | 소유한 행 Quarantine(reason 저장). 같은 client_request_id로 이미 나간 메시지에 포함됐을 수 있어 운영 검토 대상이다 | release하면 claim↔release 무한 반복이 된다. admin requeue가 일부 멤버만 살린 경우 |
| 발송 전 | request_changed (pin 경합·ID 불일치) | owner_changed와 같이 처리 | 소유자가 같으면 도달하지 않는다 |
| 발송 후 (MarkSent·RouteSending·Reissue) | 모든 Reason | 외부 성공을 되돌리지 않는다. 오류 반환·로그·지표. 행은 stale sweep·회수 경로 소유 | 현행 `validatePostSendRowsAffected` 의미(`repository_transitions.go:252-253`) |

---

## 3. notification-delivery 조사와 흡수안 비교

### 3.1 데이터 모델 (`schema_snapshot.golden.sql:485-509`)

- `notification_delivery_outbox` 컬럼: `kind`(4종 CHECK), `period_key varchar(20)`, `room_id`, `content_id`(=`period_key:room_id`, `hololive/hololive-shared/pkg/service/delivery/outbox_repository.go:104`), `payload jsonb{message, request?}`, `status`(PENDING·SENDING·SENT·FAILED·QUARANTINED CHECK), `attempt_count bigint`, `next_attempt_at`, `locked_at/locked_by/lock_expires_at`, `sending_started_at`, `sent_at`, `error`.
- 유일성은 `idx_ndo_kind_content (kind, content_id)`이다.
- 저장 JSON 계약: `hololive/hololive-shared/pkg/contracts/notificationdelivery/payload.go:8-21`(`Request{body, route, base_id, generation, exhausted}`). CONTRACT_MAP `docs/current/CONTRACT_MAP.md:19`.

### 3.2 생산자 (API, 별도 배포 단위)

- `delivery.OutboxRepository.EnqueueBatch`(`outbox_repository.go:93-125`)가 SQL `outbox_enqueue_batch_upsert.sql:29-64`를 실행한다.
  - 충돌 `(kind, content_id)`에서는 **FAILED이고 exhausted가 아닌 행만** PENDING으로 rearm한다(attempt 0, lock 해제).
  - request가 고정돼 있으면 room·payload를 보존한다.
- 호출처는 LLM plane scheduler 두 곳이다.
  - `hololive/hololive-api/internal/planes/llm/internal/service/majorevent/scheduler/notification_guard.go:127`
  - `.../membernews/scheduler/digest_helper.go:108`
  - 조립은 `hololive/hololive-api/internal/planes/llm/runtime/delivery_module.go:50`이다.

### 3.3 소비자 (worker `internal/egress/notificationdelivery`, 1,198 src줄 / 3,866 시험줄 / SQL 12)

- claim `outbox_claim_ready.sql:1-22` + `outbox_ready_predicate.sql:1-12`
  - PENDING, due이고 lease가 없거나 만료된 행을 대상으로 한다.
  - **같은 room에 SENDING이 있거나 더 앞선 due PENDING이 있으면 제외한다.**
  - 프로세스 내 `activeRooms`·`processedIDs` 제외(`dispatcher_claim.go:16-76`)도 한다.
  - lease 상태는 별도 어휘 없이 PENDING+locked_by+lock_expires_at이다(`store.go:42-44`).
- 전이
  - MarkSending `outbox_repository_0172_04.sql`: PENDING+owner+lease, clock.
  - MarkSent `0189_05`: PENDING|SENDING. 5-5로 SENDING만 남는다.
  - MarkFailed `0209_06`: PENDING|SENDING, owner, `attempt_count=$8`, 발송 전이면 lease 검사. 실패 정책 `failure_policy.go:16-26`.
  - Quarantine `outbox_mark_quarantined.sql`: SENDING+owner.
  - saveRequest `outbox_save_request.sql`, reissue `outbox_reissue_failed_request.sql`: **payload.request JSON 전체 등가**.
  - stale sweep `0301_10`, cleanup `0279_09`(**Go cutoff `time.Now().Add(-olderThan)` `store.go:151`**), count FAILED `0330_11`.
- Go 반환은 모두 bool이다(`store.go:78-146`, `request.go:17-38, 97-128`). 호출자가 버리는 곳(결함 5-2):
  - `request.go:90`
  - `request.go:134`(`markItemTerminalFailed`)
  - `dispatcher.go:348-357`(경고 로그만)
- 결과 라우팅(`dispatcher.go:316-342`)
  - Success → MarkSent.
  - 409 pre-handoff → reissue(세대 ≤2, 소진이면 FAILED + exhausted).
  - **OutcomeUnknown·TransportAmbiguous → QUARANTINED**(alarm-dispatch는 고정 ID가 있으면 TransportAmbiguous를 재시도한다. `runner_failure.go:120-127`).
  - Failed → MarkFailed(고정 backoff `RetryBackoff`, `MaxRetries`).
- 재시도·격리 정책 값은 worker profile `notification_delivery.*`(`internal/config/worker_profile.go:66-92`), lease 60s·finalize 5s 상수(`internal/config/worker_profile_types.go:15-19`), 타이밍 검증(`worker_profile.go:122-146`)에 있다.
- 지표: Prometheus 전용 지표는 없다. `workercontract` attempt totals(`dispatcher.go:293-306`), ready sampler(`internal/app/workerapp/worker_registry.go:44`), FAILED 누적 경보 로그(`maintenance.go:93-106`)만 있다.
- 운영 문서
  - `docs/current/runbooks/alarm-worker.md:16,33,102`
  - `docs/current/services/alarm-worker.md:24,38,74,126`
  - `docs/current/contracts/alarm.md:175`
  - `docs/current/runbooks/rollback.md:50-70`
  - `docs/current/PROJECT_MAP.md:90,94-100`

### 3.4 선택지 비교

| 항목 | (i) 테이블·producer 유지, worker 소비자가 공통 fence 기반 사용 | (ii) `alarm_dispatch_deliveries`로 행 이관 | (iii) worker 측 bridge(소스 행 claim → alarm_dispatch 게시 → 소스 행 handed-off) |
|---|---|---|---|
| 계약 | 현행 그대로. PROJECT_MAP·DEC 위반 없음 | **DEC-20260926 반전, PROJECT_MAP:98-100 위반**. 사용자 결정 필요 | DEC가 삭제한 v1·v2→v3 bridge의 재도입. 사용자 결정 필요 |
| producer·배포 | worker 단독 배포 | API producer가 alarm_dispatch event·delivery·unit을 써야 한다. 삽입 로직은 worker internal(`dispatchoutbox/repository_insert.go`, `dedupe_key.go`, `dispatch_group.go`)이라 shared로 옮기거나 API 전용 writer를 새로 만든다. worker 선배포 → API 전환 → 구 테이블 drain → 구 소비자 삭제의 다중 릴리스 | API 무변경. worker 1회 배포 + notification status CHECK migration(`HANDED_OFF`) |
| 순서(room FIFO) | 현행 SQL 유지 | send unit에 serial lane 열 + 고빈도 alarm claim SQL에 NOT EXISTS 추가(전 경로 claim 비용 증가) | (ii)와 같음 |
| 재시도·격리 의미 | 현행 유지 | 고정 backoff·MaxRetries vs 5s 선형·3/6회, TransportAmbiguous 격리 vs 재시도. 계획 §1에 따라 소스별 정책 분기가 runner에 필요 | (ii)와 같음 |
| rearm(FAILED 재적재) | 현행 유지 | alarm dedupe는 종료 중복을 `rejected_terminal`로 거절(`model.go:87-93`)하므로 재시도 재적재 의미가 사라진다 | 소스 행 상태와 alarm 결과 미러링이 필요하다 |
| identity | (kind, period:room) | alarm 쪽 digest identity는 message hash 포함 `ContentIdentity`(`hololive/hololive-shared/pkg/domain/alarm_dispatch_source.go:81-99`). 같은 period·room의 새 본문이 재발송된다(현행은 SENT면 무시). `rollback.md:65-69`가 경고한 비호환 | (ii)와 같음 |
| 보존 | CleanupAfter 유지 | 상태별 retention days로 바뀐다 | 두 표 공존 |
| in-flight 행 | 없음 | PENDING(고정 request 포함)·SENDING 행의 client_request_id `hololive-delivery:`(`dispatcher.go:428-446`)를 unit ID로 옮기지 않으면 Iris dedup을 잃는다. SENDING은 drain만 가능하다 | bridge 시점에 같은 처리 |
| 4단계 reconciler | notification 표에 handoff 확인 열·SQL 한 벌이 더 필요하다(S4) | 단일 reconciler | 단일 reconciler |
| 삭제량 | notification SQL 2~3개 단순화, payload JSON CAS 제거, bool API 제거. 패키지는 유지 | notificationdelivery 패키지 전체(≈5.1k줄, SQL 12), profile executor `notification_delivery`(stack-secrets 프로필 변경) | 소비 코드 대부분 삭제. bridge 코드 신규 |
| 재사용 근거 | — | 소비 측 digest 경로가 이미 있다. 렌더 `alarm_dispatch_render.go:76-100`, 그룹 키 `alarm_dispatch_group.go:133-140`, dedupe `dispatch_group.go:53-54`, `dedupe_key.go:72,159-165` | 같음 |

**권고: (i).** 근거는 세 가지다.
- (ii)·(iii)은 현행 정본 계약(DEC + PROJECT_MAP)을 뒤집는다.
- 계획 §1이 4.2 결정 전까지 유지하라고 한 순서·재시도 한도·보존 정책을 모두 바꾸거나, 소스별 분기로 다시 구현해야 한다.
- API·worker 교차 배포와 rollback 차단 조건(`rollback.md:50-70`)을 다시 만든다.

(ii)의 실익(단일 reconciler, 패키지 삭제)은 실제로 있다. 그래서 open question으로 올린다. 사용자가 단일 원장을 원하면 4단계 이후 별도 계획으로 DEC를 갱신하고 진행한다. 그때 alarm-dispatch 소비 측 digest 경로가 재사용 기반이다.

반대로 (i)을 택하면 소비 측 `delivery_digest` 잔재가 생산자 없는 죽은 경로가 된다. 대상은 위 렌더·그룹·dedupe 분기와 `domain.DeliveryDigestDispatchPayload`, `rollback.md` preflight다. 처리는 open question 2로 남겼다. 유지 근거는 rollback preflight가 그 행 존재를 검사하는 운영 절차라는 점이다. 3a 범위에서는 손대지 않는다.

### 3.5 순서 제약 표현

- 실제 제약은 **room당 동시에 하나, 그리고 (next_attempt_at, created_at, id) 순 head-of-line**이다(`outbox_ready_predicate.sql:4-12`, `dispatcher_claim.go:13-15`).
- kind·period_key는 순서에 참여하지 않는다. 유일성 키다.
- (i)에서는 이 SQL을 그대로 둔다. 시각만 `now_value`(이미 clock_timestamp) 한 곳이다.
- 공통 개념으로 문서화할 이름은 **serial lane key = room_id**이고, **send unit group key(메시지 병합 경계)와 다른 축**이다.
- (ii)를 택하는 경우의 표현
  - `alarm_dispatch_send_units.serial_lane text NULL`(digest만 `room_id`)을 둔다.
  - claim에 `NOT EXISTS (같은 lane의 leased·sending 멤버가 있는 unit 또는 더 앞선 due unit)`를 넣는다.
  - group key는 기존 `room|source|delivery_digest|kind|content` 그대로 두어 digest마다 한 unit이 되게 한다.

### 3.6 (i) notification 튜플 매핑 (PR 3a)

| 축 | 현재 | 목표 |
|---|---|---|
| 행 | MarkSending은 status+owner+lease(attempt 없음). MarkFailed는 status+owner+attempt. MarkSent·Quarantine은 status+owner | 모든 소유자 전이: status + `locked_by` + `attempt_count = $claimed`(+ 발송 전이면 `lock_expires_at > clock.now`). lease 상태 어휘는 늘리지 않는다(PENDING+owner+lease가 leased) |
| 단위 | payload.request JSON 전체 등가(`outbox_save_request.sql:5-6`, `outbox_reissue_failed_request.sql:8`) | 멤버 1개라 멤버십은 자명. 요청 fence = `payload->'request' IS NOT NULL AND (payload->'request'->>'generation')::int = $gen AND COALESCE(payload->'request'->>'exhausted','false') <> 'true'`. 본문·route·base_id는 pin 뒤 불변이다(pin은 request NULL일 때만 `outbox_save_request.sql:6`, producer는 고정 request 보존 `outbox_enqueue_batch_upsert.sql:51-54`, reissue는 generation·exhausted만 바꾼다 `request.go:83-88`). pin 조건은 `request IS NULL`만 남긴다 |
| 시간 | 대부분 clock_timestamp. cleanup만 Go cutoff | cleanup은 `$1 = olderThan ms`, SQL `statement_timestamp() - $1·1ms`. MarkFailed·reissue backoff는 이미 ms·clock |
| 미스 | bool | 전 전이 `*sendfence.Miss`. 호출자 처리는 §4 0-2 결과를 일반화한다(소유하지 않은 행은 손대지 않고 로그·지표) |

---

## 4. 0단계 의존 지점

| 결함 | 브랜치(진행 중) | 3단계가 전제하는 결과 | 의존 위치 |
|---|---|---|---|
| 5-1 unit 시각 | `fix/alarm-dispatch-unit-clock` (`hololive-bot-apsm-0-1`): ClaimDue가 ready 멤버가 하나라도 있는 unit의 `pending|retry` 멤버 전원을 lease(`repository_claim_0053_02.sql` 내부 count·picked 조건 교체) | unit 전원 claim이 claim의 불변식 소유자다. 3b는 이를 유지하고 시각까지 SQL로 단일화해 근본 수정한다(계획 5-1 "4.1의 시간원 통일이 근본 수정") | 3b `repository_claim_0053_02.sql` 편집은 0-1 머지 뒤 rebase. 0-1이 추가한 `send_unit_dbtest_test.go` 시험은 3b 회귀 기준 |
| 5-2 notification 재발급 미스 | `fix/notification-reissue-fence-miss` (`hololive-bot-apsm-0-2`): reissue·saveRequest SQL이 잠근 행의 조건 boolean(status/owner/lease/attempt/request_matches)을 반환 | 3a의 `sendfence.Observation` 열 이름·분류 규칙이 이 초안과 같다. 3a는 0-2의 패키지 내부 typed error를 `sendfence.Miss`로 대체하고, `request_matches`를 JSON 등가에서 generation 등가로 바꾼다 | 3a `request.go`, `outbox_reissue_failed_request.sql`, `outbox_save_request.sql`. 0-2가 추가한 지표 이름을 sendfence reason 라벨로 일반화 |
| 5-5 MarkSent SENDING만 | `fix/notification-marksent-sending-only` (`hololive-bot-apsm-0-5`): `outbox_repository_0189_05.sql` 조건 축소 + `store.go` | 3a는 MarkSent를 `status='SENDING' AND owner AND attempt` 관측 SQL로 다시 쓴다. 0-5 시험(PENDING MarkSent는 전이되지 않음)을 회귀 기준으로 유지 | 3a `outbox_repository_0189_05.sql` |
| 5-4 lease < 보존 | `fix/worker-profile-lease-retention` | 3b의 lease ms 전환 뒤에도 검증식 단위가 일치해야 한다(`worker_profile.go:118`의 attempt<lease와 함께) | 3b `wholeSeconds` 제거 시 확인 |
| 5-3 youtube | S3B | 해당 없음 | — |

---

## 5. 4단계(Iris 202 reconciler)용 확장 지점

S4가 이 기반 위에 쓰는 것:

1. **소유자 없는 unit 전이 fence**
   - reconciler 대상은 `status='sent' AND handoff_checked_at IS NULL`이고 lease를 잡지 않는다.
   - 이 경로의 버전은 `(status='sent', handoff_checked_at IS NULL, client_request_id = 관측값)`이다.
   - sendfence의 `OwnerMatches`를 `TRUE`로 계산해 같은 Check를 쓴다.
   - `failed` → retry + generation+1은 3b의 단일 문장 `send_request_reissue.sql`의 CTE 구조(멤버 전원 잠금 → clock → unit 조건 → 같은 `next_attempt_at`)를 재사용한다. from-status만 `sent`인 별도 파일 `send_request_reconcile_reissue.sql`로 둔다.
   - 시도 예산은 `attempt_count + 1` CAS(RouteSendingFailures와 같은 규칙)다.
2. **clock CTE 규약**
   - 대상 선택 `sent_at <= clock.now - 5s`
   - 다음 확인 시각 `clock.now + check_after_ms`
   - 지평 `sent_at + horizon_ms < clock.now`
   - Go 시각은 쓰지 않는다.
3. **Reason 추가 여지**: reconciler 미스는 `status_changed`(admin settle·retention이 먼저 처리)와 `request_changed`로 충분하다. 새 Reason은 필요 없다.
4. **shape CHECK**: sent→retry 시 `sent_at`을 NULL로 할지는 S4가 결정한다. `chk_alarm_dispatch_deliveries_state_shape`는 retry에 sent_at을 요구하지도 금지하지도 않는다(`schema_snapshot.golden.sql:98`). 대신 sent retention SQL이 `status='sent'`만 보므로 어느 쪽도 안전하다.
5. **(i) 선택의 비용**: notification은 별도 표라 reconciler SQL 한 벌(claim·checked·reissue·quarantine)과 `handoff_checked_at` 열 migration이 더 필요하다. reconciler Go 루프는 원장별 4메서드 인터페이스(ClaimUnchecked, MarkChecked, RequeueFailed, QuarantineUnknown)로 alarm·notification(·youtube) 구현을 받는다. 구현이 셋이라 인터페이스는 정당하다.
6. **quarantine 임계 3배 규칙**(`consumer.go:103-111`) 제거는 S4 소관이다. 3b는 threshold를 ms로만 바꾸고 값 규칙은 건드리지 않는다.

---

## 6. PR 분할

모든 PR은 다음을 지킨다.
- 한국어 주석.
- 소유 불변식 시험만 둔다(배선·문자열·SQL 텍스트 시험은 두지 않는다).
- CHANGELOG `## 미출시` 한 항목.
- 검증은 편집 완료 뒤 한 번 한다.

### 3a — sendfence 도입 + notification 소비자 적용 (의존: 0-2, 0-5 머지)

파일 소유
- 신규 `internal/egress/sendfence/fence.go`, `fence_test.go`
- `internal/egress/notificationdelivery/{store.go, request.go, dispatcher.go, failure_policy.go, maintenance.go}`
- `notificationdelivery/queries/{outbox_repository_0172_04.sql, outbox_repository_0189_05.sql, outbox_repository_0209_06.sql, outbox_mark_quarantined.sql, outbox_save_request.sql, outbox_reissue_failed_request.sql, outbox_repository_0279_09.sql}`
- `scripts/architecture/check-sql-ownership.py`(CLOCK 정책 2줄)
- `docs/current/services/alarm-worker.md:126`(소비 저장소 설명 한 문장)

변경
- 전이 메서드 시그니처를 `(bool, error)`에서 `error`로 바꾼다(`store.go:78-146`, `request.go:17-38, 97-128`, 인터페이스 `dispatcher.go:57-67`).
- MarkSending·MarkSent·Quarantine 입력에 claim 당시 `attempt_count`를 추가한다(`item.AttemptCount`).
- 호출자(`dispatcher.go:344-423`, `request.go:40-62, 82-94, 130-155`)는 Miss를 받으면 발송 전에는 중단하고 발송 후에는 오류 로그·지표만 남긴다. 소유하지 않은 행은 쓰지 않는다.
- `Cleanup(olderThan)`은 Go cutoff를 없앤다(`store.go:150-157`).
- `positiveDurationMilliseconds`의 0 이하 → 기본 lease fallback(`store.go:247-257`)과 `QuarantineStaleSending`의 기본값 대입(`store.go:202-208`)을 삭제한다. 설정 검증이 이미 양수를 강제하므로(`dispatcher.go:148-181`) 숨은 fallback이다. 0 이하는 오류로 바꾼다.

삭제
- payload JSON 전체 등가 조건
- bool 반환 경로
- 위 fallback

시험(실DB는 `dbtest.NewPool(t)` 패키지 내부 관례, 예 `store_settlement_concurrency_test.go`)
1. `sendfence.Check`: 이유 판정 순서, 부분 적용 시 UnappliedDeliveryIDs, 전원 적용 nil(순수 단위 시험).
2. stale owner(다른 owner로 재claim된 행)의 MarkSending·MarkSent·MarkFailed·Quarantine·reissue가 행을 바꾸지 않고 `owner_changed` Miss를 반환한다(`TestMarkSendingCannotUsePreviousOwnerAfterConcurrentReclaim`, `TestOutboxSettlementCannotOverwriteConcurrentQuarantine` 갱신).
3. 같은 owner·다른 attempt(FAILED→rearm 뒤 재claim)의 이전 attempt 정산이 `version_changed`로 거절된다(신규).
4. 재발급 세대 fence: 다른 generation 기록 뒤 이전 세대 reissue가 `request_changed`가 되고 SENDING 잔류가 없다(`TestGenericRequestGenerationCASRejectsStaleWriter`, `TestGenericPayloadMismatchDoesNotReissue` 갱신. 후자는 JSON 등가 전제라 generation 등가로 다시 쓴다).
5. cleanup 보존 경계가 DB 시각 기준이다(`store_cleanup_test.go` 갱신. Go `time.Now()` 기반 backdate는 `clock_timestamp()` 기반으로 바꾼다).
6. 기존 순서 시험(`store_ready_test.go`, `dispatcher_claim_test.go`)은 변경 없이 통과해야 한다.

bool을 고정하던 assertion이 있는 갱신 대상 시험 파일
- `store_test.go`, `store_lease_test.go`, `request_test.go`, `failure_policy_test.go`, `dispatcher_test.go`, `dispatcher_reliability_test.go`, `store_quarantine_test.go`

검증
- `gofmt -l hololive/hololive-alarm-worker/internal/egress`
- `go build ./... && go vet ./internal/egress/...` (cwd `hololive/hololive-alarm-worker`)
- `go test -race ./internal/egress/sendfence/... ./internal/egress/notificationdelivery/...`
- `python3 scripts/architecture/check-sql-ownership.py`

배포: worker 단독. 스키마 변경 없음. 구·신 worker 혼재 시에도 SQL 조건은 행 데이터만 읽으므로 rolling 호환이다. 단 신 worker의 attempt 조건은 구 worker가 claim한 행에 적용되지 않는다(각자 자기 claim만 정산).

### 3b — alarm-dispatch fence 정리 (의존: 3a의 sendfence, 0-1 머지, 0-4 머지)

파일 소유
- `internal/service/alarm/dispatchoutbox/{model.go, consumer.go, consumer_send_request.go, repository.go, repository_claim.go, repository_transitions.go, repository_terminal.go, repository_maintenance.go, send_request.go, metrics.go, sql.go}`
- `dispatchoutbox/queries/`: §2.3 표의 파일. 신규 `send_request_begin.sql`, `repository_terminal_dlq.sql`, `repository_terminal_quarantine.sql`. 삭제 `repository_terminal_0019_01.sql`, `send_request_reissue_delivery.sql`, 독립 MarkSending 용도의 `repository_transitions_0020_01.sql`
- `internal/egress/alarmdispatch/{alarm_dispatch_runner.go, alarm_dispatch_runner_failure.go}`
- `hololive/hololive-shared/pkg/domain/alarm.go`(NextVisibleAt 삭제)
- `scripts/architecture/check-sql-ownership.py`
- `docs/current/runbooks/alarm-worker.md`(fence 미스 지표 라벨 한 줄)

삭제
- `PartialTransitionError`, `ErrSendRequestFence`, `ErrSendRequestLeaseExpired`
- `wholeSeconds`, `expectRowsAffected`, `validatePostSendRowsAffected`, `partialFailureRoutingError`, `unappliedFailureIDs`, `requeueReissuedDeliveries`
- Consumer의 MarkSending 타입 단언 fallback
- `FailureUpdate.NextAttemptAt`, `AlarmQueueRetryMetadata.NextVisibleAt`
- `repository_transitions_0020_01.sql`의 `$4`(파일과 함께)

시험
1. 같은 unit 멤버의 실패 전이(RouteFailures·RouteSendingFailures·reissue)는 모든 멤버에 같은 `next_attempt_at`을 쓰고, 그 값은 `clock_timestamp()` 기준 + retry_after다. 실DB 시험이다. 5-1 근본 수정을 증명한다. 0-1 재현 시험을 그대로 통과해야 한다.
2. `BeginSendRequest` 단일 문장: lease 만료·owner 변경·멤버 추가·요청 미고정·client_request_id 변경 각각에서 상태 변화가 없고 Reason이 정확하다(`TestBeginSendRequestRejectsChangedFencesAtomically` 갱신, `TestSendRequestChecksLeaseAfterAllLocks` 유지).
3. `TestSendingTransitionExpiryBoundaryUsesSuppliedClock`(`send_request_lease_clock_test.go:304`)는 삭제한다. supplied clock 파라미터가 사라지는 구현 세부 고정이다.
4. 발송 전 membership_changed는 release이고 quarantine이 아니다. member_terminal은 quarantine이다(runner 수준. `alarm_dispatch_lease_clock_test.go`·`alarm_dispatch_immutable_request_test.go` 관례).
5. 발송 후 MarkSent 미스(stale sweep이 먼저 격리)에서 행을 덮어쓰지 않고 Miss를 반환하며 claim key를 해제하지 않는다(`repository_integration_test.go` partial 시험을 Miss로 갱신).
6. Terminal·Release·MarkSent에 attempt 불일치를 주면 `version_changed`로 거절한다(신규).
7. 같은 worker ID 재claim ABA 불변식: runner가 이전 배치 정산 전에 다음 claim을 하지 않는다(`alarm_dispatch_runner_test.go`의 fake consumer 호출 순서로 확인).
8. lease ms: 1500ms lease가 1.5s로 저장된다(`repository_duration_args_test.go` 갱신. 현재는 초 절사를 고정한다).
9. Reissue 세대 소진은 `ErrGenerationExhausted`이고 격리로 간다(`send_request_test.go:15` 갱신).

갱신 대상 시험 파일
- dispatchoutbox: `consumer_test.go`, `consumer_marksending_metrics_test.go`, `repository_transitions_partial_test.go`(sendfence 시험으로 이동·삭제), `repository_integration_test.go`, `repository_lease_clock_test.go`, `send_request_test.go`, `send_request_lease_clock_test.go`, `recovery_send_unit_test.go`, `repository_duration_args_test.go`
- alarmdispatch: `alarm_dispatch_runner_test.go`, `alarm_dispatch_runner_sending_retry_test.go`, `alarm_dispatch_finalize_failure_test.go`, `alarm_dispatch_runner_cancellation_test.go`, `alarm_dispatch_lease_clock_test.go`, `alarm_dispatch_request_fixtures_test.go`
- 기타: `internal/app/workerapp/alarm_dispatch_see_more_fold_test.go`

검증
- `gofmt -l`
- `go build ./... && go vet ./internal/service/alarm/dispatchoutbox/... ./internal/egress/...`
- `go test -race ./internal/service/alarm/dispatchoutbox/... ./internal/egress/alarmdispatch/... ./internal/app/workerapp/...`
- hololive-shared `go test ./pkg/domain/...`
- hololive-api admin `go test ./internal/planes/admin/internal/service/dispatchops/...`(정합 확인)
- `python3 scripts/architecture/check-sql-ownership.py`
- 성능 비교(계획 §7): claim·전이 SQL의 `EXPLAIN (ANALYZE, BUFFERS)`를 dbtest 고정 데이터에서 전후 비교한다. 관측 반환 형태 때문에 잠금 대상이 "소유 행"에서 "요청 ID 전체"로 넓어지므로 대기 시간을 비교한다.

배포
- worker 단독, 스키마 변경 없음.
- rolling 중 구 worker는 초 단위 lease, 신 worker는 ms lease를 쓴다. 각자 자기 claim만 정산하므로 호환된다.
- `NextVisibleAt` 삭제는 shared 모듈 변경이지만 worker만 소비한다. API는 이 필드를 쓰지 않는다(`git grep` 0건).

### 이후

- S3B youtube: sendfence 소비. Observation `VersionMatches`=row_version, `LeaseValid`=locked_at+lease_ms, floor 허용 규약.
- S4: §5 확장 지점.

---

## 7. 위험

1. 관측 반환 SQL은 소유 여부와 무관하게 요청 ID 전원을 잠근다. 다른 소유자의 진행 중 문장을 짧게 기다릴 수 있다. ID 순 잠금이라 교착은 없지만 대기는 늘 수 있다. 3b에서 EXPLAIN·잠금 대기를 비교한다.
2. 발송 전 membership 미스를 release로 바꾸면, 종료 멤버 판정이 틀릴 경우 claim↔release가 반복될 수 있다. member_terminal 분류를 실DB로 시험하고, release 반복을 지표 reason 라벨로 감시한다.
3. 같은 worker ID ABA는 runner 직렬성에 기대는 불변식이다. runner를 병렬화하면 claim별 토큰(row_version 또는 locked_by nonce)이 필요하다.
4. (i)를 택하면 4단계 reconciler가 원장별로 한 벌씩 필요하다(notification 표 migration 포함). S4 작업량이 늘어난다.
5. 0-2 초안의 열 이름이 머지 전 바뀌면 3a의 Observation 열을 맞춰야 한다.
6. 병렬 설계가 같은 migration 번호 325를 노릴 수 있다. 3a·3b는 migration이 없으므로 직접 충돌은 없다.

## 8. open questions (사용자 결정)

1. notification 단일 원장 흡수 (ii)/(iii)
   - DEC-20260926 반전과 PROJECT_MAP 계약 변경이 필요하다.
   - 순서·재시도·rearm·identity·보존 의미가 바뀌거나 소스별 분기가 필요하다.
   - 권고는 (i)로 지금 진행하고, 단일 원장은 4단계 뒤 별도 결정으로 미루는 것이다.
2. (i) 확정 시 alarm-dispatch 소비 측 `delivery_digest` 잔재를 삭제할지 유지할지
   - 잔재 위치: 렌더 `alarm_dispatch_render.go:76-100`, 그룹 `alarm_dispatch_group.go:133-140`, dedupe `dispatch_group.go:53-54`·`dedupe_key.go:72,159-165`, domain 타입, rollback preflight.
   - 유지 근거: 운영 rollback preflight 절차가 남아 있다.
3. 계획 6절의 "kind·period_key·room 순서 제약" 문구를 실제 계약 "room serial lane + (kind, content_id) 유일성"으로 고칠지(계획 문서 갱신은 부모 소관).
4. notification의 TransportAmbiguous → QUARANTINED와 alarm-dispatch의 고정 ID 재시도의 차이를 4.2 이후 통일할지. 현행값 유지 원칙으로 3단계에서는 바꾸지 않는다.
