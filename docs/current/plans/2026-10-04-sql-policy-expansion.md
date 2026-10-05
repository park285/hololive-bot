# SQL 정책 이관 확대

## 목표와 제약

API·alarm-worker·collector의 SQL 상태 결정과 중복 정책을 조사하고 기존 상태 전이·관측 시각·원자성·fence를 보존하면서 Go로 이관한다. 기존 작업을 보존하고 운영 DB 접근·배포·Git 발행·의존성 변경은 하지 않는다. Fallback delta: none.

## 조사와 구현 결과

- [x] 세 모듈의 실행 SQL 310개 파일을 대상으로 조건 분기와 상태 갱신 경로를 조사했다. 테스트 SQL은 집계에서 제외했다. 아래는 호출부·저장 계약까지 확인한 주요 경로다.
- [x] 추가 SELECT 없이 판단 가능한 네 경로를 이관했다.
- [x] 지난 upcoming 후보 경로의 잠금 범위와 해독 실패 동작을 보완했다.
- [x] 최종 성능 비교와 변경 검토 결과를 기록했다.

| 경로 | 결정과 근거 |
| --- | --- |
| API source observation Retry | ClaimBatch의 attempt_count를 Go까지 전달해 PENDING/DEAD_LETTER를 결정한다. DB는 token·동일 attempt_count·lease 만료를 검증한다. 횟수가 맞지 않으면 ErrClaimLost이며 추가 조회나 재시도는 없다. 소진 시 기존 available_at을 유지한다. 단독 DeadLetter는 추가 BEGIN/COMMIT 없이 fenced UPDATE 한 번으로 실행해 왕복을 3회에서 1회로 줄이고, consume의 기존 트랜잭션에서는 같은 저장 함수를 해당 트랜잭션으로 실행한다. |
| API bot reply outbox Settle | Go 정책표가 출발 상태, 재시도 시각 갱신, terminal payload 삭제 또는 manual_review 보존을 함께 선택한다. 고정 SQL 하나를 실행한다. accepted를 재발송 가능 상태로 되돌리지 않는다. |
| Worker dispatch RouteFailures/RouteSendingFailures | Go에서 목표 상태·재시도 시각 갱신 여부·DLQ 시각 기록 여부를 결정한 뒤 한 UPDATE로 적용한다. DLQ에는 불필요한 next_attempt_at을 전송하지 않는다. 고정 SQL은 초기화 시 한 번만 읽는다. 소유권·attempt CAS·pre/post-send의 lease 조건 차이·부분 반영 보고를 유지한다. 같은 ID의 상충 입력은 쓰기 전에 거부한다. RequeuePreSend는 횟수를 증가시키지 않는다. |
| Collector publish 결과 | 원자적 저장이 반환한 충돌 여부·기존 행 여부·checkpoint 전진 사실로 Go가 INSERTED/DUPLICATE/COLLISION과 수락 간격을 계산한다. DB transaction 시각을 사용하고 최초 수락·동일 slot·충돌에서는 간격을 만들지 않는다. |
| Worker upcoming 후보 | 먼저 최대 1,000건을 잠그고 Go로 판정·상태별 일괄 저장한다. JSON 해독은 commit 뒤에 실행한다. 잘못된 payload가 다른 후보의 확정 상태를 롤백하지 않고 checked_at도 전진하므로 같은 불량 행이 후보 순서를 고정하지 않는다. |

## DB에 유지한 경로

| 경로 | 유지 이유와 다음 작업의 전제 |
| --- | --- |
| live session ON CONFLICT 병합 | live·schedule·premiere 경로가 서로 다른 subject lock을 사용하며 schedule 모델은 전체 session 필드를 갖지 않는다. 기존 행의 FOR UPDATE만으로 신규 행 생성 경합을 막을 수 없다. 전면 이관 전에 모든 writer의 video 단위 직렬화와 전체 이전 상태 전달을 함께 설계해야 한다. |
| YouTube delivery 집계·SENT/QUARANTINED 원장 | 집계는 과거 stale count→update 경합을 단문 SQL로 고친 경로다. 원장 upsert는 동일 key에 대한 SENT 우선순위와 최초/최종 시각을 동시 conflict에서 보존한다. 단순 분리는 추가 잠금과 왕복을 요구한다. |
| Collector lease acquire/defer | 만료 판정·slot 보존·retry 최소/최대 경계가 DB 시각에 의존한다. 애플리케이션 시각으로 바꾸면 호스트 시계 차이가 정책을 바꾼다. 기존 typed Go 입력 검증과 DB fence를 유지한다. |
| Bot inbox release/reclaim 및 reply outbox claim/reclaim | 재시도 횟수·operator replay grant·자동 replay 지평을 잠근 행과 DB 시각으로 판정한다. accepted/outcome_unknown을 잘못 재발송하지 않도록 claim 경쟁과 시간 경계를 함께 검증하는 후속 이관이 필요하다. |
| API observation claim/replay/retention | 같은 subject의 목록 선두 순서, replay cutoff, 만료 claim과 보존 삭제를 원자적으로 보호한다. claim 정책을 단순 후처리로 옮기면 LIMIT 이전 선택 집합이 달라질 수 있다. |
| monotonic offset/channel head·조회 정렬/집계 | LEAST/GREATEST, timestamp 보존, 관계형 필터·정렬은 원자적 병합 또는 DB에서 줄여 읽기 위한 연산으로 유지한다. SQL 조건식이라는 이유만으로 애플리케이션으로 옮기지 않는다. |
| live review receipt 함수·payload lock/retention 함수 | 영수증과 현재 사실의 CAS, 제한된 DB 권한으로 수행하는 잠금·삭제 경계가 있다. migration 222는 기존 진단 backfill trigger를 제거한다. 이번에 schema/권한을 바꾸지 않는다. |
| Admin dispatch settle 및 livequery/LLM 조회 | 최근 별도 작업과 겹치는 관리 기능, 조회 최신성/날짜 경계는 이번 쓰기 경로 이관에서 제외했다. |

## 검증

- 통과: API sourceobservation, YouTube runtime, bot durability, collector sourceobservation, worker checker의 PostgreSQL 테스트 및 race.
- 통과: 저장소의 폐기 가능한 PostgreSQL provisioner를 사용한 worker dispatchoutbox `-race -tags=integration`. 혼합 retry/DLQ 원자성·기존 필드 보존·중복 ID 거부를 실제 DB에서 확인했다.
- 통과: 세 runtime 모듈 build, 변경 패키지 golangci-lint·NilAway·staticcheck, SQL 소유권, stack DB 접근 정책.
- 관측 retry는 마지막 횟수 경계·잘못된 횟수·lease 만료/잠금 대기·재실행을 검증했다. Collector는 미래 checkpoint 시각의 수락 간격이 0으로 제한되는 것도 확인했다.
- 성능 비교: `go test ./hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox -run '^$' -bench '^BenchmarkFailureRoutingPolicy$' -benchtime=30x -count=3`. 각 방식에 독립 DB fixture를 만들고 1·64·512건 혼합 배치의 전체 호출을 비교한다. 행 초기화는 측정에서 제외하며 기존 방식의 매 호출 SQL 자산 로딩·상태 검증, 양쪽의 JSON 직렬화·DB 전송·결과 수집을 포함한다. 전송 byte 수도 출력한다. 운영 처리량을 대변하지 않는다.

## 성능 결과와 한계

최종 구현·기존 SQL을 각각 독립 fixture에서 30회씩 3번 실행한 결과의 중간값이다. PostgreSQL 18.6 / 운영과 같은 `cache_statement` 모드 / kapu 로컬 Docker를 사용했다. 복구용 행 초기화는 timer 밖이며, 기존 방식의 SQL 자산 로딩까지 포함한다.

| 배치 | 기존 호출 | 변경 호출 | 기존/변경 JSON 전송량 | 기존/변경 Go 할당 byte |
| --- | ---: | ---: | ---: | ---: |
| 1건 | 0.595 ms | 0.538 ms | 168 / 168 | 4,365 / 1,175 |
| 64건 | 2.771 ms | 2.560 ms | 10,680 / 9,656 | 52,902 / 49,728 |
| 512건 | 20.889 ms | 19.822 ms | 85,909 / 77,717 | 463,305 / 459,362 |

