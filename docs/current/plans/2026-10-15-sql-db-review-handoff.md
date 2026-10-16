# SQL·DB 리뷰 후속 작업 인계 — 2026-10-15

## 시작점과 승인 경계

**필수 1–5번의 로컬 수정·실제 caller smoke·관련 회귀 검증을 완료했습니다. 운영에는 적용하지 않았으므로 운영 문제는 잔존합니다.** 아래 최초 리뷰 근거는 당시 관측으로 보존하며, 이번 실행 결과와 제한은 문서 마지막에 구분합니다. 이 문서는 운영 runbook을 대체하지 않습니다.

- 작업 루트: `/home/kapu/work/iris-stack/hololive-bot`. API·collector·worker·shared·현재 스키마를 4개 읽기 전용 에이전트가 검토하고 2개가 독립 반증했습니다. 메인이 운영 읽기와 로컬 SQL 재현을 수행했습니다.
- 최초 리뷰 요청은 리뷰와 인계 문서화까지였습니다. 이후 사용자가 이 문서의 작업 인계와 `/skill:ultracode` 실행을 요청하여 필수 5개 항목의 로컬 구현·검증을 진행했습니다. 운영 DDL·데이터 변경·배포·재시작·원격 쓰기·권한 확대·Git 발행은 포함하지 않았습니다.
- 최초 리뷰에서는 소스·운영 설정·데이터를 변경하지 않았으며 SQL 축소 재현만 실행했습니다. 이번 후속 실행은 전체 migration을 적용한 로컬 PostgreSQL 18.6과 실제 Go caller로 검증했습니다. 두 시점의 검증 범위를 혼동하지 않습니다.
- 검토 당시 운영 ledger는 migration **314까지 적용**되어 있었습니다. Git SHA를 기준으로 고정한 리뷰가 아니므로 다음 세션은 현재 구현·manifest와 해당 경로의 변경을 확인해야 합니다. 줄 번호는 탐색 힌트입니다.
- [10월 14일 최적화 계획](2026-10-14-sql-optimization-and-compression.md)은 구현·운영 적용 완료 기록입니다. 그 작업을 다시 열거나 이미 제거한 인덱스를 복원하지 말고, 이 문서의 새 관측과 후속 항목을 구분하십시오.
- [소유 경계](../PROJECT_MAP.md), [migration 규약](../../../hololive/hololive-api/scripts/migrations/CONVENTIONS.md), [검증 소유권](../../../README.md), 상위 AGENTS와 `/home/kapu/work/iris-stack/.agents/workflows.md`를 따릅니다. 소스 작업은 hololive-bot checkout에서 수행하며 사용자 변경을 보존합니다.
- 기존 migration은 주석까지 수정하지 않았습니다. 구현 당시 `max+1`인 **315_source_retention_bounded_scan.sql**을 manifest 176번에 추가하고 catalog snapshot을 생성했습니다. checksum 원장은 기존 runner가 실제 파일에서 계산하는 계약을 유지합니다.
- 이번 실행은 `ultracode`·`executing-plans`·`use-modern-go`를 적용하고 kapu에서만 빌드·테스트했습니다. 이후 운영 DB 조회는 `stack-platform-ops`, 배포는 `hololive-bot-ops` 및 별도 승인이 필요하며, 이 문서는 권한을 부여하지 않습니다.

이하 경로 약어는 저장소 루트 기준입니다: `A=hololive/hololive-api`, `W=hololive/hololive-alarm-worker`, `C=hololive/hololive-youtube-collector`, `S=hololive/hololive-shared`, `D=hololive/hololive-dbtest`. 모든 핵심 근거를 본문에 넣었으므로 이전 세션의 `local://` 파일이나 에이전트 기록 없이 이어갈 수 있습니다.

## 보존할 관측 근거

### 운영 읽기 — 관측 시점의 사실이지 현재 상태 보장이 아닙니다

대상은 `hololive-seoul` / `100.100.1.8`, `holo-postgres`, DB `hololive`, PG 18.6입니다. 각 SQL 세션에서 `default_transaction_read_only=on`을 설정하고 `SHOW transaction_read_only` 결과 `on`을 확인했습니다. 진단에는 `statement_timeout=5s`, `lock_timeout=1s`를 적용했습니다. 비밀·사용자 메시지·payload 원문을 보존하지 않았습니다.

| 항목 | 관측값과 범위 |
|---|---|
| 전체 DB / source 원본 / applications | 2,045MB / 907MB / 746MB, 초기 크기 단면 |
| source retention 평균 실행 시간 | 9.9917초, 2026-10-15 10:00 KST 조회의 최근 30분 |
| source retention DB 슬롯 평균 대기 | 0.00964초, 같은 Prometheus 구간; DB 내부 row-lock 대기와는 다른 지표 |
| 원본·application 변화 | 2,151.5초간 원본 INSERT 5,225 / DELETE 0, application INSERT 11,394 / DELETE 0 |
| queue 변화 | 같은 구간 DELETE 5,269; 원본 정리 전 단계는 진행됨 |
| 원본 보호 함수 내부 SQL | 별도 2,026.253초간 480,850회, 누적 74,683.29ms, queryid `-623781673164071241`; SQL 형태를 보호 함수와 대조함 |
| 오래된 보호 prefix | `live_snapshot`, 예시 cutoff 7일, 오래된 순 100건: live application 보호 100, queue 보호 0, CURRENT/STAGING 보호 0, ACTIVE lease 보호 4; 보호 사유는 중복 가능 |
| 최근 session HOT | 표 통계 구간의 UPDATE 2,411 중 HOT 2,410 |
| 인덱스·연결 단면 | invalid index 0, DB 연결 26개; peak 포화나 여유를 보장하는 수치는 아님 |

Loki `{host="hololive-seoul"} |= "retention"`의 최근 1시간/최대 30개 제한 조회에서, **09:44·46·48·50·52·54·56·58 KST**에 다음 오류를 확인했습니다.

```text
youtube plane retention failed
retain source observations: ... source_observations:
delete evidence transaction: ... delete source observation evidence:
timeout: context deadline exceeded
rollback: failed to deallocate cached statement(s): conn closed
```

재조회에 사용할 기존 지표는 다음과 같습니다. `increase`의 소수값은 보간 결과이므로 정확한 실패 횟수로 보고하지 않습니다.

```promql
sum by (table) (increase(hololive_youtube_plane_retention_errors_total[30m]))
sum(rate(hololive_youtube_plane_retention_tick_seconds_sum{phase="source"}[30m]))
  / sum(rate(hololive_youtube_plane_retention_tick_seconds_count{phase="source"}[30m]))
sum(rate(hololive_youtube_plane_db_slot_wait_seconds_sum{operation="retention_source"}[30m]))
  / sum(rate(hololive_youtube_plane_db_slot_wait_seconds_count{operation="retention_source"}[30m]))
```

관측 해석 제한:

- `pg_stat_statements` reset은 2026-08-23이었습니다. 누적 순위에는 최근 개선 이전 비용이 섞입니다. 위 구간 차분은 `userid`·`toplevel`별 행을 queryid로 합친 **후** 두 시점을 뺐습니다. queryid만 dictionary key로 덮어쓰면 잘못된 차분이 됩니다. 함수 내부/외부 실행 시간은 겹치므로 합산하지 않습니다.
- 깨끗한 2,026초 구간은 초기 진단 SELECT와 timeout probe 이후입니다. 초기 짧은 구간의 진단 부하는 현재 런타임 비용으로 사용하지 않습니다.
- 별도 production SELECT-only retention 후보 진단은 5초 제한에 걸렸습니다. 이 진단은 실제 삭제 wrapper의 `SET jit=off`와 설정이 달랐으므로 **그 timeout 자체를 실제 삭제 실패의 증거로 사용하지 않습니다**. 운영 실패 근거는 위 로그·지표·삭제 차분입니다.
- 오래된 100건 표본의 7일 cutoff는 코드 기본값을 이용한 진단 조건이며 runtime override 확인을 대신하지 않습니다. 전체 보호율도 아닙니다.

## 실행 순서와 완료 기준

retention을 가장 먼저 진행합니다. cleanup·집계·RSS·due 지표는 retention 구현에 의존하지 않는 독립 작업입니다. 병렬 작업이 승인되면 파일 소유자를 나누되 migration 번호·manifest 변경과 최종 통합 검증은 한 소유자가 조정합니다. 순서가 검증 생략이나 배포 승인을 의미하지는 않습니다.

### 1. P1 — source retention의 실제 timeout과 진행 정체

- [x] 보호 조건을 유지하면서 **검사량과 삭제량이 모두 제한되고 다음 tick에 진행하는** 원본 정리 경로를 구현·검증했습니다.

**근거/진입점:** `A/scripts/migrations/307_source_application_cascade.sql:68–123`의 `source_observation_retention_protected`와 `delete_source_observation_retention_batch`, `A/internal/youtube/sourceobservation/repository_retention.go:248–273,318–341`, `A/internal/planes/youtube/runtime/supervisor_retention.go:59–85,113–124`.

현재 SQL은 kind/time 범위에서 보호 판정을 먼저 하고 LIMIT를 적용합니다. 삭제 개수는 제한하지만 방문 후보 수는 제한하지 않습니다. source 단계는 live singleton guard를 보유하며, 오류 시 뒤의 payload·live absence·checkpoint 단계에 도달하지 못합니다. 앞선 queue 등의 별도 commit은 보존하는 현재 오류·집계 계약을 유지해야 합니다.

**실행한 로컬 재현:** 현재 307의 보호 함수를 그대로 사용한 축소 스키마, `jit=off`, 오래된 원본 11,000건 중 앞 10,000건은 queue로 보호, 뒤 1,000건은 비보호. 같은 후보 필터/정렬의 `EXPLAIN (ANALYZE,BUFFERS,TIMING OFF)` 결과 `Rows Removed by Filter: 10000`, `Heap Fetches: 11000`, 결과 1,000건, 867.409ms였습니다. 운영 시간의 예측값이 아니라 LIMIT 의미를 입증한 결과입니다.

**설계 방향:** 삭제 수와 별도의 candidate scan budget, `(received_at,id)` keyset 진행 및 순환 재방문을 검토합니다. 커서의 수명·rollback·재시작·kind 간 공정성·보호 해제 후 재방문을 명시하십시오. 앞 N개만 잘라 매번 다시 읽는 수정은 뒤쪽 기아를 만듭니다. parent batch가 작아도 application CASCADE 및 SET NULL의 자식 쓰기량은 클 수 있으므로 fan-out 비용을 별도 검증하십시오.

**유지할 불변식:** queue/replay·활성 live·CURRENT/STAGING·ACTIVE lease 보호, live guard→원본 행 잠금 순서, FK/감사 원자성, `BacklogKnown=false`의 unknown 의미, 앞 단계의 확정 삭제량. guard 제거·상태 보호 약화·무제한 timeout/재시도·예외 삼키기로 해결하지 않습니다. 현재 prefix 스캔은 유력한 비용 원인이지만 timeout 전체에서 스캔·FK·row-lock의 기여를 분리하지는 못했습니다.

**완료 기준:** 전체 migration을 적용한 로컬 DB에서 대규모 보호 prefix 뒤의 적격 행을 유한 tick 내 처리하고, prefix 보호 해제 후 앞쪽도 재방문합니다. 모든 행 보호/잠금 경쟁/중간 rollback/재시작에서도 안전하고 후속 정리 단계가 정상 tick에서 실행됩니다. 기존 보호·backlog·부분 commit 회귀를 유지하고 실제 함수 실행/계획으로 비용 상한을 증명합니다. 운영 회복 완료는 별도 승인된 적용 뒤 연속 tick 성공·적격 원본/application 삭제 진행·후속 단계 실행으로 확인합니다. 로컬 성공만으로 운영 해결 완료 처리하지 않습니다.

기존 검증 출발점: `repository_retention_test.go`, `repository_retention_accounting_test.go`, `live_runtime_guard_test.go`, `queue_retention_plan_test.go` 및 `D/source_observation_retention_test.go`. `TestRetentionBacklogDoesNotClaimEmptyBeyondProtectedScanLimit`, `TestRetentionKeepsCommittedCountsOnLaterFailure`, `TestEvidenceRetentionGuardsBeforeCandidateAndPreservesAvailability`는 현재 계약을 확인할 행동 테스트입니다.

### 2. P2 — notification cleanup이 재활성화된 PENDING을 삭제

- [x] 최종 DELETE의 최신 상태·cutoff 재검사와 결정적 두 세션 회귀를 추가했습니다.

**파일:** `W/internal/egress/notificationdelivery/queries/outbox_repository_0279_09.sql:2–10`, `S/pkg/service/delivery/queries/outbox_enqueue_batch_upsert.sql:49–64`, `W/internal/egress/notificationdelivery/store.go:132–164`.

후보 SELECT만 terminal 상태를 검사하고 최종 DELETE는 ID만 검사합니다. 원본 enqueue/cleanup SQL을 사용한 로컬 축소 스키마 재현:

1. 오래된 `FAILED` 행을 준비합니다. 예시 key는 `MEMBER_NEWS_WEEKLY / 2026-W01:review-room`, `created_at=now()-90 days`, payload `{}`입니다.
2. 세션 A에서 실제 batch upsert로 같은 key를 `PENDING`으로 rearm하고 commit을 보류합니다.
3. 세션 B에서 실제 cleanup SQL을 cutoff 30일/batch 1,000으로 실행합니다. `pg_stat_activity`로 B의 Lock wait를 확인한 뒤 A를 commit합니다.
4. 양쪽 정상 종료였으나 B는 `DELETE 1`, 잔여 행 0을 반환했습니다. enqueue 성공 후 미발송 행이 사라집니다.

**노출 조건:** 위 90일 weekly fixture는 SQL 경합을 위한 것이며 정상 scheduler 경로라고 주장하지 않습니다. 실제 월간 알림은 같은 월/방 key로 재실행 가능하므로 retention이 월보다 짧으면 정상 경로에서도 조건이 생길 수 있습니다. 운영 cutoff는 확인하지 않았고 test profile의 7일 값을 운영값으로 취급하지 않습니다. 일반 FAILED는 request exhaustion과 별개여서 rearm이 허용됩니다.

