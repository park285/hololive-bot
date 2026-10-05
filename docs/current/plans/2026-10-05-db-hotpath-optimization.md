# DB hot path 최적화 후속 작업

## 목표와 제약

2026-10-05 점검에서 미반영 또는 부분 반영으로 확인한 여섯 항목을 처리한다. 각 항목은 측정이나 계약 근거가 있을 때만 바꾸고, 기존 fence·원자성·잠금 순서·관측 시각 계약을 유지한다.

- 운영 DB 조회는 stack-platform-ops를 거친다. 운영 migration 적용·배포는 hololive-bot-ops 절차와 별도 승인이 필요하다.
- SQL 정책 이관 작업(`2026-10-04-sql-policy-expansion.md`)은 #571로 `origin/main`(`23f6497ec`)에 병합됐다. 이 계획 문서와 함께 옮기려던 미커밋 파일 4개는 #572(`0f40d93cc`)에 이미 포함됐으므로, 작업은 `0f40d93cc`에서 만든 worktree(`iris-stack/hololive-hotpath-20261005`, 브랜치 `perf/db-hotpath-20261005`)에서 진행했다. 새 migration은 266, 267이다.
- 검사는 제품 결함만 잡는다. SQL 문자열 검사나 구조 예산 같은 gate를 새로 만들지 않는다. benchmark는 수동 측정 도구로만 둔다.

## A. 코드만으로 진행할 작업 (운영 접근 불필요)

### A1. 유튜브 발송의 방별 중복 조회 제거 (P2)

근거: 모든 executor 경로는 `processPendingDeliveriesWithLifecycle`에서 `PrepareClaimed`를 거친 `ActiveRows`만 받는다. `PrepareClaimed`는 `(kind, logical_id, room_id)` 원장을 배치로 읽어 SENT를 Fulfilled로 걸러 내고, `BeginSending`은 발송 직전에 원장을 `FOR UPDATE`로 다시 읽어 Active가 아니면 conflict로 막는다. 원장 키와 방별 조회 키는 같은 `ResolveDeliveryLogicalID`로 만든다. 따라서 `claim_manager_gate.go`의 AlreadySent 분기에서 실행하는 `roomAlreadyReceivedPost`(`dispatcher_claim_acquire_0131_01.sql`)는 같은 사실을 세 번째로 확인한다.

- [x] post 단위 판정이 AlreadySent이면 방별 조회 없이 claim token 없는 Proceed로 처리한다. 기존 "이 방은 아직 받지 않음" 경로와 같은 결과다.
- [x] `roomAlreadyReceivedPost`, `resolveRoomDeliveryDecision`, `sentSiblingRowsContainPost`, `dispatcher_claim_acquire_0131_01.sql`과 이를 읽는 SQL 등록을 삭제한다. AlreadySent 행만 모아 다시 `PrepareClaimed`하던 `applyLifecycleClaimSelection`의 경로, `deliveryClaimSelection.alreadySent*` 필드, `lifecycleTransition.PrepareClaimed`도 쓰는 곳이 없어져 함께 지웠다.
- [x] 4개 테스트를 `processPendingDeliveries`와 `store.RecordDeliveryLedgerWrites`로 남긴 SENT 원장 기준으로 다시 썼다(`TestProcessPendingDeliveriesSkipsRoomWithSentLedger`의 community·short 하위 시험, `FiltersSentLedgerPostOutOfGroup`, `SendsSentPostToRoomWithoutLedger`).
- [x] 회귀 시험 `TestProcessPendingDeliveriesSentLedgerAfterPrepareBlocksSend`를 추가했다. 원장 기록을 빼면 세 시험이 실패하는 것을 확인했다. 원장 backfill은 migration 227이 적용 시점에 확인하므로 원장 없는 과거 SENT 행에 기대는 경로는 남기지 않는다.

### A2. 유튜브 claim SQL의 prepared plan 검증 보강 (P2)

근거: `fanout_claim.sql`, `transition_claim_pending.sql`, `transition_stale_sending.sql`은 부분 인덱스를 위한 상수 조건을 갖지만, 테스트는 몇 행짜리 fixture에서 EXPLAIN을 `t.Logf`로 출력만 한다. bot prune 테스트(`ledger_prune_plan_db_test.go`)는 보존 이력과 `auto` 모드 8회 실행으로 인덱스 사용을 단정한다.

