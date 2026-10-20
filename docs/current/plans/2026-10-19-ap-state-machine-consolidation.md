# 상태머신·최소 한 번 처리 로직의 AP 이관 계획

작성일: 2026-10-19
개정: 2026-10-19 코드 리뷰 반영. 원안의 방향 선언은 유지하고, 코드와 어긋난 전제·빠진 결함·Iris 202 추상화 결정을 추가했다.
상태: 0·1·2단계와 4b를 로컬 브랜치로 구현했다(미게시). 3단계 이후는 다른 세션에서 이어 간다. 진행 상황·브랜치·남은 작업·게이트는 [인계 문서](2026-10-19-ap-state-machine-consolidation-handoff.md)에 있다.

## 0. 원안에서 바꾼 것

| 항목 | 원안 | 개정 | 근거 |
|---|---|---|---|
| 작업 소유권 fence | "경로별 fence를 유지한다" | 행당 fence 튜플 하나로 통일한다 | youtube BeginSending은 claim 시점 `row_version`을 쓰지 않고(`transition_begin.go:105,110`), `PreparationLease`는 재조회 행으로 만들어 같은 행과 비교하는 동어반복이다(`transition_types.go:203`, `resolver.go:86`). alarm MarkSent는 설계상 lease 만료를 무시한다. "오래된 소유자는 정산할 수 없다"는 현재 보장되지 않는다. |
| 발송 경로 통합 | "원장별 의미가 다른 부분까지 하나로 합치지 않는다" | 공통 기반으로 수렴한다 | 세 경로의 차이는 원장 의미가 아니라 구현 우연이다. lease 단위가 초·ms·Go 시각으로, 요청 고정 위치가 컬럼·별도 테이블·jsonb로 갈린다. |
| handoff 결과 불명 | 보존 계약 | 4.2절의 결정 사항 | Iris 202 동기 취급 여부가 이 계획의 핵심 선택지다. |
| 전역 guard | "정책 이관과 동시에 제거하지 않는다" | 2단계에 대체안을 포함한다 | 트리거만 Go 호출로 바꾸고 guard 행이 남으면 직렬화 비용이 그대로라 단계의 실익이 없다. |
| writer 목록 | 미완료 | 확인 완료 | 라이브 테이블 writer는 `hololive-api/internal/youtube/sourceobservation` 하나다. 워커·수집기의 참조는 모두 읽기 SQL이다. |
| 결함 목록 | 없음 | 5절 추가 | 리뷰에서 결함 5건을 확인했다. |
| 범위 | 봇 inbox·관측 큐·수집기 lease 포함 | 발송 세 경로 + 라이브 상태 + 봇 reply outbox(4단계) | 봇 inbox와 수집기 lease는 별도 소유자·계약이며 이 계획의 의존성이 아니다. 봇 reply outbox는 같은 handoff 폴링과 reissue ladder를 가지므로 4단계에 함께 넣는다(사용자 결정 2026-10-19). |

원안에서 맞았던 사실은 그대로 둔다. `repository_live_session_upsert_0047_47.sql`은 ENDED 유지, LIVE에서 UPCOMING 후퇴 방지, 관측 시각 우선순위를 SQL CASE로 다시 판단한다. `repository_claim_0012_12.sql`은 선점과 함께 replay epoch 만료·시도 소진을 DEAD_LETTER로 정산한다. 둘 다 Go reducer·Retry와 중복이다.

## 1. 목표와 제약

DB에 남은 업무 상태 전이 정책을 소유 모듈의 Go 코드로 모으고, 최소 한 번 전달을 위한 세 발송 경로의 소유권·요청 고정·재시도·복구 로직을 하나의 fence 모델로 수렴한다.

- AP는 애플리케이션 계층이다. API의 관측 소비·canonical 저장, worker의 발송, collector의 수집 소유권은 [프로젝트 지도](../PROJECT_MAP.md)를 유지한다.
- 정책 판단은 Go, DB는 UNIQUE/FK/CHECK·행 잠금·SKIP LOCKED·조건부 갱신·원자적 commit을 맡는다. 집합 조회·집계 SQL은 유지하고 행별 왕복으로 바꾸지 않는다.
- 감사 원장 불변 트리거, `chk_*_state_shape`, `chk_alarm_send_request_shape`, ledger shape CHECK, `record_alarm_dispatch_closeout`의 SERIALIZABLE 강제와 잠금 순서는 업무 상태 전이가 아니라 방어선이므로 유지한다.
- 범용 상태머신 엔진, 새 큐·브로커, 메모리 상태 정본은 추가하지 않는다.
- 전달 보장·재시도 한도·순서·보존 정책은 4.2 결정 전까지 현재 값을 유지한다.