64·512건의 전송량은 약 9.5% 줄었다. 단건의 SQL 자산 재로딩 제거 효과도 할당량에서 확인했다. 짧은 로컬 표본이므로 처리 시간의 통계적 유의성이나 운영 처리량 개선을 확정하지 않는다. 앞선 실험에서는 속도 이득이 일관되지 않았으므로 최종 판단은 전송량 감소·불필요한 SQL 로딩 제거·DB 왕복 감소·잠금 범위 축소와 계약 보존을 중심으로 한다.

상태별 UPDATE 두 개와 PostgreSQL 배열 전달 방식은 측정에서 비용이 늘어 채택하지 않았다. 최종 worker SQL은 하나의 UPDATE를 사용하며, SQL의 CASE는 Go가 지정한 `mark_dlq`에 DB 시각을 적용하는 용도로만 남는다. 상태 문자열을 보고 다음 정책을 결정하지 않는다.

공통 `check-stack-retry-contract.sh`는 [stack AGENTS.md](../../../../AGENTS.md)의 `Never scan Iris/ root, Iris/native/** or Iris/tools/**` 지침과 충돌하는 Iris 경로 검색을 포함해 실행하지 않았다. 스택 공통 재시도·보존 기간 상수는 변경하지 않았고, 이번 변경의 attempt/lease/terminal 계약은 해당 저장소의 PostgreSQL·race 시험으로 검증했다.

현재 승인이나 외부 상태를 기다리는 작업은 없다. 운영 부하·네트워크 지연에서의 검증은 수행하지 않았다.

## 2026-10-05 확장 조사

이번 추가 요청은 조사 범위를 넓히는 작업으로 수행했다. 기존 구현을 보존하고 shared 저장소, bot durability, 관리자 정산, livequery, LLM event, template, 현재 DB 함수·트리거까지 확인했다. 아래의 후보와 결함은 후속 구현 대상이며 이번 조사에서 애플리케이션이나 migration을 추가 변경하지 않았다.

### 범위와 증거

| 범위 | 실행 SQL 파일 | CASE 포함 파일 |
| --- | ---: | ---: |
| API internal | 190 | 17 |
| alarm-worker internal | 92 | 9 |
| collector internal | 28 | 4 |
| shared | 101 | 12 |
| 합계 | 411 | 42 |

`queries/*.sql` 기준이며 testdata/fixture와 migration은 제외했다. 별도로 db-migrate 명령의 role 점검 SQL 1개가 있다. Go 문자열로 조립하는 SQL도 관련 호출부에서 확인했으나 이 파일 수에 포함하지 않았다. 411개 전체의 목록·조건식을 살폈고, 아래 경로는 호출부·반환 계약·잠금까지 추적했다. 모든 SQL을 실제 부하로 측정했다는 의미는 아니다.

폐기 가능한 PostgreSQL 18.6에 manifest 전체를 replay했다. 마지막 migration은 `263_youtube_live_review_bounded_facts.sql`이며, 재생 후 public 함수는 33개(SQL 14, PL/pgSQL 19), 그중 SECURITY DEFINER는 21개였다. 사용자 트리거 9개는 감사·영수증 관련 7개, template row_version 1개, command 결과 정규화 1개다. 과거 migration에 등장하는 함수 수를 현재 기능 수로 세지 않았다. 운영 DB의 실제 catalog와는 비교하지 않았다.

### 우선 수정: 잠금 대기 중 수동 재발급 제한 초과

**격리 DB에서 확인한 결함이다.** [grant_bot_reply_outbox_manual_replay](../../../hololive/hololive-api/scripts/migrations/001_schema_epoch2_baseline.sql)는 `granted_at := clock_timestamp()`를 선언부에서 평가한 다음 행을 `FOR UPDATE`로 잠근다. 이후 144시간 cutoff 검사와 감사·갱신 시각에 그 값을 사용한다. 따라서 cutoff 전에 호출하고 잠금 대기 중 cutoff를 넘기면 지난 시각으로 승인을 내린다. 전체 migration replay에서 추출한 현재 함수도 같은 정의였다.

재현은 다음 순서로 실행했다.

1. 합성 manual_review 행을 만들고 별도 트랜잭션에서 잠갔다. `created_at + 144 hours`를 DB 시각 기준 2초 뒤로 설정했다.
2. 다른 연결에서 재발급 함수를 호출하고 `pg_blocking_pids`로 cutoff 전에 실제 잠금 대기에 진입했음을 확인했다.
3. DB 시각으로 cutoff를 지난 후 잠금을 해제했다.
4. 함수는 `cutoff_expired` 대신 `replayed`를 반환하고 `pending` 및 승인 감사 행을 기록했다. 최초 재현의 cutoff는 `00:25:51.881864+09:00`, 잠금 해제 직전은 `00:25:51.986007+09:00`, 승인 기록은 `00:25:49.883963+09:00`였다.
5. 추가 재현에서 `attempts=5`, `first_attempt_at=DB 시각 - 1시간`인 행에 현재 `reply_outbox_claim.sql`을 실행했다. 잘못 승인된 grant를 사용해 `submitting`, `attempts=6`으로 전이했다. Claim은 first_attempt_at 기준 안전 경계를 사용하므로 created_at 기준 재발급 cutoff를 대신 보장하지 않는다. 외부 발송은 수행하지 않았다.

권장 수정은 새 forward migration에서 잠금 획득 후 DB 시각을 읽고 그 시각으로 제한·감사·available_at을 일관되게 정하는 것이다. 기존 baseline을 고쳐 적용 이력을 바꾸지 않는다. 기존 `TestManualReviewReplayCutoff`는 즉시 호출의 전·정각·후 경계만 다루므로, 잠금 대기로 경계를 넘기는 회귀 시험이 추가로 필요하다. 이 부분은 상태 정책 이관보다 먼저 처리할 대상으로 분류한다.

같은 catalog에서 `discard_bot_reply_outbox_manual_review`도 `decided_at`을 잠금 전에 저장한다. 이 함수에는 재발급 cutoff 분기가 없으므로 동일한 제한 우회로 단정하지 않는다. 감사 시각이 요청 시작 시각인지 실제 결정 시각인지 확인할 후속 대상이다. 나머지 함수의 시간·잠금 사용도 검색했으며 같은 재발급 분기는 찾지 못했다.

### 추가 조회 없이 이관할 후보

| 우선순위 | 경로와 현재 근거 | 제안 및 보존 조건 |
| --- | --- | --- |
| 1 | Shared delivery 실패·재발급: `outbox_claim_ready.sql`이 attempt_count를 반환한다. `dispatcher.go`의 markItemFailed와 `request.go`의 reissueRequest는 이미 claim된 item을 받는 흐름이지만 저장 경계에서 ID만 전달해 SQL이 FAILED/PENDING을 다시 결정한다. | claim 횟수를 저장 입력까지 전달하고 Go가 소진·상태·오류 prefix를 정한다. attempt CAS를 추가하고 owner·SENDING·frozen request 일치 검사를 유지한다. 재시도 시각은 잠금 획득 후 DB 시각에 backoff를 더한다. 선행 SELECT는 필요하지 않다. |
| 1 | Bot Inbox 명시적 Release: InboxClaim은 Attempts를 갖지만 Release에는 ID/token/maxAttempts만 넘어간다. `inbox_release.sql`이 retry/dead, payload 삭제, 종료 사유를 정한다. | claim Attempts를 전달해 Go에서 전이 형태를 고르고 동일 횟수를 검증한다. ordering key 조회·advisory transaction lock·다음 head 이동은 현재 트랜잭션에 남긴다. 만료 행을 직접 선택하는 reclaim과 별개로 이관할 수 있다. |
| 1 | Admin dispatch settle: `settle.go`는 Serializable 트랜잭션에서 그룹을 FOR UPDATE로 읽고 상태·revision·action을 검증한다. `queries/settle.sql`에서 cancel/quarantine 및 시각 선택을 다시 수행한다. | 이미 읽은 사실로 Go가 저장 형태를 고른다. 그룹 전체 검증·revision·감사 CTE와 `GREATEST(clock_timestamp(), updated_at + 1 microsecond)`를 보존한다. 추가 조회는 필요하지 않다. |
| 2 | Livequery 진단 사유: `snapshot.sql`의 invalid_projection→uncollected→inconsistent→confirming_end→invalid_clock→stale→incomplete→covered 우선순위가 SQL에 있다. 전체 응답 Complete/Partial/Unavailable은 이미 Go가 계산한다. | 같은 snapshot에서 집계한 사실만 반환하고 채널별 사유를 Go로 분리한다. 활성 ID·최신성 필터·정렬·LIMIT은 DB에 유지한다. 최대 10,000채널의 추가 전송량 및 1초 query budget을 비교해야 하며 속도 개선을 단정하지 않는다. |
| 2 | Member SetGraduation과 Upsert가 boolean→active/graduated 정책을 나눠 갖는다. Xspaces Observe는 Go에서 상태를 정하면서 SQL에서 code 공백 여부로 성공 시각 기록을 다시 정한다. | Go의 기존 결정으로 목표 상태·성공 기록 여부를 전달한다. Xspaces active_revision fence와 DB 시각을 유지한다. 정책 중복 제거 효과이며 큰 성능 이득의 근거는 없다. |

