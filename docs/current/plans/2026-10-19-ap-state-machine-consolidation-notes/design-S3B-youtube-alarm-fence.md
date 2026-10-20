# 설계 S3B: youtube-outbox를 공통 fence 기반으로 수렴 (계획 2.1·4.1·5-3·6절 3단계)

작성: 2026-10-19, 기준 코드 `/home/kapu/work/iris-stack/hololive-bot-apsm-1` (cb105cc97). 저장소는 읽기만 했고 빌드·시험은 실행하지 않았다.
경로 약어: `YD` = `hololive/hololive-alarm-worker/internal/egress/youtubedispatch`, `ST` = `YD/store`, `OBS` = `hololive/hololive-shared/pkg/service/youtube/tracking/observation`, `DO` = `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox`.
공통 기반은 설계 A(S3A)가 `hololive-alarm-worker/internal/egress/sendfence`로 정의 중이며, 이 문서의 5절 요구를 S3A가 수용한다고 회신했다(2026-10-19 peer 메시지).

---

## 0. 결론 요약

1. youtube의 행 fence는 `(status, locked_by, row_version)`으로 한다. `attempt_count`는 claim·defer·follower mirror에서 바뀌지 않아 claim 세대를 구분할 수 없으므로 버전 축은 `row_version`만 가능하다.
2. 결함 5-3의 원인은 `BeginSending`·`ApplyPreparedFailure`·`PrepareClaimed`가 호출자 claim 버전 대신 같은 트랜잭션에서 다시 읽은 행을 `before`로 쓰는 것이다(`ST/transition_begin.go:105,110,184`, `ST/transition_failure.go:125`, `ST/transition_prepare.go:75`). `PreparationLease`는 그 재조회 행으로 만들어 같은 행과 비교하므로 검증력이 없다(`ST/transition_types.go:198-215`, `YD/preparation/resolver.go:81-96`). 둘을 "호출자 claim fence와 재조회 행 비교" 하나로 대체한다.
3. 3중 CAS(`ST/queries/transition_apply_rows.sql:52-55`)의 `attempt_count`·`locked_at` 조건은 `row_version` 등가에 함의된다(상태·시도·잠금을 바꾸는 모든 writer가 버전을 올린다. 3.2절 전수). 제거하고 `locked_by IS NOT DISTINCT FROM`으로 바꾼다.
4. 시간원: claim·만료·`next_attempt_at`·`sent_at`·`locked_at`을 모두 SQL clock CTE(`clock_timestamp()` 한 곳)에서 만든다. Go는 `delay_ms`·`lease_ms`와 DB 유래 하한(floor)만 넘긴다(S3A 규약). 이 때문에 commit 응답 유실 adjudication은 시각 필드 비교를 버리고 `(status, row_version, attempt_count, locked_by, error)`로 post-state를 식별한다.
5. `youtube_community_shorts_alarm_states.authorized_at` claim은 post 단위(방 무관) 잠금이라 방별 중복 발송을 막지 못하고, 방별 중복은 ledger·논리 그룹·SENDING 단일 owner·Iris `client_request_id`가 이미 막는다. 제거해도 외부 dedup 보장은 줄지 않는다(4절). `ClaimToken`·`Reused`·`claimDecisionCache`·`batchClaimResolver`가 함께 사라진다.
6. 논리 그룹(resolver)·ledger·commit adjudication·frozen request(`member_ids`·`message_hash`·`generation`)는 유지한다.
7. PR: 3c-1 행 fence(+`locked_by` migration, `PreparationLease` 삭제, 5-3 수정) → 3c-2 시간원 통일 → 3c-3 alarm-state claim 제거(코드) → 다음 릴리스 3c-4 `authorized_at` 열 삭제 migration.

---

## 1. 현재 구조 (file:line)

### 1.1 상태 어휘

| 대상 | 값 | 근거 |
|---|---|---|
| delivery 상태 | `PENDING`·`SENDING`·`SENT`·`FAILED`·`QUARANTINED` | `YD/lifecycle/types.go:14-20`, CHECK `chk_youtube_notification_delivery_status_vocab` (`hololive-dbtest/testdata/schema_snapshot.golden.sql:1448` 표) |
| ledger 상태 | `SENT`·`QUARANTINED` | `YD/lifecycle/types.go:34-37`, `chk_youtube_notification_delivery_ledger_status`·`_shape` (golden:1472 표) |
| claim 표현 | 상태가 아니라 `PENDING + locked_at`(+`row_version+1`) | `ST/queries/transition_claim_pending.sql:33-36` |
| 전송 중 | `SENDING + locked_at(=전송 시작)` | `ST/transition_begin.go:228-238` |
| alarm_states | `DETECTED`·`ENQUEUED`(=authorized_at 있음)·`SENT` | `hololive-shared/pkg/domain/youtube_content.go:155-170`, golden:1121 표 |

### 1.2 행 컬럼과 claim

- `youtube_notification_delivery` 컬럼: `status, attempt_count, next_attempt_at, locked_at, sent_at, error, row_version, send_request_id` (golden:1448-1470). `locked_by`·`lock_expires_at` 없음.
- 인덱스: `idx_ynd_pending_due_created_id (next_attempt_at, created_at, id) WHERE PENDING`, `idx_ynd_sending_stale (locked_at, id) WHERE SENDING`.
- ClaimPending: Go 시각 `claimedAt`(`ST/transition_store.go:73`), `lockCutoff = claimedAt - LockTimeout`(`:78`)를 DB `locked_at`과 비교(`ST/queries/delivery_ready_predicate.sql:3`, `transition_claim_pending.sql:28`), `SET locked_at = $6(Go 시각), row_version+1`(`transition_claim_pending.sql:35-36`). 상태는 PENDING 그대로.
- frozen 그룹: claim은 send unit 전원이 ready일 때만(`delivery_ready_predicate.sql:5-12`), request 행 `FOR UPDATE SKIP LOCKED`(`transition_claim_pending.sql:16-21`).

### 1.3 논리 그룹·owner·ledger

- 논리 키 `(kind, logical_id, room_id)`: `ytcontentid.ResolveDeliveryKey` (`ST/transition_types.go:182-189`).
- 그룹 행 로드: 요청 키 후보로 delivery를 `FOR UPDATE`(`ST/queries/transition_group_rows.sql:45-50`), ledger도 `FOR UPDATE`(`ST/queries/transition_ledger_rows.sql:20`).
- resolver: ledger 우선, 불변식(SENDING 다중·격리+전송 혼재 등) breach, owner = `(created_at,id)` 최소 PENDING/FAILED (`YD/preparation/resolver.go:382-412, 424-455, 478-495, 543-549`).
- ledger 기록: CompleteSent가 같은 트랜잭션에서 `SENT` 기록(`ST/transition_complete.go:174-184`), stale sweep이 `QUARANTINED` 기록(`ST/transition_quarantine.go`).

### 1.4 요청 고정(send_request)

- `youtube_notification_send_request(base_id PK, room_id, message, message_hash, route, dedupe_keys, member_ids, generation 0..2)` (golden:1547).
- `base_id = "hololive-outbox:" + sha256(room, dedupe_keys)[:16]` (`YD/send_engine_flow.go:182-193`, `YD/send_engine_request.go:49-56`).
- FreezeRequest: 멤버 `FOR UPDATE` 후 호출자 버전과 비교(`ST/send_request.go:194`), 최초면 insert+bind(`:113-125`), 이미 고정이면 저장본 반환·멤버 배열 등가 검사(`:146-157`), 본문 해시 재검증(`:51-55`, `:314-331`).
- 세대: pre-handoff `CLIENT_REQUEST_ID_FAILED`만 `generation+1`(`ST/queries/send_request_advance.sql`, `YD/send_engine_request.go:69-89`), owner fence는 `status='SENDING' AND row_version AND send_request_id`(`ST/queries/send_request_fence.sql`).
- grouped fallback: singleton 요청 고정 시 owner fence `SENDING + row_version`(`ST/queries/send_request_fallback_fence.sql`, `ST/send_request_fallback.go:60`).