**완료 기준:** 위 경합에서 PENDING이 남고, 실제 terminal·기한 경과 행은 계속 정리됩니다. immutable request payload와 exhausted 정책을 유지합니다. 기존 alarm retention의 최종 predicate 재검사 패턴을 참고하되 unrelated cleanup으로 확장하지 않습니다.

### 3. P2 — YouTube 부모 집계의 stale snapshot 덮어쓰기

- [x] 실제 projector 경로를 포함한 경합 회귀를 확보하고, 최신 자식 집계가 오래된 결과로 역행하지 않도록 수정했습니다.

**파일:** `W/internal/egress/youtubedispatch/store/queries/delivery_repository_aggregate_sync.sql:12–26`, `store/delivery_repository.go`, `claim_manager_pipeline.go`, `claim_manager_revive.go`, `dispatcher.go`, `store/queries/delivery_repository_0373_10.sql`.

단문 UPDATE의 원자성은 FROM 집계의 최신성을 보장하지 않습니다. main completion·background ProjectPending·revive 뒤 Project는 실제 병행 경로입니다. periodic repair는 PENDING 부모만 선택하므로 terminal로 잘못 덮인 부모가 그 경로로 수렴한다고 가정하면 안 됩니다.

**실행한 재현:** 부모 PENDING/자식 FAILED로 시작 → A가 부모 FOR UPDATE → B가 원본 aggregate SQL로 FAILED 집계를 만든 뒤 부모 Lock wait → A가 자식·부모를 SENT로 바꾸고 commit → B 완료. 결과는 `UPDATE 1`, **부모 FAILED / 자식 SENT**, 두 세션 rc=0입니다. 부모/자식 모두 FAILED로 시작하면 no-op predicate 때문에 대기 UPDATE 자체가 생기지 않을 수 있습니다.

**한계/방향:** 위는 직접 상태 전이를 사용한 SQL 메커니즘 재현입니다. 실제 Revive→Claim→Begin/CompleteSent→Project의 전체 경합은 미검증입니다. 부모를 먼저 잠근 transaction의 **다음 statement snapshot**에서 집계하는 방식을 우선 비교하고 lock order를 검증하십시오. 같은 statement CTE에 부모 잠금만 추가해서 fresh snapshot을 얻었다고 판단하지 않습니다.

**완료 기준:** 실제 경로에서 최종 부모가 최신 자식 결과와 수렴하고 `sent_at`·`terminal_at` 의미, mixed-status 우선순위, no-op write 억제, ledger/immutable request 계약을 보존합니다. 이 결함을 delivery SENT ledger 손상이나 실제 중복 발송으로 보고하지 않습니다. 기존 회귀는 `store/delivery_repository_aggregate_test.go`를 재사용합니다.

### 4. P2 — RSS 부분 저장 실패와 공용 timestamp watermark

- [x] 실패를 실행 결과에 보존하고, 미등록 항목을 최신 pub_date만으로 영구 제외하지 않도록 수정했습니다.

**파일:** `A/internal/planes/llm/internal/service/majorevent/scraper/service.go:147–161,230–247,295–313`, `scraper/feed_scheduler.go`, `scraper/types.go`, `majorevent/queries/repository_events_0038_01.sql`, `majorevent/repository_events.go`.

개별 Upsert 오류는 continue 후 최종 nil로 끝납니다. 오래된 A 저장 실패 뒤 최신 B가 저장되면, 다음 실행은 미등록 A도 `A.pub_date < DB latest`라서 제외합니다. news/en-news가 같은 type watermark를 공유하므로 늦게 게재된 다른 feed 항목에도 같은 문제가 있습니다. 정적 경로와 독립 반증으로 확인했으며 scraper 전체 장애 주입은 미실행입니다.

**방향:** 현재 feed의 bounded external_id 집합을 한 번 조회하여 미등록 항목을 판정하고 저장 실패를 결과에 보존하는 방식을 검토합니다. 오류 반환만 추가해도 이미 저장된 B의 watermark 때문에 A가 빠지는 문제는 남습니다. 임의의 새 retry layer를 만들지 말고 기존 scheduler의 실패 계약을 유지하십시오.

**완료 기준:** A 저장 실패/B 성공 뒤 다음 실행에서 A가 저장되고 B가 중복되지 않습니다. 서로 다른 feed의 늦은 미등록 ID, 전체/부분 저장 실패, 이미 알려진 ID를 검증합니다. timestamp를 유실 가능한 배제 기준으로 쓰는 기존 구현 고정 테스트가 있다면 재고정하지 않습니다. `scraper/service_test.go`의 실제 수집 fixture를 확장합니다.

### 5. P2 — cadence 단축 시 due 지표 과소표시

- [x] 관측 SQL의 IDLE due 계산을 실제 scheduler와 일치시켰습니다.

**파일:** `A/internal/planes/youtube/runtime/queries/collection_target_observability.sql:21–25`, `C/internal/runtime/joblease/queries/repository_candidates_0144_02.sql:22–25,38–42`, `A/internal/planes/youtube/runtime/collection_metrics.go`.

scheduler는 `LEAST(next_due_at, scheduled_for + current_interval)`인데 지표는 `next_due_at`만 사용합니다. 10분→1분 단축, 마지막 slot 2분 경과 조건의 로컬 SQL 결과는 `metric_due=false`, `collector_due=true`였습니다. bundle 갱신 지연이 아니라 식의 의미 차이입니다.

**완료 기준:** 실제 CURRENT target/IDLE lease fixture로 candidate와 지표의 due/oldest_due_age가 일치합니다. cadence 연장, ACTIVE/DEFERRED, future eligible_at, expired projection의 기존 의미도 보존합니다. 수집 누락 자체를 고쳤다고 보고하지 않습니다.

## 별도 조건부 후속 항목 — 운영 장애로 승격하지 않음

필수 조치와 혼합하지 말고 해당 변경이 승인되거나 소비자 영향이 확인될 때 범위를 정합니다.

| 항목 / 파일 | 이미 확인한 근거 | 다음 검증·안전 조건 |
|---|---|---|
| Alarm claim stale takeover — `W/internal/service/alarm/dispatchoutbox/queries/repository_claim_0053_02.sql:39–54` | B의 오래된 statement snapshot을 test-only advisory barrier로 멈추고 A claim commit 뒤 진행하면 같은 delivery가 반환되고 owner가 A→B로 바뀜 | configured_workers=1 및 단일 worker가 현재 계약이므로 현행 동시 claimant 장애 아님. 최종 상태 fence를 검토하되 send-unit 전체 원자성을 깨는 일부 행 claim 금지 |
| 발송 진입 transaction clock — 같은 디렉터리 `queries/send_request_members.sql`, `queries/repository_transitions_0020_01.sql`, `send_request.go:220–263` | lease 잔여 1초로 BEGIN→1.2초 지연 후 실제 유효=false인데 membership=true/UPDATE 1/sending 진입. NOW()가 BEGIN 시각 사용 | 실제 BeginSendRequest의 unit lock 대기로 재현 필요. 잠금 획득 뒤 새 statement에서 clock_timestamp를 얻어 일관된 판정·시각 기록. 일반 lease/attempt 예산의 노출 조건과 중복 egress는 미확인 |
| JSON/JSONB 크기 경계 — `S/pkg/contracts/sourceobservation/canonical_json.go:44–75`, `C/internal/runtime/sourceobservation/repository_publish_prepare.go`, `D/testdata/schema_snapshot.golden.sql` payload CHECK | compact 1,048,576바이트가 JSONB text 1,048,577바이트로 늘어 Go 길이 조건은 true/DB 조건은 false | 유효 helper response 10개 post·helper wire≤1MiB·PrepareEnvelope 성공 조건을 함께 만족하는 실제 PublishBatch fixture 필요. canonical/storage 계약 통일 또는 정확한 사전 검증·오류 분류; 임의 한도 축소/원자성 완화 금지. cursor에는 기존 크기 여유 계산이 있어 중복 finding 아님 |
| rollback 전 삭제 metric — `W/internal/egress/alarmdispatch/alarm_dispatch_maintenance.go:161–187,208–229` | commit 전 counter 증가; 후속 실패로 rollback되어도 성공 삭제 수가 남는 정적 경로 | DB 상태가 아니라 관측 정확성 문제. commit 이후 확정 수를 기록하는 행동 회귀 검토 |
| panic cleanup — `W/internal/egress/youtubedispatch/store/transition_store.go:574–604` | callback panic 때 deferred rollback이 없는 구조 | 정상 입력 panic·실제 pool 누수 미확인. 보강 시 bounded/cancel-independent rollback과 panic 재전파, commit adjudication 유지 |
| publish/renew 자기 경합 — `C/internal/runtime/sourceobservation/queries/repository_publish_fence_0001_01.sql`, `joblease/run.go` | publish와 renew가 같은 lease 행 경합 가능 | publish timeout>renew timeout인 비기본 허용 설정 문제. 기본 동일 timeout에서 조기 취소를 단정하거나 renew 실패를 무시하지 않음 |
| 다중 구독 writer·telemetry stale owner·frozen group batch 축소·AP clock skew | 각각 다중 프로세스/설정 축소/인계 간격보다 큰 시계차 전제 | 현재 topology·설정 변경·실제 시계 문제 증거 없음. 확정 장애 목록에서 제외하고 해당 전제 변경 때 평가 |

크기/cadence 표현식 재현은 다음과 같습니다. **폐기 가능한 로컬 DB 전용**이며 typed publish나 실제 scheduler 경로 검증을 대체하지 않습니다.

```sql
WITH x AS (SELECT '{"v":"' || repeat('a', 1048568) || '"}' AS raw)
SELECT octet_length(raw), octet_length(raw::jsonb::text) FROM x;
-- 관측: 1048576 | 1048577
WITH x AS (
  SELECT statement_timestamp()-interval '2 minutes' AS scheduled_for,
         statement_timestamp()+interval '8 minutes' AS next_due_at,
         60000::bigint AS interval_ms
)
SELECT next_due_at<=statement_timestamp() AS metric_due,
       LEAST(next_due_at, scheduled_for+interval_ms*interval '1 millisecond')
         <=statement_timestamp() AS collector_due FROM x;
-- 관측: false | true
```

## 추가 최적화 후보와 하지 않을 변경

- **이번 실행 제외:** 비용이 확인된 최적화만 이후 별도 범위로 선택합니다. 아래 목록 및 앞의 조건부 후속 항목은 구현하지 않았으며 이번 요청에서 추가 최적화 전체의 승인을 추론하지 않았습니다.

| 후보 | 근거·판단 | 검증 방향 |
|---|---|---|
| 수집 후보 반복 집계 | 깨끗한 2,026초 구간 48,622회 / 평균 0.539ms, queryid `-3663127880975060423` | CURRENT 대상 수·custom/generic plan·actual rows/buffers. 313 bundle에는 max cadence/priority/membership 정보가 부족하므로 단순 대체 금지 |
| live 동기 상태 refresh | 같은 구간 10,773회 / 평균 2.498ms, queryid `-2166792207454570236` | 같은 영상의 중복 계산·의미 없는 UPDATE를 분리 측정. guard·RR snapshot·FK SET NULL·동기 정합성 보존; 내부/외부 시간을 더해 병목을 과장하지 않음 |
| 보조 이력 보존 | content absence 약 30MB, photo variants 약 7MB, profile evidence 약 5.8MB; reconciliation conflicts도 source FK SET NULL 및 독립 cleanup 부재 | late/replay/audit 보존 하한을 먼저 정함. content absence의 `(scheduled_for=$3 OR effective_at>$4)` access path도 함께 비교. 원본과 무조건 CASCADE 금지 |
| queue autovacuum | reloptions 없음, global .2/50; 초기 dead estimate 32,827, 후속 6,559로 실제 vacuum 동작 | queue만의 write/claim buffers/vacuum 비용 비교. global 하향·fillfactor만으로 indexed 상태 전이 HOT 문제를 해결한다고 주장하지 않음 |
| queue/terminal outbox 정리 | queue terminal cutoff와 updated_at 정렬, YouTube outbox terminal_at keyset의 인덱스 정렬 차이 | 큰 만료 backlog fixture/custom·generic 계획에서 확인 후에만 추가 index 판단. 현재 작은 표에 일괄 추가하지 않음 |
| 왕복 축소 | 방별 enqueue, 한 구독 변경 시 방 전체 조회, room cache miss 시 전체 N-upsert | 실제 fanout/cardinality/cache miss 빈도 우선. 부분 성공·immutable snapshot 계약 유지 |

**유지/제외:** applications의 `(observation_id,entity_kind,entity_key) WHERE observation_id IS NOT NULL`는 FK equality lookup을 지원하므로 같은 prefix 인덱스를 추가하지 않습니다. head의 복합 FK도 video_id PK로 최대 한 행에 좁혀져 자동 누락-index screening의 오탐입니다. 최근 history_sort_at/HOT 최적화·binary hash·LZ4·payload dictionary·307 CASCADE는 되돌리지 않습니다. CURRENT 전체 원본 보호는 장기 불변 세대에서 축적될 수 있지만 의도된 보호 계약이며 현재 무한 누적을 입증한 것은 아닙니다. 작은 replay/checkpoint/contract-FK 표의 추가 인덱스, application 감사 ID 제거, 단순 singleton 분할, `max_connections` 증대, VACUUM FULL/REINDEX는 현재 우선 처방이 아닙니다. 동일 template 저장의 updated_at touch는 의도일 수 있어 버그에서 제외했습니다.

## 검증·운영 확인과 다음 세션 종료 조건