앞서 보류했던 Inbox·admin·livequery는 전체 기능 이관과 위의 제한된 결정을 구분했다. 특히 Inbox 명시적 Release는 claim에 있는 사실을 사용할 수 있지만, expired reclaim과 reply outbox claim/reclaim은 잠긴 행의 선택·순서·시각·144시간 경계를 함께 처리하므로 같은 난이도가 아니다.

### 재현한 쓰기 증폭 후보

실제 PostgreSQL에서 같은 입력을 두 번 적용하거나 terminal 행에 늦은 결과를 적용하고 `xmin` 및 논리 필드를 비교했다. 아래 세 경로 모두 새 행 버전이 생겼다. 운영 호출 빈도·WAL·vacuum 비용은 측정하지 않았으므로 우선순위는 호출 빈도 자료와 함께 정해야 한다.

| 경로 | 확인한 현상 | 변경 전 해결할 계약 |
| --- | --- | --- |
| LLM major event upsert | 동일 입력에도 UPDATE로 xmin이 달라진다. | canceled 유지, ended의 CURRENT_DATE 기준 재활성화, link 변경 시 검사 상태 초기화를 보존한다. 기존 행·신규 conflict 모두 고려해야 하며 앱 시각으로 날짜 기준을 바꾸면 안 된다. |
| Template default upsert | 같은 body에서 row_version은 그대로인데 xmin은 달라진다. | 반환 행과 OLD.body는 revision 처리에 사용된다. updated_at이 마지막 저장 시각을 뜻하는지도 확인해야 한다. 단순 WHERE 조건 추가는 RETURNING 행을 없앨 수 있다. override도 같은 형태의 후속 후보이나 실제 재현은 default에서 수행했다. |
| SENT delivery ledger에 늦은 quarantine | 모든 논리 필드가 같은데 xmin이 달라진다. | SENT 우선순위와 기존 receipt를 반환하는 계약을 유지해야 한다. conflict UPDATE를 생략하면 RETURNING이 비어 버리므로 별도 읽기·잠금 비용까지 비교해야 한다. |

정책 정리를 위해 먼저 SELECT를 넣으면 이득보다 왕복·잠금 시간이 늘 수 있다. 기존 RETURNING 계약과 쓰기 생략을 동시에 만족시키는 후보만 별도 benchmark로 비교한다.

### 유지할 원자성과 제거 검토할 중복

- **Tracking batch의 트랜잭션은 필수다.** `MarkAlarmSentBatch`는 큰 SQL 하나를 실행한 뒤 Go에서 authorization_mismatch_count를 확인한다. SQL이 이미 일부 tracking 상태를 갱신했더라도 오류이면 전체 롤백해야 한다. 단일 statement라는 이유로 BEGIN/COMMIT을 없애면 불일치 결과가 커밋된다. 기존 PostgreSQL 회귀 시험을 race로 실행해 롤백을 확인했다.
- **Tracking latency 임계값은 단일 출처로 만들 수 있다.** bulk SQL의 `120000`과 Go builder의 `alarmtiming.LatencyExceededThresholdMillis`는 현재 2분으로 같다. 현재 오동작이 아니라 향후 값 불일치 위험이며, 먼저 SQL 매개변수로 통일하면 큰 상태 병합을 풀 필요가 없다.
- **Command summary 정규화 트리거는 제거 후보다.** 현재 terminal writer는 summary에 status 자체를 쓰고, CHECK도 terminal summary=status를 강제한다. 격리 DB에서 트리거를 제거하자 status-only 쓰기는 성공하고 다른 요약은 CHECK로 거절됐다. rollback 버전·외부 writer를 확인하기 전 제거를 확정하지 않는다. CHECK는 유지해야 한다.
- **감사·영수증 트리거와 SECURITY DEFINER 함수를 일괄 이관하지 않는다.** 제한된 role로 삭제·잠금·감사 불변성을 제공하는 권한 경계다. 앱으로 옮기기 위해 일반 UPDATE/DELETE 권한을 늘리는 것은 정책 중복 제거와 별도 변경이다. Template row_version도 다른 writer에 대한 보호를 제공한다.
- **Claim 선택과 동시 upsert는 DB에 남길 부분이 있다.** SKIP LOCKED, 선행 행 순서, LIMIT 이전 필터, lease 유효성, monotonic head와 SENT 우선순위는 동시 실행의 결과를 바꾼다. Go로 옮길 도메인 결정과 DB가 최종 검증할 조건을 각각 명시해야 한다.

### 이번 조사에서 실제 실행한 검증과 한계

- manifest 전체 replay 후 pg_proc/pg_trigger를 조회했다. 운영 연결·secret 조회 없이 격리 DB만 사용했다.
- 임시 Go probe로 동일 입력 쓰기 3종, command trigger/CHECK 관계, 수동 재발급의 잠금 대기 cutoff를 재현했다. probe는 조사 후 삭제하고 제품 gate에 추가하지 않았다.
- `TestRepositoryMarkAlarmSentBatchRollsBackOnClaimAuthorizationMismatch`를 PostgreSQL/race로 실행해 통과했다.
- `TestMarkFailedSchedulesRetryFromDatabaseExecutionTime`, `TestMarkFailed_FenceRejectsStaleWorkerAfterReclaim`, `TestEnqueue_FailedRetry`를 PostgreSQL/race로 실행해 통과했다.
- 이번 확장 조사에서는 실행 계획·운영 통계·production WAL을 측정하지 않았다. 확인된 결함과 로컬 쓰기 현상 외의 성능 이득은 후보이며, 실제 운영 발생 여부도 확인하지 않았다. 기존 구현 검증 결과는 위 절의 범위에 한정한다.

후속 순서는 재발급 cutoff 수정 → shared delivery·Inbox Release·admin settle의 제한된 정책 이관 → 쓰기 생략의 반환 계약 검증 및 benchmark → livequery 진단/중복 상수 정리다. 전면 이관이 필요한 live-session 병합과 claim/reclaim은 모든 writer의 잠금·시간 계약을 설계한 뒤 진행한다.

## 2026-10-05 구현 결과

확장 조사 뒤 사용자의 구현 요청에 따라 아래 변경을 완료했다. 기존 미커밋 변경을 보존했으며 운영 DB 적용·배포·Git 발행·의존성 변경은 하지 않았다. Fallback delta: none.