### 1.5 commit 응답 adjudication

- `executeTx`가 COMMIT 오류를 `commitResponseError`로 감싼다(`ST/transition_store.go:575-605`).
- 실패 시 primary read-back으로 pre/post/mixed/missing/conflict 판정(`ST/transition_begin.go:263-320`), post-state 비교는 모든 필드 등가(`sameTransitionState`, `ST/transition_begin.go:342-352`: status·attempt·row_version·locked_at·sent_at·next_attempt_at·error).
- 판정 → `ApplyOutcome`·`CommitAdjudication`(`ST/transition_adjudication.go:17-114`, `ST/transition_types.go:21-62`). pre면 1회 재시도(`transitionOperationRetryLimit=1`, `ST/transition_store.go:24`).
- CompleteSent·Quarantine은 post-state 확인 뒤 ledger read-back(`ST/transition_complete.go:327-356`, `ST/transition_quarantine.go:78-92`).

### 1.6 발송 흐름

`processPendingDeliveriesWithLifecycle`(`YD/claim_manager_pipeline.go:126-189`): ClaimPending → PrepareClaimed(ActiveRows = 호출자 claim 행, `ST/transition_prepare.go:51-55, 275-293`) → `dispatchDeliveryRows`(`YD/send_engine_send.go:39-95`) → 그룹별 `selectClaimedDeliveries`(alarm-state claim) → FreezeRequest → BeginSending → provider → CompleteSent/실패 전이. 라운드는 한 프로세스 안에서 동기 순차(`YD/dispatcher.go:200-224`), 배포는 compose 단일 인스턴스(`docs/current/runbooks/alarm-worker.md:6`, 계약 `docs/current/architecture/youtube-egress-lifecycle-contract-20260831.md:420` replica=1).

---

## 2. 제거 대상 전수

### 2.1 `PreparationLease`

- 정의: `YD/lifecycle/types.go:89-128` (`PreparationLease`, `NewPreparationLease`, 접근자, `validateFence`).
- 생성: `ST/transition_types.go:191-217` `transitionRow.snapshot(current)` — `current`면 재조회 행 `r`의 `ID·RowVersion·LockedAt`로 lease 생성(`:203`), 같은 `r`의 값으로 `DeliverySnapshot` 필드를 채움(`:209-215`).
- 검증: `YD/preparation/resolver.go:44-48`(InCurrentBatch면 검사), `:81-96` `validateCurrentLease` — lease 필드와 snapshot 필드 등가(`:86`) + `Status == PENDING`(`:90`).
- 기타: `resolver.go:32`(필드), `:361-380`(`mergeSameSnapshot`이 lease 무시·복사), 시험 `YD/preparation/resolver_test.go:257-262`.
- 호출처: `snapshot(current)`은 `ST/transition_store.go:260`(resolveClaimedGroups 경유: PrepareClaimed `transition_prepare.go:75`, BeginSending `transition_begin.go:105`, ApplyPreparedFailure `transition_failure.go:125`)와 `ST/transition_quarantine.go:435`(`current=false`, lease 없음).
- **동어반복 검증**: `:86` 비교의 양변은 모두 같은 재조회 행 `r`에서 왔다. `NewPreparationLease`는 `locked_at`을 `CanonicalTime`(UTC·µs 절삭)으로, snapshot은 `r.LockedAt.UTC()`로 넣지만 DB 값은 이미 µs라 항상 같다. 실효 검사는 (a) 재조회 행이 `locked_at` 있음·`row_version>0`(`validateFence`, 실패 시 resolveClaimedGroups 오류), (b) `PENDING`뿐이다. 둘 다 **호출자의 claim**과 비교하지 않으므로 다른 실행이 다시 claim한 행도 통과한다. 계약 문서는 BeginSending이 "exact PENDING + PreparationLease"를 요구한다고 적었지만(`youtube-egress-lifecycle-contract-20260831.md:474-489`, SQL 예시 `:482-484` `row_version = $expected_version`) 구현은 재조회 값을 expected로 쓴다.
- 보호하던 불변식: "provider 호출 권한은 claim 소유자에게만"(I-007, 계약 `:226-228`). 현재 실제로는 status 검사와 단일 SENDING 불변식(resolver `:442-446`)이 중복 발송만 막는다.
- 대체: 호출자 claim fence `(ID, row_version, locked_by)`와 재조회 행 비교(3.3절). `PreparationLease`·`Lease` 필드·`validateCurrentLease`·`validateFence` 전부 삭제. `RowMutation`의 미사용 접근자 `ExpectedStatus/ExpectedVersion/ExpectedAttempt/ExpectedLockedAt`(`YD/lifecycle/policy.go:103-106`, 저장소 전체에서 호출 없음)와 `expectedLockedAt` 필드(`:94,237,352`)도 같은 PR에서 삭제.

### 2.2 3중 CAS

- SQL: `ST/queries/transition_apply_rows.sql:51-55` — `status AND row_version AND attempt_count AND locked_at IS NOT DISTINCT FROM`.
- Go 입력: `transitionVectors`(`ST/transition_store.go:429-442`), `appendTransition`(`:521-534`), rows-affected 검사 `equalSortedIDs`(`:480-485`, 불일치 → 비공개 `transitionConflictError`).
- 호출처(`applyRowTransitions`): PrepareClaimed(`transition_prepare.go`), DeferFollower(`transition_prepare.go:413-447`), BeginSending(`transition_begin.go`), CompleteSent(`transition_complete.go:164`), 실패 전이(`transition_failure.go` applyFailureTransitions), AdvanceRequestGeneration(`send_request.go:262`), Quarantine, ExpirePending(`transition_expiry.go:72`), Revive.
- 함의 증명: 운영 SQL 중 delivery를 UPDATE하는 것은 `transition_claim_pending.sql`(버전+1), `transition_apply_rows.sql`(next_version=before+1 — `transition_prepare.go:298`, `transition_begin.go:231`, `transition_complete.go:118`, `lifecycle/policy.go:238,353` 등 모든 after가 `RowVersion++`), `send_request_bind.sql`·`send_request_fallback_bind.sql`(버전을 올리지 않지만 `send_request_id`만 바꿈) 뿐이다(`rg "UPDATE youtube_notification_delivery"` 결과, 시험 제외). 따라서 `row_version` 같음 ⇒ `status·attempt_count·locked_at` 같음. `attempt_count`·`locked_at` 조건은 정보가 없다. 계약 문서도 "모든 physical row mutation은 version을 1 증가"라고 명시(`contract:194`).
- 대체: `status = expected AND row_version = expected AND locked_by IS NOT DISTINCT FROM expected_locked_by`.

### 2.3 `ClaimToken.Reused`

- 정의: `hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate/state.go:29-35`.
- 설정: `YD/claim_manager_gate.go:220`(`claimDecisionCache` hit면 true, `YD/claim_decision_cache.go:28-60`).
- 사용: `YD/batch_claim_resolver.go:48`(Reused 아닌 token만 해제 권한), `YD/claim_manager_release.go:173`(Reused면 해제 생략).
- 역할: 한 배치 안에서 같은 post를 여러 방이 공유할 때, 최초 claim 소유자만 해제하고 나머지는 "완료 증명"(CompleteSent의 exact token 검사)만 쓰게 한다. `authorized_at` post claim이 delivery lease와 겹쳐서 생긴 장치다(계획 2.3 표).
- 대체: 없음. alarm-state claim(2.4)과 함께 삭제.

