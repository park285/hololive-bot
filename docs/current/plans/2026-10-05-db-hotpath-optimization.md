# DB hot path 최적화 후속 작업

## 목표와 제약

2026-10-05 점검에서 미반영 또는 부분 반영으로 확인한 여섯 항목을 처리한다. 각 항목은 측정이나 계약 근거가 있을 때만 바꾸고, 기존 fence·원자성·잠금 순서·관측 시각 계약을 유지한다.

- 운영 DB 조회는 stack-platform-ops를 거친다. 운영 migration 적용·배포는 hololive-bot-ops 절차와 별도 승인이 필요하다.
- SQL 정책 이관 작업(`2026-10-04-sql-policy-expansion.md`)은 #571로 `origin/main`(`23f6497ec`)에 병합됐다. 모든 단계는 이 커밋 위에서 시작한다. 마지막 migration은 `265_bot_manual_replay_lock_clock.sql`이므로 새 번호는 266 이상이다.
- 검사는 제품 결함만 잡는다. SQL 문자열 검사나 구조 예산 같은 gate를 새로 만들지 않는다. benchmark는 수동 측정 도구로만 둔다.

## A. 코드만으로 진행할 작업 (운영 접근 불필요)

### A1. 유튜브 발송의 방별 중복 조회 제거 (P2)

근거: 모든 executor 경로는 `processPendingDeliveriesWithLifecycle`에서 `PrepareClaimed`를 거친 `ActiveRows`만 받는다. `PrepareClaimed`는 `(kind, logical_id, room_id)` 원장을 배치로 읽어 SENT를 Fulfilled로 걸러 내고, `BeginSending`은 발송 직전에 원장을 `FOR UPDATE`로 다시 읽어 Active가 아니면 conflict로 막는다. 원장 키와 방별 조회 키는 같은 `ResolveDeliveryLogicalID`로 만든다. 따라서 `claim_manager_gate.go`의 AlreadySent 분기에서 실행하는 `roomAlreadyReceivedPost`(`dispatcher_claim_acquire_0131_01.sql`)는 같은 사실을 세 번째로 확인한다.

- [ ] post 단위 판정이 AlreadySent이면 방별 조회 없이 claim token 없는 Proceed로 처리한다. 기존 "이 방은 아직 받지 않음" 경로와 같은 결과다.
- [ ] `roomAlreadyReceivedPost`, `resolveRoomDeliveryDecision`, `sentSiblingRowsContainPost`, `dispatcher_claim_acquire_0131_01.sql`과 이를 읽는 SQL 등록을 삭제한다.
- [ ] `PrepareClaimed`를 건너뛰고 `dispatchDeliveryRows`를 직접 호출하며 원장 없는 SENT sibling에 기대는 테스트를 운영 순서(`processPendingDeliveries`)와 원장 행 기준으로 바꾼다. 대상은 `claim_manager_gate_test.go`의 `SkipsAlreadySentDuplicateWithoutSending`, `SkipsAlreadySentTrackingRowWithoutReclaim`, `GroupedSendFiltersOutAlreadySentDuplicate`, `SendsAlreadySentPostToRoomWithoutSentRow`다.
- [ ] 회귀 시험: 같은 방에 SENT 원장이 있으면 발송하지 않는다. 다른 방만 받은 post는 이 방에 발송한다. prepare 이후 다른 실행이 같은 키를 SENT로 기록하면 `BeginSending` conflict로 발송하지 않고, 행이 이후 회수·재준비로 Fulfilled에 수렴한다.

### A2. 유튜브 claim SQL의 prepared plan 검증 보강 (P2)

근거: `fanout_claim.sql`, `transition_claim_pending.sql`, `transition_stale_sending.sql`은 부분 인덱스를 위한 상수 조건을 갖지만, 테스트는 몇 행짜리 fixture에서 EXPLAIN을 `t.Logf`로 출력만 한다. bot prune 테스트(`ledger_prune_plan_db_test.go`)는 보존 이력과 `auto` 모드 8회 실행으로 인덱스 사용을 단정한다.

- [ ] 세 SQL에 terminal 이력이 많은 fixture를 만들고 `force_custom_plan`과 `auto`(여러 번 실행 후)에서 대상 부분 인덱스(`idx_yno_pending_due_created_id`, `idx_ynd_pending_due_created_id`, `idx_ynd_sending_stale`) 사용과 seq scan 부재를 단정한다. `force_generic_plan`은 실제 결과로 판단해 요구 여부를 정한다.
- [ ] SQL 문자열만 검사하는 `TestTransitionClaimAndStaleSQLDeclarePartialIndexPredicates`는 plan 단정으로 대체한 뒤 삭제한다(stack AGENTS.md의 text-only test 금지).
- [ ] 상태 가드 결과 검사(`PreparedModesPreserve*Guard`)는 유지한다.

### A3. 관측 큐 선점의 적체 증가 측정과 dead-letter 분류 정리 (P1)

#571이 `repository_claim_0012_12.sql`에 `attempt_count` 반환을 추가했다. 그 버전 위에서 작업한다.