- [x] `store/claim_history_plan_test.go`가 terminal 이력 2만 행(outbox SENT, delivery SENT·FAILED)과 후보 5행씩으로 세 SQL을 `force_custom_plan`(1회), `auto`(8회), `force_generic_plan`(1회)에서 `EXPLAIN ANALYZE`한다. 대상 테이블의 인덱스 조회, outbox·delivery seq scan 부재, 대상 테이블에서 읽은 행 40 이하를 단정한다.
  - 인덱스 이름은 단정하지 않는다. fanout custom plan은 `idx_yno_status_created`(status, created_at)를 골랐지만 status 동등 조건으로 PENDING 15행만 읽어 결과가 같았다.
  - `force_generic_plan`도 요구한다. `transition_stale_sending.sql`에서 상수 상태 조건을 빼자 generic plan만 seq scan으로 바뀌어 네 단정이 모두 실패했다. 상수 조건이 막는 결함을 이 모드가 잡는다.
- [x] `TestTransitionClaimAndStaleSQLDeclarePartialIndexPredicates`와 EXPLAIN을 출력만 하던 `logPreparedPlan`을 삭제했다.
- [x] 상태 가드 결과 검사(`PreparedModesPreserve*Guard`)는 유지했다.

### A3. 관측 큐 선점의 적체 증가 측정과 dead-letter 분류 정리 (P1)

#571이 `repository_claim_0012_12.sql`에 `attempt_count` 반환을 추가했다. 그 버전 위에서 작업한다.

- [x] `BenchmarkClaimBacklog`(`claim_backlog_benchmark_test.go`)를 추가했다. kind는 video_list·shorts_list이고 이력은 PROCESSED 20만 행이다. 수치는 3회 평균 서버 실행 시간(ms)과 CTE별 읽은 행이다. custom·generic plan 차이는 5% 이내라 custom만 적는다.

  | 활성 / 이력 | 변경 전 ms | 변경 후 ms | active_backlog 행 | exhausted 행 (전 → 후) |
  | --- | ---: | ---: | ---: | ---: |
  | 5천 / 0 | 13.8 | 13.9 | 10,002 | 5,001 → 0 |
  | 5천 / 20만 | 62.8 | 62.4 | 210,002 | 5,001 → 0 |
  | 5만 / 0 | 154.8 | 159.3 | 100,002 | 50,001 → 0 |
  | 5만 / 20만 | 208.4 | 214.7 | 300,002 | 50,001 → 0 |
  | 20만 / 0 | 632.6 | 644.8 | 400,002 | 200,001 → 0 |
  | 20만 / 20만 | 841.2 | 832.6 | 800,002 | 400,001 → 0 |

- [x] 확인 결과 `exhausted_candidates`는 활성 20만 행에서 queue를 seq scan해 이력 포함 40만 행을 읽었다. 계획대로 `active_backlog`에 `attempt_count`와 `due`를 추가하고, 시도 소진 후보를 그 결과에서 고른 뒤 queue PK LATERAL 잠금(`FOR UPDATE SKIP LOCKED`)으로 바꿨다. 읽는 행은 0이 됐지만 실행 시간 차이는 측정 오차 안이다. 시간은 `active_backlog`가 차지한다.
  - `claim_backlog_plan_test.go`의 dead-letter CTE 상한을 후보 잠금 상한(128)으로 좁혔다. 이전 SQL은 이 단정에서 50,002행으로 실패한다. materialized `active_backlog`를 읽는 CTE가 셋이 되어 그 상한은 활성 행의 3배로 고쳤다.
  - claim의 시도 소진 분류를 직접 검증하는 시험이 없어 `TestClaimDeadLettersDueExhaustedRowsInQueueOrderSkippingLocked`를 추가했다. 순서, LIMIT, 다른 실행이 잠근 행 건너뛰기, due가 아니거나 lease가 유효한 행 보존을 단정하며 이전 SQL에서도 통과한다.
- [x] `active_backlog`는 활성 행이 적어도 PROCESSED 이력이 있으면 `source_observations`를 seq scan한다(활성 5천·이력 20만에서 21만 행, 63 ms). 운영 queue는 2026-10-05 기준 활성 1행, PROCESSED 23.2만 행이고 claim 평균은 0.36~0.46 ms(최대 290 ms)라 1초 예산과 거리가 멀다. 구조 변경은 하지 않는다.
- 검증: `claim_backlog_plan_test.go`(custom·generic), `shorts_claim_plan_test.go`, sourceobservation 패키지 전체를 `-race`로 통과했다.