### 2.4 `youtube_community_shorts_alarm_states.authorized_at` claim (이중 lease)

| 구성 | 위치 |
|---|---|
| 선점 | `YD/claim_manager_acquire.go:18-65`(Go 시각 `claimAt` `:29`), `:209-243`; SQL `OBS/queries/alarm_state_repository_claim_0044_01.sql:5-18`(`WHERE authorized_at IS NULL AND alarm_sent_at IS NULL`); `OBS/alarm_state_repository_claim.go:29-93` |
| stale 판정 | `YD/claim_manager_acquire.go:166-207`(Go `claimAt - min(LockTimeout, 2m)`과 DB `authorized_at` 비교), `YD/claim_manager_release.go:196-207`, `YD/claim_manager_gate.go:18` |
| 확인 | `YD/claim_manager_finalize.go:18-84`(재조회 후 `AuthorizedAt == claimAt`) |
| 해제 | `OBS/queries/alarm_state_repository_claim_0109_02.sql`, `OBS/alarm_state_repository_claim.go:95-117`; `YD/claim_manager_release.go:162-194`; bool 반환 버림(`claim_manager_acquire.go:182`, `claim_manager_release.go:177`) |
| 배치 공유 | `YD/claim_decision_cache.go`(전부), `YD/batch_claim_resolver.go`(전부), `YD/send_engine_send.go:44-55` |
| defer | 다른 실행이 보유 → `DeferFollower`(Go `time.Now()+RetryBackoff`, `YD/claim_manager_gate.go:163-206`) |
| 완료 증명 | CompleteSent 전제 `validateTrackingRequirements`(`ST/transition_complete.go:159, 226-308`): community/shorts owner는 alarm_state·tracking SENT이거나 exact token(`authorized_at` 등가) 필요, 아니면 conflict |
| sent 기록 | `ST/delivery_repository_tracking.go:44-111`(token → `AlarmSentMark.AuthorizedAt`), `OBS/queries/repository_delivery_state_bulk_0011_01.sql:47-75`(`claimed_state_finalized`·`authorization_mismatches`), 불일치면 오류 `OBS/repository_delivery_state.go:67-69` |
| 결과 전달 | `dispatchstate.DispatchResult.SuccessClaimTokens`(`state.go:40`), `YD/metrics_recorder_per_room.go:91`, `YD/metrics_recorder_grouped.go:87`(외부 소비자 없음, `rg SuccessClaimTokens` 결과 생산만 있음) |
| 스키마 | 열 `authorized_at`, 인덱스 `idx_ycsas_authorized_at`(golden:1137), `delivery_status`의 `ENQUEUED` |
| API 쪽 | API는 `authorized_at=nil`로 upsert하고 SQL CASE가 기존 값을 보존(`OBS/alarm_state_repository_upsert.go:30-43`, 호출 `hololive-api/internal/youtube/canonicalwrite/writer.go:163-166`) |

보호하던 것과 실제 효과:
- post 단위(방 무관) 직렬화. 다른 실행이 보유하면 이 방도 defer한다. 방별 중복 방지는 하지 않는다(키에 room 없음).
- 결과 불명(outcome unknown)이면 해제하지 않으므로 같은 post의 다른 방 발송이 최대 2분(또는 LockTimeout) 지연된다(`claim_manager_gate.go:18`, `claim_manager_release.go:196-207`). 안전 이득 없는 방 간 결합이다.
- [INFERENCE] 다중 실행에서 발송 중 claim이 stale로 재선점되면 CompleteSent의 exact token 검사가 conflict를 내고(`transition_complete.go:282-297`) 행이 SENDING에 남아 stale sweep이 QUARANTINED로 굳힌다(실제 발송됐는데 격리). 현재 replica=1·동기 라운드라 도달 경로는 관측하지 못했다.

---

## 3. 4.1 튜플로의 매핑

### 3.1 매핑 표

| 4.1 항목 | youtube 목표 | 비고 |
|---|---|---|
| 행: status | 기존 `status` | |
| 행: locked_by | 새 열 `locked_by text NULL` | 소유자 = `leaseowner.InstanceID("youtube-dispatcher")`(`hololive-alarm-worker/internal/leaseowner/instance.go:12-27`, alarm·notification과 같은 생성기) |
| 행: 버전 | `row_version` | `attempt_count`는 claim에서 불변이라 불가 |
| 단위: 멤버 배열 등가 | `youtube_notification_send_request.member_ids` vs 호출자 멤버(`ST/send_request.go:146-157`, `YD/send_engine_request.go:154-166`) | 유지 |
| 단위: 요청 본문 해시 | `message_hash`(`ST/send_request.go:51-55, 314-331`) | 유지 |
| 단위: generation | `generation` + `send_request_advance.sql`(`generation = $2`) | 유지, owner fence에 `locked_by` 추가 |
| 시간 | 각 문장 clock CTE `clock_timestamp()` 한 곳 | 3.5절 |
| 미스 | `*sendfence.Miss`(S3A) | 3.4절 |

### 3.2 `locked_by` 도입 필요 여부 (갈림길)

- 선택지 A(권고): `locked_by` 열 추가. 안전성은 `row_version`이 단독으로 보장한다(claim마다 버전이 오르므로 claim 세대 = 버전). `locked_by`의 가치는 (1) 4.1 결정 튜플과 sendfence `Observation.OwnerMatches`의 정합, (2) BeginSending post-state를 소유자 고유로 만들어 3.6절 adjudication 축소를 안전하게 함, (3) 운영 진단(누가 잡았는지). 비용: nullable 열 추가 1개(테이블 재작성 없음).
- 선택지 B: 버전만. migration은 없으나 공통 기반과 튜플이 어긋나고 adjudication에서 소유자 구분 근거가 없다.
- 주의: 컨테이너 이름 고정(`runbooks/alarm-worker.md:6`)이라 재시작 뒤 hostname·pid가 같아 `locked_by`가 이전 프로세스와 같을 수 있다. 버전이 구분하므로 안전 문제는 없고, `locked_by`만으로 fence하지 않는다는 점을 문서에 남긴다.
- `lock_expires_at`은 추가하지 않는다. 만료는 `locked_at + lease_ms`를 clock CTE와 비교한다. 이유: 구 워커가 남긴 PENDING/SENDING 행(`locked_at`만 있음)을 compat 분기 없이 그대로 처리할 수 있고(새 열 backfill에 필요한 LockTimeout은 stack-secrets 프로필 값이라 migration이 알 수 없다), 기존 `idx_ynd_sending_stale (locked_at, id)`를 그대로 쓴다. S3A가 `locked_at+lease_ms` 방식을 `LeaseLive` 계산으로 허용했다.

### 3.3 claim fence 비교 (결함 5-3 + PreparationLease 대체)

`resolveClaimedGroups`(`ST/transition_store.go:139-196`)에 호출자 행과 재조회 행 비교를 넣는다. 현재 `validateClaimedRowsPresent`(`:268-283`)를 대체한다.

```text
for each claimed row c (호출자 값):
  r := rowsByID[c.ID]            // 같은 트랜잭션의 FOR UPDATE 재조회
  missing          : r 없음
  version_changed  : r.row_version != c.RowVersion
  owner_changed    : r.locked_by  != c.LockedBy
  status_changed   : r.status     != PENDING
→ 하나라도 있으면 *sendfence.Miss{Action, Rows}를 반환, 아무 행도 바꾸지 않는다.
```

