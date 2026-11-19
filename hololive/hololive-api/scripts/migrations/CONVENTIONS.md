# Hololive PostgreSQL 마이그레이션

## Epoch 4: 20.0.0 적용 경계

`001_schema_epoch4_baseline.sql`은 빈 DB 한 곳의 업무 스키마와 초기 데이터 전체다. 19.2.0의 실제 032 head를 PG18에서 재생하여 추출한 테이블·타입·함수·트리거·제약·시퀀스·시드·권한·DB 기본값을 포함한다. 실행용 원본은 배포 이미지에 내장한다. 과거 001–032 SQL과 006의 동시 인덱스 삭제, 031의 배치 커밋은 보관 태그에만 있고 새 설치에서 재생하지 않는다.

| 입력 | 실행 |
|---|---|
| 빈 PG18 DB | `/app/bin/db-migrate --initialize`. private migrator socket·DB 소유권·`public`의 CREATE/USAGE, 기존 init-db의 역할·기본 ACL·확장 설치가 선행 조건이다. 스키마와 seed·setval, 실제 파일명·SHA256의 양쪽 원장을 하나의 트랜잭션으로 커밋한다. 실패 시 새 원장까지 롤백한다. |
| 완료된 epoch 3 또는 epoch 4 | 20.0.0 배포 one-shot `--schema-only`는 풀러 인증·DB명·migrator·Unix socket, 두 원장 전체, 실제 카드 객체·활성 트리거와 인덱스를 확인만 한다. 정확한 32쌍 또는 실제 실행한 새 1쌍만 수용한다. 구 원장의 이름·SHA·시각, 기존 사용자 데이터, ACL, 역할, 시퀀스, DB 설정에 쓰기 0. baseline 실행·채택 영수증은 만들지 않는다. |
| 유효한 epoch-3 접두 구간 | 현재 러너는 거부한다. 동일 원본을 가진 보관 릴리스의 공식 migrator와 독립 checkout·백업에서만 빠진 단계부터 마친다. 기존 staging·운영 checkout과 섞지 않는다. 정상 030은 19.2 계열의 풀링·여섯 writer drain·관리자 guard·검증한 복구점 아래 전용 카드 절차만 허용한다. 031 단독·부분 트리거·불명 batch·실패/미확인 hold는 자동 재개하지 않는다. |
| epoch 2 | 233쌍의 372 완료 여부를 해당 세대 원본으로 검증한다. 불완전하면 epoch-2 공식 migrator로 먼저 372까지 올린다. 구 릴리스의 독립 복원본에서 승인된 epoch-2→3 전환과 그 이후 단계·카드 창을 마친 뒤 완전한 epoch-3 032를 새 러너에 제시한다. epoch 4가 epoch-2 원장을 지우거나 덮어쓰지 않는다. |
| 원장 한쪽·해시 불일치·구멍·혼합·서로 다른 출처·미추적 객체·체크섬 없는 복원 | 자동 추정과 watermark가 없다. 데이터나 원장을 수정하지 말고 hold와 원본 증거를 보존한다. 별도 복원·원인 확인·fix-forward는 운영 승인 대상이다. |

이미 수용한 DB에서 로그인 비밀번호를 회전할 때는 별도 관리자 창에서 독립 CLI `--configure-login-roles`를 사용한다. 이 명령은 먼저 같은 보호 socket·완료 head를 검증하며 배포 전용 이미지에서 허용하지 않는다. 마이그레이션 skip이 암호를 회전하지 않는다.