### A4. 관측 발행의 payload 처리와 지표 (P2)

#571이 `repository_publish_set_0032_32.sql`의 결과 분류를 Go로 옮겼다. 그 버전 위에서 작업한다.

- [x] `youtube_observation_publish_duration_seconds{provider,kind}`(모든 발행 시도)와 `youtube_observation_publish_encoded_bytes{provider,kind}`(commit된 발행, 6·7·8 MiB 버킷 포함)를 추가했다. 인코딩 크기는 `PublishBatchResult.EncodedBytes`로 전달한다. `TestCollectAndPublishRecordsPublishDurationAndEncodedBytes`가 실제 DB 발행으로 확인한다.
- [x] `BenchmarkPublishSetLargeBatch`(`repository_publish_set_benchmark_test.go`)를 추가했다. 1,024행(subject별 약 7 KB, 인코딩 7.84 MiB) 중 절반은 기존 관측, 절반은 신규이고 발행 SQL만 트랜잭션에서 실행한 뒤 되돌린다.
- [x] 대안을 같은 benchmark로 비교해 채택했다. 3회 측정 범위는 다음과 같다. digest 충돌·내용 불일치 검사 함수는 그대로이고 `TestPayloadDigestCollisionAndCorruptionFailClosed`가 통과한다.

  | SQL | wall ms | 서버 실행 ms | materialized CTE 저장 합 | Go 할당 |
  | --- | ---: | ---: | ---: | ---: |
  | 변경 전 | 394~398 | 361~364 | 99,825 kB | 24.9 MB |
  | `existing`에서 payload 제외 | 373~377 | 337~343 | 58,541 kB | 24.9 MB |

  CTE 저장 합은 PG 18 EXPLAIN의 `Maximum Storage`를 모든 CTE scan에 대해 더한 비교용 값이며 같은 CTE를 여러 번 읽으면 중복 합산된다.
- 바이트 기준 배치 분할은 하지 않는다. 발행과 checkpoint를 한 트랜잭션으로 묶는 원자성 계약이 바뀌기 때문이다. 8 MiB·1,024행 상한 거부는 유지하고, 지표로 상한 근접 빈도를 본 뒤 다시 판단한다.

## B. 운영 증적이 필요한 작업

### B1. 읽기 전용 운영 증적 수집 (완료, 2026-10-05 05:09 UTC)

stack-platform-ops 절차로 `hololive-osaka`의 `holo-postgres`(PostgreSQL 18.6)를 조회했다. 모든 세션에서 `transaction_read_only=on`을 먼저 확인했고 쓰기·DDL·통계 리셋은 하지 않았다. 스냅샷 스크립트의 MVCC 통계 SQL(database state, table·toast·index·transaction section)과 bounded `pg_stat_statements` 집계만 실행했다. 쓰기 SQL을 EXPLAIN하는 claim 계획 수집은 제외했다.

- 통계 구간: `pg_stat_database.stats_reset`은 NULL(리셋 없음)이고 checkpointer·WAL·`pg_stat_statements` 리셋과 가장 오래된 autovacuum이 2026-08-23이다. 2026-10-02 재시작 뒤에도 통계가 유지됐으므로 약 42일 구간으로 판정했다.
- [x] 수집 대상에 `bot_webhook_inbox`, `bot_command_executions`, `youtube_notification_delivery_ledger`를 추가했다(`scripts/runtime/lib/pg-hotpath-catalog-sql.sh`).
- 오래 열린 transaction은 없었다.

### B2. 임대 테이블의 넓은 due 인덱스 삭제 (P1): 진행 판정