- 적용: PrepareClaimed, BeginSending, ApplyPreparedFailure(세 곳 모두 이 함수를 거친다). BeginSending의 `ActiveRows`는 PrepareClaimed가 돌려준 호출자 claim 행이고(`transition_prepare.go:51-55`), PrepareClaimed는 active owner 행을 바꾸지 않으며 `send_request_bind.sql`도 버전을 올리지 않으므로 BeginSending 시점에도 claim 버전이 유효하다.
- BeginSending 추가 조건: claim lease가 살아 있어야 한다(`locked_at + lease_ms > clock`, Miss reason `lease_expired`). alarm의 MarkSending(`DO/queries/repository_transitions_0020_01.sql:21`)과 같은 의미로, 정지했던 소유자가 lease 밖에서 외부 부수효과를 시작하지 않게 한다. 정산(CompleteSent·발송 뒤 실패)은 lease를 보지 않는다(외부 성공을 DB 정산 실패로 되돌리지 않는다, 계획 3절).
- DeferFollower(`transition_prepare.go:413-447`)·FreezeRequest(`send_request.go:181-201`, `send_request_lock.sql`에 `locked_by` 추가)·AdvanceRequestGeneration(`send_request_fence.sql`)·FreezeFallbackRequests(`send_request_fallback_fence.sql`)는 이미 호출자 버전을 쓴다. `locked_by` 비교만 더한다. 이로써 "같은 토큰의 계약이 경로마다 다르다"(결함 5-3 서술)가 하나로 정리된다.
- 만료 권한자(ClaimPending 재선점, QuarantineStale, ExpirePending, Revive)는 소유자 토큰 없이 clock 기준 만료 행만 `FOR UPDATE SKIP LOCKED`로 잡고 재조회 값으로 CAS한다. 현재와 같다.
- 단일 replica·동기 라운드에서는 stale 소유자가 동시에 존재하지 않으므로 5-3은 현재 배치에서 [INFERENCE] 도달하지 않는다. 배포 겹침·다중 replica·장시간 정지에서 의미가 있고, 계약(`contract:474-489`)과 구현을 일치시키는 수정이다.

### 3.4 미스 처리

- `applyRowTransitions`의 rows-affected 불일치(`ST/transition_store.go:480-485`)를 `*sendfence.Miss`로 바꾼다. 전이 SQL이 잠근 행의 관측값을 RETURNING하고 `sendfence.Check`가 분류한다(S3A 규약, 왕복 추가 없음).
- 기존 `transitionConflictError`는 fence가 아닌 해석 충돌(resolution이 Active가 아님 등, `transition_begin.go:166-186`)에만 남긴다. `ApplyConflict` 결과는 유지하되 fence 미스는 항상 `error`로도 반환해 호출자가 버릴 수 없게 한다.
- 버려지던 반환값 정리: `YD/send_engine_request.go:208-215` `deferIncompleteFrozenGroup`이 `ApplyConflict`를 무시한다 → `observeLifecycleApply("defer_follower", …)`와 로그. alarm-state 해제의 bool 무시(2.4)는 3c-3에서 코드째 사라진다.

### 3.5 시간원 통일 (Go 시각 ↔ DB 시각 비교 제거)

현재 Go 시각이 DB 시각과 비교되거나 DB에 기록되는 곳:

| 위치 | 현재 | 변경 |
|---|---|---|
| ClaimPending `ST/transition_store.go:73-90`, SQL `transition_claim_pending.sql:28-30,35`, `delivery_ready_predicate.sql:3-4,10-11` | Go now, cutoff를 `$2,$3,$4`로 전달, `locked_at=$6` | `clock AS MATERIALIZED (SELECT clock_timestamp() AS at)`; 조건 `locked_at IS NULL OR locked_at <= (SELECT at FROM clock) - lease`, `next_attempt_at <= (SELECT at FROM clock)`, `created_at >= (SELECT at FROM clock) - freshness`; `SET locked_at = clock.at, locked_by = $owner`. 인자는 `lease_ms`·`freshness_ms`. 스칼라 서브쿼리(InitPlan)로 써야 `idx_ynd_pending_due_created_id` 조건으로 쓰인다(volatile 함수 직접 사용 금지) |
| ReadySnapshot `ST/ready_snapshot.go:15-22`, `delivery_ready_snapshot.sql` | 같은 predicate에 Go 시각 | predicate 공유 유지, 같은 clock CTE |
| QuarantineStale `ST/transition_quarantine.go:29-34, 271`, `transition_stale_sending.sql:23` | Go cutoff | 후보 SQL `locked_at < clock.at - lease`; 문장이 `checked_at`(clock.at)을 RETURNING, Go의 재확인(`:271`)은 이 값 사용 |
| ExpirePending `ST/transition_expiry.go:34, 53, 138, 149`, `transition_expired_pending.sql:21-22` | Go `at` | 동일 방식(`checked_at` 반환) |
| Revive `ST/transition_revive.go:37, 112, 296, 307`, `transition_revive_candidates.sql:21-22`, `lifecycle.EvaluateRevive`의 신선도 비교(`YD/lifecycle/policy.go:291`) | Go `at` | 후보 SQL이 `checked_at` 반환, EvaluateRevive에 그 값 전달 |
| 전이 시각 `locked_at`·`sent_at`·`next_attempt_at` (`sendingTransitionRow` `transition_begin.go:228-238`, `completeSentTransitions` `transition_complete.go:99-150`, `deferTransition` `transition_prepare.go:295-306`, `EvaluateFailure` `policy.go:175-187`, revive `policy.go:349-355`) | Go 계산 절대 시각 | apply SQL이 clock CTE로 계산: `lock_action ∈ {set, clear, keep}`, `sent_action ∈ {set, clear, keep}`, `due_mode ∈ {keep, delay}`, `next_attempt_at = GREATEST(clock.at + delay_ms, COALESCE(due_floor, '-infinity'))`. floor는 DB 유래 값만(owner `next_attempt_at`, in-flight `locked_at + lease`) |
| DeferFollower 호출자 `YD/claim_manager_gate.go:190`, `YD/send_engine_request.go:211` | `time.Now()+RetryBackoff` | `DeferCommand{Delivery, Delay}`로 바꾸고 SQL이 계산 |
| CompleteSent 이후 ledger·tracking·telemetry `transition_complete.go:174-184, 199-223` | Go `sentAt` | apply 문장이 RETURNING한 `sent_at`을 같은 트랜잭션의 ledger·`MarkAlarmSentBatch`·telemetry에 전달(DB 유래 값) |
| fanout 임대 `ST/transition_fanout.go:42-48, 81, 186, 220`, `fanout_claim.sql:37-38,50`, `fanout_revive_failed.sql:7`, `fanout_cleanup_expired.sql:77`, `YD/claim_manager_release.go:85-87` | Go 시각 | 같은 clock CTE 규약. outbox 쪽 fence(`locked_at` 등가, `fanout_*`)는 claim이 DB 시각을 넣으므로 DB 생성 토큰이 된다. `locked_by`·버전 도입은 하지 않는다(outbox 테이블 migration이 필요하고 확인된 결함이 없다) |

그대로 두는 것(명시적 비범위): 보존 삭제 cutoff(`CleanupTerminalOutboxes` `ST/transition_cleanup.go:50-110`, `CleanupOrphanRequests` `ST/send_request.go:300-311`). 상태 전이·소유권 판정이 아니라 일 단위 보존 창이다. telemetry 시도 시각(`recordAttemptTelemetry`)은 기록값이라 apply가 돌려준 DB 시각을 쓰되 정책 판단에 쓰지 않는다.

`lifecycle` 정책 변경: `EvaluateFailure`는 절대 due 대신 `Delay`(retry: `max(backoff, retryAfter)`)와 terminal의 `keep`을 돌려준다(`policy.go:171-201`). `RowMutation.nextAttemptAt` → `due DueSpec{Mode, Delay, Floor}`. 정책 판단(소진·permanent·follower mirror)은 Go에 남는다(계획 1절).