## 2. 확인된 현재 구조

### 2.1 발송 세 경로

`build_egress.go`에서 세 러너가 모두 활성이다.

| 경로 | 패키지 | 소스 줄 | 시험 줄 | SQL |
|---|---|---|---|---|
| alarm-dispatch | dispatchoutbox + alarmdispatch | 6,533 | 14,362 | 39 |
| youtube-outbox | youtubedispatch + store + lifecycle + preparation | 13,103 | 20,929 | 42 |
| notification-delivery | notificationdelivery | 1,198 | 3,866 | 12 |

| 메커니즘 | alarm-dispatch | youtube-outbox | notification-delivery |
|---|---|---|---|
| 상태 어휘 | 8종 소문자 | 5종 + ledger 2종 | 5종 |
| 소유자 | `locked_by` | 없음, 재조회 FOR UPDATE | `locked_by` |
| 만료 | `lock_expires_at`, 발송 전만 | `locked_at` + LockTimeout, claim 때만 | `lock_expires_at`, MarkSending만 |
| 버전 CAS | `attempt_count + 1` | `row_version`·`locked_at`·`attempt_count` 3중 | `attempt_count` + payload JSON 전체 등가 |
| 단위 멤버십 | `request_delivery_ids` | `member_ids` + 논리 그룹 owner | 없음 |
| 요청 세대 | `client_request_id`·`base`·`generation` | `send_request_id`·`generation`·`message_hash` | `generation`·`exhausted` |
| 외부 dedup | Iris 승인 + Valkey claim key | Iris 승인 + `alarm_states.authorized_at` | Iris 승인 |
| 시간원 | SQL `NOW()`와 Go RFC3339 혼용 | Go 시각을 DB `locked_at`과 비교 | SQL `clock_timestamp()` |

### 2.2 Iris 비동기 처리

- text 경로: `SendMessage`가 durable admission에서 반환한다. handoff를 기다리지 않는다.
- markdown 경로: `SendMarkdown` 202 뒤 `iris_sender.go`가 250ms 간격으로 `GetReplyStatus`를 폴링해 `handoff_completed`만 성공으로 본다. 폴링은 attempt timeout에 묶이고, lease를 넘길 수 있어 dispatchoutbox는 quarantine 임계를 lease의 3배로 둔다.
- 봇 plane: `transport/reply_handoff.go`가 같은 폴링을 하고, `bot_reply_outbox`는 `accepted`를 포함한 10개 상태 어휘와 자체 reissue ladder를 가진다.
- 결과 분류 `sendoutcome.Classify`는 Success·Failed·OutcomeUnknown·TransportAmbiguous 4종이며, 경로마다 라우팅이 다르다.

### 2.3 DB 쪽 상태 로직

| 분류 | 대상 | 처리 |
|---|---|---|
| 유지 | `chk_alarm_dispatch_deliveries_state_shape`, `chk_alarm_send_request_shape`, ledger shape, closeout receipts 불변 트리거, `record_alarm_dispatch_closeout` | 방어선. 호출 측 `dispatchops/settle.go`·`requeue.go`가 Serializable로 시작하고 40001을 충돌 오류로 매핑하는 것과 정합한다. |
| 이관 1순위 | `youtube_live_runtime_state` + `refresh_youtube_live_runtime_state(_statement)` + `lock_youtube_live_runtime_state` | guard 행이 세션·head·pending_ends·availability의 모든 DML과 `source_observations` DELETE/TRUNCATE를 직렬화한다. 소비자는 `live_check_videos.sql`·`collection_target_observability.sql` 둘뿐이다. |
| 이관 1순위 | `repository_live_session_upsert_0047_47.sql`의 CASE 정책 | `live.Reduce`와 중복. |
| 이관 2순위 | `youtube_community_shorts_alarm_states.authorized_at` claim | delivery lease와 이중 lease. `ClaimToken.Reused`는 이 겹침을 메우는 장치다. |
| 이관 2순위 | `repository_claim_0012_12.sql`의 DEAD_LETTER 정산 | Go Retry와 중복. 선점 트랜잭션은 유지한다. |

## 3. 보장 경계