| 경로 | 구현 결과 |
| --- | --- |
| 수동 재발급 cutoff | 새 `264_bot_manual_replay_lock_clock.sql`에서 잠금 획득 후 `clock_timestamp()`를 읽도록 함수를 교체했다. 제한 검사·승인 감사·available_at은 같은 시각을 사용한다. 기존 함수의 인자·반환 문자열·SECURITY DEFINER·search_path·권한은 유지한다. manifest와 재생 schema snapshot도 갱신했다. |
| Shared delivery 실패·재발급 | claim의 AttemptCount를 실패·준비 실패·세대 재발급까지 전달한다. Go가 FAILED/PENDING, generation 소진 오류 prefix, 재시도 시각 기록 여부를 결정한다. SQL은 소유권·동일 횟수·frozen request를 검증하며 SENDING 정산과 발송 전 lease 조건을 보존한다. 두 SQL을 매 호출 읽는 비용도 제거했다. 재발급 backoff는 명시적 행 잠금 뒤 DB 시각에 적용한다. |
| Bot Inbox Release | claim Attempts를 받아 Go에서 retry/dead 쿼리를 선택한다. 두 쿼리 모두 token·processing·동일 횟수를 검증한다. Retry는 payload·terminal 필드·head를 유지하며, dead는 payload 삭제와 다음 head 이동을 기존 트랜잭션에서 수행한다. Runtime 호출부와 기존 회귀 시험도 현재 claim 횟수를 전달한다. |
| Admin dispatch settle | 이미 잠긴 그룹에서 Go가 목표 상태·취소 시각 갱신 여부·새로 격리할 ID를 결정한다. 한 UPDATE와 감사 CTE로 저장하며 Serializable·revision 검증·기존 격리 시각·monotonic updated_at을 유지한다. |
| Livequery | projection·수집·불일치·종료 대기·미래 시각·stale 사실을 SQL에서 집계하고 Go가 기존 우선순위로 사유를 정한다. 여섯 사실은 작은 비트 값으로 전달한다. 활성 ID·최신성 필터·정렬·LIMIT·단일 snapshot을 유지하고 외부 응답 형식도 바꾸지 않았다. |
| Member / Xspaces | Member 생성·졸업 변경의 active/graduated 판정을 같은 Go 함수로 통일했다. Xspaces의 성공 시각 기록 여부는 Go의 관측 결과로 전달하며 active_revision 검증과 DB 시각을 유지했다. |
| Tracking latency | bulk SQL의 120000 상수를 Go의 기존 공통 임계값 매개변수로 바꿨다. 실제 값은 2분으로 동일하며 authorization mismatch 시 전체 롤백하는 트랜잭션을 유지했다. |

정책 이관 경로에 선행 SELECT나 DB 왕복을 추가하지 않았다. DB의 CASE는 전달된 기록 여부에 따른 기존 값 보존·DB 시각 적용 등에 남으며 다음 도메인 상태를 다시 결정하지 않는다. 일반 reclaim과 동시 upsert 병합은 이번 이관 대상에 넣지 않았다.

### 성능 확인

Livequery의 진단 전달을 boolean 배열로 시도했을 때 10,000채널에서 채널당 30바이트가 늘고 로컬 호출 시간이 약 9% 길어져 채택하지 않았다. 최종 비트 표현은 같은 snapshot을 SQL 사유 분류/Go 사유 분류로 비교했다. 각각 10회씩 3개 표본의 중간값이며, 합성 roster·신선한 channel coverage·live 항목 0개인 격리 PostgreSQL 18.6을 사용했다. 전체 조회·전송·JSON 해독·사유 판정을 포함하고 fixture 준비는 제외했다.

| 채널 수 | 기존 / 변경 호출 | 기존 / 변경 진단 JSON | 기존 / 변경 Go 할당 byte |
| --- | ---: | ---: | ---: |
| 74 | 2.367 / 2.371 ms | 14,569 / 13,903 | 30,776 / 35,662 |
| 10,000 | 117.274 / 118.675 ms | 1,988,894 / 1,898,894 | 7,154,444 / 8,030,068 |

최종 전송량은 약 4.5% 감소했다. 10,000채널에서도 기존 1초 query budget 안에서 통과했지만, 호출 시간은 약 1.2% 길고 Go 할당은 증가했다. 정책 소유권과 전송량 개선으로 채택했으며 속도 향상으로 주장하지 않는다. 로컬 합성 데이터의 짧은 표본이고 장기 이력·운영 네트워크·운영 부하를 대표하지 않는다. 임시 비교 harness는 결과 기록 후 삭제했다.

### 검증

- 수동 재발급 잠금 대기 회귀 시험은 수정 전 `replayed`를 반환해 실패했고, 수정 후 `cutoff_expired`·감사 행 0개·claim 불가를 검증했다. 호스트 시계 대신 DB 시각과 실제 blocking PID로 경계 통과를 확인한다.
- PostgreSQL/race 통과: shared delivery, bot durability/runtime, livequery, member, Xspaces, tracking observation. 새 시험은 잘못된 claim 횟수에서 저장을 거부하고, 소진 시 기존 재시도 시각을 보존하며, Inbox 선행 head가 유지되는 것을 확인한다.
- PostgreSQL/race/integration 통과: admin dispatchops. 혼합 dlq/quarantined 그룹의 보존 시각·새 격리 시각·감사·stale revision·중복 정산을 확인했다.
- 통과: 세 runtime 모듈 build, 변경 8개 패키지 golangci-lint(0 issues)·staticcheck·NilAway, schema snapshot 전체 replay, migration manifest, SQL 소유권, stack DB 접근 정책. 마지막 테스트 함수 정리 뒤에도 해당 회귀 시험/race·정적 검사와 전체 변경 패키지 lint를 다시 통과했다.
- Stack retry/reissue/projection 공통 검사는 금지된 `Iris/native/**`·`Iris/tools/**` 경로를 읽으므로 실행하지 않았다. 근거는 [stack AGENTS.md](../../../../AGENTS.md)의 `Never scan Iris/ root, Iris/native/** or Iris/tools/**`다. 공통 기간·세대 상수나 Iris 계약값을 변경하지 않았으며 관련 Hololive의 실제 DB 회귀 시험을 실행했다.

### 보존한 범위와 운영 반영

동일 입력 쓰기 생략 세 후보는 기존 구현을 유지했다. Event와 template는 저장 시 updated_at을 바꾸며 template는 OLD.body와 기존 결과 행을 반환한다. SENT 원장의 quarantine도 기존 receipt를 반환해야 한다. 단순 조건부 UPDATE는 반환 행을 없애며, 이를 보완하는 읽기·잠금은 추가 왕복과 신규 conflict 처리가 필요하다. 운영 호출 빈도와 반환값/저장 시각 계약을 확인하기 전 쓰기 감소만을 위해 이 경로를 바꾸지 않는다. 이 세 경로의 속도 개선은 이번 구현 결과에 포함하지 않는다.

Command summary 정규화 트리거도 rollback/외부 writer 확인 전 유지했다. 감사·영수증 트리거와 DB 권한은 변경하지 않았다. 재발급 결함의 운영 수정은 새 migration 264를 운영에 적용해야 활성화된다. 현재 결과는 로컬 소스·테스트·배포 준비이며 운영 반영은 수행하지 않았다.

## 2026-10-05 2차 확장 구현

사용자의 추가 확장 요청에 따라 원장의 반환값 소비자, 만료 회수의 잠금 순서, live session의 분류 전용 저장까지 추적했다. 이번에는 세 경로를 추가 구현했다. 이전 구현·미커밋 작업을 보존했고 운영 DB·배포·Git 발행·의존성·권한 변경은 없다. Fallback delta: none.

### 원장: 반환값 소비 계약을 재확인하고 쓰기 증폭 제거

앞선 보류 사유 중 원장의 «기존 receipt를 반환해야 한다»는 판단을 수정했다. `RecordDeliveryLedgerWrites`는 호출부에 error만 반환한다. 내부 SELECT 결과는 개수·상태 확인 뒤 버리고, 실제 receipt 소비자는 별도 원장 조회를 사용한다. 따라서 이 함수에서는 영수증 전체를 다시 읽지 않아도 계약을 유지할 수 있었다.