CLOCK_QUERY_POLICIES(`scripts/architecture/check-sql-ownership.py:54-65`)에 `YOUTUBE_DELIVERY_QUERIES = "hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/queries"`를 추가하고 `transition_claim_pending.sql`, `transition_apply_rows.sql`, `transition_stale_sending.sql`, `transition_expired_pending.sql`, `transition_revive_candidates.sql`, `delivery_ready_snapshot.sql`, `fanout_claim.sql`, `fanout_revive_failed.sql`, `fanout_cleanup_expired.sql`를 `clock_timestamp`로 등록한다(경로 glob 하나 `…/store/queries/*.sql`이면 cleanup SQL에 시계 함수가 없어 영향 없음 — 구현 시 확인).

### 3.6 commit adjudication 유지 방법

- 유지: `executeTx`의 `commitResponseError`, pre/post/mixed/missing/conflict 분류, 1회 재시도, ledger read-back.
- 변경: `sameTransitionState`(`ST/transition_begin.go:342-352`)를 `ID, OutboxID, Status, AttemptCount, RowVersion, LockedBy, Error`로 줄인다. 시각은 SQL이 만들므로 Go가 기대값을 모른다.
- 안전 근거: post-state의 `row_version = before+1`은 같은 pre-version에서 CAS에 이긴 한 전이만 만들 수 있다. 다른 행위자가 같은 pre-version에서 같은 모양(상태·시도·소유자·오류)의 전이에 이긴 경우만 구분하지 못하는데, 그 경우는 같은 의미의 전이(예: 두 defer)라 `Applied` 판정이 결과를 바꾸지 않는다. provider 권한을 만드는 BeginSending post-state는 `locked_by = 나`를 포함하므로 소유자 고유다.
- 재시도 경로: 시각을 SQL이 만들므로 pre 판정 뒤 재시도에서 같은 `transitions`(기대 pre-state·delay·action)를 그대로 다시 보낸다. 현재처럼 시도마다 다시 만들 필요가 없다.

### 3.7 유지 대상

| 대상 | 이유 | 위치 |
|---|---|---|
| 논리 그룹·resolver | 방별 같은 논리 알림의 중복 방지·owner 단일화 | `YD/preparation/resolver.go` |
| ledger | 물리 행 삭제 뒤에도 종단 증거 유지 | `ST/delivery_ledger.go`, `ledger_*` SQL |
| commit adjudication | DB commit 응답 유실을 rollback으로 오판하지 않음 | 3.6절 |
| frozen request | 재렌더링 금지·멤버 고정·세대 상한 | `ST/send_request*.go` |
| grouped fallback | 계약상 예외 경로(`contract:517-536`) | `YD/send_engine_send.go:375-491` |

---

## 4. `alarm_states.authorized_at` 제거와 외부 dedup 논증

### 4.1 외부 dedup이 유지되는 이유

외부 중복 발송은 "같은 방에 같은 논리 알림이 두 번"이다. 이를 막는 장치는 모두 방 단위이고 `authorized_at`과 무관하다.

1. ledger PK `(kind, logical_id, room_id)`(golden:1472) + 그룹 `FOR UPDATE`(`transition_group_rows.sql:50`, `transition_ledger_rows.sql:20`) + resolver의 SENT ledger 우선(`resolver.go:392-394`)과 단일 SENDING 불변식(`:442-446`) → 한 논리 키에 provider 권한은 한 번에 하나.
2. BeginSending이 owner를 PENDING→SENDING으로 CAS(3.3절) → 같은 행의 두 번째 발송 권한 없음. 결과 불명은 SENDING에 남아 stale sweep이 QUARANTINED로 끝내고 재발송하지 않음(I-008, `contract:230-232`).
3. Iris admission 멱등: 고정된 `client_request_id = base_id(:rN)`(`YD/send_engine_flow.go:182-193`, `ST/send_request.go:31-43`). Iris는 같은 ID 재제출을 `CLIENT_REQUEST_ID_ALREADY_EXISTS`/`PAYLOAD_MISMATCH`/`OUTCOME_UNKNOWN` 409로 거절하고, 재발급은 handoff 이전 실패가 확정된 `CLIENT_REQUEST_ID_FAILED`에서만 허용된다(`iris-client-go/README.md:106-111`). 세대 상한 2는 CHECK(golden:1547 표 `generation <= 2`)와 `send_request_advance.sql`이 강제.
4. `authorized_at`은 `(kind, post_id)` 키라 방을 구분하지 못한다(golden:1121, PK `(kind, post_id)`). 방 A 발송이 방 B 발송의 중복이 아니므로 이 claim은 dedup 장치가 아니라 post 단위 직렬화다. `AlreadySent` 판정도 "다른 방 발송일 수 있다"며 토큰 없이 진행한다(`YD/claim_manager_gate.go:153-157`).
5. post 단위 "알람 발송됨" 기록(`alarm_sent_at`)은 claim 없이도 `MarkAlarmSentBatch`의 `existing_state_updated`·`missing_state_inserted` 경로가 가장 이른 시각으로 단조 기록한다(`OBS/queries/repository_delivery_state_bulk_0011_01.sql:94-165`). tracking 행은 outbox와 같은 API 트랜잭션에서 만들어진다(`hololive-api/internal/youtube/canonicalwrite/writer.go:142-173`).

잃는 것: 다른 실행이 같은 post를 다른 방에 동시에 보내지 못하게 하던 post 단위 직렬화. 방이 다르면 별개 메시지이므로 이는 안전 요건이 아니다. 결과 불명 뒤 다른 방이 최대 2분 지연되던 결합(2.4)은 사라진다(동작 변화, 3c-3 시험으로 고정).

### 4.2 CompleteSent 전제 변경

- `validateTrackingRequirements`의 사전 검사(`ST/transition_complete.go:159`)를 삭제한다. 첫 발송은 항상 sent mark가 없고 토큰도 없으므로 남기면 모든 community/shorts 성공이 conflict가 된다.
- post-state 확인(`validateSentPostState` `:327-356`)의 tracking 확인은 유지하되 토큰 인자 없이 "alarm_state 또는 tracking이 SENT"만 본다(같은 트랜잭션이 기록했어야 하는 결과의 read-back).

### 4.3 컬럼·테이블 처리와 rolling 호환

- 테이블 `youtube_community_shorts_alarm_states`는 유지한다(`alarm_sent_at`·감지 시각 기록, API upsert 소유). 테이블 자체 폐지는 범위 밖(open question).
- 3c-3(릴리스 N, 코드만):
  - 워커: claim 코드 삭제(5절 목록). `MarkAlarmSentBatch`에 `AuthorizedAt` 없이 호출.
  - shared(`OBS`): `TryClaimAlarmState`·`ReleaseAlarmStateClaim`·`alarmStateClaimMatches`·`normalizeAlarmStateClaim`·claim SQL 2개 삭제. `AlarmSentMark.AuthorizedAt`(`OBS/repository.go:65`)과 bulk SQL의 `authorized_at` 입력·`claimed_state_finalized`·`authorization_mismatches` CTE 삭제, `existing_state_updated`는 `authorized_at = NULL`을 계속 써서 구 워커가 남긴 ENQUEUED 행을 정리(열 삭제 전까지). upsert(`OBS/alarm_state_repository_upsert.go:30-43, 88, 126-138, 168-181, 219-221`)는 insert 열 목록에서 `authorized_at`을 빼고 `delivery_status`를 `alarm_sent_at`만으로 계산. `domain.ResolveYouTubeCommunityShortsAlarmStateStatus`를 `alarmSentAt` 단일 인자로, `YouTubeCommunityShortsAlarmStateStatusEnqueued`·`AuthorizedAt` 필드(`hololive-shared/pkg/domain/youtube_content.go:141,156`) 삭제. API(`canonicalwrite/alarm_state.go:104`) 호출 갱신.
  - API·워커가 shared를 같이 쓰므로 같은 릴리스에서 둘 다 배포. migration 없음. 구 워커가 잠시 남아도 열이 있으므로 깨지지 않는다.