- 실제 migration을 적용한 기존 dbtest fixture를 재사용합니다. `schema_snapshot.golden.sql`은 catalog 직렬화 문서이며 실행 가능한 DDL이 아닙니다. synthetic SQL 재현은 전체 schema·trigger·role·Go caller 검증을 대신하지 않습니다.
- 오류/경합은 sleep만으로 우연을 기다리지 말고 잠금 대기·barrier를 확인해 failing-before/passing-after 행동 회귀로 남깁니다. 원본 SQL/실제 caller를 실행하는 smoke도 수행합니다. 새 테스트는 사용자 결과·상태 전이·보호·진행성·경계를 검증해야 합니다. SQL/문서 문자열, wiring, checker 자체를 검증하는 테스트는 추가하지 않습니다.
- 아래는 **향후 변경에 따라 선택할 명령이며 이 리뷰에서 실행·통과한 명령이 아닙니다.** 저장소 루트에서 수행하고 현재 README 및 모듈 구성을 우선합니다.

```bash
go test ./hololive/hololive-api/internal/youtube/sourceobservation ./hololive/hololive-dbtest
go test ./hololive/hololive-alarm-worker/internal/egress/notificationdelivery ./hololive/hololive-shared/pkg/service/delivery
go test ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store
go test ./hololive/hololive-api/internal/planes/llm/internal/service/majorevent/scraper
go test ./hololive/hololive-api/internal/planes/youtube/runtime ./hololive/hololive-youtube-collector/internal/runtime/joblease
```

- 관련 race/NilAway/빌드·DB 접근 정책·retry/retention 계약 검사는 변경 범위와 저장소 규칙에 맞게 수행합니다. 스택 DB 정책 검사는 스택 루트의 `bash tools/checks/check-stack-db-access-policy.sh`가 소유합니다. publication이 승인된 경우에만 해당 pre-push gate/릴리스 절차를 적용합니다.
- 운영 재측정은 먼저 현재 소유 runbook/DB skill에서 host를 확인하고 읽기 전용 가드를 증명합니다. 기존 인증은 내부 사용만 허용하며 credential/env/payload 덤프를 남기지 않습니다. 운영 `EXPLAIN ANALYZE DELETE/UPDATE`, rollback을 핑계로 한 운영 쓰기, 통계 reset, query cancel/terminate, 임의 청소는 하지 않습니다.
- retention 정상화 여부는 새 시점의 로그·실행/슬롯 대기·부모/자식 삭제 차분·후속 정리 단계로 판단합니다. 이 문서의 수치를 재조회 결과처럼 재사용하지 않습니다. 사용자 보고 실패를 단순 재확인하려고 같은 실패 검사를 반복하지 않습니다.
- [x] 승인된 필수 5개 항목의 구현·실제 smoke·관련 회귀 결과를 아래에 기록했습니다. 운영 적용/발행은 하지 않았으며 새 registry나 proof gate는 만들지 않았습니다.
- [x] 현재 API·worker 서비스 계약과 CHANGELOG를 갱신하고 일회성 Go 프로그램·SQL·수동 smoke DB 컨테이너를 제거했습니다. **로컬 수정 완료 / 운영 문제 잔존**입니다.

## 후속 구현·검증 결과 — 2026-10-15

메인이 retention·migration·최종 검증을 맡고 구현 에이전트 4개가 독립 항목을 처리했습니다. 별도 reviewer 2개가 보호/진행/권한 및 나머지 SQL 정확성을 반증 검토했으며 차단 finding은 없었습니다. 리뷰 결과를 테스트 실행으로 대체하지 않고 메인이 아래 검증을 직접 수행했습니다. 기존 `docs/current/README.md` 변경은 건드리지 않았고 Git commit/push/tag도 하지 않았습니다.

최종 diff에서 `go.work.sum`의 `/go.mod` checksum 17개 추가를 발견했습니다. 이 작업에서 의존성 버전을 바꾸거나 해당 파일을 직접 편집하지 않았고, 변경 주체를 확정할 수 없어 보존했습니다. SQL 수정의 의존성 업그레이드로 취급하지 않습니다.

### 구현과 행동 증거

| 항목 | 구현 | 실제 검증 |
|---|---|---|
| retention | 315의 DB singleton에 `(received_at,id)` 커서·고정 순회 상한을 저장합니다. 최대 16개 kind별 1,000개 후보 반환 후 전역 최대 1,000개 ID를 materialize하고 그 안에서만 보호/잠금을 검사합니다. 삭제는 요청 한도 이내이며 cursor와 함께 commit/rollback합니다. 함수 내부 live guard가 모든 source 잠금보다 앞섭니다. | 전체 migration DB에서 보호 10,000개+적격 1,000개를 실제 `RunRetentionTick`으로 실행: 첫 10 tick 삭제 0, 11번째 1,000개 삭제. 보호 해제 후 10 tick에 앞쪽 10,000개 삭제. 매 tick 새 Repository를 만들어 진행 수명이 메모리 객체와 무관함을 확인했습니다. |
| cleanup | 최종 DELETE에도 terminal 상태와 cutoff predicate를 적용합니다. | 원본 producer upsert를 미커밋한 채 실제 `Store.Cleanup`을 실행하고 `pg_blocking_pids`로 대기 확인 후 commit: PENDING 보존. immutable/exhausted·created_at/sent_at 갱신·실제 terminal 삭제 회귀 통과. |
| YouTube 집계 | 부모 ID 오름차순 `FOR NO KEY UPDATE` 후 별도 READ COMMITTED statement에서 집계합니다. 기존 집계 SQL·시각·no-op 억제는 유지합니다. | 실제 Project/ProjectPending 대기 중 Revive→Claim→Prepare→Freeze→Begin→CompleteSent→Project를 실행하는 두 세션 회귀 통과. 부모/자식/ledger SENT, 최신 sent_at/terminal_at, frozen request 보존. 별도 `go run` lifecycle smoke도 통과했으며 외부 전송은 하지 않았습니다. |
| RSS | 현재 feed ID를 `IncrementalLimit` chunk로 조회하고 timestamp 배제를 제거했습니다. `FailedEvents`와 joined error로 저장 실패를 보존합니다. | 실제 HTTP fixture→FeedFetcher→RSSParser→repository→PostgreSQL smoke에서 A CHECK 실패/B 성공, 다음 실행 A 저장/B skip, 세 번째 둘 다 skip. 다른 feed의 늦은 ID·전체 실패·known ID·기존 scheduler retry 회귀 통과. |
| due 관측 | IDLE에 scheduler와 같은 `LEAST(next_due_at, scheduled_for + current_interval)`을 사용합니다. | CURRENT target/lease 및 bundle trigger fixture에서 10분→1분 단축: 실제 candidate 1개, metric due 1, oldest_due_age 약 60초. 연장·ACTIVE/DEFERRED·future eligibility·만료 projection 회귀 통과. |

retention의 추가 검증:

- `TestSourceRetentionBoundsProtectionChecksAndRollsBackCursor`: 원본 11,000개(보호 prefix 10,000개)에서 실제 함수의 `pg_stat_xact_user_functions`로 최초 보호 검사 **1,000회**만 실행하고 rollback 뒤 cursor가 복구됨을 확인했습니다.
- `TestSourceRetentionBoundsLockedWindowAndRevisits`, `TestSourceRetentionFixedSweepRevisitsDespiteNewTail`: 잠긴 앞 1,000개를 무한히 훑지 않고 다음 창으로 진행하며, 잠금 해제와 신규 tail 유입 뒤에도 앞쪽을 다시 방문합니다.
- `TestRetentionProtectedWindowContinuesLaterStepsAndRestart`: 전부 보호된 창에서도 payload·live absence·checkpoint 각 1개 정리, backlog unknown, 다음 Repository에서 적격 tail 삭제를 확인했습니다.
- 기존 보호 7개 분기·global delete limit·부분 commit accounting·guard-before-row·availability/FK rollback 회귀를 유지했습니다. 이전 쿼리 복제 기반 plan 테스트와 SQL 문자열 wiring 테스트는 삭제하고 실제 함수 진행/상한/경합 회귀로 대체했습니다.
- 실제 함수의 nested `auto_explain`에서 원본 PK scan의 `Rows Removed by Filter=1000`을 확인했습니다. 첫 함수 141.067ms, 별도 backlog 조회 132.609ms였으며 이 fixture의 전체 보호 tick은 약 257–277ms, 적격 1,000개 삭제 tick은 약 731ms였습니다. 운영 지연의 예측값이 아닙니다.
- 별도 실제 `EXPLAIN (ANALYZE,BUFFERS,TIMING OFF)`에서 원본 1개에 application 10,000개(CASCADE), reconciliation conflict 10,000개(SET NULL), availability 1개(SET NULL)를 연결했습니다. 함수 실행 407.719ms, shared hit 103,667; 원본/application 0개, conflict 10,000개와 availability 1개는 source=NULL로 보존됐습니다. application 10,000개 rollback 원자성도 영구 회귀로 검증했습니다.
- 로컬 역할 probe에서 과도한 cursor-table 권한을 부여한 뒤 315를 재실행해 runtime/scraper 직접 접근 제거, 기존 runtime EXECUTE 보존, cursor 유지와 runtime 역할의 실제 함수 호출을 확인했습니다. 운영 권한을 변경한 것은 아닙니다.

### 실행한 검증

전체 영향 패키지 race 테스트:

```bash
env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  go test -race -p 2 -count=1 \
  ./hololive/hololive-api/internal/youtube/sourceobservation \
  ./hololive/hololive-dbtest \
  ./hololive/hololive-alarm-worker/internal/egress/notificationdelivery \
  ./hololive/hololive-shared/pkg/service/delivery \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch \
  ./hololive/hololive-api/internal/planes/llm/internal/service/majorevent \
  ./hololive/hololive-api/internal/planes/llm/internal/service/majorevent/scraper \
  ./hololive/hololive-api/internal/planes/youtube/runtime \
  ./hololive/hololive-youtube-collector/internal/runtime/joblease
```

- 위 **10개 패키지 전부 통과**. 이후 테스트 helper/단언 정리 뒤 변경된 5개 패키지의 해당 경합·RSS·cadence 회귀를 `-race -count=1`로 다시 실행해 통과했습니다.
- `SCHEMA_SNAPSHOT_UPDATE=1`의 `TestSchemaSnapshotGolden` 생성 성공 후 위 dbtest 전체 race에서 update 없이 snapshot 비교도 통과했습니다.
- 변경된 8개 패키지에 저장소 고정 golangci-lint **0 issues**, patched NilAway 및 patched staticcheck 통과. 마지막 테스트 helper 변경 뒤 lint/NilAway도 다시 통과했습니다. 전체 `local-ci.sh`나 publication gate를 실행했다는 뜻은 아닙니다.
- `go build ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/... ./hololive/hololive-shared/...` 통과.
- `bash scripts/architecture/check-migration-manifest.sh`, 스택 루트의 `bash tools/checks/check-stack-db-access-policy.sh`, `bash tools/checks/check-stack-retry-contract.sh` 통과.
- 검증 중 발견한 잘못된 checkpoint fixture hash와 RSS 통합 테스트 import cycle은 수정했습니다. lint의 shadowing·복잡도·서식 지적도 gate 우회 없이 해소했습니다.
- API/worker `go run` smoke, SQL fan-out/ACL probe는 모두 폐기 가능한 로컬 PostgreSQL 18.6에서 실행했습니다. 프로그램·SQL·수동 컨테이너는 제거했습니다. testcontainers는 기존 dbtest lifecycle을 사용했습니다.

### 적용 전제와 남은 한계

1. **운영 회복은 미확인입니다.** 이번 세션은 운영 접속/변경/배포/재시작/발행을 하지 않았습니다. 별도 승인 뒤 소유 runbook으로 migration **315를 신규 API보다 먼저** 적용해야 합니다. worker의 cleanup/집계 수정도 별도 배포가 필요합니다.
2. 후보 ID/보호 호출 수와 부모 삭제 수는 제한했으나 물리 MVCC/index 방문량, 개별 보호 함수의 자식 조회량, 잠금 대기, 자식 CASCADE/SET NULL 쓰기량의 절대 상한은 아닙니다. parent 1개가 대형 fan-out이면 여전히 시간 예산을 소비할 수 있습니다. timeout/재시도/보호를 완화하지 않았습니다.
3. 운영 완료 판정은 적용 뒤 연속 tick 성공, 적격 원본/application 삭제 진행, 후속 정리 단계 실행을 새 로그·지표·차분으로 확인해야 합니다. 이 문서의 최초 운영 관측이나 로컬 수치를 새 운영 증거로 재사용하지 않습니다.
4. RSS 복구는 다음에도 feed에 포함된 ID 범위입니다. 이미 feed에서 사라진 항목을 복구하는 durable replay나 새 retry 계층은 추가하지 않았습니다.
5. 조건부 후속 항목·추가 최적화는 이번 실행 범위 밖입니다. **Fallback delta: none** — 기존 scheduler의 실패 경로를 연결했으며 새 fallback/자동 재시도 정책은 없습니다.

## 적대적 추가 검토와 개선 — 2026-10-15

사용자의 후속 요청으로 위 5개 구현의 미검증 경합·실패·정책 변경·비용 경계를 검토했습니다. 기존 통과 결과를 재실행만 해서 근거로 삼지 않고 실제 caller와 전체 migration DB의 새 시나리오를 실행했습니다. 이번에도 운영 접속/변경/배포/발행은 하지 않았습니다.

발굴 3개·독립 반증 3개의 리뷰 작업을 모두 종료했습니다. cleanup 조기 종료는 앞선 구현의 P2 회귀로 확인했고, RSS는 기존 실패 처리의 복구 기회 손실로 확인해 각각 수정했습니다. 다른 후보는 아래처럼 근거와 계약에 따라 기각하거나 한계로 남겼습니다.

### 확정한 회귀: cleanup의 삭제 수 기반 조기 종료

