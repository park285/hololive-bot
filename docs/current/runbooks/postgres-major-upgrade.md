# PostgreSQL major 전환 (18 → 19)

## 범위

중앙 `hololive-seoul`의 `holo-postgres`를 PostgreSQL 18에서 19로 옮기는 절차입니다. `pg_upgrade`가 아니라
dump/restore로 새 볼륨 `hololive-bot_holo-pg-data19`에 옮기며, 기존 `hololive-bot_holo-pg-data`는 읽기만 하고
롤백용으로 보존합니다. 도구는 `scripts/deploy/pg-major-upgrade.sh`이고, root로
`/opt/hololive-bot/compose/current`에 설치된 사본만 실행할 수 있습니다.

운영 실행은 `hololive-bot-ops`의 승인된 정비 창에서 하고, 이미지 적재와 `compose.env` 변경은 `stack-platform-ops`가
소유합니다. 전환 중 dump 파일은 운영 데이터의 전체 사본이므로 보존 기간은 백업 정책의 별도 승인을 따릅니다.

## 순서

1. 이 리비전의 PostgreSQL 19 이미지(arm64)를 서울에 적재합니다.
2. 이 리비전을 `sync-opt-current.sh` 전체 모드로 설치합니다. unit과 `quadlet-prestart`를 설치하고 `daemon-reload`만
   하며 `holo-postgres`는 재시작하지 않습니다. 이때부터 `holo-postgres`가 다시 기동되면 prestart가 빈 19 볼륨의
   기동을 거부하므로(아래 보호 장치), 2~4단계는 같은 정비 창에서 이어서 진행합니다.
3. `stack-platform-ops`로 `compose.env`의 `POSTGRES_IMAGE`를 19 이미지 ID(`sha256:<id>`)로 바꿉니다(dry-run 뒤 apply).
   되돌릴 때 쓰도록 현재 18 이미지 ID를 기록해 둡니다.
4. `sudo /opt/hololive-bot/compose/current/scripts/deploy/pg-major-upgrade.sh --check`로 사전 검사만 하고,
   통과하면 옵션 없이 실행합니다.
5. 전환이 끝난 뒤에 이 리비전의 앱 배포와 `hololive-db-migrate` schema job을 진행합니다. 이 리비전의 풀러 인증
   bootstrap은 `pg_stat_statements` 1.13을 요구하므로 PostgreSQL 18에서는 schema job이 실패합니다. 반대로 이전
   리비전의 schema job은 1.12를 요구하므로 전환 뒤에는 이전 이미지로 schema job을 실행하지 않습니다. 전환 직후에는
   설치되어 있던 이전 앱이 19에서 다시 기동하며, 이전 앱 코드에는 PostgreSQL major 검사가 없습니다.

## 사전 검사

`--check`와 본 실행은 같은 검사를 먼저 합니다. 하나라도 실패하면 아무것도 멈추지 않고 끝납니다.

- 전환 표식 `/var/lib/hololive-bot/.pg-major.pending`이 없어야 합니다.
- 설치된 `holo-postgres.container`, `holo-pg-data.volume`, `quadlet-prestart`가 이 리비전의 파일과 같아야 합니다.
- `holo-postgres`가 활성이고 `hololive-db-migrate`는 실행 중이 아니어야 합니다.
- 실행 중인 컨테이너는 대상 볼륨이 아닌 볼륨을 쓰고, 대상 볼륨은 아직 없어야 합니다.
- 실행 중인 이미지는 major 18, `POSTGRES_IMAGE`는 적재된 major 19 이미지이며 두 이미지의 아키텍처가 같아야 합니다.
- 상태 디렉터리와 Podman 저장소에 DB 크기의 세 배 이상 여유가 있어야 합니다.
- init-db가 만들지 않는 역할(`iris_db_nightly`, `postgres_exporter` 등), 멤버십, 역할·DB 설정, DB 권한을 옮길 SQL을
  만들 수 있어야 합니다. bootstrap 목록 밖의 superuser·replication·bypassrls 역할이 있으면 수동 검토를 요구하며 멈춥니다.

## 동작

