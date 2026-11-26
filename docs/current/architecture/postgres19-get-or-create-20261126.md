# PostgreSQL 19 get-or-create 개선과 후속 최적화

## 범위와 현재 상태

이 변경은 PostgreSQL 19의 `INSERT ... ON CONFLICT DO SELECT`를 두 저장소 경로에 적용한다.
기존 데이터의 의미를 유지하면서 충돌 행을 가져오기 위한 별도 SELECT를 줄인다.
스키마, 역할 권한, 트랜잭션 경계와 명시적 행 잠금은 추가하거나 변경하지 않는다.
서버 설정, 관측 데이터 보존 정책, 인덱스와 collector payload 계약은 후속 작업이다.

아래 시험은 2026-11-26 kapu의 `scripts/ci/pre-push-gate.sh` 전체 실행(일회용 PostgreSQL 19, golangci-lint, race 시험)에서 통과했다.
소스 변경과 검증 설계는 운영 반영을 의미하지 않는다.

## 줄어드는 데이터베이스 왕복

| 저장소 경로 | 이전 데이터 문장 수 | 변경 후 | 유지할 결과 |
|---|---:|---:|---|
| replay epoch 최초 생성 | 1 | 1 | 최초 cutoff·작성자·사유와 `Activated=true` |
| replay epoch 재요청 | 2 | 1 | 저장된 최초 값과 `Activated=false` |
| frozen request 최초 생성 | 2 | 1 | 저장된 요청과 generation |
| frozen request 재요청 | 2 | 1 | 저장된 요청 검증 또는 collision 오류 |

문장 수는 해당 저장소 함수의 데이터 SQL 기준이며 BEGIN·COMMIT, 상위 처리의 잠금·검증 문장은 제외한다.
기존 두 경로는 `DO NOTHING` 뒤에 조회하므로 다른 경로의 더미 UPDATE 제거 측정치를 적용하지 않는다.
이번 변경의 직접 효과는 조회 왕복과 분기 감소다. 전체 처리 시간과 WAL 감소율은 별도 측정으로 판단한다.

## Replay epoch

`activate_replay_epoch_tx.sql`은 singleton 키 충돌 때 최초 행을 반환한다.
`RETURNING cutoff_received_at, activated_by, reason, OLD.singleton IS NULL AS activated`로 값과 생성 여부를 함께 읽는다.
최초 INSERT에는 OLD 행이 없고, 충돌 SELECT에는 기존 singleton 행이 있으므로 생성 여부가 구분된다.

`RowsAffected()`는 INSERT와 충돌 SELECT 모두 반환 처리한 행을 세므로 신규 여부를 판별할 수 없다.
`Activated`는 SQL의 OLD 판정에서만 가져온다. 충돌 때 호출자가 보낸 작성자나 사유로 최초 값을 대체하지 않는다.
저장된 cutoff를 반환하며, 응답의 UTC 정규화와 입력 검증은 유지한다.
전용 두 번째 조회 SQL과 그 분기만 제거한다.

runtime의 기존 SELECT·INSERT 권한으로 실행하는 계약을 유지한다.
`FOR UPDATE` 등의 잠금을 추가하면 권한과 경합 특성이 달라지므로 사용하지 않는다.
여기서 잠금을 추가하지 않는다는 것은 unique 충돌을 해결하는 PostgreSQL 내부 대기가 없다는 뜻은 아니다.

## Frozen send request

`send_request_insert.sql`은 `base_id` 충돌 때 기존 요청을 같은 문장에서 반환한다.
반환 열은 `base_id`, `room_id`, `message`, `message_hash`, `route`, `dedupe_keys`, `member_ids`, `generation`이다.
Go는 `pgxdb.Query`와 `pgx.CollectExactlyOneRow`로 결과를 읽고 기존 `scanFrozenRequest`를 재사용한다.

호출 입력의 message hash 계산, 저장된 message와 hash의 일치 확인, 요청 간 collision 판정은 유지한다.
room, route, message hash, member ID 배열과 dedupe key 배열을 비교하며 배열 순서도 기존 계약대로 비교한다.
동일 요청이면 저장된 snapshot과 generation을 반환하고, 다른 요청이면 기존 행을 덮어쓰지 않고 오류를 반환한다.
저장된 hash가 손상된 경우에도 정상 요청으로 취급하지 않는다.

별도 요청 조회 SQL은 이미 묶인 delivery와 fallback 복원 경로에서 계속 사용한다.
get-or-create 호출 뒤의 delivery binding, owner fence와 기존 발송 상태 전이는 유지한다.
이 변경은 새 발송을 만들거나 이미 발송된 요청을 재전송하는 정책 변경을 포함하지 않는다.

## READ COMMITTED 경합 계약

두 트랜잭션이 같은 키를 동시에 INSERT하면 후행 문장은 선행 트랜잭션의 결과를 기다린다.
선행 트랜잭션이 COMMIT하면 충돌 SELECT가 저장된 행을 반환한다.
이 행은 후행 문장의 시작 스냅샷에는 없었더라도 PG19의 충돌 처리 경로에서 반환될 수 있다.
선행 트랜잭션이 ROLLBACK하면 후행 요청이 새 행을 생성할 수 있다.