- **앞선 최종 DELETE 재검사와 기존 반복문의 불일치:** `cleanupInBatches`는 실제 삭제 수가 batch보다 작으면 종료했습니다. 재활성화된 행을 올바르게 보존하면 삭제 수만 작아지므로, 후보 창 뒤에 만료 행이 남아 있어도 성공 종료했습니다. PENDING 유실이 아니라 만료 행 정리 지연입니다.
- 실제 shared producer로 1,001개를 적재하고 오래된 FAILED로 만든 뒤, 첫 행의 실제 upsert를 미커밋 상태로 유지했습니다. 공개 `Store.Cleanup`의 Lock wait를 확인하고 producer를 commit하자 수정 전 `deleted=999 pending=1 expired_terminal=1`이었습니다.
- SQL은 `picked AS MATERIALIZED`와 guarded `deleted` CTE에서 후보 수와 삭제 수를 별도로 반환합니다. Go는 후보 수가 batch 미만일 때만 종료하고 실제 삭제 수만 누적합니다. 기존 최신 상태/cutoff 재검사·immutable/exhausted 정책·yield·context를 유지합니다.
- 같은 실제 caller smoke의 수정 후 결과는 `deleted=1000 pending=1 expired_terminal=0`입니다.
- `TestCleanupContinuesPastRearmedFullBatch`는 작은 batch 1/원본 2개에서 잠금 대기를 확인하여, 삭제 0인 가득 찬 창 뒤의 terminal 행도 같은 호출에서 삭제함을 검증합니다. 수정 전 기대 삭제 1/실제 0으로 실패했고 수정 후 통과했습니다.
- `TestCleanupPreservesCommittedCountOnLaterBatchFailure`는 둘째 배치의 실제 DELETE trigger 오류에서 첫 배치의 확정 삭제 수 1과 남은 행을 검증합니다.
- `TestCleanupCancellationPreservesCommittedBatches`는 첫 2개 삭제 commit 뒤 셋째 행의 Lock wait에서 취소하고, 확인된 삭제 수 2와 남은 원본을 검증합니다. 최초 테스트는 client의 취소 반환 직후 server도 멈췄다고 가정해 실패했습니다. PostgreSQL backend 중단을 관측한 뒤 holder를 해제하는 barrier로 수정했습니다. client 취소/transport 오류만으로 마지막 statement rollback을 단정하거나 추가 retry를 도입하지 않습니다.

### 확정한 개선: 부분 성공 retry가 남은 복구 기회를 제거

- **기존 잠재 결함, 앞선 SQL 수정이 새로 만든 결함은 아님:** `Scrape`는 일부 feed 실패/다른 feed 성공을 결과에 담고 nil 오류를 반환합니다. `FeedScheduler.executeCycle`은 이를 완전한 복구로 해석해 기존 잔여 예약도 취소했습니다.
- 실제 HTTP→RSS parser→PostgreSQL repository→scheduler로 A 저장 CHECK 실패/B 저장 성공을 만들면 retry 2개가 예약됩니다. 첫 retry에서 A feed만 HTTP 503이고 B는 이미 알려진 항목이면, 수정 전 `before_retries=2 after_partial_retry=0 missing_a=1 saved_b=1`이었습니다. 실패를 재현하는 영구 회귀도 수정 전에 기대 잔여 시각과 실제 nil의 불일치로 실패했습니다.
- `feed_scheduler.go`에서 **retry이고 `FeedsFailed>0`인 경우만** 기존 실패 처리기로 보내 남아 있는 원래 예약을 보존합니다. `Scrape` API·regular 부분 성공·전체 성공 시 취소·횟수·시각·timeout은 유지합니다. 새로운 attempt를 만들거나 실패할 때 예산을 재설정하지 않습니다.
- 동일한 실제 caller smoke의 수정 후 결과는 `before_retries=2 after_partial_retry=1 missing_a=1 saved_b=1`입니다. 실패 항목을 이미 복구했다고 주장하지 않고 기존 다음 기회를 보존합니다.
- `TestFeedSchedulerPartialRetryPreservesRemainingRecovery`: regular 부분 성공에는 새 retry 없음 → 저장 실패로 2개 예약 → 첫 retry 부분 실패 뒤 정확한 마지막 시각 보존 → 마지막 retry에서 A 저장/B skip을 검증합니다.
- `TestFeedSchedulerRetryCompletionAndExhaustion`: 첫 retry 완전 성공이 비어 있지 않은 남은 예약을 취소하고, 연속 부분 실패는 원래 예산을 소진하며 재예약하지 않음을 각각 검증합니다.

### retention 반증·비용 및 보강

- 실제 함수에서 보호 prefix 1,000개 후 cursor=1000/upper=20000을 만든 뒤 cutoff를 엄격하게 바꾸면 삭제 0/cursor 초기화, 다시 완화·보호 해제하면 앞쪽 1,000개를 처리했습니다.
- 새 `TestSourceRetentionRevisitsAfterPolicyChangeAndMissingHorizon`은 cutoff/활성 kind 변경, 순회 상한 원본의 삭제, cursor 앞에 늦게 들어온 다른 kind의 원본을 함께 다룹니다. 적격 1,001개를 유한 tick에 처리하고 cutoff 안으로 들어오지 않은 최신 원본은 보존합니다.
- 로컬 advisory barrier로 최종 DELETE를 멈춘 뒤 실제 collector `Acquire`가 새 CURRENT generation 999의 lease를 획득하고 삭제를 재개하면, RETIRED generation 998 원본 1개 삭제와 ACTIVE lease가 함께 관측됐습니다. 두 작업은 겹치므로 retention→Acquire 순서로 해석 가능하며 새 video-live lease가 그 퇴역 원본을 요구한다는 근거가 없습니다. 독립 반증에서도 새 결함으로 채택하지 않았고 전역/lease 잠금은 추가하지 않았습니다. 이를 수집 유실이나 운영 장애로 보고하지 않습니다.
- source `received_at`/kind의 관리자 동시 UPDATE 및 다른 하한/차원의 SQL array는 실제 immutable writer/정상 Go slice 계약 밖입니다. 그 입력만으로 신규 결함이나 권한 우회를 주장하지 않았습니다.
- 20,000개 적격 원본에서 `BatchSize=1`은 보호 함수 1,001회/삭제 1개, 1,000 배치는 보호 함수 2,000회/삭제 1,000개였습니다. 작은 배치에도 1,000개 후보 창을 검사하는 비용은 실제로 남지만 상한 위반은 아닙니다.
- 별도 warm nested plan은 후보 보호/잠금 194.109ms, DELETE 382.065ms, 전체 함수 585.016ms였습니다. 앞선 cold full-batch 호출은 2,390.289ms여서 같은 시간을 운영 성능으로 일반화하지 않습니다. 함수 내부/외부 시간을 중복 합산하지 않았습니다. 정렬·공정성을 planner 순서에 의존시키거나 행별 루프로 바꾸는 최적화는 채택하지 않았습니다.

### 추가 실행 검증

- scraper·majorevent 전체 `go test -race -p 2 -count=1` 통과. 마지막 병렬 subtest 정리 뒤 scraper 전체 race도 다시 통과.
- dbtest `go test -race -count=1 -run '^TestSourceRetention'` 통과.
- scraper·dbtest 범위 golangci-lint 0 issues, patched NilAway·staticcheck 통과.
- API 전체 `go build ./hololive/hololive-api/...` 및 스택 retry 계약 검사 통과.
- notificationdelivery·shared producer 전체 `go test -race -p 2 -count=1` 통과. notificationdelivery lint 0 issues, patched NilAway·staticcheck, worker 전체 build 및 스택 DB 접근 정책 검사 통과.
- 마지막 취소 barrier 보정 뒤 `-race -run '^TestCleanup'`, notificationdelivery lint/NilAway/staticcheck도 다시 통과했습니다. 일회성 Go adapter/프로그램·SQL/계획 로그·수동 PostgreSQL 컨테이너는 모두 제거했습니다.
- 새 migration이나 runtime dependency는 추가하지 않았습니다. **Fallback delta: none. Retry delta: 기존 잔여 예약의 조기 취소만 방지; 새 횟수/시각/예산 없음.**

## 범위 확장 조사와 추가 개선안 — 2026-10-15

이번 후속 요청의 범위는 **수집·저장·발송·조회 전반의 조사와 개선안 확정**입니다. 아래 항목은 구현 완료 목록이 아닙니다. 앞선 로컬 수정은 보존하며 운영 DDL·데이터 변경·배포·발행은 하지 않습니다. 앞선 구현 단계와 달리 이번 확장 조사에서는 보호된 운영 읽기와 새로운 로컬 실제 caller 재현을 수행했습니다.

### 새 운영 관측과 최적화 판단

- **2026-10-15 13:22 KST 관측:** Seoul PG 18.6 primary, DB 2,203,760,319바이트, 적용 ledger는 여전히 314까지였습니다. 따라서 배포된 retention 비용을 로컬 315의 성능으로 해석하지 않습니다. 각 세션의 read-only `on` 확인, statement timeout 5초·lock timeout 1초를 유지했고 업무 행·메시지·payload 원문은 조회·보존하지 않았습니다.
- CURRENT target 588개, YouTubeJS bundle 446개, lease ACTIVE 4/IDLE 451개였습니다. queue는 PENDING 6/PROCESSED 214,331개, notification outbox는 비어 있고 YouTube delivery는 SENT 65개였습니다. 연결 24/max 60·lock waiter 0은 단면일 뿐 용량 보장이 아닙니다.
- 아래는 같은 **336.239초** 구간의 `pg_stat_statements` 차분입니다. `queryid+toplevel`을 구분하고 사라진/새로 생긴/reset 그룹은 비교에서 제외했습니다. 실행 시간은 DB executor 시간이며 CPU 사용률이나 client latency가 아닙니다.

| 경로 | 호출 수 | 평균 실행 ms | 초당 executor ms |
|---|---:|---:|---:|
| subject 후보 조회 | 8,064 | 0.560 | 13.427 |
| global 후보 조회 | 5,376 | 0.159 | 2.535 |
| source publish set | 762 | 3.069 | 6.956 |
| live refresh 내부 함수 | 1,894 | 2.508 | 14.128 |

내부 함수 시간을 enclosing 문장에 더하지 않습니다. publish set의 반환 관측은 840개, 호출당 약 1.10개였습니다. 현재 일반 작업을 1,024행 최대 배치 benchmark로 대표하지 않습니다. Prometheus의 projection refresh DB-slot 대기 평균 약 230ms도 **애플리케이션 입장 대기**이며 PostgreSQL row-lock 대기라고 단정하지 않습니다.

후보 조회는 합산 약 40회/초지만 즉시 재설계할 근거는 부족했습니다. 전체 migration 315의 로컬 PG에 CURRENT 588개·retained 276,361개·current subject lease 446개를 넣고 실제 SQL을 `PREPARE/EXECUTE`했습니다. `custom_plans=9`, `generic_plans=9`를 확인했고, 세 번째 warm 실행은 다음과 같았습니다.

| subject job | custom 실행 ms | generic 실행 ms | 양쪽 shared hit | 양쪽 target 실제 행 |
|---|---:|---:|---:|---:|
| channel_live | 0.384 | 0.407 | 27 | 119 |
| channel_metadata | 0.455 | 0.500 | 263 | 238 |
| content | 0.352 | 0.335 | 33 | 44 |

모두 `youtube_collection_targets_pkey`를 사용했고 target의 `Rows Removed by Filter=0`, shared read=0이었습니다. 이 fixture에서 퇴역 세대 전체 스캔은 없었습니다. 이는 모든 lease 상태·membership 함수 내부 generic plan·운영 지연의 증명은 아닙니다. **313 bundle 단순 대체와 후보 조회용 새 인덱스는 채택하지 않습니다.** 313에는 max cadence·priority·membership 증명 및 collector ACL 계약이 부족합니다.

### 확정한 개선 방향과 착수 순서

| 항목 | 우선순위 / 항목 | 변경 범위와 결정 |
|---|---|---|
| N1 | P1 — 발송 진입의 잠금 후 lease 시계 | unit와 전체 member 잠금을 모두 얻은 뒤 DB wall clock 한 번을 채취해 만료 판정·sending 시각·새 lease 만료에 공통 사용합니다. |
| N6 | P1 — YouTube의 확정된 발송 결과를 취소 후에도 제한적으로 저장 | provider 호출은 취소 가능하게 두고, 알려진 결과의 DB 확정·기존 read-back만 하나의 bounded cancellation-independent context에서 수행합니다. shutdown 예산 결정이 선행합니다. |
| N2 | P2 — publish 결과에서 DB 오류 원인 보존 | 정상 iteration 종료 뒤 `rows.Err()`를 completeness 검사보다 먼저 처리합니다. 실제 Scan/행 불변식 오류의 우선순위는 유지합니다. |
| N3 | P2 — RSS 복구된 실패의 집계·원인 보존 | 예약한 source마다 결과를 남기고 guard 반환 오류를 callback 밖에서 기록합니다. 전체 source 실패의 typed cause를 보존하되 regular 부분 성공의 nil 계약은 유지합니다. |
| N7 | P2 — 기존 alarm metric의 rollback 과대계상·분기 누락 | retention은 commit 확인 뒤 기존 7개 label을 기록하고, Consumer의 immutable MarkSending 실패도 기존 operation counter에 한 번 반영합니다. 새 metric·retry는 추가하지 않습니다. |
| N4 | P2 — payload의 canonical/JSONB 크기 계약 일치 | 기존 canonical/raw 상한과 DB JSONB-text 1MiB를 모두 유지하는 방향입니다. payload 경계의 검증·오류 코드/분류 계약을 먼저 확정해야 하며 임의 상한 축소나 DB 한도 증가는 하지 않습니다. |
| N5 | P2 — 방 카탈로그의 겹친 전체 refresh 합치기 | 같은 Catalog에서 진행 중인 전체 HTTP 조회와 기존 per-room 저장 순회만 공유합니다. waiter별 취소와 bounded owner context를 분리하고 지속적 negative cache는 추가하지 않습니다. |

N1–N3과 N7은 기존 시간·실패·metric 계약의 보정입니다. N4는 공유 payload/error 소유 계약, N5는 전체 refresh의 기존 호출 예산에 맞는 owner 수명, N6은 기존 process shutdown 여유 안의 finalization 예산 결정이 구현 전제입니다. **착수 우선순위는 N1·N6, 다음 N2·N3, N7, N4·N5**로 둡니다. N4의 오류 분류보다 N2의 원인 보존을 먼저 처리합니다. 이번 조사가 새 오류 정책·timeout 값·운영 변경을 승인하지는 않습니다.

#### N1 — 실제 `BeginSendRequest`의 만료 후 성공