- 3c-4(릴리스 N+1, 3c-3 배포 확인 뒤): migration `NNN_drop_youtube_alarm_state_authorization.sql`(NNN = 작성 시점 `max+1`; 현재 main·apsm worktree 최대 324라 325 이상, 다른 단계가 먼저 번호를 쓸 수 있음). 256(`hololive-api/scripts/migrations/256_drop_youtube_delivery_request_snapshot_allowed.sql`)·286 선례 형식:
  ```sql
  -- 3c-3 이후 authorized_at을 읽거나 쓰는 코드가 없다. 남은 미발송 ENQUEUED 행을 DETECTED로 되돌리고 열을 지운다.
  BEGIN;
  SET LOCAL lock_timeout = '3s';
  SET LOCAL statement_timeout = '30s';
  UPDATE public.youtube_community_shorts_alarm_states
  SET authorized_at = NULL, delivery_status = 'DETECTED', updated_at = clock_timestamp()
  WHERE alarm_sent_at IS NULL AND (authorized_at IS NOT NULL OR delivery_status = 'ENQUEUED');
  ALTER TABLE public.youtube_community_shorts_alarm_states
      DROP CONSTRAINT IF EXISTS chk_youtube_community_shorts_alarm_states_delivery_status_vocab;
  ALTER TABLE public.youtube_community_shorts_alarm_states
      ADD CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab
      CHECK (delivery_status = ANY (ARRAY['DETECTED'::text, 'SENT'::text])) NOT VALID;
  COMMIT;
  ALTER TABLE public.youtube_community_shorts_alarm_states
      VALIDATE CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab;
  DROP INDEX CONCURRENTLY IF EXISTS public.idx_ycsas_authorized_at;
  ALTER TABLE public.youtube_community_shorts_alarm_states DROP COLUMN IF EXISTS authorized_at;
  ```
  재실행 멱등: UPDATE는 0행, DROP CONSTRAINT/ADD는 같은 결과, VALIDATE 재실행 무해, `IF EXISTS`. CONVENTIONS 규칙(`CONVENTIONS.md:15-24, 61-64`): CONCURRENTLY는 블록 밖, VALIDATE는 블록 밖. manifest.txt에 추가, `schema_snapshot.golden.sql` 재생성.
- 배포 순서: migrate가 앱보다 먼저 돈다(`docs/current/DEPLOYMENT_BASELINE.md:128`). 그래서 열 삭제를 코드 변경과 같은 릴리스에 두면 migrate~재시작 사이 구 바이너리가 깨진다 → 반드시 다음 릴리스. 3c-1의 `ADD COLUMN locked_by`는 구 바이너리가 열 목록을 명시하므로(`transition_claim_pending.sql:40-50` 등) 같은 릴리스에서 안전.

---

## 5. 공통 기반(S3A `sendfence`)과의 관계

### 5.1 alarm-dispatch에서 재사용 가능한 것

| 대상 | 재사용 | 근거 |
|---|---|---|
| 소유자 ID 생성 `leaseowner.InstanceID` | 그대로 사용 | `hololive-alarm-worker/internal/leaseowner/instance.go:12-27`, alarm `DO/consumer.go:95`, notification `egress/notificationdelivery/dispatcher.go:140` |
| 잠금 뒤 clock CTE SQL 규약 | 패턴 재사용 | `DO/queries/repository_transitions_0020_01.sql:1-21`, `DO/queries/send_request_members.sql:1-16` |
| typed 미스·관측 분류 | `sendfence.Miss`·`Check` | S3A 초안 |
| 멤버 배열 등가 | `slices.Equal(slices.Sorted…)` 형태 동일(`DO/send_request.go:173-175`) | youtube는 이미 `uniqueSortedInt64s`+`slices.Equal`(`ST/send_request.go:152`) — 공용 helper가 생기면 교체 |
| 세대 재발급 규칙 | `iris.ReissuedClientRequestID`·`ReplyReissueMaxGenerations` 이미 공용 SDK | `ST/send_request.go:36-43, 222` |

### 5.2 재사용 불가(테이블 고유로 남김)

- claim SQL: alarm은 상태 `leased`·`lock_expires_at` 초 단위·send unit 테이블 잠금(`DO/queries/repository_claim_0053_02.sql:1-62`). youtube는 PENDING 유지·`row_version`·논리 그룹·frozen request 잠금이 다르다.
- 버전 축: alarm·notification은 `attempt_count`, youtube는 `row_version`.
- 논리 그룹 resolver·ledger·adjudication: youtube 고유.
- 요청 저장소: alarm은 `alarm_dispatch_send_units` 열, youtube는 별도 테이블. 테이블별 SQL·runner 공유 금지(S3A 규약, 계획 1절 범용 엔진 금지).

### 5.3 youtube가 기반에 요구한 인터페이스 (S3A 수용 회신 받음)

1. `sendfence.Observation.VersionMatches`는 열 무관(youtube=`row_version`).
2. `LeaseLive`는 테이블 SQL이 계산(`locked_at + lease_ms > clock`).
3. `next_attempt_at` 규약에 `GREATEST(clock + delay_ms, floor)` 허용, floor는 DB 유래 timestamptz만, `keep` 모드 허용.
4. post-state adjudication은 youtube 고유로 두고 sendfence는 관여하지 않음.
5. Miss reason 집합 `owner_changed|status_changed|version_changed|lease_expired|membership_changed|request_changed|missing`과 `UnappliedDeliveryIDs()` 사용.

의존: 3c-1은 `sendfence` 패키지(S3A의 3b PR)가 머지된 뒤 시작한다. 패키지 경로·이름이 바뀌면 3c-1에서 따른다.

---

## 6. 구현 PR 분할

공통: 모듈 `hololive/hololive-alarm-worker`(3c-3은 `hololive-shared`·`hololive-api` 포함). 새 주석 한국어. 각 PR은 해당 계약 문서(`docs/current/architecture/youtube-egress-lifecycle-contract-20260831.md`, `…-commit-adjudication-20260831.md`)를 같은 변경에서 고친다. 시험 기준선: `YD` 아래 시험 20,865줄(`find YD -name '*_test.go' | xargs cat | wc -l`, 계획 표의 20,929와 차이는 계수 범위 차이로 추정 [INFERENCE]).

### 3c-1 행 fence 튜플 + claim 버전 fence(5-3) + `PreparationLease`·3중 CAS 제거

