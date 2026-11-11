# 마이그레이션 작성 규약

## epoch-3 적용과 복구 하한

- manifest의 첫 파일은 `001_schema_epoch3_baseline.sql`, 다음 파일은 `002_drop_x_space_login_attempts.sql`입니다.
  baseline은 epoch-2의 372 head 스키마·시드·시퀀스 값·권한·주석·DB 기본값을 통합하되 자동 로그인 시도 표는 제외합니다.
- 빈 DB는 baseline을 실행합니다. `schema_migrations`와 checksum 기록까지 같은 트랜잭션으로 커밋합니다.
- 기존 DB는 **epoch-2 manifest 전체 233개 파일의 이름과 sha256이 모두 일치할 때만** baseline SQL 실행 없이
  두 원장을 epoch-3 baseline 한 항목으로 원자 교체합니다. 봉인 목록은 러너에 포함된
  `internal/migrationrunner/epoch2_head.sha256`이며 삭제된 SQL을 다시 실행하는 경로는 없습니다.
- 부분 적용, manifest 밖 행, 누락·불일치 checksum은 새 migration SQL 실행 전에 거부합니다.
  **이전 릴리스로 372까지 올리고 정상 적용 기록을 확인한 뒤 새 릴리스를 실행**해야 합니다.
  epoch-1 checkpoint와 잔재를 수용하거나 임의로 원장을 지우는 복구는 지원하지 않습니다.
- epoch-2 원장 전환 코드(`internal/migrationrunner/transition.go`, 봉인 목록 `epoch2_head.sha256`, `runner.go`의
  `transitionEpoch2` 호출)는 `hololive-api` migration runner(`internal/migrationrunner`, `cmd/db-migrate`)가 소유합니다.
  제거 조건은 운영 DB를 포함해 이 러너로 올릴 수 있는 모든 DB에서 `schema_migrations`의 첫 항목이
  `001_schema_epoch3_baseline.sql`이고 manifest 밖 행이 0임을 `stack-platform-ops` 경유 읽기 전용 조회로 확인하는 것입니다.
  전환 전 백업의 보존 기한과 epoch-2 복원 경로의 폐기 시점은 운영 결정이므로 별도 승인이 필요합니다.
  manifest 밖 원장을 거부하는 `epoch.go`의 `guardEpochResidue`·`hasLegacyResidue`는 현행 계약이며 제거 대상이 아닙니다.
- 원장 교체 뒤 `002`가 `x_space_login_attempts`를 멱등적으로 삭제합니다. 기존 운영 1행의 삭제는 승인된 변경입니다.
  의존 객체 등으로 DROP이 실패하면 baseline 원장은 유지되며 다음 실행은 DROP부터 재개합니다. CASCADE는 쓰지 않습니다.
- 빈 원장과 기존 스키마는 자동 추정하지 않습니다. 확인된 epoch-3 스키마의 마지막 파일을
  `MIGRATION_BASELINE_THROUGH`에 지정하면 해당 지점까지 checksum·원장을 원자 기록하고 나머지를 실행합니다.
  옛 파일명은 watermark로 사용할 수 없습니다.
- 신규 migrator 이미지의 `db-migrate`가 실행되는 시점에 전환됩니다. 배포 전에 이전 릴리스와 백업을 보존하고
  Go의 자동 로그인 복구 읽기 제거·Console 복구 표시 제거를 함께 준비합니다. 새 스키마에 옛 migrator를 실행하거나
  삭제된 표를 읽는 옛 앱으로 되돌릴 수 없습니다. 롤백에는 이전 이미지와 **전환 전 백업의 별도 복원**이 필요합니다.
- 수동 `epoch2_acl_tail.sql`은 옛 baseline 생성 보조물이므로 제거했습니다.
  `manual/youtube_live_lifecycle_cutover.sql`은 ladder가 아닌 source 계약 활성화 운영 SQL로 유지합니다.
  baseline은 372 head와 동일하게 YouTube.js live_snapshot schema1/gen2·video_live_check schema1/gen1을 시드합니다.
  최초 활성화는 queue drain·collector quiescence·검증한 bundle·별도 승인 뒤 이 SQL로 gen3/2를 설정합니다.
  기존 DB의 운영 세대는 baseline 전환이 변경하지 않습니다.

## 러너 의미론

- 적용 경로는 `hololive-api/cmd/db-migrate` 하나입니다. 운영 one-shot `hololive-db-migrate`와 로컬·복구가 같은 러너를 씁니다.
  dbtest도 같은 statement/transaction 의미론으로 실행합니다.
- 파일은 `hololive-shared/pkg/sqlsplit`으로 나누어 autocommit 실행합니다. 파일 중간 실패 시 원장을 기록하지 않으므로
  다시 실행하면 파일 전체를 재적용합니다. 각 문장은 멱등이어야 합니다.
- top-level `BEGIN;`/`COMMIT;`는 pinned connection의 실제 트랜잭션입니다. 실패한 블록은 롤백하고,
  COMMIT 뒤 문장은 autocommit으로 이어집니다. `ROLLBACK`·`SAVEPOINT`·isolation 지정 BEGIN 등은 거부합니다.