`--initialize`는 빈 DB 전용이다. 지원하는 완료 head에서 다시 호출하면 읽기만 하고, 기존 lineage에 pending suffix가 있으면 첫 파일명을 보고하고 쓰기 없이 거부한다. 신규 DB에 사용자 정의 standalone FDW 같은 미추적 객체가 있어도 거부한다. runtime·scraper는 표 권한과 별도로 `public` USAGE가 있어야 한다. 일반 배포 서비스에는 이 옵션과 관리자 비밀번호를 넣지 않는다. 독립 신규 초기화에 관리자 자격을 명시한 경우에만 허용된 빈 DB 판정 후 기존 역할 bootstrap을 호출한다. 제외된 역할이 있는 격리 시험에서는 해당 조건부 GRANT를 수행하지 않는다. 운영에서는 `hololive_migrator`가 업무 객체를 소유하고 runtime, NOLOGIN scraper 그룹·slot별 collector, 전용 alarm-worker login을 먼저 준비한다. `pg_stat_statements`는 PG init-db/관리자 소유이며 업무 baseline이 복제하거나 소유권을 바꾸지 않는다. 기본 권한에 따라 생기는 projection guard의 열 권한과 audit 객체의 불필요한 기본 권한 회수도 유지한다.

## 후속 변경과 읽기 전용 확인

20.0.0의 `manifest.txt`는 기준선 하나만 나열한다. 다음 변경은 사용하지 않은 연속 번호의 `NNN_epoch4_<이름>.sql`을 실제 SQL로 추가하고 같은 순서로 manifest에 올린다. 과거 32개 파일명과 겹치지 않는다. 같은 이름의 `contracts/<파일명>`은 migration이 아닌 **그 head의 전체 물리·권한 계약을 검사하는 읽기 전용 SQL**이다. `public.<대상>`과 `pg_catalog`를 명시한 단일 행·단일 boolean SELECT를 사용하고, 변경되지 않은 필수 권한·객체 계약도 포함한다. 부작용 있는 함수 호출은 금지한다. CLI와 배포 gate는 후보와 같은 소스의 계약을 사용한다.

- 먼저 현재 원장 전체가 정확한 32쌍 또는 새 기준선 1쌍 **중 한쪽**, 그리고 해당 방식에서 실제 실행한 연속 suffix인지 확인한다. 짝 없는 행·누락·해시 불일치·순서 구멍·다른 후보의 추가 행은 SQL 실행 전에 거부한다. 원장은 바이트 원문의 SHA256을 사용한다.
- 해당 DB에 **마지막으로 적용된 head**의 물리·권한을 먼저 검증한다. 아직 만들어지지 않은 후보 객체를 선행 조건으로 읽지 않는다. `--check`는 PostgreSQL READ ONLY 트랜잭션이며 빠진 단계가 있으면 첫 파일명을 반환하고 실패한다. 역할 bootstrap, 업무 SQL, 원장 기록을 하지 않는다.
- 적용 명령은 누락된 suffix만 manifest 순서로 실행한다. 각 파일은 비어 있지 않은 `BEGIN; … COMMIT;` 블록 하나다. runner가 같은 트랜잭션 안에서 각 단계의 SQL → 그 단계의 실물 계약 → 원문 checksum·실행 행을 처리한다. 각 파일 시작과 실물 계약 검사에서 `search_path`를 `pg_catalog, public, pg_temp`로 고정하고 `check_function_bodies`를 켠다. 신규 SQL이 이전 원장의 정의나 기존 행(내용·시각·물리적 행 버전)을 바꾸면 거부한다. 검사에는 같은 호출에서 앞서 추가한 쌍도 포함된다. 한 단계라도 실패하면 이번 호출 전체를 롤백한다. 같은 head 재실행은 검증만 한다. 032 완료 DB에 신규 baseline 영수증은 남지 않는다.
- `--schema-only`는 정상 보호 endpoint에서만 후속 변경을 수행한다. 현재 20.0.0에는 suffix가 없으므로 완료 DB에 쓰기가 없다. collector c를 `quiesce-verified`로 정지할 때는 현재 실적용 head와 그 실물 계약을 읽고 pending suffix를 허용한다. `deploy`·`resume`은 후보의 실제 pooled SQL을 검증하고 활성화 직전 pending 0과 최종 실물 계약을 다시 확인한다. 실패하면 잡고 있던 hold를 해제하지 않는다. 후속 migration을 동반한 배포는 별도 승인된 정비 창에서 진행한다.
- `CONCURRENTLY`, 파일 중간 COMMIT, `PREPARE TRANSACTION`, 배치별 커밋·autocommit 재개는 지원하지 않는다. 기존 영속 sequence의 `nextval`/`setval`(암묵적 ID 할당 포함), 외부 I/O처럼 PostgreSQL rollback의 보호를 받지 못하는 부작용을 원자적인 suffix로 취급하지 않는다. 이런 작업에 대해 실패 시 트랜잭션 밖에서 다시 실행하거나 영수증만 쓰는 경로는 없다. 구 006·031은 구 릴리스 절차에만 속한다.

