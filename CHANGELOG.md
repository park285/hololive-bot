# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **security workflow의 shared-go 고정 commit 갱신:** `go.work`가 r1.2.22부터 `../shared-go/sqlite`를 쓰는데, security workflow는 이 모듈이 없는 `postgres/v1.0.4` commit을 받아 main의 NilAway와 govulncheck가 모듈을 읽지 못하고 실패했습니다. 고정 commit을 `sqlite/v1.0.0`(`3a5abfd`)으로 올렸습니다. 검사 내용은 바뀌지 않습니다.

## r1.2.22 - 2026-11-28

- **bot plane 내구 저장소 PostgreSQL → SQLite 전환:** `hololive-api` bot plane의 webhook inbox·heads, 명령 기록, reply outbox, 감사 두 표, webhook nonce를 `BOT_DURABLE_STORE_PATH`(중앙 Quadlet `/app/bot-durable/bot-durable.db`, 호스트 `/opt/hololive-bot/compose/bot-durable/`)의 SQLite 파일로 옮겼습니다. 이 디렉터리는 `hololive-api`에만 마운트해, `current/data`를 함께 쓰는 collector·alarm-worker가 bot 상태에 접근하지 못하게 합니다. `source-observation-replay-epoch`는 이 변수 없이 실행됩니다. 공용 `shared-go/sqlite`를 쓰며 `<path>.lock` 배타 잠금으로 한 프로세스만 소유합니다. 전환은 `bot-durable-admin import-postgres` 일회성 이관(한 스냅숏, 표·상태별 행 수 대조 뒤 한 트랜잭션 커밋)으로 하며, 운영 전환은 API와 alarm-worker를 `--quiesce`로 멈춘 창에서 일회성 container로 실행합니다(`docs/current/runbooks/hololive-api.md`). 인증된 readiness에 `durable_store`를 추가했습니다. 전환 뒤에는 전환 전 image로 돌아가지 않고 fix-forward합니다. SQLite 파일 백업은 아직 없습니다.
- **스크립트 동작 시험과 배포 스크립트 공용 함수의 프로세스 생성 감소:** 시험 fixture와 가짜 명령은 파일을 읽거나 문자열을 바꿀 때마다 하위 셸을 만들고 `cat`·`tr`을 실행했습니다. 배포 스크립트가 불러오는 `postgres_capacity_trim`도 용량 정책과 env 파일의 줄마다 하위 셸을 만들었습니다. 이를 bash 내장 기능(`$(<file)`, `printf -v`, 매개변수 확장)으로 바꾸고, 가짜 명령은 `env`를 거치지 않고 bash를 직접 실행합니다. `compose_env_key_exists_in_file`은 키 목록을 정렬해 `grep`에 넘기던 파이프라인 대신 awk 한 번으로 키를 찾습니다. `collector_slot_matches`는 열마다 슬롯 표를 다시 읽지 않고 awk 한 번으로 모든 열을 대조하며, `po_verify_image`는 image ID 파일을 `cat` 대신 `$(<file)`로 읽고, 적재된 image의 revision·플랫폼·ID를 `podman image inspect` 세 번 대신 한 번에 읽습니다. 용량 검사(`postgres_capacity_effective_env`)는 Quadlet 환경을 정책 키마다 해석하지 않고 서비스마다 한 번 해석하며, `collector_slots_file`은 `dirname` 대신 매개변수 확장으로 디렉터리를 구합니다. 로컬 측정에서 시험이 만드는 프로세스는 pocentral 패키지가 207,656개에서 106,585개로, quadlet 패키지가 97,218개에서 53,191개로 줄었습니다. 용량 판정 결과는 바뀌지 않습니다. 키 존재 확인은 이제 키를 정규식이 아닌 문자열로 비교하므로 `A.C`가 `ABC`와 일치하지 않습니다. 슬롯 대조는 슬롯 표가 비었거나 없을 때 통과하던 결함을 고쳐 실패로 판정합니다.
- **스크립트 동작 시험을 Go 모듈 검사와 병렬 실행:** CI의 스크립트 동작 시험은 다섯 Go 모듈 job이 끝난 뒤에야 시작했습니다. 이 시험을 별도 `script-tests` job으로 옮겨 Go 모듈 job과 함께 돌리고, 필수 `fast-gate`는 두 job이 모두 성공했는지만 판정합니다. 검사 내용은 바뀌지 않습니다.
- **릴리스:** 루트 VERSION 1.2.22, hololive-api 1.2.15, hololive-alarm-worker 1.0.13(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).

## r1.2.21 - 2026-11-27