- 위치: `W/internal/service/alarm/dispatchoutbox/send_request.go:58–75,126–160,220–263`, `queries/send_request_members.sql`, `queries/repository_transitions_0020_01.sql`.
- 실제 `InsertBatch → ClaimDue(1분) → PinSendRequest → BeginSendRequest`를 호출했습니다. fixture의 잔여 lease만 10초/1초로 조정하고 별도 transaction에서 실제 send-unit 행을 잠갔습니다. `pg_blocking_pids`로 공개 method의 대기를 확인했습니다.
- 10초 control은 만료 전 정상 진입했습니다. 1초 case는 **이전 lease가 실제 만료된 뒤** blocker를 풀어도 오류 nil·상태 `sending`이었고, `sending_started_at`은 약 1.057초 전 transaction 시작 시각이었습니다. 외부 egress·다른 owner 인계·중복 발송은 실행하거나 입증하지 않았습니다.
- `statement_timestamp()`로만 바꾸거나 member row를 읽는 도중 clock을 채취하면 뒤쪽 member 잠금 대기 동안 앞쪽 lease가 만료되는 경우를 놓칩니다. 전체 lock set 뒤의 단일 시각이어야 합니다.
- 공통 leased fence의 **Load/Pin**도 대상입니다. **Reissue의 sending 상태는 의도적으로 lease 만료를 면제**하므로 이를 강화하지 않습니다. 같은 transition SQL을 쓰는 `repository_transitions.go:23–42`의 **MarkSending** 호출 인자·시계와 기존 partial-row 계약도 함께 다뤄야 합니다.
- acceptance: unit 대기의 만료 전/후, 마지막 member 대기 중 앞 member 만료, 하나라도 실패하면 전체 sending 전이 0, 정상 호출의 잠금 후 timestamp/정확한 연장, owner/전체 IDs/request ID/generation fence, 만료된 sending의 적법한 Reissue, 실패 뒤 provider 호출 없음.
- 기존 downstream 처리도 유지합니다. Load/Pin fence 실패는 기존 Quarantine, Begin 실패는 기존 same-attempt `RequeuePreSend` 경로이므로 “만료 거절은 항상 recovery에 맡긴다”로 바꾸지 않습니다. 이미 post-lock clock 회귀가 있는 **RouteFailures는 새 결함이 아닙니다.**

#### N2·N4 — 유효 typed 입력의 실제 DB 거절과 오류 가림

- 위치: `C/internal/runtime/sourceobservation/repository_publish_batch.go:147–193`, `repository_publish_prepare.go:100–111`, `S/pkg/contracts/sourceobservation/canonical_json.go`, `A/scripts/migrations/238_source_observation_payload_prepare.sql:10–12`.
- 합성 10-post/1-page 응답을 **실제 로컬 HTTP → `youtubejs.NewRPC.FetchCommunity` → CommunityRunner → PrepareEnvelope/Validate → 전체 PublishBatch**로 실행했습니다. 실제 RPC의 body budget·Content-Type·protocol/success 검사를 통과했습니다. 각 post text는 최대 98,384바이트이고 HTTPS thumbnail URL은 각 2,048바이트 미만입니다.
- 관측: wire **1,048,200**, canonical **1,048,544**, JSONB text **1,048,995**바이트, 상한 **1,048,576**. Go envelope는 유효하지만 pgx tracer는 `23514 / source_observation_payloads_payload_check`를 기록했습니다. 공개 caller에서는 `*pgconn.PgError`가 보존되지 않고 **`incomplete set result`**만 반환됐습니다.
- `collectPublishSetRows`가 `Next()==false` 뒤 completeness를 먼저 확인하여 caller의 `rows.Err()`까지 도달하지 못합니다. 수정은 정상 stream 종료의 오류 확인을 앞으로 옮기는 것이며, 앞서 난 Scan/duplicate ordinal 등의 오류를 나중의 drain 오류로 덮는 방식은 아닙니다.
- DB 원자성은 유지됐습니다: payload·observation·queue·checkpoint 모두 0, lease ACTIVE. text를 2,048바이트 줄인 control은 네 표에 각 1개 저장되고 lease IDLE로 완료됐습니다.
- 합성 경계 입력에 대한 실제 collector 경로 증명입니다. **Node helper의 실제 mapper/paginator·upstream 발생 빈도나 운영 장애는 입증하지 않았습니다.**
- N2 acceptance: 0행/일부 행/기대 행 뒤 stream 실패, 깨끗한 누락 결과, 중복·범위 밖 ordinal, Scan 실패, 정상 재정렬 결과, 실제 CHECK/취소의 `errors.Is/As`, collision/checkpoint/terminal 원자성.
- N4는 `ErrInvalidEnvelope`를 단순 재사용하면 안 됩니다. `collectorruntime/publisher.go:238–250`가 이를 명시적 INTERNAL로 바꾸며 scheduler는 이를 fatal로 보고합니다. **`publish_rejected/RESOURCE_LIMIT`는 허용된 durable tuple이 아닙니다.** 기존 `response_too_large/RESOURCE_LIMIT`를 저장 크기 진단에 사용할지도 소유 계약에서 결정해야 합니다. 기존 resource-limit 경로는 bounded DEFER이며 retry 제거를 뜻하지 않습니다.
- N4 acceptance: PostgreSQL과 정확한 상한/상한+1 parity, 중첩·빈 container·다국어·escape·canonical 정수, 기존 accepted bytes/digest 불변, 혼합 배치 전체 rollback, duplicate/collision 불변, 유효 code/class tuple와 의도한 fatal/nonfatal 처리. scope/cursor까지 바꾸는 전역 canonicalizer 수정·punctuation의 단순 문자 수 계산·임의 slack·payload 절단은 제외합니다.

#### N3 — RSS의 숨은 실패

- 위치: `A/internal/planes/llm/internal/service/majorevent/scraper/service.go:43–49,113–235`, `feed_scheduler.go:153–181`.
- 실제 HTTP/parser/PG repository 경로에서 lookup panic을 주입하면 panicguard는 복구하지만 결과 append가 건너뛰어 **`FeedsAttempted=0, FeedsFailed=0, error=nil`**이었습니다. 취소된 실제 repository lookup만 있는 경우에는 attempted/failed=1이지만 전체 실패 오류의 `errors.Is(context.Canceled)=false`였습니다. 자연 발생 운영 panic을 주장하지 않습니다.
- source/stage를 가진 wrapped cause를 보존하고 예약 source별 결과를 보장합니다. 기존 errgroup 취소 동작·동시성 제한·정렬/dedup·성공한 개별 저장은 유지합니다. source 오류를 항상 join해 반환하면 regular 부분 성공도 새 retry를 예약하므로 그렇게 하지 않습니다.
- acceptance: 전부 panic/혼합 panic의 정확한 source 수, 취소·deadline·typed DB cause, 건강한 source 저장, regular 부분 성공에 새 retry 없음, 부분 retry의 원래 잔여 예약만 유지, 전체 회복 시 취소, 원래 예산 소진. `FailedEvents`를 조회 실패 항목 수로 바꾸지 않습니다.

#### N5 — 방 카탈로그의 조회·왕복 증폭

- 위치: `S/pkg/service/kakaoroom/catalog.go:43–136`, `queries/upsert.sql`. 실제 Catalog·HTTP lister·PG store에서 동일한 미등록 방의 겹친 miss를 barrier로 만들었습니다.
- 방 100개에서 단일 miss는 HTTP 1/SQL 100회, 동시 miss 20개는 **HTTP 20/SQL 2,000회**였습니다. 두 번째 경우 DB 저장값이 같아 **변경 행 0/WAL 0**이었으므로 물리 쓰기 증폭이라고 보고하지 않습니다. 다음 응답에 방을 추가하면 독립 조회 1회로 즉시 발견됐습니다.
- whole-refresh를 방 ID별이 아니라 Catalog 단위로 합치되 완료 뒤의 새 miss는 다시 조회할 수 있어야 합니다. HTTP만 공유하고 저장 순회를 반복하는 안은 제외합니다. owner의 상한은 HTTP와 DB 작업 전체를 덮어야 합니다.
- acceptance: 겹친 miss는 HTTP 1회·저장 순회 1회, leader/follower 취소가 건강한 waiter에 전파되지 않음, owner timeout 뒤 새 refresh 가능, 실패 뒤 후속 재시도 가능, 뒤늦게 추가된 방 발견, DB lookup 오류 시 HTTP fallback 없음. 메모리가 DB보다 먼저 갱신되므로 “메모리가 같으면 Observe 저장 생략”은 금지합니다. webhook의 명시적 방 metadata 우선순위와 unknown의 plain 처리도 유지합니다.
- 운영 unknown-miss 빈도는 측정하지 않았습니다. 개선 효과가 있는 입력은 입증했지만 상시 운영 병목으로 단정하지 않습니다.

#### N6 — provider 성공을 확인한 뒤 취소되면 DB 확정도 중단

- 위치: `W/internal/egress/youtubedispatch/send_engine_send.go:235–251,372–389,529–539`, `send_engine_request.go:165–186`, `send_engine_lifecycle.go:76–118`, `store/transition_complete.go:17–95`, `store/transition_adjudication.go`.
- 실제 PG store의 `ClaimPending/PrepareClaimed`와 **실제 per-room SendEngine의 FreezeRequest/BeginSending/CompleteSent**를 일회성 adapter로 호출했습니다. 로컬 PreparedMessageSender가 성공을 반환한 뒤, engine이 CompleteSent에 들어가는 지점에서 원래 parent만 취소하고 실제 store에 위임했습니다. 취소된 store를 단독 호출한 실험이나 가짜 DB 응답이 아닙니다.
- control은 provider 1회·apply `applied`·delivery/ledger `SENT`·보고 성공 1이었습니다. 취소 case는 provider 1회였지만 apply `indeterminate`·delivery `SENDING`·ledger 없음·보고 성공 0이었고, 실제 BEGIN과 primary read-back 모두 `context canceled`였습니다. 호출이 끝난 뒤 독립 context로 DB를 확인했습니다.
- 이는 합성 성공 경계의 engine 재현이며 실제 Iris 발송·운영 shutdown 유실·중복 발송은 입증하지 않았습니다. 전체 `Dispatcher.Start`, grouped/frozen/fallback 및 COMMIT 뒤 응답 유실은 이번 smoke에서 실행하지 않았습니다.
- **상태 확정만** engine의 known-outcome 경계에서 분리합니다. fallback provider 호출까지 포함한 handler 전체를 detach하거나 public store 전체가 caller 취소를 무시하도록 바꾸지 않습니다. 하나의 예산 안에서 기존 adjudication과 `transitionOperationRetryLimit=1`을 유지하며, query마다 deadline을 재설정하지 않습니다.
- 일반 `DeliverySendTimeout`은 child context이므로 이 parent 취소 결함과 구분합니다. provider 호출 뒤 raw context 오류는 의도된 unknown 결과이며, 이를 “확정 미발송”으로 바꾸거나 새 send를 만들지 않습니다. 일반 room 실패만으로 errgroup sibling이 취소된다고도 주장하지 않습니다.
- acceptance: per-room/grouped/frozen/기존 fallback의 확정 성공·확정 실패 뒤 parent 취소, actual COMMIT 후 응답 유실의 동일 post-state 확인, 하나의 bounded shutdown-compatible 예산, provider 1회 및 불명 결과에 추가 발송 없음, frozen ID/body/route/membership·row version·generation/attempt/backoff·delivery/tracking/telemetry/ledger 원자성 보존. 이미 bounded state context가 있는 alarm/notification 경로에는 같은 결함을 보고하지 않습니다.

#### N7 — 기존 alarm metric 두 경로의 정합성

- **rollback 전 삭제 계상:** `W/internal/egress/alarmdispatch/alarm_dispatch_maintenance.go:135–220,313–326`에서 retention의 sent/dlq/quarantined/cancelled/send_unit/event와 내부 upcoming_candidates metric은 같은 transaction이 commit되기 전에 증가합니다.
- 실제 producer·PG maintenance `RunOnce`에서 SENT 삭제 뒤 send-unit 삭제 trigger가 실패하도록 했습니다. 결과는 **원본 1개 보존·오류 반환인데 sent counter +1**이었습니다. trigger를 제거하고 정상 실행하자 원본은 삭제됐지만 동일한 durable 삭제 1개에 counter 합계는 2가 됐습니다. 다른 6개 label은 별도로 fault-injection하지 않았으며 같은 transaction 안의 기록 위치를 소스로 확인했습니다.
- 7개 count를 sweep 결과로 모으고 commit이 확인된 뒤 기록합니다. 특히 `DeleteOrphanEvents` 내부 upcoming_candidates의 metric 소유도 바깥으로 옮겨야 합니다. advisory lock 미획득 성공은 count 0, COMMIT 응답 불명은 확정 count를 만들지 않습니다. metric을 맞추려고 삭제를 재시도하지 않습니다. commit 직후 process 종료의 과소계상까지 막는 durable 감사 원장으로 확대하지 않습니다.
- **immutable 실패 분기 누락:** `W/internal/service/alarm/dispatchoutbox/consumer.go:210–229`의 실제 `BeginSendRequest` 분기는 기존 `alarm_dispatch_pg_mark_sending_failed_total`을 증가시키지 않습니다. 실제 InsertBatch/Consumer.DrainBatch/Pin 뒤 owner를 다르게 한 MarkSending은 **fence 오류·등록된 counter 증가 0**이었고, owner를 복원한 정상 control도 오류 nil·증가 0이었습니다.
- Consumer operation 경계에서 실패당 한 번 기록하며 기존 metric 이름/label·error chain·빈 입력/정상 성공은 유지합니다. 이 probe는 Consumer 자체만 호출했고 runner의 provider 호출 방지까지 검증한 것은 아닙니다.
- acceptance: 모든 retention 후속 phase의 rollback/timeout에 7개 label 증가 0, 정상 commit의 실제 count, lock-skipped no-op, 불명 commit 처리; immutable/fallback MarkSending 실패 각각 +1, 빈 입력/성공 0. retention/발송의 원자성·cutoff·횟수·재시도 계약은 변경하지 않습니다.