- 두 원장 upsert를 `Exec` 한 번으로 처리한다. 사용하지 않는 9개 열의 RETURNING·정렬·행 scan과 매 호출 SQL 자산 읽기를 제거했다. SQL은 시작 시 한 번 적재한다.
- SENT에 대한 늦은 quarantine은 UPDATE를 생략한다. 같은 상태에서도 최초/최종 시각·최초 SENT의 출처·누락된 출처가 실제로 바뀔 때만 쓴다.
- 동시 conflict의 최신 행을 PostgreSQL이 잠근 상태에서 검사한다. 추가 SELECT나 트랜잭션·왕복을 넣지 않았고, 없는 행을 다른 연결이 생성하는 경우도 ON CONFLICT가 처리한다.
- 상태 vocab·필드 shape·시각 순서·출처 ID는 기존 DB CHECK로 검증한다. SQL 쓰기를 생략하더라도 잘못된 입력의 INSERT 제약 검증을 생략하지 않는다.
- 같은 배치의 중복 논리 키는 Go에서 쓰기 전에 거부한다. 기존 SQL이 거부하던 중복 입력을 no-op 조건 때문에 성공으로 바꾸지 않는다. 외부로 전달하는 오류에 논리 키 원문을 넣지 않는다.

실제 PG 시험은 동일 SENT/QUARANTINED 반복의 xmin 보존, 늦은 quarantine의 xmin 보존, 이른 SENT 출처·늦은 관측 시각 병합, 누락 출처 보완, 중복 배치의 무변경, 잘못된 입력과 형제 행의 전체 롤백을 확인했다. 신규 원장 행이 아직 commit되지 않은 상태에서 다른 연결이 기다리도록 만들어 양쪽 삽입 순서 모두 SENT가 유지되는 것도 확인했다.

### Inbox 만료 회수: 잠긴 사실로 Go에서 전이 결정

기존 ordering key 조회 → advisory transaction lock → 만료 head 선택 순서를 유지한다. advisory lock과 행 조회를 한 statement로 합치면 잠금 대기 전에 만들어진 snapshot을 사용할 수 있으므로 합치지 않았다.

1. 잠금 획득 뒤 DB 시각으로 만료된 현재 head만 정렬·LIMIT·FOR UPDATE SKIP LOCKED로 선택한다. Go로 전달하는 것은 ID와 attempts뿐이다.
2. Go가 retry/dead와 terminal 필드 정리 여부를 결정한다.
3. 한 일괄 UPDATE에서 동일 횟수·processing을 확인하고 payload 삭제·다음 head 이동·결과 집계를 함께 처리한다. 적용 개수가 잠긴 후보 개수와 다르면 오류로 전체 트랜잭션을 롤백한다.

만료 ordering key가 없는 호출은 기존과 같은 비용이다. 후보가 있는 호출은 조회 한 번이 늘어난다. Runtime의 기존 batch는 100이며 상한·재시도 횟수·maintenance 주기는 바꾸지 않았다. 잠긴 head를 건너뛰면서 다른 head를 회수하고, lease가 살아 있는 행을 유지하며, 소진된 head만 successor로 전진시키는 DB/race 시험을 추가했다.

### Live session: 분류 전용 쓰기 선택을 Go로 이동

`classificationOnlyOnConflict`를 SQL 전체의 반복 조건으로 전달하는 대신 Go가 일반 병합 쿼리와 Premiere 분류 전용 쿼리를 선택한다. 신규 행은 동일한 16개 필드로 생성하며, 기존 행의 분류 전용 저장은 미확정 is_premiere만 보완한다. 기존 수명·제목·일정·관측 시각은 건드리지 않는다. 이미 분류가 있거나 새 분류가 없으면 기존 no-op 동작을 유지한다.

일반 session 병합의 ENDED 우선순위·LIVE→UPCOMING 후퇴 방지·메타데이터 증거 시각·최초/최종 시각 병합은 동시 writer를 보호하므로 SQL에 유지했다. Session A→head A→session B→head B의 저장 순서와 64세션/128문장 배치도 그대로다. Session·head SQL은 호출마다 읽지 않고 시작 시 적재한다. 기존 noop/metadata/Premiere/배치 롤백 시험에 분류 전용 호출이 수명과 제목을 덮지 않는 검증을 보강했다.

### 로컬 비용 측정

PostgreSQL 18.6, 로컬 Docker, 각 방식별 독립 DB fixture에서 30회씩 3개 표본을 측정한 중간값이다. Go 입력 구성·DB 전송·결과 처리까지 포함하고 준비·행 복구는 측정에서 제외했다. 기존 원장 비교의 SQL 자산도 미리 읽었으므로 기존 방식의 매 호출 자산 재로딩 비용은 비교에서 제외했다. 임시 harness는 결과 기록 후 제거했다.

| 경로 / 배치 | 기존 / 변경 호출 | 기존 / 변경 Go 할당 byte |
| --- | ---: | ---: |
| SENT에 늦은 quarantine / 1 | 0.400 / 0.246 ms | 8,632 / 1,077 |
| SENT에 늦은 quarantine / 64 | 1.670 / 1.237 ms | 68,927 / 43,565 |
| SENT에 늦은 quarantine / 512 | 12.006 / 8.515 ms | 556,154 / 391,223 |
| Inbox 만료 회수 / 1 | 0.961 / 1.082 ms | 5,698 / 4,226 |
| Inbox 만료 회수 / 100 | 2.880 / 2.990 ms | 41,683 / 75,858 |

원장 512건은 이 표본에서 호출 시간이 약 29% 감소했다. 동일 배치에 `EXPLAIN (ANALYZE,WAL,BUFFERS,FORMAT JSON)`을 반복한 결과, 기존 statement는 WAL 170,318바이트/2,023 records, 변경 statement는 27,648바이트/512 records였다. 양쪽 FPI는 0이고 변경 쿼리는 conflict filter에서 512건 모두 쓰기를 생략했다. 약 83.8% 감소한 로컬 관측이며, 행 잠금 때문에 no-op에서도 WAL은 발생한다. 전체 운영 WAL·처리량 개선율로 일반화하지 않는다.

Inbox는 100건에서 약 0.110ms/3.8% 느리고 할당도 증가했다. 정책을 Go로 옮기는 비용으로 명시하며 속도 개선으로 주장하지 않는다. 이미 트랜잭션을 사용하는 100건 maintenance 경로라는 범위에서 채택했다. 장거리 DB 연결에서는 추가 왕복의 영향이 더 클 수 있으며 운영 지연은 측정하지 않았다.

### 더 넓게 확인했지만 유지한 경로

| 경로 | 유지 근거 |
| --- | --- |
| Template 동일 body 저장 | Upsert/UpsertWithRevision이 실제 template와 OLD.body를 호출자에게 반환하고 updated_at을 갱신한다. 원장의 error-only 계약과 달라 같은 방식으로 생략할 수 없다. |
| Major event upsert | canceled 유지·CURRENT_DATE 기준 재활성화·link 변경 시 검사 초기화와 저장 시각을 함께 병합한다. 다른 writer의 상태 변경과 신규 키 conflict까지 직렬화하는 설계가 필요하다. |
| Reply outbox claim/reclaim | accepted는 admission만 증명하므로 자동 재발송할 수 없다. attempts·operator grants·first_attempt_at의 144시간 경계와 room 순서·LIMIT을 함께 보호한다. 단건 claim을 SELECT→Go→UPDATE로 풀면 현재 단문 경로에 트랜잭션·왕복이 늘어난다. Inbox와 같은 비용 구조로 보지 않았다. |
| Live/schedule 전체 upsert | `live`와 `schedule:<group>`의 잠금 범위가 다르고 schedule의 상태 모델도 전체 session을 포함하지 않는다. 없는 video 행의 동시 생성까지 해결하지 않은 전면 이관은 보류했다. 분류 전용 정책 분리는 이 전제를 바꾸지 않는다. |
| LLM 기간 조회 | UTC DATE의 올림 경계와 non-finite/NULL scan 오류 보존, 정렬 전 후보 선택이 기존 DB 시험으로 계약화되어 있다. 조회 필터를 전부 Go로 옮기면 대량 후보 전송을 다시 늘릴 수 있어 유지했다. |