- `idx_youtube_collection_job_due`: `idx_scan=9`, `last_idx_scan=2026-10-03 10:31`, 크기 3.4 MB(행 1,092개). PK는 48억 회 사용됐다.
- 9회의 출처를 `pg_stat_statements`로 추적했다. 런타임 role(`hololive_runtime`, `hololive_scraper`)에서 `job_key` 동등 조건이 없는 문장은 INSERT, `projection_generation` FK 확인, 보존 삭제뿐이며 이 인덱스를 쓰지 않는다. `slot_state`로 거르는 문장은 `postgres_admin`의 수동 진단 쿼리와 `hololive_migrator`의 사전 점검(2회)뿐이다. 둘 다 1,092행 seq scan으로 충분하다.
- 쓰기 비용: 42일간 UPDATE 1,410만 건, HOT 0.43%, autovacuum 26,349회(약 2.3분마다), 스냅샷 시점 dead 31.8%. renew가 바꾸는 `lease_expires_at`이 이 인덱스 key라 HOT이 막힌다. 삭제하면 renew는 PK·`projection_generation` 인덱스 key를 바꾸지 않으므로 HOT 대상이 된다.
- [x] `266_drop_youtube_collection_job_due_index.sql`을 추가하고 manifest와 schema snapshot을 갱신했다. 이 테이블을 읽는 런타임 SQL이 모두 `job_key` PK로 접근함도 코드에서 다시 확인했다.
- [x] 운영 적용 후 비교(아래 "운영 반영" 표). HOT 비율이 0%에서 82%로 올랐고 autovacuum 간격은 약 1.2분에서 1.7분으로 늘었다. acquire 평균은 0.376 ms에서 0.329 ms다. renew 문장은 두 구간 모두 호출이 없어 비교하지 못했다.

### B3. 테이블별 autovacuum 조정 (P2): 변경 불필요로 종료

autovacuum이 따라가지 못하는 대상 테이블은 없었다. 기본값은 scale factor 0.2, threshold 50, naptime 60초다.

| 테이블 | live / dead | 42일 UPDATE | HOT | 판단 |
| --- | --- | ---: | ---: | --- |
| `source_observation_queue` | 23만 / 5천(2.2%) | 2,254만 | 1.0% | 기준 4.6만에 한참 못 미치고 12분 전 vacuum됨 |
| `youtube_collection_job_leases` | 1,092 / 510 | 1,410만 | 0.4% | 약 2.3분마다 vacuum됨. B2로 해결 |
| `source_collection_checkpoints` | 5,530 / 160 | 502만 | 2.2% | vacuum은 충분함. 낮은 HOT은 B4 참고 |
| `youtube_notification_delivery_ledger` | 198 / 0 | 0 | - | 조정 대상 아님 |
| bot inbox·outbox·executions | 각 64행 이하 | 1천 미만 | - | 조정 대상 아님 |

큐·임대 테이블의 낮은 HOT은 인덱스 key 변경 때문이므로 fillfactor로 해결되지 않는다.

### B4. checkpoint의 updated_at 인덱스 (신규 후보)

- `idx_source_collection_checkpoints_updated_identity`(`updated_at, provider, observation_kind, subject_key, scope_sha256`, migration 186)는 보존 삭제(`repository_retention_delete_checkpoints_0084_84.sql`)용이다. upsert마다 `updated_at`이 바뀌어 HOT이 2.2%에 그친다. 인덱스는 5.4 MB로 PK(984 kB)보다 크다.
- 마지막 사용은 2026-09-29 23:19다. 행이 5,530개뿐이라 보존 삭제를 seq scan으로 처리해도 비용이 작다.
- [x] 2026-10-05 09:16 UTC 읽기 전용 조회(가드 `transaction_read_only=on` 확인) 결과, 보존 삭제는 계속 실행 중이었다(runtime 호출 15,432회, 누적 삭제 344만 행). 인덱스를 쓰지 않은 이유는 planner 선택이다. 6,187행 중 5,269행이 2일 cutoff보다 오래되어 조건 선택도가 낮고, 두 테이블 seq scan과 hash semi join을 골라 9 ms에 끝났다(현재 후보 0건).
- [x] 누적 삽입·삭제 344만 건은 과거 scope 교체 폭증의 흔적이다. 10분 측정에서 삽입·삭제는 0건, UPDATE는 1,616건(하루 약 23만 건)이고 HOT은 0건이었다.
- [x] 폭증 모양(한 subject에 scope 2.4만 개, 2.2만 개가 오래됨) dbtest에서 인덱스를 지워도 계획은 hash semi join으로 바뀌고 31.7 ms(인덱스 사용 시 34.5 ms)였으며, 삭제 호출은 50 ms(81 ms)였다. 잠금은 후보 CTE의 `FOR UPDATE SKIP LOCKED`가 LIMIT 안의 후보에만 걸므로 접근 경로와 무관하다.
- [x] `267_drop_source_checkpoint_retention_index.sql`을 추가했다. 인덱스 사용을 단정하던 `TestCheckpointRetentionCandidatePlanUsesBoundedIndexes`는 후보마다 checkpoint를 다시 seq scan하는 2차 계획을 막는 `TestCheckpointRetentionCandidatePlanReadsCheckpointsOnce`로 바꿨다.
- [x] 운영 적용 후 checkpoint HOT 비율은 0%에서 99.7%가 됐고 같은 구간 autovacuum은 3회에서 0회다. 보존 삭제 평균은 10.4 ms에서 12.2 ms로 측정 오차 범위다.