### 추가 후보의 확정된 처분

아래는 누락된 작업이 아니라 **선행 측정·소유 계약 없이는 구현하지 않기로 결정한 후보**입니다. 정적 work shape와 운영 병목을 구분하며 임의의 절감률·DB 크기 비율을 새 gate로 만들지 않습니다.

| 후보 / 위치 | 처분과 다음 판정 기준 |
|---|---|
| live 동기 refresh의 의미 없는 입력 변경 — migration 312, review source 값의 310 | **316으로 채택.** 아래 후속 실행 결과에 문장별 동등성·실제 writer·비용 비교를 기록했습니다. 소비·reviewability·review source 입력이 같은 UPDATE의 key만 제외하며 guard·stale snapshot 거절·동기 visibility를 유지합니다. 운영 workload 비율·전체 절감률은 미확인입니다. |
| non-live Finalize도 live singleton을 획득 — `A/internal/youtube/sourceobservation/repository_finalize.go` | **구조 변경 보류.** 현재 callback은 observation kind와 다른 live 효과도 합법적으로 쓸 수 있습니다. 실측 guard 대기가 기존 예산을 침해하고 callback 효과 범위를 명시적으로 좁히기 전에는 kind만 보고 잠금을 빼거나 claim 뒤로 옮기지 않습니다. |
| source parent 삭제의 child fanout·보호 진단 비용 — 307/315, `repository_retention_backlog.go` | **post315 실측 후.** 부모 batch 상한은 CASCADE/SET NULL 총량 상한이 아닙니다. Finalize 1회의 application 상한을 observation 평생의 자식 상한으로 오해하지 않습니다. 실제 다음 적격 창의 자식 분포·보호/진단/WAL·guard 시간을 분리 측정하며 기존 보호와 committed-stage accounting을 보존합니다. |
| 오래된 source가 CURRENT/STAGING·ACTIVE·queue/replay/head에 보호됨 | **의도된 정책 유지.** 겹치는 보호 이유별 age/bytes와 replay/audit 필요부터 확인합니다. 강제 generation 교체, lease 만료만으로 보호 해제, 최신 감사 ID만 남기기는 승인 없는 정책 변경입니다. |
| payload GC의 낮은 ID 재방문·최근 tail | **조건부 공정성/비용 측정.** source 생성률을 unique payload 유입이나 실제 GC 처리량으로 대체하지 않습니다. 성공한 tick·실제 적격 유입·wrap 지연·reference/skip 비율·tail buffers를 확인하고 1시간 유예·KEY SHARE/FK·새 snapshot 재검사를 유지합니다. |
| queue 만료 검색·vacuum | **현 상태에서 설정/인덱스 변경 안 함.** PROCESSED 전체가 만료 backlog는 아닙니다. 적격 backlog와 prepared plan·sort/temp·write/claim 비용, heap/index/TOAST·old snapshot·실제 vacuum 영향을 먼저 측정합니다. HOT 0만으로 fillfactor/global vacuum 처방을 내리지 않습니다. |
| projection retention | **누락된 drain 구현 아님.** 이미 최대 64회 별도 commit/tick입니다. 반복 lease 조회·guard 대기와 실제 유입/처리량을 확인하되 reasons-before-targets·CURRENT/STAGING/참조 보호·부분 commit 수 집계를 유지합니다. |
| content absence/profile/photo/conflict 보조 증거 | **보존 정책 선결.** source FK SET NULL 뒤 남는 수명 차이는 확인했지만 leak으로 확정하지 않습니다. growth/age/NULL-reference 비율과 content absence의 same-slot/late-positive OR 조회를 측정합니다. audit/replay/late-arrival 하한 없이 새 expiry/CASCADE를 적용하지 않습니다. |
| pending upsert/no-op·pending observation ID lookup | **실제 잔여 비용 확인 후.** SQL의 persisted-field 비교와 content clock의 no-op 회피는 이미 있습니다. pending의 observation-only 보호 조회는 FK 누락 문제가 아닙니다. 현행 pending cardinality와 post315 계획 없이 인덱스를 추가하지 않습니다. |
| snapshot/renew/empty complete의 순차 DB 왕복·membership generic plan | **측정 후보.** 후보 SQL의 generic 결과가 membership 함수 내부까지 증명하지 않습니다. 기존 순서의 statement pipeline 효과를 따로 측정하고 잠금 대기 뒤 새 snapshot·실패 우선순위·batch drain·terminal rollback을 유지합니다. |
| publish와 자신의 renew 경합 | **실제 전체 transaction overlap 재현 전 보류.** timeout을 늘리거나 renew를 무시하지 않습니다. 정상 1–2개 envelope에서 lease 잔여 시간·renew timeout·publish lock-held 시간을 대조해야 합니다. |
| 구독 add/remove의 전체 방 조회·ClearRoom의 채널별 SQL/cache 왕복 | **작업량 확인, 성능 순위 미확정.** 실제 room/channel cardinality를 측정하고 기존 identity/HostID/default type 및 다른 방의 구독을 보존하는 point lookup·bounded existence batch만 비교합니다. subscriber read-through refill은 unsubscribe 재생성 위험 때문에 기각합니다. |
| 방별 enqueue·member-news membership/name N+1 | **부분 성공/시야 계약 선결.** bulk는 room별 성공·FailedRooms/period marker·중간 구독 변경의 의미를 바꿀 수 있습니다. 실제 fanout/왕복 비용을 먼저 확인하고 무조건 전체 batch/전역 negative cache로 치환하지 않습니다. |
| alarm orphan 검색·notification room-head claim·YouTube terminal 정리·admin list | **backlog fixture로 검증 후.** LIMIT은 부적격 parent/선행 room 행 검사량을 제한하지 않습니다. 현재 작은 outbox에 일괄 인덱스를 추가하지 않고 sparse/dense·generic/custom·첫/깊은 cursor·잠긴 head·ledger 보호를 실제 쿼리로 비교합니다. |
| history topic/enrichment·live ended-head 진단 | **조회 계약 보존 조건부.** sparse topic와 retained-ended 규모별 비용을 측정합니다. LIMIT을 topic 필터 앞으로 옮기거나 진단을 active-only로 축소하지 않습니다. history의 caller 취소와 자체 timeout의 Truncated, live의 단일 snapshot을 보존합니다. |
| RSS 후속 chunk 실패의 이미 분류된 prefix 저장 | **별도 부분 성공 정책.** chunk 2/새 항목 5개에서 둘째 조회를 실패시키면 앞의 2개도 저장되지 않았고, 다음 정상 cycle에서는 5개 모두 복구됐습니다. 기본 chunk 200 초과 feed 노출·영구 유실은 미확인입니다. failed source가 Parsed/Skipped/Stored에 기여할지, 모든 source가 실패했지만 prefix가 있는 경우의 의미부터 정해야 합니다. |
| RSS GUID/type 및 알려진 ID의 내용 갱신 | **도메인/상류 근거 선결.** type별 조회와 external_id 전역 유일성·known-ID skip이 서로 다른 계약입니다. 실제 충돌/수정 정책 없이 uniqueness나 재갱신 동작을 바꾸지 않습니다. |
| transaction panic cleanup·다중 claimant/frozen membership·시계차 | **조건부 유지.** callback 안의 정상 입력 panic, 구성된 동시 claimant나 실제 시계 문제는 입증하지 못했습니다. provider panic을 이미 끝난 DB transaction 누수의 증거로 사용하지 않습니다. 전체 send unit·commit adjudication·현재 단일-worker 계약을 보존합니다. |

FK 인덱스의 추가 누락은 독립 검토에서도 확정하지 못했습니다. application의 부분 UNIQUE, head의 video PK, pending/receipt의 참조 UNIQUE와 기존 source-leading 접근을 보존합니다. `max_connections` 증대, blanket vacuum 조정, VACUUM FULL/REINDEX, history/HOT/LZ4 되돌리기, 새 cache/retry/fallback은 이번 개선안에 포함하지 않습니다.

### 조사 완료와 실행 한계

- 수집·저장·발송·조회 4개 읽기 전용 매핑과 2개 독립 반증 결과를 통합했습니다. 매퍼 2개의 결과 직렬화 오류는 저장된 완성 분석 본문을 복구해 독립 검토에 전달한 뒤 해당 작업을 종료했습니다. 실행 결과·우선순위·계약 전제는 위 본문에 모두 남겼습니다.
- kapu의 폐기 가능한 PG 18.6에 **전체 migration 315까지** 적용하고 RSS, room catalog, 실제 RPC/publish, alarm send-entry, YouTube known-success settlement, alarm metric의 6종 caller smoke와 실제 prepared-plan 비교를 실행했습니다. 일부 실패는 의도한 fault injection이며 control과 DB 결과를 대조했습니다.
- 로컬 DB는 tmpfs와 durability 비활성 설정을 사용했습니다. 시간 값은 작은 로컬 비교일 뿐 운영 성능 예상치가 아닙니다. 운영 발생·external send·중복 발송·전체 shutdown/모든 branch 검증으로 확대 해석하지 않습니다.
- 조사이므로 영구 회귀 테스트·새 제품 구현·migration·runtime dependency를 추가하지 않았습니다. 이 단계에서 전체 build/lint/race를 재실행했다고 보고하지 않습니다. 임시 Go 프로그램/adapter 9개·빈 probe 디렉터리·소유 PostgreSQL 컨테이너를 모두 제거했습니다.
- 이 후속 단계의 영구 변경은 이 인계 문서입니다. 기존 사용자·앞선 구현 변경은 보존했습니다. **운영 쓰기/배포/재시작/Git 발행 없음. Fallback delta: none. Retry delta: none.**

## N1–N7 로컬 구현과 통합 검증 — 2026-10-15

개선안 확정 뒤 사용자의 `ㄱㄱ` 승인으로 N1–N7을 로컬 구현했습니다. 위 조사 단계의 “미구현” 표기는 당시 상태이며, 아래 결과가 이후 상태입니다. 조건부 성능·보존 정책 후보는 승인 없이 확대하지 않았습니다.

### 구현 계약

| 항목 | 반영 내용 |
|---|---|
| N1 | unit와 전체 member 잠금 뒤 단일 DB wall clock을 사용합니다. Load/Pin/Begin fence, Begin의 sending/updated 시각과 연장 기한을 맞추고, standalone MarkSending의 부분-row 전이 및 sending Reissue의 만료 면제는 보존했습니다. |
| N6 | engine의 CompleteSent·ApplyStartedFailure·generation 전이와 기존 adjudication/read-back에 하나의 취소 독립 5초 예산을 적용했습니다. 기존 10초 종료 대기 경계 안의 상태 저장 예산이며 provider와 다음 fallback 호출은 원래 context를 사용합니다. fallback singleton 준비·고정 실패의 SENDING 보류는 기존 lifecycle 계약대로 유지했습니다. |
| N2 | 정상 Next 종료 뒤 rows.Err를 completeness보다 먼저 확인합니다. Scan·중복/범위 밖 ordinal 등 앞선 실패는 drain 오류로 덮지 않습니다. |
| N3 | 예약 source마다 결과를 확보하고 panicguard 반환을 callback 밖에서 반영합니다. 전체 source 실패와 저장 실패의 오류 chain에 source/stage 및 typed cause를 보존하며 regular 부분 성공은 nil을 유지합니다. |
| N7 | 같은 transaction의 7개 삭제 count를 모아 commit 확인 뒤 기존 metric에 반영합니다. lock 미획득·rollback·commit 응답 불명에는 계상하지 않습니다. Consumer의 immutable/fallback MarkSending 오류는 operation당 한 번 계상합니다. |
| N4 | raw/canonical과 JSONB-text의 기존 1MiB 상한을 함께 검증합니다. payload 전용 ErrPayloadTooLarge를 기존 response_too_large/RESOURCE_LIMIT로 분류합니다. Repository는 검증 API가 이미 반환한 canonical payload를 소유한 복사본에 사용하여 저장 표현·digest를 맞춥니다. |
| N5 | Catalog 단위 singleflight가 전체 HTTP와 저장 순회를 공유합니다. owner의 HTTP+DB 총 예산은 기존 QueryTimeout.Default 5초이며 waiter별 취소를 분리합니다. timeout 뒤에도 확보된 memory facts는 유효하고, 후속 miss는 새 refresh가 가능합니다. |

N4의 범위는 payload뿐입니다. scope/cursor의 canonicalizer 오류 정책·SQL CHECK·hash/collision guard는 유지했습니다. N5에는 negative cache·메모리 일치 시 Observe 저장 생략·DB 오류 시 HTTP 전환을 넣지 않았습니다. N6에는 새 provider 재시도·재발급 횟수·timeout 설정을 넣지 않았습니다.

### 독립 검토에서 추가로 막은 경계

- **N5 회귀:** 요청 방을 이미 remember한 뒤 그 방 또는 뒤쪽 다른 방의 저장이 owner deadline을 소진하면, 최초의 건강한 waiter만 false/plain을 받았습니다. 두 저장 위치 모두 failing-before 테스트로 확인한 뒤, 완료된 flight의 오류 유무가 아니라 살아 있는 waiter가 확보한 facts를 판정하도록 수정했습니다. 취소된 waiter의 조기 반환과 남은 저장 중단은 유지합니다.
- **N4 잔여 불일치:** collaborator 이름의 후행 공백으로 raw 1MiB를 만든 유효 schedule envelope는 typed 정규화·hash 검증을 통과하지만 원본 JSONB가 한도를 넘었습니다. 실제 PublishBatch의 CHECK 실패를 먼저 재현했습니다. 기존 ValidateAndCanonicalPayload 결과를 그대로 저장하도록 맞춘 뒤 caller 원본 불변, 같은 slot 중복, 다음 slot의 같은 payload 재사용을 검증했습니다. 새 정규화 규칙이나 hash 재계산 우회가 아닙니다.
- 과거에 이미 저장된 비정규 표현이 같은 digest의 canonical JSONB와 다른지는 운영에서 조사하지 않았습니다. 기존 dictionary equality guard는 그대로이며 이를 우회하거나 과거 행을 재작성하는 migration은 추가하지 않았습니다.