### 검증과 남은 한계

- 실제 PostgreSQL/race: youtubedispatch 전체(저장소·복구·준비·수명·포맷), bot durability/runtime, API sourceobservation 전체 통과.
- 원장 xmin 회귀 시험은 기존 코드에서 실패한 뒤 새 구현에서 통과했다. 추가 동시 생성·중복 입력·시간 병합·invalid batch 시험도 실행했다.
- 변경 3개 패키지 golangci-lint(0 issues)·NilAway·staticcheck 및 세 runtime build가 통과했다. SQL 소유권 검사에서 일괄 입력의 SELECT wildcard를 발견해 명시적 열로 수정하고 검사와 Inbox 회귀 시험을 다시 통과했다. 마지막 원장 invalid batch 시험/race·정적 검사, stack DB 접근 정책, diff/문서 링크/임시 probe 정리도 확인했다.
- 운영 데이터·운영 catalog·실제 호출 빈도는 조회하지 않았다. 권한·스키마·배포·공통 retry/reissue 기간은 이번 확장에서 변경하지 않았다. 앞서 명시한 Iris 금지 경로를 읽는 공통 검사는 계속 실행하지 않았다.

## 2026-10-05 3차 확장 구현

추가 확장 요청으로 Reply outbox 만료 회수, Collector의 잠금 대기 중 시간 판정, Template revision 저장까지 진행했다. 기존 변경을 보존했고 운영 적용·배포·Git 발행·의존성·DB 권한·스키마 변경은 없다. Fallback delta: none. 새 재시도 경로나 호환 분기를 만들지 않았다.

### Collector: 잠금 대기로 최소 간격과 lease fence가 무효화되는 결함 수정

격리 PostgreSQL에서 실제 blocking PID로 대기를 확인한 뒤 잠금을 해제하는 회귀 시험을 만들었다. 기존 코드에서 다음 실패가 재현됐다.

- Defer의 `statement_timestamp()` 기반 최소 대기가 잠금 대기 시간에 소모됐다. 최소 100ms인데 200ms를 잠금에서 기다리면 해제 뒤 최소 간격이 남지 않았다.
- 행 버전을 바꾸지 않는 `SELECT ... FOR UPDATE`가 잠금을 잡고 있을 때, 대기 중 lease가 만료돼도 기존 Defer UPDATE가 성공했다. 만료 조건이 잠금 전에 평가됐기 때문이다.
- 같은 방식으로 shutdown·renew 실패·superseded Release도 만료 뒤 성공했다. 일반 Release의 jitter 역시 대기 전에 계산되어 해제 뒤 최소 지연을 보장하지 못했다.

Defer와 두 Release SQL은 `locked` CTE에서 먼저 행을 잠근 뒤 `eligible` CTE에서 owner·epoch·generation·scheduled slot·ACTIVE·lease 만료를 검사한다. 그 이후 얻은 단일 DB 시각으로 retry/jitter, 진단 시각, updated_at을 기록한다. 일반 backoff의 최소/최대, 명시적 not-before의 하한, 실패 tuple whitelist와 진단 보존은 유지했다. 각 호출은 여전히 SQL 한 번이며 추가 DB 왕복이 없다.

위 회귀 시험은 수정 전 실제로 실패했고 수정 후 race로 통과했다. 만료된 호출은 `ErrFenceLost`이며 행을 ACTIVE 상태 그대로 유지한다. Renew·CompleteCurrent는 이미 트랜잭션에서 행을 잠근 후 별도 UPDATE로 만료를 다시 검사하는 경로임을 확인했다. 해당 경로의 저장 방식은 변경하지 않았다.

### Reply outbox: 만료 회수의 상태·사유·집계를 Go로 이동

공개 `ReclaimExpired` 입력과 결과 구조는 유지했다. 트랜잭션 안에서 만료된 submitting/accepted 행을 기존 순서·LIMIT·SKIP LOCKED로 잠그고, ID·상태·횟수·재발급 권한 횟수·DB가 판정한 시간 경계 사실만 읽는다. Payload는 전송하지 않는다.

Go는 accepted를 항상 manual_review로 보내고, submitting의 횟수/권한/144시간 경계로 pending 또는 manual_review와 오류 사유·집계를 결정한다. 단일 일괄 UPDATE로 적용하며 payload·발송 식별자·시도 횟수·기존 available_at을 보존한다. 재발급 권한 fixture도 실제 수동 재발급 함수로 감사 기록을 만든 뒤 검증했다.

조회 뒤 저장 전에 시간 경계를 넘길 가능성은 최종 SQL 조건으로 다시 검사한다. 이 조건은 다음 상태를 바꾸지 않고 유효하지 않은 pending 저장을 거절한다. 적용 개수가 잠긴 후보와 다르면 전체 트랜잭션을 롤백하고 오류를 반환한다. 동일 호출 안에서 자동 재시도하지 않는다. 기존 다음 maintenance 호출에서 새 DB 사실로 판정하며, 이 경계 경합에서는 같은 배치의 다른 행도 다음 회수까지 기다릴 수 있다.

새 DB/race 시험은 accepted·일반 retry·시도 소진·유효한 운영자 grant·시간 소진이 섞인 배치, 잠긴 행 건너뛰기, payload 보존, 조회/저장 사이 경계 통과의 전체 롤백과 다음 회수의 정상 진행을 확인했다. 단건 Claim은 기존 SQL을 유지해 빈 polling 및 실제 발송 진입의 왕복을 늘리지 않았다.

만료 회수 자체는 기존 단문 1회에서 후보가 있으면 BEGIN→조회→UPDATE→COMMIT 4회, 없으면 BEGIN→조회→COMMIT 3회가 됐다. 기존 maintenance 호출에 한정하며 주기와 runtime batch 100은 변경하지 않았다. 이 비용을 성능 개선으로 표현하지 않는다.

### Template: 반환값과 저장 시각을 보존하면서 revision 왕복 감소

`UpsertWithRevision`의 실제 덮어쓴 OLD.body, 현재 template 반환값, updated_at, 동일 body의 revision 미생성 계약을 유지했다. 보존 개수가 양수이면 revision INSERT와 prune DELETE를 기존 `dbx.ExecStatements`로 한 번에 전송한다. 두 문장은 같은 트랜잭션에서 순서대로 실행되므로 prune이 방금 삽입한 revision도 읽는다. 보존 개수가 0 이하인 경로는 기존 단독 INSERT를 유지했다.

한 CTE로 합쳐 새 revision을 무조건 최상위로 취급하지 않는다. 미래 created_at을 가진 기존 revision을 포함해 실제 `created_at DESC, id DESC` 순서로 보존 대상을 고른다. Prune이 실패하면 먼저 실행된 revision INSERT와 template 본문 변경도 롤백된다. 두 경우를 새 PostgreSQL 시험으로 확인했고 기존 동시 저장·반환값·취소·보존 개수 시험도 통과했다.

준비된 문장을 사용하는 본문 변경+revision 정리 호출은 BEGIN·UPSERT·INSERT·DELETE·COMMIT의 5회 전송에서 INSERT/DELETE 배치를 묶은 4회로 줄었다. SQL 문장 수와 트랜잭션 경계는 같다. 최초 statement 준비 비용과 실제 운영 네트워크 지연은 별도다.

### 비용 측정

PostgreSQL 18.6 / 로컬 Docker / 30회씩 3개 표본의 중간값이다. 각 방식은 독립 fixture를 사용하고 준비·복구는 측정에서 제외했다. SQL 문자열은 미리 적재하고 양쪽 statement를 warmup했다. Reply는 서로 다른 방의 합성 만료 행이며, accepted/submitting과 횟수 소진을 섞었다. 임시 harness는 결과 기록 후 삭제했다.