### 참고: projection generation 급증의 잔여분

- 은퇴 generation 32,774개와 그 하위 target 2,022만 행이 남아 있다(`youtube_collection_targets` 3.9 GB, `youtube_collection_target_reasons` 4.8 GB). 일별 생성 수는 09-30 4,601, 10-01 9,299, 10-02 8,814, 10-03 8,307, 10-04 1,693이고, 10-05는 시간당 3~5개다.
- 행 단위 `valid_until` 갱신 문장이 누적 6.8만 회·5.97억 행을 기록했지만, 조회 사이 약 2분 동안 target UPDATE는 46건만 늘었다. 따라서 #566(heartbeat를 generation 단위로 이동)이 운영에 반영된 것으로 보인다.
- 잔여 행은 은퇴 보존 기간(`YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS`)이 지나면 보존 작업이 지운다. tick마다 최대 64배치이고 generation 하나에 약 2배치가 들므로, 대상이 되면 하루 2만 개 이상을 지울 수 있다. 실제 운영 설정값은 비밀 파일을 읽어야 해서 확인하지 않았다.
- [x] 2026-10-05 11:25 UTC 확인: 은퇴 generation 32,829개 중 7일 보존을 넘긴 것은 09-28분 6개뿐이었다. 11:13 이후 대상이 된 2개와 하위 target·reason 각 1,085행, lease 1행이 삭제돼 보존 경로 작동을 확인했다.
- [x] 폭증분은 보존 기간을 기다리지 않고 2026-10-05 12:05~12:50 UTC에 사용자 요청으로 지웠다. 운영 보존 작업과 같은 `delete_retired_youtube_collection_job_leases`·`delete_retired_youtube_projection_batch`를 cutoff `now() - 1 hour`, 호출당 1,000행으로 autocommit 실행했다(500회마다 3초 휴지, statement 30초·lock 5초 timeout, 호스트 여유 20 GiB 미만이면 중단).
  - 삭제: lease 645행(현행 대상 밖 subject), generation 32,816개, target·reason 각 20,244,088행. 남은 RETIRED는 최근 1시간분 22개다. 현행 채널 job의 lease는 모두 남았다.
  - 영향: 2코어 호스트의 부하 평균이 약 4.5, IO pressure가 최대 46%까지 올랐다. 그동안 API 보존 tick 8건(`source_observations`·`source_observation_applications` 삭제)과 관측 처리 1건이 `context deadline exceeded`로 실패했다. 해당 관측은 이후 PROCESSED였고 queue에 DEAD_LETTER는 없었다. 삭제가 끝난 뒤 오류는 0건이다.
  - 사후: autovacuum이 바로 처리해 dead tuple이 target 2개, reason 약 4.7만 개로 줄었다. WAL은 `max_wal_size` 안(최대 약 1 GB)이었고 호스트 여유는 56 GiB에서 55 GiB가 됐다.
- [x] 사용자 요청으로 13:24~13:26 UTC에 `VACUUM (FULL, ANALYZE)`를 reason, target 순으로 실행했다(`lock_timeout` 5초, 각 약 48초). pgstattuple_approx 기준 빈 공간은 99% 이상이었다. pg_repack은 운영 이미지에 확장이 없고 넣으려면 이미지 재빌드와 `holo-postgres` 재생성이 필요해 쓰지 않았다.
  - 결과: target 3.9 GB→4.4 MB, reason 4.8 GB→5.6 MB, DB 11 GB→2.6 GB, 호스트 여유 55 GiB→64 GiB.
  - 영향: target 잠금 동안 API DB 슬롯이 고갈돼 관측 consume과 그 Retry가 `acquire DB slot: context deadline exceeded`로 실패했고, API가 이를 runtime error로 종료해 13:25:26~13:25:35 UTC에 한 번 재시작했다(`restarts=1`). collector c는 수집 job 56건이 실패 뒤 재시도됐다. 해당 관측은 2차 시도로 PROCESSED였고, 같은 시간대 webhook·명령 실행 기록은 없었으며, 13:27 이후 오류는 0건이다.
  - 다음에 같은 rewrite가 필요하면 API·collector를 먼저 멈추거나 점검 창에서 실행한다. 큰 테이블의 잠금은 consume 실패를 프로세스 종료로 키운다.