### 실제 caller 재실행

kapu의 별도 PostgreSQL 18.6에 전체 migration 315를 적용했습니다. 실제 HTTP/parser/repository/engine을 사용하고, 외부 송신과 실패 조건만 로컬 adapter·잠금·trigger로 제어했습니다.

| 경로 | 관측 결과 |
|---|---|
| alarm 진입 | 실제 unit 잠금 대기 뒤 live control은 sending으로 전이했습니다. 만료 case는 fence 오류로 거절되고 leased 상태를 보존했습니다. control의 sending timestamp는 잠금 해제 이후 시각이었습니다. |
| YouTube known success | 정상/성공 확인 직후 parent 취소 모두 provider 1회, apply=applied, delivery/ledger=SENT, 보고 성공 1이었습니다. 실제 Iris 송신은 하지 않았습니다. |
| RSS | regular 부분 성공은 attempted=2/failed=1/stored=1/error=nil입니다. 취소·panic은 attempted=failed=1과 원인을 보존했습니다. 후속 정상 cycle의 누락 5개 저장도 확인했습니다. |
| payload 상한 | 실제 RPC의 wire 1,047,698·canonical 1,048,126·JSONB 1,048,577바이트 응답은 response_too_large/RESOURCE_LIMIT, 저장 0입니다. JSONB 정확히 1,048,576바이트인 control은 INSERTED·lease IDLE로 확정됐습니다. |
| DB 오류 | 유효 payload에 별도 CHECK 실패를 주입하자 공개 PublishBatch까지 SQLSTATE 23514와 constraint 이름이 유지됐습니다. payload/observation/queue/checkpoint는 0이고 lease는 ACTIVE였습니다. |
| 저장 표현 | raw 1,048,576·원본 JSONB 1,048,591바이트의 schedule이 검증된 JSONB 245바이트로 저장됐습니다. caller bytes 불변, 같은 slot DUPLICATE, 다음 slot INSERTED이며 payload 1/observation 2/queue 2행입니다. |
| Catalog 공유 | 겹친 miss 20개·방 100개에서 HTTP 1회/실제 upsert 100회입니다. 이후 응답에 추가된 방은 독립 HTTP 1회로 발견했고 initiator 취소도 건강한 follower에 전파되지 않았습니다. |
| Catalog timeout | 실제 뒤쪽 방 row lock으로 전체 owner 예산을 소진했습니다. 약 5.014초 후 건강한 waiter는 이미 확보한 요청 방을 반환했고, 취소 waiter는 false, 미방문 방 저장은 0, HTTP는 1회였습니다. |
| retention metric | 후속 삭제 trigger 실패 시 원본 1개 보존·counter 증가 0입니다. trigger 제거 후 실제 삭제 1개·counter 합계 1을 확인했습니다. |
| MarkSending metric | 실제 owner fence 실패는 기존 등록 counter +1, 정상 control은 +0입니다. |

이 값은 tmpfs·durability 비활성의 격리 DB 재현이며 운영 latency·빈도·내구성 성능을 뜻하지 않습니다. 실제 외부 발송, 전체 process shutdown, 운영 장애·중복 발송을 검증했다고 해석하지 않습니다. 일회성 Go 프로그램/adapter 8개와 빈 probe 디렉터리, 소유 PostgreSQL 컨테이너는 검증 뒤 제거했습니다.

### 회귀·정적 검사 결과

- `go test -race -p 2 -count=1`: shared sourceobservation·Catalog, RSS scraper·scheduler, bot transport, alarm worker의 dispatchoutbox·egress·alarmdispatch·youtubedispatch와 하위 패키지, collector의 sourceobservation·collection·collectorruntime·3개 runner를 포함한 **19개 패키지 통과**.
- 테스트 구조·안전성 보정 뒤 변경된 7개 패키지 전체 race를 다시 통과했습니다. 마지막 RPC fixture의 명시적 평가 순서 수정 뒤 `TestCommunityRPCPayloadStorageBoundaryAndScheduler`의 실제 HTTP·scheduler·PG 경계 시나리오도 race로 통과했습니다.
- 영향 범위 golangci-lint, 프로젝트의 pinned patched Staticcheck·NilAway 통과. 초기 검사에서 지적된 새 테스트의 미검사 타입 단언·context 소유·복잡도·오류 처리·불필요한 복사를 수정했습니다. 영속 SQL 열 `cancelled_at`의 철자 오탐에만 statement 한정 `misspell` 예외와 이유를 남겼으며, 해당 상태의 rollback/commit 회귀를 유지했습니다. 검사 설정·제품 검증 기준은 변경하지 않았습니다.
- `go build -p 2`로 hololive-api·hololive-alarm-worker·hololive-youtube-collector·hololive-shared의 전체 Go 패키지 통과. `check-stack-db-access-policy.sh` 통과. 변경한 기존 service 문서·CHANGELOG의 diff/whitespace를 확인했습니다.
- retry/reissue의 횟수·horizon·wire code와 worker schema/profile 값은 변경하지 않았습니다. 해당 정적 계약 gate는 이번에 재실행하지 않았으며, 금지된 `Iris/native/**` 경로를 스캔하지 않았습니다. 변경된 상태 전이·재발급·취소·보존 계상은 위 PostgreSQL/race 시나리오로 검증했습니다. 전체 local-ci/pre-push, Docker image build, Node helper upstream mapper/paginator 검증을 수행했다고 보고하지 않습니다.
- 이 단계에서는 새 migration·runtime dependency·운영 읽기/쓰기·배포·재시작·Git 발행이 없습니다. 앞선 관측의 운영 ledger 314를 새 운영 확인으로 갱신하지 않습니다. **Fallback delta: none. Retry delta: 기존 횟수·시각·실패 정책 유지; 상태 저장의 취소 독립 예산만 적용.**

### 독립 리뷰 종료와 재발급 결합 회귀

- 구현 6개 작업, 독립 리뷰 2개, 회귀 테스트 구조·안전성 보정 2개 작업을 통합했습니다. Data 리뷰의 N4/N5 경계는 위 재현·수정으로 해소했고, Egress 리뷰는 N1/N6/N7에서 추가 제품 결함을 찾지 못했습니다.
- Egress 리뷰가 지적한 **parent 취소 + AdvanceRequestGeneration의 실제 COMMIT 뒤 응답 유실** 결합 공백은 `TestSendEngineCanceledReissueCommitLossReplaysPersistedGeneration`으로 채웠습니다. per-room/grouped 모두 최초 provider 1회·재발급 commit 1회·추가 settlement 0·즉시 재발송 0을 확인했습니다. DB는 PENDING/attempt 1/generation 1과 원래 backoff를 보존합니다.
- backoff 전 실제 ClaimPending은 비었으며, 테스트의 due 시각만 앞당긴 뒤 실제 ClaimPending·PrepareClaimed·frozen-group 복구로 다시 처리했습니다. 저장된 새 generation ID로 한 번만 발송하고 원래 body/route를 사용했습니다. 최종 SENT·ledger·각 attempt telemetry도 확인했습니다. 운영 시계를 변경하거나 발송을 실행한 것은 아닙니다.
- 새 결합 회귀의 race와 youtubedispatch 전체 race, 해당 패키지 golangci-lint·patched Staticcheck·NilAway 모두 통과했습니다. 이 보완은 테스트뿐이며 제품의 재발급·ambiguity 정책은 변경하지 않았습니다.

## live 동기 UPDATE refresh 필터 — 2026-10-15

사용자가 후속 범위로 **추가 성능 개선**을 선택하여 조건부 후보 중 live refresh만 실험·구현했습니다. 운영 계측·보존 정책·잠금 구조 변경으로 확대하지 않았습니다.

### 채택한 변경과 유지하는 경계

- 새 `316_youtube_live_runtime_update_filter.sql`과 manifest 177번을 추가했습니다. 기존 310/312 등 적용 migration은 수정하지 않았습니다. guard-first transaction에서 기존 statement trigger 함수만 교체하며 backfill·원본 재작성·새 index/cache/retry·권한 확대는 없습니다.
- UPDATE transition table을 `video_id`로 FULL JOIN하고 session 10개·head 17개·pending 8개·availability 3개의 비-key 입력을 NULL-safe하게 비교합니다. old/new key와 행 출현·소멸을 보존합니다. 큰 ignored-history 배열은 비교하지 않습니다.
- title/topic/premiere·reviewability·의미 영수증과 runtime 시계 입력은 모두 포함합니다. 원본 metadata/history는 계속 저장하지만 소비·검토 입력이 같으면 모델 재조회와 행 잠금을 생략합니다. INSERT/DELETE/TRUNCATE·영수증·FK 의미와 같은 문장 안의 동기 결과는 유지합니다.
- BEFORE MVCC guard는 생략하지 않습니다. 기술적 UPDATE도 고정 snapshot에서 guard가 바뀌었으면 SQLSTATE `40001`로 거절됩니다. 전역 writer 직렬화나 guard 대기 자체를 제거한 개선이 아닙니다.
- 기존 guard-first API와 함수 signature/ACL은 호환됩니다. 새 ledger에 316을 모르는 이전 migration runner를 실행하지 않습니다. 운영 DDL·배포·재시작은 별도 승인 대상이며 이번에는 실행하지 않았습니다.

### 실제 동등성과 비용 증거

- kapu의 폐기 가능한 PostgreSQL 18.6에 전체 manifest 176개/315까지 적용한 baseline과 후보 DB를 만들었습니다. active 256개·각 head의 ignored 시각 4,000개·inactive 20,000개를 비용 fixture로 사용했습니다. tmpfs·durability 비활성이므로 운영 latency·WAL 내구성 성능을 추정하지 않습니다.
- 양쪽에서 **63개 변이 문장 각각 직후** 원본과 현재 의미 영수증으로 계산한 oracle을 모델 14개 필드 전체와 비교했습니다. scalar/NULL·4개 원본의 old/new key 이동·혼합 UPDATE·삭제·수명 전이·upsert와 rollback이 일치했습니다.
- 4개 원본 × batch 1/32/256 × 기술적/소비 입력 변경/혼합 workload × 3라운드 × 양쪽 DB, 총 **216개 표본**을 비교했습니다. DB 실행 순서를 교대하고 6회 warmup 뒤 30회 UPDATE를 측정했습니다. 아래는 **라운드별 문장 평균 executor 시간의 3라운드 중앙값**, batch 32의 기술적 변경입니다. 중첩 함수 시간을 중복 합산하지 않았습니다.

| 원본 | baseline ms/문장 | 316 후보 ms/문장 |
|---|---:|---:|
| session | 1.7781 | 0.2423 |
| head | 1.9492 | 0.2701 |
| pending | 1.7717 | 0.2236 |
| availability | 2.1195 | 0.4825 |

- 기술적 변경의 refresh 호출은 모든 측정 case에서 30→0이었습니다. 혼합 batch 32/256도 개선됐지만 batch 1의 혼합 case는 의미 변경 행이 0개이므로 반반 혼합으로 해석하지 않습니다. 소비 입력이 전부 바뀌는 경로에는 비교 비용이 추가돼 auto-plan에서 최대 약 6.3% 증가한 중앙값이 있었습니다. custom/generic 강제 control도 각각 72개 표본으로 확인했으며 의미 변경의 본질적 가속을 주장하지 않습니다.
- actual `persistLiveSessions`를 guard-first `dbx.InPgxTx` 안에서 호출했습니다. 1/64 session으로 20 cycle씩 실행한 metadata heartbeat와 loaded ignored-history 변경은 baseline 20/1,280회 refresh에서 후보 0회로 줄었고 원본 저장·모델 불변을 확인했습니다. 의미 session+head 변경은 40/2,560회 refresh를 유지했습니다. 이후 양쪽 DB 전체 모델의 독립 oracle도 일치했습니다. 이 caller 실행의 시간 값은 단일 smoke이며 균형 잡힌 전체 API benchmark가 아닙니다.
- availability의 임의 `received_at` 변경은 합성 입력입니다. 실제 upsert는 effective 시각 또는 같은 시각의 더 큰 non-NULL observation ID 조건을 따르며, source FK의 SET NULL은 실제 기술적 UPDATE 경로입니다. pending의 metadata-only 빈도와 운영 workload 구성도 측정하지 않았습니다. 로컬 executor 절감률을 전체 운영 부하 절감률로 환산하지 않습니다.
- 지원되는 `dbtest.ApplyMigrations`로 baseline 복제 DB를 316까지 upgrade하고 runner를 다시 실행했습니다. 기존 모델 323행이 보존됐고 전체 oracle, 기술적 UPDATE 불변, title 변경의 영수증 무효화, rollback을 실제로 확인했습니다.

### 통합 회귀와 정적 검증

- `live_runtime_update_filter_migration_test.go`는 upgrade·재적용의 데이터/함수 보안 속성 보존, `end_candidate_kind`와 `end_candidate_observation_id` 각각의 변경·복원, 기술적 UPDATE의 REPEATABLE READ/SERIALIZABLE stale 거절을 검사합니다. 개별 field 회귀와 stale 기술적 UPDATE는 독립 안전성 리뷰의 권고를 반영했습니다.
- generated schema golden을 갱신하고 해당 회귀와 `TestSchemaSnapshotGolden`을 통과했습니다. 임시 probe/adapter 5개와 빈 디렉터리를 제거한 뒤 `go test -race -p 2 -count=1`로 dbtest·API sourceobservation·canonicalwrite·reconcile/live·YouTube runtime·targetprojection **6개 패키지 전체를 통과**했습니다.
- dbtest의 golangci-lint·pinned patched Staticcheck·NilAway, migration manifest 및 stack DB access policy 검사와 API 전체 `go build -p 2`를 통과했습니다. 초기 새 테스트의 whitespace 지적은 수정했으며 검사 설정은 바꾸지 않았습니다.
- 안전성·측정 근거의 독립 리뷰 두 결론은 모두 로컬 채택을 수락했습니다. 읽기 전용 매핑 2건의 전송 오류는 부모가 계약을 직접 확인하여 보완했고, 측정 리뷰의 HTTP 500 실패는 종료 후 범위를 좁힌 독립 재실행으로 복구했습니다. 리뷰가 체크를 별도로 실행한 것은 아닙니다. custom-plan 원시 출력은 보존 전에 만료되어 관측한 결과의 중앙값만 출처와 함께 남았으므로 독립 통계 재검증의 한계로 유지합니다.
- 변경 문서·generated schema의 diff와 whitespace를 확인했습니다. 소유 임시 PostgreSQL 컨테이너도 제거했습니다. 기존 N1–N7 및 다른 작업의 변경은 보존했습니다.
- 운영 읽기/쓰기·외부 송신·배포·재시작·Git commit/push는 없습니다. **Fallback delta: none. Retry delta: none.**