| 경로 | 기존 / 변경 호출 | 기존 / 변경 Go 할당 byte |
| --- | ---: | ---: |
| Template, revision 정리 없음(keep=0) | 0.384 / 0.375 ms | 2,339 / 2,340 |
| Template, revision 5개 보존 | 0.582 / 0.509 ms | 2,740 / 3,092 |
| Reply 회수, 0건 | 0.219 / 0.258 ms | 808 / 1,281 |
| Reply 회수, 1건 | 0.504 / 0.733 ms | 808 / 2,497 |
| Reply 회수, 100건 | 10.388 / 10.274 ms | 808 / 94,080 |

Template의 개수 제한 정리 경로는 이 표본에서 약 12.6% 짧았다. 정리하지 않는 경로는 같은 구현이며 차이를 이득으로 해석하지 않는다. Reply는 0·1건 비용과 전송 횟수·할당량이 늘었다. 100건은 앞선 표본에서 약 10.17→10.86ms로 느려지기도 했으므로 속도 개선을 주장하지 않는다. 정책을 Go로 옮기는 비용을 공개하고, 운영 RTT·부하·처리량은 측정하지 않았다.

### 검증과 유지 범위

- PostgreSQL/race 전체 통과: bot durability/runtime, collector joblease, shared repository. 잠금 대기 회귀 시험은 추가 보강 뒤 다시 실행했다.
- 통과: 세 runtime build, 변경 3개 패키지 golangci-lint(0 issues)·NilAway·staticcheck, SQL 소유권, stack DB 접근 정책. 테스트 helper의 context 전달까지 정리하고 정적 검사를 재실행했다.
- Collector 및 Reply의 새 행 잠금은 같은 호출/트랜잭션 안에서 해제한다. 새 background 작업·운영자 승인 경로·DB 권한은 추가하지 않았다.
- Event의 동시 upsert와 Template의 같은 body 저장 시각은 기존 계약을 유지한다. Template는 no-op 쓰기 생략 대신 왕복 감소를 구현했다. Reply Claim과 Collector Acquire의 후보 선택·시간 유효성·slot 결정도 유지했다.
- 앞서 명시한 금지된 Iris 경로를 읽는 공통 검사는 실행하지 않았다. 공통 retry/reissue 기간·세대 상수와 DB 권한은 변경하지 않았다. 운영에는 아직 적용하지 않았다.

## 2026-10-05 4차 확장: Schedule 저장과 상태 통합 경계

### 구현 범위

- `persistScheduleDecision`의 개별 Exec를 기존 `dbx.ExecStatements`로 묶었다. 인자 구성과 전송은 최대 128개 item/session 단위이며, 모든 item 다음 모든 session이라는 기존 순서를 유지한다. 두 종류를 한 전송에 함께 담을 수 있어 1 item + 1 session도 한 배치다. SQL 문자열은 시작 시 적재한다.
- 빈 channel session은 기존처럼 저장하지 않으며 head를 만들지 않는다. 중복 키를 제거하거나 한 bulk INSERT로 합치지 않는다. 같은 시각의 중복이 최초 값을 유지하는 순차 문장 동작과 기존 SQL의 관측 시각 비교를 보존한다.
- Schedule 상태 조회는 필요한 session의 8개 열만 잠가 읽는다. 기존에는 16개 session 열, 19개 head 열, 11개 pending 열을 읽었다. 요청한 session이 모두 있으면 조회는 3회에서 1회가 된다. Session이 없는 영상만 head의 ID/상태를 추가 조회하며, head만 존재하는 LIVE/ENDED 등의 기존 해석을 유지한다. 누적 schedule item 전체는 계속 읽지 않는다.
- 쓰기 SQL과 커밋 경계는 같다. 쓰기/WAL 자체가 감소했다고 주장하지 않는다. Fallback delta: none. 의존성·권한·스키마·런타임 주기·배포 변경은 없다.
- 배치화로 production 호출처가 사라진 일반 단건 session 저장 래퍼는 제거했다. 기존 SQL 회귀 시험은 같은 session statement를 실행하며, Premiere 분류 전용 단건 저장은 유지한다.

### 동작·경합 검증

- 실제 PostgreSQL에서 129개 item과 session, 배치 경계를 넘는 같은 키의 중복, 빈 channel, NULL title, 빈 협업자 배열, metadata_only origin과 head 미생성을 확인했다.
- 260개 입력의 뒤쪽 item 또는 session에서 실패시켜 앞선 배치와 두 테이블 모두 롤백됨을 확인했다. 실패 원인이 제거된 다음 호출은 전체를 저장한다.
- 기존 session의 행 잠금은 상태 조회부터 저장까지 유지한다. 사용하지 않는 head가 다른 트랜잭션에 잠겨 있어도 기존 session의 Schedule 조회는 진행한다. Session 없는 head의 상태는 별도 시험으로 보존을 확인했다.
- Schedule이 빈 상태를 읽은 직후 다른 writer가 LIVE 또는 ENDED를 INSERT하고 커밋하지 않는 경합을 만들었다. 실제 `pg_blocking_pids`로 배치의 unique-key 대기를 확인한 뒤 선행 writer를 커밋했다. Schedule은 새 상태·더 최신 title/예정 시각·is_premiere·observed origin·last_seen_at을 보존했다.

### Live·Schedule 공통 상태 결정의 후속 설계

이번 경합 시험은 아직 없는 행에 대한 `SELECT FOR UPDATE`만으로 신규 생성을 직렬화할 수 없음을 보여 준다. 현재 일반 session UPSERT의 충돌 병합은 이 경우에도 상태와 시각을 보호한다. 따라서 해당 CASE를 단순한 최종값 UPDATE로 교체하지 않았다.

| 쓰기 진입점 | 현재 읽기/잠금 | 함께 통합해야 할 계약 |
| --- | --- | --- |
| Live snapshot | subject advisory → session → head → pending/absence | 채널 범위와 명시적 영상 집합, 신규 생성, ENDED 우선, LIVE의 UPCOMING 역행 방지 |
| Schedule | schedule group advisory → 요청 session → 없는 session의 head | metadata_only, 필드별 관측 시각, liveness/head 미소유, 같은 영상 중복 순서 |
| Content Premiere | content 저장 → live subject advisory → session | 기존 행에서는 알려지지 않은 is_premiere만 채움, 새 행은 전체 초기값 |
| Video live check | 요청 session → head → pending | canonical session 없는 영상은 생성하지 않음, 가용성·수명 근거의 동일 트랜잭션 |
| End finalizer | due 후보 조회 → session → head → pending | 잠금 뒤 due/후보 재확인, consumer와 같은 잠금 순서 |

구현 순서는 다음과 같이 구체화했다.

1. 신규 session을 생성할 수 있는 Live/Schedule/Premiere가 명시적 video ID의 공통 키 잠금을 **session 행을 읽기 전에** 획득하게 한다. 여러 키는 중복을 제거하고 동일 순서로 획득하며, 잠금 키 충돌까지 고려해 실제 잠금 키 순서를 정한다. 모든 행/키 잠금의 획득 순서를 점검하고 종료 확정·영상 확인 경로와도 교착하지 않는지 검증한다. 채널 범위의 기존 행은 정렬된 행 잠금으로 보호하며, 채널 조회 중 새로 등장하는 영상의 스냅샷 포함 규칙도 명시해야 한다.
2. 잠긴 DB 스냅샷과 관측 입력을 분리한 session 전용 Go 병합 함수를 둔다. Nullable title, 미제공 필드, 필드별 관측 시각, 기존 channel 유지, Premiere 분류 전용 변경, SQL의 no-op 동작까지 다룬다. 범용 상태 머신 프레임워크는 추가하지 않는다.
3. 상태·필드 변경 결정은 Go로 보내고 SQL에는 INSERT/조건부 UPDATE, 최종 동시성 조건, 트랜잭션을 남긴다. Schedule의 부분 상태를 전체 행으로 덮어쓰지 않으며, 반복 video ID의 변경은 입력 순서대로 반영한다. 혼재한 이전 writer나 잠금 규약 밖 writer에 대한 처리까지 결정하기 전에 기존 conflict 보호를 제거하지 않는다.
4. 기존 행/없는 행, 서로 반대인 입력 순서, 동일·과거 관측 시각, 종료 확정·Premiere 동시 처리, 취소/오류 롤백을 검증하고 추가 조회·잠금의 비용을 다시 측정한다. 이번 변경은 이 전면 이관의 완료를 의미하지 않는다.

