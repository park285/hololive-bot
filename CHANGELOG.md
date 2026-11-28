# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **handoff 실패 발송의 관리자 취소·격리:** Iris가 요청을 받은 뒤 handoff가 실패한 DLQ(`handoff_failed`)와 확인되지 않은 격리(`handoff_unconfirmed`)에는 `sent_at`이 남습니다. 관리자 `settle`이 이 표식을 이미 보낸 발송으로 보고 409를 돌려줘, 이런 행은 보존 기간 180일이 지날 때까지 발송 실패 건수에 남았습니다. 이제 `sent_at`이 있어도 `dlq`·`quarantined` 묶음을 취소하거나 격리할 수 있습니다. `sent_at`은 보존되므로 수동 재처리 차단은 그대로이고, 상태가 `sent`인 묶음과 이미 취소된 묶음은 계속 거절합니다. Fallback delta: none.

## r1.2.23 - 2026-11-28

- **PostgreSQL bot plane 표와 함수 삭제(migration 014):** r1.2.22에서 SQLite로 옮긴 bot plane 표 7개(`bot_webhook_inbox`·`bot_webhook_heads`·`bot_command_executions`·`bot_reply_outbox`·감사 두 표·`bot_webhook_nonces`)와 `reserve_bot_webhook_nonce`·replay·discard·감사 trigger 함수 7개를 PostgreSQL에서 지웁니다. 이 표는 r1.2.22 전환 뒤 갱신되지 않았고 현행 runtime은 읽거나 쓰지 않습니다. 일회성 이관 명령 `bot-durable-admin import-postgres`와 그 SQLite 적재 경로, PostgreSQL bot 표를 검사하던 시험도 지웠습니다. head 계약은 표·함수가 없음을 확인합니다. 014는 대기 migration 적용 창(collector 정지 뒤 `--resume`)에서 적용합니다. PostgreSQL bot 표를 쓰던 r1.2.21 이하 API로는 이 migration 뒤에 돌아갈 수 없습니다. Fallback delta: none.
- **정산 쓰기 우선순위 복원:** PostgreSQL 시절 `pgxdb.WithControlAcquire`가 webhook 정산·reply 정산·heartbeat·handoff claim 9곳에 주던 우선 획득이 SQLite 전환에서 빠졌습니다. 이 9곳에 shared-go `sqlite/v1.0.1`의 `sqlitedb.WithWritePriority`를 다시 표시해, 대기 중인 일반 쓰기보다 먼저 SQLite writer를 받게 했습니다.
- **bot plane 작업자 지표 label:** `bot_webhook_inbox`·`bot_reply_outbox` 작업자의 `queue_backend` label이 `postgres`에서 `sqlite`로 바뀝니다. 두 작업자는 r1.2.22부터 SQLite 상태 DB를 씁니다.
- **security workflow의 shared-go 고정 commit 갱신:** `go.work`가 r1.2.22부터 `../shared-go/sqlite`를 쓰는데, security workflow는 이 모듈이 없는 `postgres/v1.0.4` commit을 받아 main의 NilAway와 govulncheck가 모듈을 읽지 못하고 실패했습니다. 고정 commit을 `sqlite/v1.0.1`(`78b8d8a`)으로 올렸습니다. 검사 내용은 바뀌지 않습니다.
- **ChatBotGo PostgreSQL 방화벽 규칙 삭제:** ChatBotGo가 r1.0.15에서 PostgreSQL을 퇴역해 `admin-dashboard-ingress.nft`의 5432 게시 규칙(`bot_postgres_sources` 집합과 input·forward의 허용·거부 규칙)을 지웠습니다. hololive PostgreSQL(5433) 규칙은 그대로입니다. 설치 뒤 `systemctl reload admin-dashboard-ingress-firewall.service`로 적용합니다.
- **릴리스:** 루트 VERSION 1.2.23, hololive-api 1.2.16, hololive-alarm-worker 1.0.14(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).

## r1.2.22 - 2026-11-28