## 추가 최적화·압축 재측정과 후보 순위 — 2026-10-16

사용자가 스테이징 변경 검증 뒤 운영 읽기 전용 재측정을 승인했습니다. 운영 DDL·데이터 변경·배포·재시작·Git 발행은 이번에도 하지 않았습니다. 아래 수치는 **2026-10-15 15:26–15:34 UTC**(10-16 00:26–00:34 KST) `hololive-seoul` / `holo-postgres` / PG 18.6 관측이며, 각 세션에서 `transaction_read_only=on`, `statement_timeout=5s`, `lock_timeout=1s`를 확인했습니다. 업무 행·메시지·payload 원문은 조회하지 않았고 관측 키는 숫자를 마스킹한 형태만 확인했습니다. 운영 ledger는 여전히 **314까지**이므로 로컬 315·316의 효과는 반영되지 않은 상태입니다.

### 크기와 밀도

- DB 2,362MB (10-15 04:22 UTC 2,204MB 대비 약 11시간 동안 +158MB). 같은 6.6분 구간에서 source_observations INSERT 967 / DELETE **0**, applications INSERT 2,341 / DELETE 0이었습니다. 유입은 원본 약 21만 행/일·application 약 51만 행/일 규모이며 retention 정체(P1)가 계속되는 한 DB는 하루 약 300MB씩 자랍니다.
- source_observations 1,052MB = heap 621MB + index 431MB. `pgstattuple_approx`로 heap은 tuple 92.9%·free 6.1%로 bloat가 아닙니다. 평균 행 456B입니다. `uq_source_observation_identity`는 308MB로 avg_leaf_density **62%**·leaf_fragmentation **45%**(다른 주요 인덱스는 85–90%)이며, 항목당 약 240B입니다.
- source_observation_applications 802MB = heap 426MB + index 376MB. heap tuple 99.4%, 평균 행 105B. `uq_source_observation_application_active` 287MB(밀도 85%)의 `WHERE observation_id IS NOT NULL` 술어는 307 이후 항상 참이라 의미가 없습니다.
- 종류별 원본: live_snapshot 805k(7일 초과 **112,833**·3일 초과 511k), channel_live_check 249k(10-12 시작, 약 8.6만/일), community_page 125k, shorts/video_list 각 50k, 나머지 합계 약 28k. queue는 PROCESSED 213k·PENDING 2·PROCESSING 4, RETIRED generation 416(2일 보존 정상).
- TOAST 열 413개 중 lz4 명시 10개, 나머지는 DB 기본 lz4입니다. payload 표본 97%가 lz4이고 pglz 잔존은 미미합니다. reconciliation heads의 활성 160행(평균 71KB)만 pglz로 남아 있으며 11MB 규모입니다.
- idx_scan=0 인덱스는 모두 0.1MB 이하의 소형 표 PK/UNIQUE라 제거 대상이 아닙니다.

### 행 폭 표본 (TABLESAMPLE 3%, 운영 통계 추정치)

| source_observations 열 | 평균 길이 | 표본 distinct | 비고 |
|---|---:|---:|---|
| observation_key | 64 | 39,445 | 표본 63,867행 전부 `^[0-9a-f]{64}$` |
| job_key | 61 | 413 | `collector:<provider>:<job>:<subject>` 형태 |
| collector_instance | 52 | 94 | |
| collection_job_kind | 20 | 10 | |
| subject_key | 24 | 156 | |
| provider / observation_kind / completeness / continuity | 9 / 14 / 7 / 14 | 3 / 9 / 3 / 3 | vocab |

applications는 entity_kind 18B(11종)·decision 8B(11종)·entity_key 19B입니다. 관측 1건당 application은 schedule_snapshot 약 24, community_page 약 5, live_snapshot 약 1.6, channel_live_check 1입니다.

### 현재 workload (스냅샷 차분 395.8초, executor 시간 기준)

| 경로 | 호출/초 | 평균 ms | ms/초 | 비고 |
|---|---:|---:|---:|---|
| `source_observation_retention_protected` 내부 EXISTS | 193.7 | 0.172 | 33.4 | 삭제 0건인 retention tick이 무한 prefix를 훑다 timeout. 315가 1,000행 창으로 제한 |
| subject 후보 조회(target_bundles) | 24.0 | 0.609 | 14.6 | 이전 판단 유지, 재설계 안 함 |
| `refresh_youtube_live_runtime_state` | 5.6 | 2.585 | 14.4 | 316이 기술적 UPDATE의 refresh를 0으로 |
| pending_ends upsert | 2.9 | 2.698 | 7.9 | 312 trigger가 refresh를 포함. 누적 평균 0.07ms 대비 느려진 원인 |
| publish set | 2.1 | 3.103 | 6.7 | |
| live_sessions / heads INSERT | 1.5 / 1.2 | 3.3 / 3.1 | 4.9 / 3.6 | |

최상위 문장 합계는 약 57ms/초입니다. 중첩 함수 시간은 상위 문장에 포함되므로 합산하지 않습니다. 315는 보호 prefix의 반복 탐색을 제한하고 316은 기술적 UPDATE의 refresh만 생략합니다. 실제 삭제·FK/WAL 비용과 의미 변경 비율을 측정하지 않았으므로 이 관측만으로 전체 executor 시간의 3/4가 제거된다고 추정할 수 없습니다. 운영 적용 뒤 같은 기준으로 재측정해야 합니다.

### 후보 순위와 처분

| 순위 | 후보 | 예상 효과 | 변경 범위·전제 |
|---|---|---|---|
| 1 | **315·316 발행·운영 적용** | retention 후보 순회 재개와 기술적 UPDATE refresh 절감. 실제 삭제 가능량·전체 절감률은 보호 판정과 적용 후 측정으로 확인 | 이미 구현·검증됨. 발행·배포는 별도 승인 |
| 2 | live_snapshot 보존 7일→3일 (정책) | 약 51만 행, heap+index 약 400MB | replay/audit 하한 결정 필요. 코드 기본값·runtime override 확인 후 적용 |
| 3 | provenance 사전화: job_key·collector_instance·collection_job_kind → 사전 표 FK | 행당 약 140B, 현재 규모 heap 약 180MB(−30%) | 쓰기 SQL 2개·claim SQL 1개·보호 함수의 job_key 조인·shared envelope/Go 타입. 1GB 표 재작성 점검 창 |
| 4 | applications entity_kind·decision → smallint 코드 | heap 약 94MB + UNIQUE index 약 70MB | SQL 3개·Go enum 매핑·index 재생성 |
| 5 | observation_key hex text → bytea(32) | heap·index 각 약 43MB, 재작성 시 identity index bloat 약 120MB 회수 | 308 선례(공개 hex 표현 유지). publish/claim/replay/conflict SQL |
| 6 | `REINDEX CONCURRENTLY uq_source_observation_identity` | 약 120MB | ops 단독 조치. 5번 재작성과 중복 |

3–5번은 모두 가장 큰 두 표의 재작성이므로 한 점검 창에 묶는 것이 효율적입니다. 보류·제외: queue autovacuum/index(PENDING 2·dead 6k로 근거 없음), 미사용 인덱스 제거(소형 PK뿐), 남은 hex 해시 소형 표 6개(checkpoints 1,573행 등, 효과 미미), heads pglz 잔존 재압축(11MB), subject 후보 조회 재설계(이전 판단 유지).

### 측정 한계

- 차분 구간은 6.6분이며 하루 주기 workload(retention backlog, 방송 집중 시간)를 대표하지 않습니다. `pg_stat_statements` 누적치는 08-23 이후 최적화 전 비용이 섞여 순위에 쓰지 않았습니다.
- 행 폭·distinct는 TABLESAMPLE 추정치입니다. 예상 절감은 현재 행 수 기준이며 retention 재개 뒤 정상 상태 행 수로 다시 환산해야 합니다.
- 운영 쓰기/배포/재시작/Git 발행 없음. **Fallback delta: none. Retry delta: none.**

### 구조 압축 3·4·5 로컬 구현 — 2026-10-16

사용자가 후보 3·4·5 전부를 선택했습니다. 운영 DDL·데이터 변경·배포·재시작·Git 발행은 하지 않았습니다.

- **317_source_observation_provenance_compaction.sql:** `source_observation_provenances(id integer, job_key, collector_instance, collection_job_kind, UNIQUE 3-tuple)` 사전 표와 `source_observation_provenance_id()` resolver를 만듭니다. 준비 단계 뒤 최대 5,000행씩 keyset backfill·commit하고 같은 FK 값은 다시 쓰지 않습니다. 기존 text 3열은 FK 검증 후 `observation_key`의 bytea(32) 재작성과 같은 원자적 cutover에서 제거합니다. 비-hex 키 실패 시 기존 text 열·키 표현은 남고 완료된 backfill은 유지됩니다. 공개 lock/claim API는 계속 hex text와 출처 문자열을 사용하며 ACTIVE lease 보호는 사전의 job_key 조인입니다.
- **318–321:** `source_observation_entity_kinds`·`source_observation_decisions` 사전과 resolver 2개를 만들고 코드 열을 최대 5,000행씩 별도 commit으로 채웁니다. FK·NOT NULL 검증 → 319 코드 UNIQUE index 생성 → 320 구 index의 concurrent 제거 → 321 text 열 제거·bounds/보호 함수 교체 순서입니다. 코드 backfill의 후반 실패는 완료 batch를 되돌리지 않으며 재실행으로 이어갑니다. 제거 열의 heap 공간은 별도 승인된 점검 창의 `VACUUM (FULL, ANALYZE) source_observation_applications`로 회수합니다.
- **런타임 SQL:** collector `repository_publish_set`은 distinct 출처 3-tuple마다 resolver를 한 번 호출해 FK를 넣고 RETURNING은 hex로 encode합니다. API `repository_claim_lock`은 사전 표를 조인해 기존 열 이름·순서로 돌려주므로 Go scan은 바뀌지 않았습니다. `repository_application_insert`는 resolver로 코드를 넣고 코드 index의 ON CONFLICT를 사용하며, `repository_community_window_ready`는 이름→코드 subselect로 비교합니다. collector 테스트 seed SQL도 같은 방식입니다. Go 소스는 변경하지 않았습니다.
- **권한:** provenances SELECT는 runtime·scraper, resolver EXECUTE는 scraper. entity_kinds·decisions SELECT와 resolver 2개 EXECUTE는 runtime. 사전 표 INSERT는 resolver(SECURITY DEFINER)만 수행하며 PUBLIC 권한은 모두 회수했습니다. `TestSourceObservationMigrationGrantsAreLeastPrivilege`의 post-baseline 목록에 317–321을 추가해 실제 publish/claim SQL을 각 role로 실행합니다.
- **테스트:** 최종 schema pool을 쓰는 fixture 20개 파일을 sha256 키·resolver 호출·사전 조인으로 바꿨고, migration 시점 pool(234·241·306·307·312 이전)을 쓰는 테스트는 구 schema 그대로 둡니다. 새 `source_provenance_vocab_migration_test.go`는 317의 출처·키 왕복, 사전 공유, lock API hex 계약, ACTIVE lease·LIVE session 보호, 32바이트 CHECK, resolver 멱등성·bounds, 비-hex 키 롤백 뒤 재실행, 318–321의 어휘 왕복·index 교체·중복 거부·새 어휘 추가·참조 중 어휘 삭제 거부·CASCADE를 검사합니다.
- **실행한 검증:** manifest 검사, SQL 소유권 검사, 스택 DB 정책 검사, API·collector·worker 빌드, dbtest 전체(82초), API sourceobservation·youtube runtime, collector sourceobservation·collectorruntime, worker dispatchoutbox 패키지 테스트, schema golden 재생성을 통과했습니다. 변경 범위 `local-ci.sh`(architecture·lint·staticcheck·NilAway·build·race/non-race, Go 8개 패키지)도 통과했습니다. 첫 실행의 wsl 공백 지적 2건은 새 테스트에서 수정했고 integration-only 실행은 하지 않았습니다.
- **운영 적용 절차(별도 승인):** 315·316과 함께 신규 API 이미지 배포 전에 API·worker·collector a/b/c/d를 중지한 점검 창에서 `db-migrate`로 적용합니다. 317의 원본 표(약 1GB) 재작성·index 재빌드는 새 heap/index·WAL 공간과 ACCESS EXCLUSIVE 잠금이 필요합니다. 317·318 backfill은 5,000행 단위 commit이며 중단 뒤 완료분을 보존하지만, DO 전체의 문장 timeout과 CLI 명령 제한은 그대로입니다. 318·321 이후 감사 표의 물리 공간 회수는 별도 VACUUM FULL 단계입니다. 크기 절감 추정은 적용 뒤 실제 크기·`pg_stat_statements`로 확인해야 하며, 점검 전 백업과 이전 image/compose 복구점을 준비합니다. image-only rollback은 신규 schema를 되돌리지 못합니다.
- **한계:** resolver의 40001은 REPEATABLE READ 이상에서 사전 행 동시 추가 패자에게만 발생하며 기존 호출자 재시도에 맡깁니다. 사전 표는 자동 정리하지 않으며(collector 재기동마다 instance 행이 늘 수 있음) 크기는 수 MB 규모로 추정합니다. `source_observation_collisions`·`source_reconciliation_conflicts`의 text 열은 소형이라 바꾸지 않았습니다. 사용자 작업 트리의 다른 스테이징 변경과 미스테이징 변경 2건은 보존했습니다. **Fallback delta: none. Retry delta: none.**