| 경계 | 현재 | 목표 |
|---|---|---|
| 영속 접수 | 접수 불명은 실패로 단정하지 않고 재조회한다. | 유지. |
| DB 내부 처리 | canonical 저장·적용 기록·완료가 같은 트랜잭션. | 유지. |
| 작업 소유권 | 경로마다 다르고 youtube는 stale owner가 먼저 BeginSending을 잡으면 발송한다. 중복 발송은 status 검사가 막는다. | 행당 `(소유자, 버전, 상태)` 튜플 하나. 단위 수준에는 멤버십과 요청 세대만. 시간원은 DB `clock_timestamp()`. fence 미스는 항상 관측 가능한 오류. |
| 외부 발송 | 본문·경로·ID 고정, pre-handoff 409만 세대 재발급, 결과 불명 격리. | 4.2 결정에 따른다. 고정과 세대 상한 2는 유지한다. |
| 발송 후 정산 | 외부 성공을 DB 정산 실패로 되돌리지 않는다. | 유지. |
| 복구 종료 | 미발송 claim 회수와 sending 결과 불명 격리는 별개. | 유지. 단 결함 5-1은 미발송인데 격리로 굳는 경로다. |

## 4. 결정 사항

### 4.1 fence 튜플 통일 (결정)

- 행: `status`, `locked_by`, `row_version` 또는 `attempt_count` 중 하나. 둘 다 CAS로 쓰지 않는다.
- 단위: 멤버 ID 배열 등가, 요청 본문 해시, `generation`.
- 시간: 만료 판정과 `next_attempt_at` 계산은 SQL `clock_timestamp()` 한 곳. Go가 만든 시각을 DB 시각과 비교하지 않는다.
- 제거: `PreparationLease`, youtube의 3중 CAS 중 `locked_at IS NOT DISTINCT FROM`, notification의 payload JSON 전체 등가, `ClaimToken.Reused`.
- 미스 처리: rows-affected 불일치는 예외 없이 typed error로 올린다. bool 반환을 버리는 호출은 허용하지 않는다.

### 4.2 Iris 202 동기 추상화 (결정 필요)

제안: 원장의 SENT를 "Iris durable admission 완료"로 정의하고, handoff 폴링을 발송 경로에서 제거한다. text 경로는 이미 이 의미다.

- 바뀌는 것: markdown 경로의 `waitForReplyHandoff` 제거, `sendoutcome` 4종에서 handoff 계열 제거, dispatchoutbox quarantine 임계 3배 규칙 제거, attempt timeout과 폴링 분리. 봇 plane의 `accepted` 상태와 자체 ladder도 같은 정의로 축소 가능하다.
- 유지되는 것: `DEC-20260731-reply-outcome-unknown-fail-closed`. 재발송하지 않으므로 위배가 아니다. admission 호출 자체의 transport ambiguity 재시도와 409 `CLIENT_REQUEST_ID_FAILED` 세대 재발급은 admission 단계에 남는다.
- Iris 문서로 확인한 사실(`Iris/docs/contracts/current.md` "Reply dispatch outcome", `Iris/docs/architecture/current.md` §1):
  - admission 이후 `retrySafe = true`인 pre-dispatch 실패는 Iris가 `queued`로 되돌려 자체 재시도한다. 재시작 뒤에도 `queued` 행은 durable 큐가 replay한다.
  - dispatch 시작 뒤의 연결 끊김·timeout·취소·응답 손상은 `outcome_unknown`이며 Iris가 재전송하지 않는다.
  - `completed`(SDK `handoff_completed`)는 Kakao reflective 호출이 handoff를 동기적으로 받았다는 뜻이고 수신자 전달 확인이 아니다.
  - 큐 payload는 `retry` 정책을 가진다. 재시도 상한과 소진 뒤 `failed` 전이 여부는 문서에 없다.
- Iris 코드로 확인한 사실(2026-10-19, 사용자가 `iris-runtime/src/reply` 아래 지정 파일을 출력해 확인):
  - callback 결과는 `Completed`·`RetryablePreDispatch`·`Rejected`·`OutcomeUnknown` 4종이다(`scheduler/callback.rs`).
  - `RetryablePreDispatch`는 `retry_or_reject`(`scheduler/worker_loop/handoff/retry.rs`)에서 `attempt >= MAX_DISPATCH_RETRIES`면 `reject_terminal`로 `failed`가 되고, 아니면 `schedule_retry`로 `DispatchRetry{attempt, not_before_epoch_ms}`를 올려 재큐된다. 시험 `retention_expired_queued_admission_still_spends_the_bounded_retry_budget`가 이 소진 경로를 고정한다.
  - `RetryablePreDispatch`의 원인에는 bridge mux 연결 거부, callback 미가용, dispatch claim 거부처럼 일시 장애가 포함된다. 따라서 **`failed`는 결정적 거부뿐 아니라 일시 장애가 재시도 창을 넘긴 경우에도 발생한다.**
  - `Rejected`는 예산과 무관하게 바로 `failed`다.