- [ ] 수동 benchmark를 추가한다. 활성 적체 5천·5만·20만 행과 PROCESSED 이력 유무를 조합해 claim 1회 시간과 CTE별 읽은 행을 기록한다. 결과는 이 계획에 남긴다.
- [ ] `exhausted_candidates`가 이력이 큰 queue에서 이력 행까지 읽는지 EXPLAIN으로 확인한다. 읽는다면 `active_backlog`에 `attempt_count`와 due 여부를 추가하고, exhausted 후보를 거기서 고른 뒤 `candidates`와 같은 LATERAL PK 잠금으로 바꾼다. 순서·LIMIT·SKIP LOCKED·replay epoch 제외 의미는 그대로 유지한다.
- [ ] `active_backlog`의 선형 비용 자체는 채널·종류 선두 계약 때문에 유지한다. 측정에서 운영 적체 규모가 1초 예산을 위협할 때만 선두 head 테이블 같은 구조 변경을 별도로 검토한다.
- 검증: 기존 `claim_backlog_plan_test.go`, `shorts_claim_plan_test.go`, claim 계약 테스트를 custom·generic plan에서 통과시킨다.

### A4. 관측 발행의 payload 처리와 지표 (P2)

#571이 `repository_publish_set_0032_32.sql`의 결과 분류를 Go로 옮겼다. 그 버전 위에서 작업한다.

- [ ] collector에 발행 단계 소요 시간과 인코딩 바이트 histogram을 추가한다(provider·job kind 라벨). 현재는 건수와 수집 전체 시간만 있다.
- [ ] 큰 payload benchmark(예: 1,024행·8 MiB 근처, 중복·신규 혼합)로 현재 SQL의 시간·메모리를 잰다.
- [ ] `existing` CTE에서 payload를 빼고, `payload_keys`와 일치 검사에서만 `input`을 ordinal로 다시 읽도록 바꾼 안을 같은 benchmark로 비교한다. 개선이 있을 때만 채택한다. digest 충돌·내용 불일치 검사(`lock_source_observation_payload`, `assert_source_observation_payload_match`)는 유지한다.
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
- [ ] 새 migration에 `DROP INDEX CONCURRENTLY IF EXISTS idx_youtube_collection_job_due;`를 `BEGIN/COMMIT` 밖에 두고 manifest와 schema snapshot을 갱신한다.
- [ ] 운영 적용 후 acquire·renew 지연, `n_tup_hot_upd` 비율, autovacuum 빈도를 비교한다.

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
- [ ] 보존 삭제가 최근 실행되지 않은 이유(설정값 또는 후보 없음)를 확인한다. 인덱스 없이 보존 삭제 SQL의 계획과 잠금 범위가 유지되는지 검증한 뒤 삭제를 검토한다.

### 참고: projection generation 급증의 잔여분

- 은퇴 generation 32,774개와 그 하위 target 2,022만 행이 남아 있다(`youtube_collection_targets` 3.9 GB, `youtube_collection_target_reasons` 4.8 GB). 일별 생성 수는 09-30 4,601, 10-01 9,299, 10-02 8,814, 10-03 8,307, 10-04 1,693이고, 10-05는 시간당 3~5개다.
- 행 단위 `valid_until` 갱신 문장이 누적 6.8만 회·5.97억 행을 기록했지만, 조회 사이 약 2분 동안 target UPDATE는 46건만 늘었다. 따라서 #566(heartbeat를 generation 단위로 이동)이 운영에 반영된 것으로 보인다.
- 잔여 행은 은퇴 보존 기간(`YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS`)이 지나면 보존 작업이 지운다. tick마다 최대 64배치이고 generation 하나에 약 2배치가 들므로, 대상이 되면 하루 2만 개 이상을 지울 수 있다. 실제 운영 설정값은 비밀 파일을 읽어야 해서 확인하지 않았다.
- [ ] 보존 대상이 된 뒤 삭제량, dead tuple, autovacuum을 확인한다. 디스크 파일 크기는 VACUUM 뒤에도 바로 줄지 않는다.

## 순서와 의존성

1. #571 병합으로 선행 조건이 없어졌다. 모든 단계는 `origin/main`에서 만든 별도 worktree에서 진행한다. 기존 checkout들에는 다른 작업의 미커밋 변경이 많으므로 건드리지 않는다.
2. 권장 순서는 B2, A1, A2, B4, A3, A4다. B2는 운영 판정이 끝났고 A3, A4는 측정이 먼저 필요하다.
3. B1은 완료됐다. B2는 migration 작업으로 진행할 수 있고, B3은 변경 없이 종료했다. B4는 보존 삭제 경로 확인 뒤 판단한다.

## 검증

- 변경 모듈의 PostgreSQL 테스트를 `-race`로 실행한다. alarm-worker store 통합 테스트는 `-tags=integration`을 포함한다.
- 변경 패키지의 golangci-lint, staticcheck, NilAway를 실행하고, `./build-all.sh --build-only --no-bump`로 세 runtime을 빌드한다.
- migration 단계는 `scripts/architecture/check-migration-manifest.sh`와 `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`로 확인한다.
- Git 발행 시 `scripts/ci/pre-push-gate.sh`를 실행한다.

## 필요한 승인

- B2(필요하면 B4) migration의 운영 적용과 배포 (hololive-bot-ops)