replay epoch는 COMMIT 경합에서 최초 epoch와 `Activated=false`, ROLLBACK 경합에서 후행 epoch와 `Activated=true`여야 한다.
frozen request는 COMMIT 후 동일 payload를 복원하고, 다른 payload는 collision으로 거부해야 한다.
선행 INSERT가 ROLLBACK하면 후행 payload가 최초 저장값이 된다.
이 설명은 READ COMMITTED 계약이며 더 높은 격리 수준의 직렬화 오류를 자동 재시도한다는 약속은 아니다.

## 작성한 시험과 실행할 gate

| 시험 | 확인할 동작 | 상태 |
|---|---|---|
| `TestReplayEpochActivationIsImmutableAndIdempotent` | 최초 생성·재요청의 반환 값과 단일 행 유지 | 통과 |
| `TestReplayEpochActivationWaitsForConcurrentInsert` | 실제 충돌 대기를 확인한 뒤 COMMIT·ROLLBACK 결과 비교 | 통과 |
| `TestFrozenRequestInsertReturnsStoredSnapshotWithoutRewrite` | 최초·반복 요청, generation 보존, 행 버전 무변경 | 통과 |
| `TestFrozenRequestInsertRejectsConflictingPayload` | room·본문·route·멤버·dedupe 값 및 순서 불일치 거부 | 통과 |
| `TestFrozenRequestInsertRejectsCorruptStoredHash` | 손상된 저장 hash 거부 | 통과 |
| `TestFrozenRequestInsertWaitsForConcurrentInsert` | COMMIT·ROLLBACK과 동일·상이 payload의 네 경합 조합 | 통과 |

시험은 실제 PG19와 기존 역할 권한을 사용해야 한다. 고정 sleep만으로 경합이 발생했다고 간주하지 않는다.
행 버전 무변경 확인은 충돌 경로가 기존 행을 갱신하지 않는다는 검사이며 WAL 0을 증명하지 않는다.
기존 replay cutoff, dead-letter 우선순위, 이미 claim한 관측의 fence 시험도 함께 실행한다.
frozen request의 기존 복원·fallback·delivery binding 시험을 함께 실행해 상위 계약을 확인한다.

검증 환경을 준비한 뒤 다음 순서로 실행하고 실패를 해결한다.

1. API의 `internal/youtube/sourceobservation`과 worker의 `internal/egress/youtubedispatch/store`를 실제 DB를 사용해 검사한다.
2. 두 영향 모듈의 race 시험과 역할 권한 시험에서 예기치 않은 skip이 없는지 확인한다.
3. `scripts/architecture/ci-boundary-gate.sh`와 해당 변경에 대한 `scripts/ci/pre-push-gate.sh`를 실행한다.
4. PR의 module tidy·vet·race와 `fast-gate` 결과를 확인한다. 이 기록이 로컬 게시 검사를 대체하지 않는다.

전체 검사 소유권과 실행 규칙은 [CI Gates](ci-gates.md)와 저장소 README를 따른다.

## 호환성과 배포

새 SQL은 PostgreSQL 19를 필요로 하며 PostgreSQL 18에서는 사용할 수 없다.
지원하지 않는 서버에서 별도 구형 SQL로 재시도하는 fallback은 추가하지 않는다.
마이그레이션과 데이터 backfill이 없어 이번 변경에 따른 DB 적용 순서는 없다.
검증과 배포 승인 이후 변경된 API·alarm-worker 이미지를 배포할 수 있다. collector 이미지 변경은 필요하지 않다.
이 변경만 되돌릴 때에는 이전 앱 이미지를 사용하며 스키마 rollback이나 데이터 복원은 필요하지 않다.
실제 배포·롤백은 현행 release runbook과 이미지 식별자 검증을 따른다.

## 후속 1: 관측 보존의 집합 삭제

보존 함수 변경은 별도 008 suffix와 동일 이름의 전체 물리·권한 계약으로 준비한다.
이미 적용된 baseline은 수정하지 않고 현재 manifest head 다음 번호를 작업 시점에 다시 확인한다.
`delete_source_observation_retention_window`의 변경 순서는 다음과 같다.

1. 기존 runtime advisory lock, 보존 커서 잠금, bounded scan을 유지한다.
2. 부모를 `FOR UPDATE SKIP LOCKED`로 잠근 뒤 보호 집합을 최종 재검증한다.
3. 실제 삭제 대상 ID를 고정하고 그 ID의 applications를 한 문장으로 삭제한다.
4. 같은 ID의 부모를 삭제하고 기존 규칙으로 커서를 갱신한다. 모든 변경은 같은 트랜잭션에서 처리한다.

applications는 최신 LIVE·UPCOMING 관측의 보호 근거이므로 자식을 지운 뒤 보호 여부를 재계산하지 않는다.
함수의 SECURITY DEFINER·고정 search_path와 runtime의 직접 DELETE 금지를 유지한다.
FK CASCADE 트리거는 부모별로 남는다. 집합 삭제가 그 호출 자체나 WAL을 없애지는 않는다.
보호된 자식 보존, 잠긴 부모 건너뛰기, 부모·자식·커서의 원자 롤백, 권한과 대량 fanout을 시험한다.
합성 fixture의 처리 시간·버퍼·WAL 비교와 기존 bounded retention 회귀시험 후 적용 여부를 결정한다.