변경:
- migration `NNN_youtube_delivery_locked_by.sql`: `ALTER TABLE public.youtube_notification_delivery ADD COLUMN IF NOT EXISTS locked_by text;` (`lock_timeout` 블록). manifest, golden 갱신.
- `hololive-shared/pkg/domain/youtube_delivery.go:25-37`: `LockedBy *string`(또는 string) 추가.
- `ST/queries/transition_claim_pending.sql`: `SET locked_by = $owner`, RETURNING `locked_by`. `ST/transition_store.go` `ClaimPending`에 owner 주입(`TransitionStore` 생성 시 `TransitionConfig.Owner`; `YD/construction.go:101-116`에서 `leaseowner.InstanceID("youtube-dispatcher")`). `scanTransitionDelivery`·`transitionRow`·`transition_group_rows.sql`·`transition_read_rows.sql`·`transition_stale_sending.sql`·`transition_expired_pending.sql`·`transition_revive_candidates.sql`·`send_request_lock.sql`에 `locked_by` 열.
- `ST/queries/transition_apply_rows.sql`: 입력에서 `expected_attempt`·`expected_locked_at` 삭제, `expected_locked_by`·`next_locked_by` 추가, WHERE `status AND row_version AND locked_by IS NOT DISTINCT FROM`. 잠금 CTE에서 관측값을 RETURNING해 `sendfence.Check`.
- `ST/transition_store.go`: `transitionVectors`·`appendTransition`·`checkedTransitionAttempt`(expected 부분) 정리, `validateClaimedRowsPresent` → claim fence 비교(3.3), rows-affected 미스 → `*sendfence.Miss`.
- `ST/transition_begin.go`: 3.3 비교 + lease live 조건, `sendingTransitionRow`가 `LockedBy = owner` 유지.
- `ST/transition_failure.go:118-148`·`ST/transition_prepare.go`(PrepareClaimed, DeferFollower)·`ST/send_request.go:181-201, 280-298`·`ST/send_request_fallback.go:60`·`ST/queries/send_request_fence.sql`·`send_request_fallback_fence.sql`: `locked_by` 비교.
- 삭제: `YD/lifecycle/types.go:89-128`(PreparationLease·NewPreparationLease·validateFence), `YD/preparation/resolver.go:32, 44-48, 81-96`(Lease·validateCurrentLease), `:365-376`의 lease 처리, `ST/transition_types.go:197-207`, `YD/lifecycle/policy.go:94, 103-106` 미사용 expected 접근자.
- `YD/send_engine_request.go:208-215`: defer 결과 관측.
시험:
- 새(먼저 작성해 실패 관찰): `ST` dbtest `TestBeginSendingRejectsStaleClaimVersion` — 실행 A claim → lease 경과 → B 재claim → A의 BeginSending은 `sendfence.Miss{version_changed}`, 행은 B 소유 PENDING 그대로, B는 성공. 수정 전에는 A가 SENDING을 얻어 실패해야 한다. `TestApplyPreparedFailureRejectsStaleClaim`, `TestPrepareClaimedRejectsForeignClaim`(같은 구성, 전이 0), `TestBeginSendingRejectsExpiredClaimLease`(재claim 없이 lease만 경과 → `lease_expired`, provider 미호출).
- 갱신: `YD/preparation/resolver_test.go:250-264`(lease helper 삭제), `YD/lifecycle/policy_test.go`(expected 접근자 사용 시), `ST/transition_store_test.go`·`ST/transition_telemetry_test.go`·`ST/send_request_test.go`·`ST/transition_status_literal_test.go`·`ST/claim_history_plan_test.go`(SQL 인자·열 변화), `YD/batch_claim_resolver_db_test.go:61`, `YD/claim_manager_gate_test.go:787`(직접 `locked_at` UPDATE 픽스처 — fence에 `locked_by` 반영), `YD/send_engine_lifecycle_test.go`, `YD/send_engine_settlement_test.go`(fake transition 시그니처).
삭제 대상 시험: lease 등가만 단언하는 시험이 있으면 삭제(현재 `resolver_test.go`의 helper만 해당).
검증:
```
cd /home/kapu/work/iris-stack/hololive-bot-apsm-<unit>
gofmt -l hololive/hololive-alarm-worker hololive/hololive-shared
(cd hololive/hololive-alarm-worker && go build ./... && go vet ./internal/egress/youtubedispatch/... && go test -race ./internal/egress/youtubedispatch/... ./internal/app/workerapp/...)
(cd hololive/hololive-shared && go build ./... && go test ./pkg/domain/...)
SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest && go test -run 'TestSchemaSnapshotGolden|TestMigrations' ./hololive/hololive-dbtest
python3 scripts/architecture/check-sql-ownership.py
scripts/architecture/check-migration-manifest.sh
```

### 3c-2 시간원 통일

변경: 3.5절 표 전부. `lifecycle.EvaluateFailure`/`EvaluateRevive`/`deferTransition`의 due를 `DueSpec`으로, apply SQL의 action 열, 후보 SQL의 `checked_at` RETURNING, `ReadySnapshot`, fanout SQL, `DeferCommand.Delay`, `sameTransitionState` 축소(3.6), CompleteSent가 RETURNING `sent_at`을 ledger·tracking·telemetry에 전달, CLOCK_QUERY_POLICIES 등록.
시험:
- 새: `TestClaimPendingLeaseUsesDatabaseClock`(DB 기준 `locked_at = clock - (lease-1s)`는 재claim 안 됨, `clock - lease - 1s`는 됨), `TestRetryDueFromDatabaseClockRespectsFloor`(retryAfter>backoff면 `next_attempt_at - checked_at = retryAfter`; follower defer는 `GREATEST(clock+backoff, owner due)`), `TestStaleSendingQuarantineUsesDatabaseClock`.
- 갱신: `YD/lifecycle/policy_test.go`(절대 due 단언 → DueSpec), `ST/transition_store_test.go:65-98`(`TestTransitionCompleteSentConfirmsLostCommitResponse` — 축소 비교로 계속 통과해야 함), `ST/ready_snapshot_test.go:59,76`, `ST/fanout_claim_execution_test.go`, `ST/claim_history_plan_test.go`·`ST/transition_group_rows_plan_test.go`(인덱스 사용 계획 시험이 clock InitPlan에서도 유지되는지가 핵심 회귀 신호), `YD/dispatcher_cleanup_orphan_pending_test.go`, `YD/claim_manager_revive_test.go`, `YD/dispatcher_restart_recovery_test.go`, `hololive-alarm-worker/internal/app/workerapp/worker_registry_youtube_test.go:46-104`.
검증: 3c-1과 같은 명령 + `go test -race ./internal/egress/youtubedispatch/...` 전체.

### 3c-3 alarm-state claim 제거 (`ClaimToken`·`Reused` 포함, 코드만)