- 결정: **B안.** 발송 경로는 202 durable admission에서 SENT로 끝낸다. 별도 reconciler가 `GetReplyStatus`로 `failed`를 발견하면 그 행을 기존 retry 전이로 되돌리고, 세대를 올려 `:r1/:r2`로 재발급한다. 상한 2와 `outcome_unknown` 격리는 그대로다. 새 행을 만들지 않는다.
  - 오늘의 워커 동작(폴링 → `failed` → 같은 ID 재시도 → 409 `CLIENT_REQUEST_ID_FAILED` → 재발급)과 결과가 같다. 차이는 폴링이 발송 트랜잭션·lease 밖으로 나가고, 확인 주기가 250ms 폴링에서 reconciler 주기로 바뀐다는 점이다.
  - Iris 재시도 창(2차 확인, `handoff/retry.rs`): `MAX_DISPATCH_RETRIES = 3`, backoff `250ms × 2^(attempt-1)`, 상한 4,000ms, jitter는 base/4 이하. 첫 시도 포함 dispatch 4회, 누적 지연 약 1.75~2.2초다. 이 창을 넘기는 bridge 장애는 모두 `failed`가 된다. `Rejected`는 platform callback 응답 `ok=false`이며 즉시 `failed`다.
  - 상태 조회 보존: 메모리 status store는 TTL 168h, 상한 4,096건(오래된 것부터 축출). 축출 뒤에는 durable admission 상태로 fallback하며 `failed`도 보고한다. client request ID 없는 요청은 fallback이 없으나 워커·봇은 항상 ID를 보낸다. terminal 큐 잔재와 168h 지난 admission은 매시간 sweep한다.
  - reconciler 규칙: 첫 확인은 SENT 뒤 5초(Iris 창의 2배 이상), 미종결이면 15s → 60s → 5min 간격으로 재확인, 지평은 현행 attempt timeout(워커 프로필, stack-secrets 소유)과 같게 시작한다. 지평을 넘겨도 미종결이면 "handoff 미확인" 사유로 격리한다. 오늘의 `ErrHandoffOutcomeUnknown` 격리와 같은 결과이고, lease를 잡지 않으므로 지평을 lease와 독립적으로 늘릴 수 있다.
  - 원장에는 "admission 완료, handoff 미확인"을 표현할 필드가 하나 필요하다(예: `handoff_checked_at`). 상태 어휘는 늘리지 않는다.
  - 부수 효과: text 경로는 오늘 SDK `SendMessage`가 admission에서 반환하고 아무도 상태를 조회하지 않아 `failed`가 조용히 유실된다(`iris-client-go`에 handoff 대기 없음). reconciler가 text 경로도 확인하면 이 유실이 닫힌다. 적용 범위에 text 경로를 포함한다.
- 바뀌는 계약 문서: [alarm.md](../contracts/alarm.md) "Iris Markdown admission" 절. 봇 plane 상태 어휘를 줄이면 `bot_reply_outbox` CHECK migration이 필요하다.
- 승인: 제품 계약 변경이므로 사용자 결정이 필요하다. 결정 전에는 4단계를 시작하지 않는다.

### 4.3 전역 guard 대체 (결정)

- Go가 runtime_state를 직접 쓰면 statement trigger가 사라지므로 guard 호출처는 `repository_finalize.go:90`·`live_finalizer.go:31` 둘만 남는다.
- 대체: 단일 writer 모듈 안에서 `pg_advisory_xact_lock`을 같은 순서로 잡는다. RR snapshot 거절 의도는 Go의 40001 재시도 정책으로 명시한다.
- 전환 기간: 트리거 유지 + Go 쓰기, `TestLiveRuntimeStateDriftCountsEachMismatch` 형태의 drift 계측으로 불일치 0을 확인한 뒤 migration으로 트리거·guard를 제거한다.

## 5. 확인된 결함 (이관과 독립, 먼저 수정)