## 배포·복구 증거

- 구 원본은 `v19.2.0` (`85c236c00433e55c420dc08f2731c06741746b42`)이다. 19.2.1 병합 소스 `efafcf4005878f53ff239f8e8a1b6255e3b59b75`와 이 커밋의 `hololive/hololive-api/scripts/migrations` Git tree는 모두 `17fe597a85a614c948c46ff5471590fbfb666168`다. `lineage_head.sql`의 경로·SHA는 원래 32개 바이트에 대한 수용 값이지 신규 실행 기록이 아니다. 실제 출하 revision은 release 기록에서 별도로 확인한다.
- 선행 19.2.1 source-only sync는 migration 추가/삭제·보호 coordinator 경로 변경을 거부해야 한다. 20.0.0은 그 경로를 재사용하지 않는다. 새 full-source staging, 중앙·AP peer CAS, 릴리스 이미지·프로필 교체에는 별도 승인이 필요하다. 자격 확인은 새 러너가 central PostgreSQL/PgBouncer CID·실행 세대, 서비스·보호 inode 또는 endpoint를 변경할 권한을 주지 않는다.
- DB 수용 후에도 실제 candidate 버전·commit·API/worker image ID와 프로필 쌍, 각 collector/issuer pair, slot별 풀링 endpoint와 역할별 실제 SQL, 멤버별 카드 읽기·준비 상태를 확인하기 전에는 writer를 열지 않는다. 기존 서비스·인프라 inode/세대가 바뀌면 중단한다. 앱 rollback과 DB rollback은 별개다. 신규 설치든 기존 head든 구 migrator 자동실행은 지원하지 않는다. 필요하면 이전 백업을 별도 복원하거나 fix-forward한다.
- 로컬 전용: `bash scripts/ci/verify-epoch4-lineage.sh` (지정된 PG18 이미지를 먼저 준비). 태그 소스로 빌드한 이전 CLI와 현재 CLI를 외부 네트워크·운영 볼륨 없는 서버에서 비교한다. 스키마·ACL·seed 및 sequence/DB 설정, 실제 233쌍 epoch-2 head와 공식 이전 러너의 032 전환, 005·030·031, 원장 변형·물리적 부분 상태를 검사한다. 같은 fixture에서 두 뿌리의 다음 2개 합성 릴리스(원문 SQL과 실제 검증 쿼리 포함)를 빌드해 pending 확인, 바로 다음 단계, 실패 롤백, 후속 권한 누락·shadow schema 거부 및 재실행 무변경을 비교한다. `go test ./hololive/hololive-api/internal/migrationrunner ./hololive/hololive-api/cmd/db-migrate ./hololive/hololive-dbtest`는 독립 fixture의 실제 초기화·트리거·권한·원자 실패/재시도를 검증한다.
- `hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql`은 카탈로그 직렬화이고 실행할 SQL이 아니다. API·dbtest의 신규 설치와 이미지 실행에는 구 SQL/태그/이미지가 필요 없다. 기존 원본 DML의 경계 시험은 보관 태그 checkout에서 재현할 수 있다.