- **관측 어휘 텍스트 열 삭제(migration 013):** r1.2.20에서 id로 옮긴 `source_observations`의 `provider`, `observation_kind`, `completeness`, `continuity` 텍스트 열과 이름 일치 FK, vocab CHECK, `derive_source_observation_vocabulary` trigger와 함수, 이전 인덱스 `idx_source_observations_kind_received_id`(운영 54MB)를 지우고 표를 다시 씁니다. completeness와 continuity는 사전 표 id를 직접 참조하고, provider와 kind는 계약 표의 id 쌍 FK로 검증합니다. 운영 SQL은 r1.2.20에서 이미 id만 쓰므로 바뀌지 않았고, 시험 fixture는 id 열로 고쳤습니다. 운영 규모 합성 표(108만 행)에서 migration 013은 4.2초였고, heap은 314MB에서 265MB로, 인덱스는 204MB에서 150MB로 줄었습니다. 실행 중인 r1.2.20 collector와 API는 텍스트 열을 쓰지 않으므로 대기 migration 적용 창에서 적용하고 collector는 교체하지 않습니다. 이 migration 뒤에는 API와 collector를 r1.2.19 이하로 되돌릴 수 없습니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.21, hololive-api 1.2.14, hololive-alarm-worker 1.0.12(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).

## r1.2.20 - 2026-11-27

- **관측 어휘 열을 smallint 사전 id로(migration 012):** `source_observations`의 `provider`, `observation_kind`, `completeness`, `continuity`를 고정 사전 표 4개(`source_observation_providers`, `source_observation_kinds`, `source_observation_completeness_values`, `source_observation_continuity_values`)의 `smallint` id 열로도 저장합니다. `observation_contract_generations`에도 `provider_id`, `observation_kind_id`와 id 기준 unique를 더해 관측의 계약 FK를 id로 겁니다. 이번 단계에서는 텍스트 열을 지우지 않습니다. `derive_source_observation_vocabulary` trigger가 INSERT 때 빠진 쪽을 채우고, `(id, name)` FK가 두 값의 일치를 강제합니다. 새 collector는 id만 씁니다. API 소비 SQL과 보존·fence·잠금 DB 함수는 id로 거르고 이름은 사전 표에서 읽습니다. 보존 삭제는 새 인덱스 `idx_source_observations_kind_id_received_id`를 씁니다. evidence hash와 Go 계약 값은 계속 텍스트 이름을 쓰므로 바뀌지 않습니다. 운영 규모 합성 표(108만 행)에서 migration 전체는 12.8초였습니다. 텍스트 열과 이전 인덱스를 지우고 표를 다시 쓰는 다음 단계까지 마치면 heap은 303MB에서 265MB로, 인덱스는 198MB에서 150MB로 줄어듭니다. 같은 어휘를 쓰는 다른 16개 표의 23개 열은 이번 범위가 아닙니다. Fallback delta: none.
- **대기 migration 창의 collector 호환성:** migration 012는 collector가 쓰는 표를 바꾸지만, r1.2.19 collector의 텍스트 INSERT는 trigger가 id를 채우므로 그대로 동작합니다. r1.2.19 collector의 게시 SQL을 012 스키마에 넣고 collector 시험 전체를 돌렸고, collector DB identity로 이름만 쓴 INSERT와 id만 쓴 INSERT가 모두 fence를 통과하는 시험을 더했습니다. 그래서 012는 대기 migration 적용 창에서 적용하고 collector는 API 수용 뒤에 교체합니다. 012 뒤에도 API와 collector를 r1.2.19로 되돌릴 수 있습니다. 텍스트 열을 지우는 다음 migration 뒤에는 되돌릴 수 없습니다.
- **구형 `SendMarkdown` 전달 경로 정리:** `iris-client-go`를 `v6.1.0`으로 올리고 퇴역한 `RuntimeIrisClient.SendMarkdown`과 테스트 스텁을 제거했습니다. Markdown 응답은 기존과 같이 `SendRichText`(`type:"richtext"`) 경로만 사용합니다. Fallback delta: none.
- **스크립트 시험 모듈 lint·vet 복원:** r1.2.19에서 스크립트 시험을 제품 모듈과 분리하면서 `scripts/scripttest`가 `golangci-lint`와 canonical vet 범위에서 빠졌습니다. 그사이 생긴 시험 하네스 위반 7건(Mutex embedding, 미확인 type assertion, 공백)을 고치고, 시험 대상 패키지를 lint·vet에 다시 넣었습니다.
- **릴리스:** 루트 VERSION 1.2.20, hololive-api 1.2.13, hololive-alarm-worker 1.0.11. `iris-client-go` v6.1.0 갱신과 `SendMarkdown` 제거(#906)가 `hololive-shared`를 바꾸므로 alarm-worker image도 바뀝니다.