1. **alarm-dispatch send unit 격리.** `alarm_dispatch_runner_failure.go:347`·`:314`가 봉투마다 `time.Now()`로 `next_attempt_at`을 만들고 `consumer.go:356`이 그대로 저장한다. `repository_claim_0053_02.sql:35`는 행 단위 ready 판정이라 마이크로초 창에 unit 일부만 leased가 되고, `send_request.go:154` fence가 실패해 `alarm_dispatch_runner.go:265`가 Quarantine으로 보낸다. 남은 멤버도 다음 claim에서 membership 불일치로 격리된다. 만료 복구 경로는 SQL `NOW()`를 공유해 안전하다. 관련 시험 없음.
   - 수정: 그룹당 한 번 시각을 계산하거나, ClaimDue가 ready 멤버가 하나라도 있는 unit을 전원 claim한다. 4.1의 시간원 통일이 근본 수정이다.
2. **notification 재발급 fence 미스가 조용함.** `request.go:90`이 bool을 버려 0행이면 SENDING에 남고 stale sweep이 "결과 불명" 사유로 격리한다. 실제는 확정된 pre-handoff 충돌이다. `request.go:61`의 saveRequest도 같다.
   - 수정: 미스를 로그·지표와 함께 즉시 실패 전이로 처리한다.
3. **youtube claim 버전 미검증.** `FreezeRequest`(`send_request.go:194`)와 `DeferFollower`는 호출자 버전을 fence로 쓰지만 `BeginSending`은 쓰지 않는다. 같은 토큰의 계약이 경로마다 다르다.
   - 수정: 4.1에서 하나로 정리한다. 단독 수정이면 BeginSending에 claim 버전 비교를 추가한다.
4. **168h > 60s 불변식 미강제.** `notificationdelivery/dispatcher.go:426` 주석에만 있다.
   - 수정: 워커 프로필 검증에 Iris admission 보존 기간 대비 lease 상한을 추가한다.
5. **notification PENDING→SENT 허용.** `outbox_repository_0189_05.sql`이 `status IN (PENDING, SENDING)`으로 MarkSent를 받는다. 코드 경로상 도달하지 않지만 상태 폭이 넓다.
   - 수정: SENDING만 허용한다.

## 6. 단계와 순서

### 0단계: 결함 수정

- [ ] 5-1: 시각 단일화 또는 unit 단위 claim. 재현 시험: 멤버 두 개의 `next_attempt_at`이 1µs 차이일 때 claim과 fence가 unit을 격리하지 않음.
- [ ] 5-2: bool 반환 처리와 로그·지표. 시험: fence 미스 시 SENDING 잔류 없음.
- [ ] 5-4: 프로필 검증 추가. 시험: lease가 보존 기간을 넘기면 기동 오류.
- [ ] 5-5: SQL 조건 축소와 기존 시험 통과.
- 의존성 없음. 각각 독립 PR.

### 1단계: 라이브 상태 결정·저장 중복 정리

- [ ] writer는 `sourceobservation` 하나다. 관측 소비, live check, finalizer, 최초공개, 시각 확정, maintenance 경로의 잠금 순서만 표로 확정한다.
- [ ] `repository_live_session_upsert_0047_47.sql`의 status·observed_at CASE를 제거하고, `live.Reduce` 결과를 잠금 안에서 읽은 최신 행 기준으로 조건부 UPDATE한다. 적용 0행은 충돌로 보고한다.
- [ ] `repository_claim_0012_12.sql`의 DEAD_LETTER 정산을 Go Retry로 옮긴다. 선점·원자 저장은 같은 트랜잭션에 남긴다.
- 잠금 없이 읽고 무조건 UPDATE하는 방식은 채택하지 않는다.

### 2단계: `youtube_live_runtime_state` 명시 갱신과 guard 대체

- [ ] `persistLiveDecision`·finalize 트랜잭션에서 변경 key를 모아 runtime_state를 직접 upsert한다. 외부 호출은 트랜잭션에 넣지 않는다.
- [ ] 4.3의 advisory lock과 40001 재시도 정책을 구현한다.
- [ ] 트리거 병행 기간에 drift 0을 확인한 뒤 새 migration으로 트리거·함수·guard를 제거한다. 적용된 312·313·316은 수정하지 않는다.
- [ ] dbtest의 runtime_state 시험 11개와 guard 시험 2개를 Go 쓰기 전제로 재작성한다.
- 의존: 1단계. writer 하나이므로 rolling 호환은 요구하지 않고 API 단일 배포로 전환한다.