### 비용과 최종 검증

PostgreSQL 18.6의 격리 Docker DB에서 변경 전 Schedule 함수와 변경 후 함수를 비교했다. 현재의 동일한 session/item 쓰기 SQL을 사용하며, 트랜잭션 시작 → 그룹 잠금 → 상태 조회 → reducer → 저장 → 커밋을 측정했다. Payload decode, observation claim/finalize와 notification 작업은 범위 밖이다. 같은 연결에서 기존 행과 문장을 두 번 warmup한 후 매번 더 새 관측 시각으로 실제 갱신한다. 각 표는 3개 표본의 중간값이다.

| 항목 수 | 로컬 기존 / 변경(ms) | 전송당 1ms 추가 기존 / 변경(ms) | DB 전송 기존 / 변경 |
| --- | ---: | ---: | ---: |
| 0 | 0.198 / 0.182 | 3.512 / 3.536 | 3 / 3 |
| 1 | 1.121 / 0.691 | 10.049 / 6.380 | 8 / 5 |
| 64 | 23.798 / 13.862 | 176.140 / 19.997 | 134 / 5 |
| 129 | 47.509 / 27.964 | 348.359 / 37.153 | 264 / 7 |
| 1,000 | 383.977 / 200.309 | 2,656.471 / 228.334 | 2,006 / 20 |

- 로컬은 표본당 30회, 1ms 추가 시험은 10회다. 연결의 `Write` 호출 횟수를 직접 센다. 1ms는 테스트 전용 연결 래퍼가 전송마다 추가한 고정 대기이며, 운영 RTT나 실제 네트워크를 조작/측정한 결과가 아니다.
- 로컬 1,000항목은 약 47.8% 짧았고, Go 할당은 7,961,494 → 6,657,555 byte/호출이었다. 빈 입력은 실행 경로가 같아 작은 차이를 개선으로 해석하지 않는다. 배치 인자 메모리는 제한하지만 전체 입력·reducer 상태가 상수 공간이라는 뜻은 아니다.
- 같은 group에 같은 관측을 넣는 4-worker/4-connection 경합도 30회씩 3개 표본으로 측정했다. 129항목의 **4개 트랜잭션 완료 시간**은 93.204 → 44.556ms, 이를 4로 나눈 값은 23.301 → 11.139ms였다. 이 값은 개별 요청 지연의 p95/p99가 아니다. 중복 관측이므로 session 재저장은 없으며, 관측당 전송은 135 → 6회였다. 1항목의 4개 완료 시간은 3.290 → 1.791ms였다.
- 기존 비교용 함수는 임시 파일에서 실행 후 제거했다. 현재 구현의 `BenchmarkScheduleReconcile`과 `BenchmarkScheduleReconcileContended`는 남겨 같은 데이터 규모·통신 대기·경합 시나리오를 재현할 수 있게 했다.

실제 검증:

- `env -u TEST_DATABASE_URL go test -race ./hololive/hololive-api/internal/youtube/sourceobservation ./hololive/hololive-api/internal/youtube/reconcile/... ./hololive/hololive-api/internal/planes/youtube/runtime -count=1` 통과. Source observation 48.626s, runtime 46.572s, 모든 reducer 통과.
- 단건 저장 래퍼 제거와 테스트 helper 정리 뒤 Schedule·Live no-op/metadata·Premiere·lifecycle 회귀를 race로 재실행해 통과(11.662s).
- `go build ./hololive/hololive-api/...` 통과. 변경 패키지의 최종 golangci-lint 0 issues, NilAway, staticcheck 통과.
- SQL 소유권 검사, stack DB 접근 정책 검사 통과. Diff 검토와 임시 비교 코드 제거를 확인했다.
- 운영 호출 빈도·운영 DB·운영 catalog는 조회하지 않았다. 이전에 명시한 Iris 금지 경로를 읽는 공통 검사는 실행하지 않았다. 상태 병합 전면 Go 이관은 위 후속 설계 범위이며 이번에 완료한 것으로 표시하지 않는다. 운영 적용과 Git 발행은 하지 않았다.

## 2026-10-05 적대적 리뷰 결함 해소와 발행 준비

리뷰에서 기존 구현부터 남아 있던 lease 만료 결함 두 경로를 실제 PostgreSQL에서 재현했다. Source observation의 일반 Retry·시도 소진 Retry·DeadLetter 세 경우는 행을 변경하지 않는 `SELECT FOR UPDATE` 대기 중 lease가 만료돼도 성공했다. Worker의 `RouteFailures`도 같은 상황에서 만료된 행을 DLQ로 보냈다. 기존 observation 시험은 잠근 행을 UPDATE하여 PostgreSQL의 행 버전 재평가를 일으켰으므로 변경 없는 잠금 반례를 놓쳤다.

- Observation의 세 SQL은 `locked`에서 행을 먼저 잠근 뒤 `timed`에서 DB 시각을 한 번 읽는다. 그 시각으로 lease 유효성, 재시도 available_at, dead_lettered_at, updated_at을 판정·기록한다. Token/attempt/status fence와 Go의 상태 결정은 유지하며 추가 왕복은 없다.
- Worker 실패 배치는 ID 순서로 대상 행을 잠근다. `locked`를 끝까지 소비한 뒤 한 번 읽은 시각으로 만료를 검사한다. 뒤쪽 행의 잠금을 기다리는 동안 앞쪽 행의 lease가 만료되는 경우도 보호한다. 만료 행은 변경하지 않고 유효한 나머지 행만 반영하는 기존 `PartialTransitionError` 계약을 유지한다. 발송 후 확정 결과를 정산하는 `RouteSendingFailures`의 만료 허용 계약은 바꾸지 않는다.
- 새 회귀 시험은 observation 3경로, worker retry/DLQ 각각 단건과 뒤쪽 행 대기의 4경로를 실제 DB·race로 확인한다. 원본 checkout의 집중 시험은 두 패키지 모두 통과했다. Fallback delta: none.
- 원격 main의 `f999eb4a9`에는 `264_youtube_live_review_materialized_snapshot.sql`이 이미 추가됐다. 릴리스 worktree는 이 main을 기준으로 만들었고, 미적용 수동 재발급 수정은 `265_bot_manual_replay_lock_clock.sql`(manifest 순번 126)로 옮겼다. 기존 264와 그 catalog snapshot을 보존했다. 위 이력의 264 표기는 당시 미발행 파일명이다.
- 별도 작업의 `docs/current/services/hololive-api.md` 수정과 다른 저장소의 미커밋 변경은 발행 범위에서 제외했다. 빌드/검증용 shared-go와 iris-client-go는 각각 go.mod가 고정한 v2.8.0, v3.0.4의 깨끗한 worktree를 사용한다.
- 사용자 요청은 결함 해소, 누적 구현 커밋·원격 푸시, 라이브 적용까지 포함한다. 중앙 API·worker·collector c 및 AP a/b/d의 검증된 artifact와 migration 265를 적용 대상으로 준비한다. DB 함수 signature/권한과 payload 형식은 유지하며 운영 데이터 재처리·삭제는 포함하지 않는다.
- 최신 main 통합 뒤 lease 회귀/race, 전체 migration schema snapshot, migration manifest, SQL 소유권, 변경 두 패키지 golangci-lint(0 issues)가 통과했다. 배포 전 필수 topology·Compose service·AP version·native deploy·systemd Compose 검사도 통과했다. 전체 publication gate는 원격 push 시 저장소 hook에서 수행한다.
- 운영 DB는 `default_transaction_read_only=on`과 `SHOW transaction_read_only=on`을 확인한 세션으로 최근 migration 파일명 5개만 조회했다. 운영에는 기존 264가 적용돼 있고 265는 아직 없다. 중앙은 arm64, Osaka a/d는 amd64 native, Seoul b는 arm64 Compose임을 확인했다. 운영 데이터 내용은 조회하지 않았다.