- **bot plane 내구 저장소 PostgreSQL → SQLite 전환:** `hololive-api` bot plane의 webhook inbox·heads, 명령 기록, reply outbox, 감사 두 표, webhook nonce를 `BOT_DURABLE_STORE_PATH`(중앙 Quadlet `/app/bot-durable/bot-durable.db`, 호스트 `/opt/hololive-bot/compose/bot-durable/`)의 SQLite 파일로 옮겼습니다. 이 디렉터리는 `hololive-api`에만 마운트해, `current/data`를 함께 쓰는 collector·alarm-worker가 bot 상태에 접근하지 못하게 합니다. `source-observation-replay-epoch`는 이 변수 없이 실행됩니다. 공용 `shared-go/sqlite`를 쓰며 `<path>.lock` 배타 잠금으로 한 프로세스만 소유합니다. 전환은 `bot-durable-admin import-postgres` 일회성 이관(한 스냅숏, 표·상태별 행 수 대조 뒤 한 트랜잭션 커밋)으로 하며, 운영 전환은 API와 alarm-worker를 `--quiesce`로 멈춘 창에서 일회성 container로 실행합니다(`docs/current/runbooks/hololive-api.md`). 인증된 readiness에 `durable_store`를 추가했습니다. 전환 뒤에는 전환 전 image로 돌아가지 않고 fix-forward합니다. SQLite 파일 백업은 아직 없습니다.
- **스크립트 동작 시험과 배포 스크립트 공용 함수의 프로세스 생성 감소:** 시험 fixture와 가짜 명령은 파일을 읽거나 문자열을 바꿀 때마다 하위 셸을 만들고 `cat`·`tr`을 실행했습니다. 배포 스크립트가 불러오는 `postgres_capacity_trim`도 용량 정책과 env 파일의 줄마다 하위 셸을 만들었습니다. 이를 bash 내장 기능(`$(<file)`, `printf -v`, 매개변수 확장)으로 바꾸고, 가짜 명령은 `env`를 거치지 않고 bash를 직접 실행합니다. `compose_env_key_exists_in_file`은 키 목록을 정렬해 `grep`에 넘기던 파이프라인 대신 awk 한 번으로 키를 찾습니다. `collector_slot_matches`는 열마다 슬롯 표를 다시 읽지 않고 awk 한 번으로 모든 열을 대조하며, `po_verify_image`는 image ID 파일을 `cat` 대신 `$(<file)`로 읽고, 적재된 image의 revision·플랫폼·ID를 `podman image inspect` 세 번 대신 한 번에 읽습니다. 용량 검사(`postgres_capacity_effective_env`)는 Quadlet 환경을 정책 키마다 해석하지 않고 서비스마다 한 번 해석하며, `collector_slots_file`은 `dirname` 대신 매개변수 확장으로 디렉터리를 구합니다. 로컬 측정에서 시험이 만드는 프로세스는 pocentral 패키지가 207,656개에서 106,585개로, quadlet 패키지가 97,218개에서 53,191개로 줄었습니다. 용량 판정 결과는 바뀌지 않습니다. 키 존재 확인은 이제 키를 정규식이 아닌 문자열로 비교하므로 `A.C`가 `ABC`와 일치하지 않습니다. 슬롯 대조는 슬롯 표가 비었거나 없을 때 통과하던 결함을 고쳐 실패로 판정합니다.
- **스크립트 동작 시험을 Go 모듈 검사와 병렬 실행:** CI의 스크립트 동작 시험은 다섯 Go 모듈 job이 끝난 뒤에야 시작했습니다. 이 시험을 별도 `script-tests` job으로 옮겨 Go 모듈 job과 함께 돌리고, 필수 `fast-gate`는 두 job이 모두 성공했는지만 판정합니다. 검사 내용은 바뀌지 않습니다.
- **릴리스:** 루트 VERSION 1.2.22, hololive-api 1.2.15, hololive-alarm-worker 1.0.13(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).

## r1.2.21 - 2026-11-27

- **관측 어휘 텍스트 열 삭제(migration 013):** r1.2.20에서 id로 옮긴 `source_observations`의 `provider`, `observation_kind`, `completeness`, `continuity` 텍스트 열과 이름 일치 FK, vocab CHECK, `derive_source_observation_vocabulary` trigger와 함수, 이전 인덱스 `idx_source_observations_kind_received_id`(운영 54MB)를 지우고 표를 다시 씁니다. completeness와 continuity는 사전 표 id를 직접 참조하고, provider와 kind는 계약 표의 id 쌍 FK로 검증합니다. 운영 SQL은 r1.2.20에서 이미 id만 쓰므로 바뀌지 않았고, 시험 fixture는 id 열로 고쳤습니다. 운영 규모 합성 표(108만 행)에서 migration 013은 4.2초였고, heap은 314MB에서 265MB로, 인덱스는 204MB에서 150MB로 줄었습니다. 실행 중인 r1.2.20 collector와 API는 텍스트 열을 쓰지 않으므로 대기 migration 적용 창에서 적용하고 collector는 교체하지 않습니다. 이 migration 뒤에는 API와 collector를 r1.2.19 이하로 되돌릴 수 없습니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.21, hololive-api 1.2.14, hololive-alarm-worker 1.0.12(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).