### 3단계: fence 튜플 통일과 경로 수렴

- [ ] notification-delivery를 alarm-dispatch 소비자로 흡수한다. kind·period_key·room 순서 제약은 send unit 그룹 키로 표현한다. 가장 단순하고 SQL 12개다.
- [ ] alarm-dispatch의 fence를 4.1 튜플로 정리한다. `checkedAt` 시각 전달은 SQL clock CTE로 대체한다.
- [ ] youtube-outbox는 마지막이다. 논리 그룹·ledger·commit 응답 adjudication은 유지 가치가 있으므로 공통 기반 위에 옮긴다. `PreparationLease`, 3중 CAS, `Reused`, alarm_states 이중 lease를 제거한다.
- 의존: 0단계. 2단계와 독립.

### 4단계: Iris 202 추상화

- [x] 4.2의 `failed` 의미를 Iris 코드로 확인했다. B안으로 진행한다.
- [ ] 워커: markdown 폴링 제거, `sendoutcome`을 Success·Failed·TransportAmbiguous로 축소, quarantine 임계 3배 규칙 제거, attempt timeout을 admission 호출에만 적용.
- [ ] 워커 reconciler(alarm-dispatch 소비자 안의 별도 claim 경로): 대상은 `status='sent' AND handoff_checked_at IS NULL AND sent_at <= clock_timestamp() - 5s`, SKIP LOCKED 배치. 전이는 `handoff_completed` → `handoff_checked_at` 기록, `failed` → generation < 2이고 시도 예산이 남으면 `retry`로 되돌리고 generation+1(다음 claim이 기존 재발급 경로로 `:r1/:r2` 발송), 아니면 `dlq`, `outcome_unknown`·404 → `quarantined`, 그 외 비종결 → 다음 확인 시각 갱신, 지평 초과 → `quarantined`("handoff 미확인"). text·markdown 모두 대상.
- [ ] 시험: reconciler 전이 6종, `failed` 재구동이 세대 상한 2를 넘지 않음, 재확인 간격, 지평 초과 격리, 상태 조회 오류(관측 실패)는 전이 없이 다음 확인으로 미룸.
- [ ] 봇 plane: `transport/reply_handoff.go` 폴링 제거, `bot_reply_outbox` 상태 어휘에서 `accepted`·`outcome_unknown` 계열을 줄이고 CHECK migration을 추가한다. `reply_outbox_reclaim.go`의 "accepted는 재발송하지 않음" 규칙은 reconciler 쪽으로 옮긴다.
- [ ] alarm.md "Iris Markdown admission" 절과 봇 plane 계약 갱신.
- 의존: 3단계의 alarm-dispatch 정리. 봇 plane은 워커와 같은 정의를 쓰되 배포 단위는 API라 별도 PR.

## 7. 검증

- 단계별로 영향 모듈의 `go test`와 dbtest 통합 시험을 먼저 실행하고, 새로 불확실해지는 경계에만 시험을 보강한다.
- 격리 DB에서: 동일 작업 동시 claim, lease 만료 뒤 stale owner 정산 거절, 멤버 간 시각 차이, 재발급 fence 미스, 접수 commit 뒤·claim 뒤·정산 commit 전후 중단 복구.
- 2단계: 원본·파생 모델 rollback, DELETE/FK 영향, 동시 writer, drift 0.
- 변경 경로의 DB 왕복·잠금 대기·처리량을 이전 구현과 비교한다. 정책을 옮겼다는 이유로 성능 개선을 주장하지 않는다.
- 원안이 수행한 임시 Go 프로그램 실행은 회귀 검증이 아니며 완료 근거로 쓰지 않는다.

## 8. 승인과 미확정

- 4.2는 B안으로 결정했다(2026-10-19). 계약 문서 갱신과 봇 plane CHECK migration은 4단계 PR에서 함께 검토한다.
- reconciler 지평의 기준이 되는 attempt timeout 값은 stack-secrets가 배포하는 워커 프로필에 있다. 4단계 착수 시 stack-platform-ops로 현재 값을 확인한다.
- 운영 migration·데이터 변경·runtime 배포·권한 확대·Git 발행은 별도 승인이다.
- 전역 guard의 실제 경합 빈도와 결함 5-1의 운영 발생 여부는 미측정이다. 수정 우선순위는 코드 근거만으로 정했다.