## 순서와 의존성

1. B2, A1, A2, B4, A3, A4 순서로 코드 작업을 마쳤다(2026-10-05). B1은 완료, B3은 변경 없이 종료했다.
2. PR #573으로 main(`ab078d31d`)에 병합하고 운영에 반영했다. 은퇴 projection 폭증분도 2026-10-05에 정리해 이 계획의 남은 작업은 없다.

## 검증

- 변경 모듈의 PostgreSQL 테스트를 `-race`로 실행한다. alarm-worker store 통합 테스트는 `-tags=integration`을 포함한다.
- 변경 패키지의 golangci-lint, staticcheck, NilAway를 실행하고, `./build-all.sh --build-only --no-bump`로 세 runtime을 빌드한다.
- migration 단계는 `scripts/architecture/check-migration-manifest.sh`와 `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`로 확인한다.
- Git 발행 시 `scripts/ci/pre-push-gate.sh`를 실행한다.

### 2026-10-05 검증 결과

- `-race` PostgreSQL 테스트: alarm-worker `internal/egress/youtubedispatch/...`·`internal/service/alarm/dispatchoutbox/...`(`-tags=integration`), api `internal/youtube/sourceobservation/...`, `hololive-dbtest/...`, collector `internal/runtime/...`·`testkit/...`가 통과했다. collector의 youtubejs helper 시험은 새 worktree에 `npm ci --ignore-scripts`로 의존성을 설치한 뒤 통과했다.
- 변경 패키지의 golangci-lint(0 issues), staticcheck, NilAway가 통과했다.
- `check-migration-manifest.sh`와 `TestSchemaSnapshotGolden`이 통과했다. snapshot에서는 두 인덱스 줄만 빠졌다.
- `./build-all.sh --build-only --no-bump`가 local CI(전체 Go test·race test 포함, integration은 기본값대로 생략)와 이미지 빌드를 통과했다. `scripts/ci/pre-push-gate.sh`는 Git 발행 때 실행한다.

### 2026-10-05 운영 반영

- PR #573을 main `ab078d31d`로 병합했다. 중앙 `hololive-osaka`에서 migration 266·267을 적용(`applied=2`)한 뒤 API·alarm-worker를 재생성하고, collector c·issuer는 `po-central-cutover.sh`로 교체했다. AP Seoul(b)·Osaka(a)·Osaka2(d)도 같은 revision으로 배포해 각 완료 검사를 통과했다. 모든 서비스가 healthy이고 배포 뒤 alarm-worker ERROR·WARN 로그는 없었다.
- 새 발행 지표는 collector 네 대 모두에서 Prometheus로 수집된다. 새 관측 claim SQL은 배포 뒤 평균 0.573 ms(최대 8.9 ms)로 이전 버전(0.583 ms)과 같다.
- 읽기 전용 통계 비교(적용 전 10:09~10:32 UTC, 적용 후 10:44~11:09 UTC):

  | 지표 | 적용 전(23.6분) | 적용 후(25.7분) |
  | --- | ---: | ---: |
  | lease HOT | 0 / 7,226 (0%) | 6,356 / 7,739 (82.1%) |
  | lease autovacuum | 19회 | 15회 |
  | lease acquire 평균(scraper) | 0.376 ms | 0.329 ms |
  | checkpoint HOT | 0 / 3,991 (0%) | 4,192 / 4,206 (99.7%) |
  | checkpoint autovacuum | 3회 | 0회 |
  | checkpoint 보존 삭제 평균 | 10.4 ms | 12.2 ms |

- 수용 뒤 이번 배포의 rollback tag(중앙 4개, Seoul 2개)와 전송 staging·백업 디렉터리를 정리했다. native Osaka·Osaka2는 배포 방식대로 직전 release를 `previous`로 보존한다.