- `CREATE/DROP INDEX CONCURRENTLY`는 단일 문장 파일로 둡니다. 트랜잭션 안에서는 사용할 수 없습니다.
  기존 DB의 plain `DROP INDEX`는 SQL 실행 전에 거부합니다. 점검 창에서 서비스 정지를 확인한 경우만
  `MIGRATION_ALLOW_BLOCKING_INDEX_DROP=true`를 명시합니다. fresh bootstrap은 plain drop을 허용합니다.
- 노출된 migration은 주석까지 불변입니다. 적용 파일 checksum이 다르면 skip하지 않고 실패합니다.
  새 변경은 새 파일로 추가합니다. 현행 epoch 밖 원장은 명시된 233개 전환 이외에는 거부합니다.
- 순서는 `manifest.txt`가 소유합니다. 형식은 `<001부터 연속 ordinal> <파일명.sql>`이며 파일 프리픽스는 중복 없이
  기존 최대 번호+1을 씁니다.

## SQL 안전 정책

`internal/migrationrunner`의 DB 없는 시험 `TestEmbeddedMigrationsFollowConventions`는 내장 migration에
러너와 같은 manifest 파서(`dbmigrate.Manifest`)와 문장 분리기(`sqlsplit`)를 적용해 누락·중복·잘못된 적용 순서,
위험한 baseline 명령, 온라인 인덱스 생성 및 트랜잭션 규칙, 무방비 NOT NULL 변경을 검사합니다. 함수·권한·제약 동작은 DB 시험이 검증합니다.
소스·문서 문구, 과거 파일 hash 일치, 검사기 자체 시험을 gate로 만들지 않습니다.

- `SET NOT NULL` 전에 `CHECK (col IS NOT NULL) NOT VALID`와 `VALIDATE CONSTRAINT`를 적용합니다.
  큰 표에서는 ADD와 VALIDATE를 별도 autocommit 문장으로 두어 ACCESS EXCLUSIVE 잠금이 전 행 검증까지 이어지지 않게 합니다.
  PostgreSQL 18의 NOT NULL NOT VALID 제약도 사용할 수 있습니다. `lock_timeout`은 획득 대기만 제한합니다.
- 대형 backfill은 keyset 배치로 나눕니다. top-level DO에서 batch COMMIT을 쓰면 바깥 BEGIN으로 감싸지 않습니다.
  실패 전 완료 batch는 남고 재실행은 미완료 행만 처리해야 합니다. statement timeout은 DO 전체에 적용됩니다.
- 값이 같으면 UPDATE하지 않도록 `IS DISTINCT FROM`을 사용합니다. 반복하는 비싼 정규화·조회는 같은 migration의
  임시 표로 한 번만 계산합니다. 임시 객체는 종료 전에 제거합니다.
- 시드는 multi-VALUES와 명시적인 conflict arbiter 또는 `WHERE NOT EXISTS`로 보호합니다.
  대상 없는 `ON CONFLICT DO NOTHING`은 금지합니다. members의 slug arbiter는 `idx_members_slug`입니다.
- 템플릿 개정은 `channel_id IS NULL AND body = old`의 CAS UPDATE로 사용자 본문을 보존합니다.
  운영에 한 번도 기록되지 않은 같은 key의 연속 개정만 병합 전에 합칠 수 있습니다. 배포된 checksum은 고치지 않습니다.
- 대량 보존 삭제는 배치 또는 파티셔닝을 사용합니다. 인덱스 수에 비례하는 쓰기 비용·WAL·vacuum도 고려합니다.
- 라이브 원본 행 변경은 원본 잠금 전에 `SELECT public.lock_youtube_live_runtime_state();`를 실행하고,
  같은 READ COMMITTED 트랜잭션 끝에서 변경 전후 video_id를 모두 넣어 `refresh_youtube_live_runtime_state(ARRAY[...])`를
  호출합니다. 누락은 `scripts/maintenance/queries/live_runtime_state_drift.sql`로 확인합니다.

## 스키마와 시험

- `hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql`은 catalog 직렬화 문서입니다.
  `TestSchemaSnapshotGolden`과 전환 시험이 같은 `dbtest.SnapshotSchema`로 비교합니다.
- PostgreSQL 18.6 고정 일회용 DB에서 실행하며 버전 fallback은 없습니다. 외부 DB는 폐기 가능한 서버임을
  `ALLOW_EXTERNAL_TEST_DB=true` 또는 소유 sentinel로 증명해야 합니다. 운영 DSN은 사용하지 않습니다.
- 갱신: `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`.
  수동 역사 재생 시험은 남기지 않고 현행 함수·트리거·제약·권한의 행동을 baseline 위에서 시험합니다.
- 내부 어휘는 TEXT+CHECK 또는 정수 FK 사전으로 제한합니다. 사전 UNIQUE는 허용 어휘 검증을 대신하지 않습니다.
  공개 SQL·Go 값은 문자열을 유지하며 사전 생성은 SECURITY DEFINER resolver가 소유합니다.
  sha256 식별자는 bytea(32)로 저장하고 경계에서 hex encode/decode합니다.
- 표시명은 member_name TEXT·room_name VARCHAR(255), channel_id 64·room_id 100 규약을 유지합니다.
  식별자·alarm_type enum을 임의 변경하지 않습니다. VARCHAR 축소·TEXT→VARCHAR는 금지합니다.