1. 표식을 `phase=dump`로 쓰고 `hololive-api`, `hololive-alarm-worker`, 중앙 collector, `holo-pgbouncer`를 멈춥니다.
   앱 세션은 모두 풀러를 거치므로 이후에는 관리자, exporter, nightly 세션만 남아야 하며 아니면 멈춥니다.
   다른 host의 collector a·b·d는 계속 실행되며 풀러가 돌아올 때까지 접속에 실패하고 재시도합니다.
2. `/var/lib/hololive-bot/pg-major/<UTC>`(0700)에 `pg_dump -Fc`, 역할 SQL, 원본 지문을 만들고 18을 멈춥니다.
3. 표식을 `phase=restore`로 바꾸고 19를 새 볼륨에서 기동합니다(init-db가 bootstrap 역할과 확장을 만듭니다).
   unit의 healthy 검사는 init 중 임시 서버에도 통과하므로, 컨테이너의 PID 1이 `postgres`가 되고 접속을 받을 때까지
   최대 600초 더 기다린 뒤에 SQL을 보냅니다.
4. DB 연결 한도를 0으로 두고 일반 세션을 끊은 뒤, 역할 SQL을 한 트랜잭션으로 적용하고, init-db가 미리 만든 기본 권한을
   비운 다음 `pg_restore --single-transaction --exit-on-error`로 복원합니다. 기본 권한은 dump가 복원 마지막에
   다시 만듭니다. 미리 두면 복원 중 생기는 identity 시퀀스가 원본에 없던 `hololive_runtime` 권한을 받습니다.
5. 지문(역할, 멤버십, 설정, DB·스키마·관계·열·함수 권한, 기본 권한, 제약, 인덱스, 트리거, 타입, 시퀀스 값, 테이블별 행
   해시)이 원본과 같아야 합니다. CHECK와 인덱스 정의는 19가 중첩 AND의 괄호를 다르게 출력하므로 괄호와 공백을 뺀
   값으로 비교합니다.
6. `ANALYZE` 뒤 원래 연결 한도를 복원하고 표식을 지운 다음, 멈췄던 unit을 풀러, collector, worker, API 순서로 기동합니다.

쓰기 중단 시간은 1단계부터 6단계까지이며 dump와 복원 시간이 대부분입니다.

## 보호 장치

- 표식이 있으면 prestart가 `holo-pgbouncer`의 기동을 거부하고, `holo-postgres`는 `phase=restore`일 때만 기동합니다.
- 표식이 없을 때도 대상 볼륨에 클러스터가 없고 이전 `hololive-bot_holo-pg-data*` 볼륨이 있으면 prestart가
  `holo-postgres`의 기동을 거부합니다. 전체 설치 뒤 재시작이 빈 19 클러스터를 initdb하는 것을 막습니다.

## 실패와 복구

18을 멈추기 전에 실패하면 도구가 표식을 지우고 멈춘 unit을 다시 기동합니다. 18은 계속 서비스합니다.

18을 멈춘 뒤 실패하면 `holo-postgres`를 멈추고 표식을 남기며 쓰기 서비스도 멈춘 채로 둡니다. 도구가 출력하는 순서로
18로 돌아갑니다. `stack-platform-ops`로 `POSTGRES_IMAGE`를 기록한 18 이미지 ID로 되돌리고, 이전 리비전의
`holo-postgres.container`와 `holo-pg-data.volume`을 설치한 뒤 `systemctl daemon-reload`를 실행합니다. 그다음 표식을
지우고 `holo-postgres`와 멈췄던 unit을 기동합니다. 다시 시도하려면 먼저 18로 돌아간 뒤 대상 볼륨을 확인하고 지웁니다.

전환이 끝난 뒤의 18 볼륨은 전환 시점의 사본입니다. 그 뒤로 19에 쓴 데이터는 18 볼륨에 없으므로, 전환 후 롤백은
데이터 손실을 감수하는 별도 결정입니다.

## 정리

전환 디렉터리의 dump는 비밀번호 해시를 포함한 전체 사본입니다. 보존 여부를 정한 뒤 지우며, 이전 볼륨 삭제도 별도
승인 범위입니다.