## 후속 2: 인덱스와 identity 계약

latest application 인덱스의 부분화는 동적으로 배정되는 `entity_kind_id`를 먼저 해결해야 한다.
특정 숫자를 모든 DB의 `youtube_live_session` ID로 가정하지 않는다. predicate와 실제 query plan도 함께 검증한다.
별도 head를 도입한다면 현재 최신성 기준인 최대 observation ID를 유지해야 한다.
처리 완료 시각으로 대체하거나 APPLIED만 반영하면 기존 REPLAY 등 감사 기록의 보호 의미가 달라진다.
backfill, 역순·병행 처리, retention과 replay, head 복구 경로를 함께 이행한 뒤 기존 인덱스를 제거한다.

observation identity 해시는 provider·kind·subject·scope·scheduled time을 포함한다.
그러나 DB의 길이 제약만으로 해시와 원래 열의 대응을 증명할 수는 없다.
UNIQUE를 해시·schema version·contract generation으로 줄이기 전 기존 중복·불일치를 감사한다.
identity 조회, advisory lock, uniqueness, semantic evidence collision 기록이 동일한 식별 규칙을 사용하게 한다.
현재 migrator가 `CONCURRENTLY`를 지원하지 않으므로 인덱스 변경의 잠금 시간과 정비 절차도 별도로 설계한다.

어휘 resolver는 SELECT-first를 유지한다. INSERT-first로 일괄 치환하면 충돌에도 identity sequence가 소모될 수 있다.
특히 smallint kind·decision ID는 반복 조회로 번호를 소비하지 않도록 해야 한다.

## 후속 3: WAL·저장 공간을 줄이기 위한 측정

세 관측 테이블의 크기는 각 테이블의 heap·TOAST·index를 같은 단위로 나누어 측정하고 합산한다.
과거 관측값과 표본만으로 현재 용량이나 이번 변경의 절감량을 단정하지 않는다.
파일의 여유 공간은 재사용될 수 있으므로 비어 있는 비율을 즉시 회수 가능한 지속 절감량으로 취급하지 않는다.

전후 측정은 여러 checkpoint와 autovacuum 주기를 포함하는 동일 길이의 정상 부하 창에서 수행한다.
각 창의 시작·종료 시각, 서버 시작 및 통계 reset 시각, 수집량·삭제량·업무 처리량을 함께 기록한다.
재시작·통계 reset·dump/restore·bulk load·수동 ANALYZE가 섞인 창은 정상 구간과 나누어 비교한다.
`pg_stat_wal`의 WAL bytes·FPI bytes, checkpoint 원인·주기, 테이블별 vacuum 시간·행 증감·인덱스 크기를 수집한다.
문장별 WAL과 전체 WAL의 차이를 곧바로 autovacuum 탓으로 확정하지 않고 vacuum 기록과 시간대를 대조한다.

checkpoint 간격 확대는 FPI 감소 가능성과 crash recovery 재생량·시간 예산을 함께 평가한다.
eager freeze나 parallel vacuum 설정은 실제 동결·인덱스 정리 비용이 확인된 테이블에 대해 각각 검증한다.
설정 변경은 한 번에 하나씩 적용하고 처리 지연·잠금 대기·WAL과 복구 시험의 수용 기준을 먼저 정한다.

## 후속 4: payload·감사 이력·공간 회수

`schedule_observed_at`을 payload 밖으로 옮기거나 양자화하려면 6시간 신선도 판정의 의미를 먼저 보존해야 한다.
collector와 API의 schema version, 경계 시각, 오래된 스케줄·재생 처리 시험을 함께 변경한다.
applications를 상태 변화 때만 기록하는 안은 감사·replay 요구를 결정한 뒤 추진한다.
REPLAY·RAW_RETAINED·변화 없는 APPLIED를 지우는 정책을 저장 공간 개선만으로 결정하지 않는다.

REPACK은 유입과 보존 구조를 개선한 뒤 살아 있는 데이터 크기를 다시 측정하고 검토한다.
장기 트랜잭션의 xmin 유지, queue·outbox vacuum 지연, 추가 작업 공간과 취소·복구 절차를 별도로 평가한다.
작업 직후 파일 크기뿐 아니라 정상 유입·vacuum 주기를 지난 뒤 크기와 지연도 비교한다.
예상 절감량과 표본 중복률은 가설로 기록하고 검증된 전체 시스템 감소율처럼 보고하지 않는다.

## PostgreSQL 문서

- [INSERT: ON CONFLICT, RETURNING, command tag](https://www.postgresql.org/docs/19/sql-insert.html)
- [Transaction Isolation: READ COMMITTED](https://www.postgresql.org/docs/19/transaction-iso.html)
- [Sequence functions: rollback과 충돌 시 번호 소비](https://www.postgresql.org/docs/19/functions-sequence.html)