삭제(전체 파일): `YD/claim_manager_acquire.go`, `YD/claim_manager_finalize.go`, `YD/claim_decision_cache.go`, `YD/batch_claim_resolver.go`, `OBS/queries/alarm_state_repository_claim_0044_01.sql`, `OBS/queries/alarm_state_repository_claim_0109_02.sql`.
삭제(부분): `YD/claim_manager_gate.go`(claim 결정·defer·`appendProceedingDeliveryClaim`; 논리 식별자 검증 `validateDeliveryLogicalIdentity`와 실패 시 prepared failure 경로만 남김 — `deliveryClaimSelection`은 `members`/`retryMembers`만), `YD/claim_manager_release.go:162-207`, `YD/claim_manager.go:19-22` `ClaimResolver.releaseDeliveryClaimsWithWarning`, `YD/send_engine_record.go:15-27`과 호출(`:35,46,55,65,75,101,112`), `YD/delivery_attempt.go:16,30,38,43,54,69`, `YD/metrics_recorder_per_room.go:75,91`, `YD/metrics_recorder_grouped.go:66,87`, `YD/send_engine.go:49`·`ST/transition_complete.go`의 `claimTokens` 인자, `ST/transition_complete.go:159, 226-308`(사전 검사), `ST/delivery_repository_tracking.go:44-171`의 token 처리, `dispatchstate/state.go:29-35,40`, `YD/send_engine_send.go:44-55`(batch resolver·cache), `OBS/alarm_state_repository_claim.go:29-117`, `OBS/repository.go:65, 114-130`, `OBS/repository_delivery_state.go:67-69, 109-165`, `OBS/repository_delivery_state_bulk.go:183,216,221-227`, `OBS/queries/repository_delivery_state_bulk_0011_01.sql`(3-20 입력열, 47-75 CTE, 113-118), `OBS/alarm_state_repository_upsert.go`(4.3절), `hololive-shared/pkg/domain/youtube_content.go:141,156,160-170`.
API: `hololive-api/internal/youtube/canonicalwrite/alarm_state.go:104` 호출 시그니처.
시험:
- 새: `TestOutcomeUnknownInOneRoomDoesNotDeferOtherRoomOfSamePost`(방 A 결과 불명 뒤 다음 라운드에서 같은 post의 방 B가 defer 없이 발송 — 기존에는 claim이 남아 defer됨, 먼저 작성해 실패 관찰), `TestCompleteSentMarksPostSentWithoutClaim`(community/shorts 첫 성공에서 tracking·alarm_state SENT, 가장 이른 시각 유지), shared `TestMarkAlarmSentBatchClearsLegacyAuthorization`(authorized_at이 남은 ENQUEUED 행이 SENT·NULL로 수렴; 3c-4에서 열과 함께 삭제).
- 삭제: `YD/claim_decision_cache_test.go`(전부), `YD/batch_claim_resolver_db_test.go`(전부), `YD/claim_manager_clock_test.go`(전부), `YD/claim_manager_gate_test.go`의 claim 시험 `:319-608, 890-1013`(`TestDispatchDeliveryRowsClaimsCommunityPostBeforeSending`, `…SkipsShortWhenAnotherExecutionOwnsRecentClaim`, `…ReleasesClaimAfterSendFailure`, `…ReclaimsStaleLegacyAuthorizationBeforeSending`, `…ConcurrentExecutionsStartCommunityShortsDeliveryOncePerPost`, `TestSelectClaimedDeliveriesTracksRowClaimOwnership`, `TestDispatchClaimedRowsIndividuallyReleasesOnlyOwnedClaimsOnFailure`, `…DefersPersistedRow`, `TestDeliveryClaimIdentityForOutboxRequiresCanonicalIdentity`). ledger 관련 `:617-889`는 유지·갱신. `OBS/alarm_state_repository_claim_returning_test.go`(전부), `OBS/repository_alarm_state_test.go`의 `:305, 433, 455, 498, 532, 550, 589, 665, 693` claim 시험.
- 갱신: `YD/send_engine_send_test.go:1745-1900`(claim spy → 해제 단언 제거, 결과 불명 hold 단언은 유지), `YD/send_engine_settlement_test.go:59, 208, 521`, `YD/construction_helpers_test.go:114-125`, `YD/dispatcher_retry_exact_once_integration_test.go:219, 380-470`(stale claim 픽스처 삭제), `YD/dispatcher_integration_test.go:712`, `YD/dispatcher_retry_selective_send_integration_test.go:179`, `YD/dispatcher_recovery_fixture_test.go:273-302`, `YD/delivery_test_db_test.go:655,754,770`·`YD/dispatcher_integration_db_test.go:293,342`(INSERT 열 목록에서 `authorized_at` 제거), `OBS/alarm_state_repository_test.go:36-140`·`OBS/repository_alarm_state_test.go:192-338`, `hololive-api/internal/youtube/canonicalwrite/{helpers_test.go:45,175,177, persisted_state_test.go:261,324,373-374, writer_test.go:546}`.
검증:
```
gofmt -l hololive/hololive-alarm-worker hololive/hololive-shared hololive/hololive-api
(cd hololive/hololive-shared && go build ./... && go vet ./pkg/service/youtube/tracking/... ./pkg/domain/... && go test ./pkg/service/youtube/tracking/observation/... ./pkg/domain/...)
(cd hololive/hololive-alarm-worker && go build ./... && go vet ./... && go test -race ./internal/egress/youtubedispatch/... ./internal/service/youtube/...)
(cd hololive/hololive-api && go build ./... && go test ./internal/youtube/canonicalwrite/...)
python3 scripts/architecture/check-sql-ownership.py
```
배포: API와 워커를 같은 릴리스로.

### 3c-4 `authorized_at` 열 삭제 migration (다음 릴리스)

변경: 4.3절 migration, manifest, golden(`schema_snapshot.golden.sql:1129,1131-1137` 부근), 3c-3 임시 시험 `TestMarkAlarmSentBatchClearsLegacyAuthorization`과 bulk SQL의 `authorized_at = NULL` 대입 삭제(열이 없으므로).
시험: `hololive-dbtest/youtube_alarm_state_authorization_drop_migration_test.go` — 미발송 ENQUEUED+authorized_at 행 → DETECTED, 발송된 행 불변, 열·인덱스 없음, CHECK가 `ENQUEUED` 거절, 재실행 무변경(선례 `hololive-dbtest/youtube_request_snapshot_drop_migration_test.go`).
검증: `go test -run 'TestYouTubeAlarmStateAuthorizationDrop|TestSchemaSnapshotGolden' ./hololive/hololive-dbtest`, `scripts/architecture/check-migration-manifest.sh`, shared·worker·API 빌드.
배포: 3c-3이 API·워커 모두 운영 반영된 뒤(구 바이너리 0) 별도 승인으로 migrate.

### 순서와 충돌

3c-1 → 3c-2 → 3c-3은 같은 파일(`ST/transition_*`, `YD/send_engine_*`)을 고쳐 순차 머지한다. 3c-3은 기술적으로 3c-1·3c-2와 독립이라 먼저 해도 되지만, 3c-2의 `CompleteSent` sent_at 전달 변경과 겹쳐 순서를 바꾸면 재작업이 생긴다. 3c-4는 릴리스 경계 뒤.

---

## 7. 결정·근거 요약

| 갈림길 | 선택지 | 권고 | 근거 |
|---|---|---|---|
| 버전 축 | row_version / attempt_count | row_version | claim·defer에서 attempt 불변(2.2) |
| locked_by | 추가 / 생략 | 추가 | 4.1 튜플·sendfence 정합·adjudication 소유자 고유(3.2) |
| 만료 표현 | lock_expires_at 추가 / locked_at+lease | locked_at+lease_ms | 구 행 무손실 처리, 인덱스 재사용, S3A 허용(3.2) |
| BeginSending lease 검사 | 함 / 안 함 | 함 | 외부 부수효과 시작 전 lease 유효성, alarm MarkSending과 같은 의미(3.3) |
| 정산 lease 검사 | 함 / 안 함 | 안 함 | 외부 성공을 DB 실패로 되돌리지 않음(계획 3절) |
| next_attempt_at | Go 절대 / SQL clock+delay | SQL clock+delay(+floor) | 4.1 시간 규약, S3A 규약(3.5) |
| adjudication | 전 필드 / 시각 제외 | 시각 제외 | SQL 생성 시각, 버전 CAS가 유일성 보장(3.6) |
| fanout outbox fence | 튜플화 / clock만 | clock만 | outbox migration 필요, 확인된 결함 없음(3.5) |
| authorized_at 제거 | 같은 릴리스 / 2단계 | 2단계 | migrate가 앱보다 먼저 돎(4.3) |
| alarm_states 테이블 | 유지 / 폐지 | 유지 | API 소유 기록, 이 계획의 대상은 claim(open question) |

## 8. 사용자 결정 필요 (open questions)

1. 3c-3의 동작 변화 수용: 같은 post의 다른 방이 결과 불명 뒤에도 즉시 발송된다(현재 최대 2분 defer). 계약 문서 `ClaimDeferred` 결과(`contract:441-443`)와 "Alarm claim token" 절(`:296-309`) 삭제 포함.
2. `youtube_community_shorts_alarm_states` 테이블 자체의 장기 존치: `alarm_sent_at`이 `youtube_content_alarm_tracking.alarm_sent_at`과 중복이며 읽는 곳은 워커 claim(3c-3에서 삭제)과 CompleteSent read-back뿐이다. 폐지하면 API writer·migration이 추가된다. 이번 범위에는 넣지 않았다.
3. `ENQUEUED` 어휘 축소(3c-4의 CHECK 변경)를 계약 변경으로 볼지. 읽는 코드는 없다(`rg ENQUEUED` 결과 domain 상수뿐).
