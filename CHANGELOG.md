# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **스크립트 동작 시험을 Go 모듈 검사와 병렬 실행:** CI의 스크립트 동작 시험은 다섯 Go 모듈 job이 끝난 뒤에야 시작했습니다. 이 시험을 별도 `script-tests` job으로 옮겨 Go 모듈 job과 함께 돌리고, 필수 `fast-gate`는 두 job이 모두 성공했는지만 판정합니다. 검사 내용은 바뀌지 않습니다.

## r1.2.21 - 2026-11-27

- **관측 어휘 텍스트 열 삭제(migration 013):** r1.2.20에서 id로 옮긴 `source_observations`의 `provider`, `observation_kind`, `completeness`, `continuity` 텍스트 열과 이름 일치 FK, vocab CHECK, `derive_source_observation_vocabulary` trigger와 함수, 이전 인덱스 `idx_source_observations_kind_received_id`(운영 54MB)를 지우고 표를 다시 씁니다. completeness와 continuity는 사전 표 id를 직접 참조하고, provider와 kind는 계약 표의 id 쌍 FK로 검증합니다. 운영 SQL은 r1.2.20에서 이미 id만 쓰므로 바뀌지 않았고, 시험 fixture는 id 열로 고쳤습니다. 운영 규모 합성 표(108만 행)에서 migration 013은 4.2초였고, heap은 314MB에서 265MB로, 인덱스는 204MB에서 150MB로 줄었습니다. 실행 중인 r1.2.20 collector와 API는 텍스트 열을 쓰지 않으므로 대기 migration 적용 창에서 적용하고 collector는 교체하지 않습니다. 이 migration 뒤에는 API와 collector를 r1.2.19 이하로 되돌릴 수 없습니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.21, hololive-api 1.2.14, hololive-alarm-worker 1.0.12(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).

## r1.2.20 - 2026-11-27

- **관측 어휘 열을 smallint 사전 id로(migration 012):** `source_observations`의 `provider`, `observation_kind`, `completeness`, `continuity`를 고정 사전 표 4개(`source_observation_providers`, `source_observation_kinds`, `source_observation_completeness_values`, `source_observation_continuity_values`)의 `smallint` id 열로도 저장합니다. `observation_contract_generations`에도 `provider_id`, `observation_kind_id`와 id 기준 unique를 더해 관측의 계약 FK를 id로 겁니다. 이번 단계에서는 텍스트 열을 지우지 않습니다. `derive_source_observation_vocabulary` trigger가 INSERT 때 빠진 쪽을 채우고, `(id, name)` FK가 두 값의 일치를 강제합니다. 새 collector는 id만 씁니다. API 소비 SQL과 보존·fence·잠금 DB 함수는 id로 거르고 이름은 사전 표에서 읽습니다. 보존 삭제는 새 인덱스 `idx_source_observations_kind_id_received_id`를 씁니다. evidence hash와 Go 계약 값은 계속 텍스트 이름을 쓰므로 바뀌지 않습니다. 운영 규모 합성 표(108만 행)에서 migration 전체는 12.8초였습니다. 텍스트 열과 이전 인덱스를 지우고 표를 다시 쓰는 다음 단계까지 마치면 heap은 303MB에서 265MB로, 인덱스는 198MB에서 150MB로 줄어듭니다. 같은 어휘를 쓰는 다른 16개 표의 23개 열은 이번 범위가 아닙니다. Fallback delta: none.
- **대기 migration 창의 collector 호환성:** migration 012는 collector가 쓰는 표를 바꾸지만, r1.2.19 collector의 텍스트 INSERT는 trigger가 id를 채우므로 그대로 동작합니다. r1.2.19 collector의 게시 SQL을 012 스키마에 넣고 collector 시험 전체를 돌렸고, collector DB identity로 이름만 쓴 INSERT와 id만 쓴 INSERT가 모두 fence를 통과하는 시험을 더했습니다. 그래서 012는 대기 migration 적용 창에서 적용하고 collector는 API 수용 뒤에 교체합니다. 012 뒤에도 API와 collector를 r1.2.19로 되돌릴 수 있습니다. 텍스트 열을 지우는 다음 migration 뒤에는 되돌릴 수 없습니다.
- **구형 `SendMarkdown` 전달 경로 정리:** `iris-client-go`를 `v6.1.0`으로 올리고 퇴역한 `RuntimeIrisClient.SendMarkdown`과 테스트 스텁을 제거했습니다. Markdown 응답은 기존과 같이 `SendRichText`(`type:"richtext"`) 경로만 사용합니다. Fallback delta: none.
- **스크립트 시험 모듈 lint·vet 복원:** r1.2.19에서 스크립트 시험을 제품 모듈과 분리하면서 `scripts/scripttest`가 `golangci-lint`와 canonical vet 범위에서 빠졌습니다. 그사이 생긴 시험 하네스 위반 7건(Mutex embedding, 미확인 type assertion, 공백)을 고치고, 시험 대상 패키지를 lint·vet에 다시 넣었습니다.
- **릴리스:** 루트 VERSION 1.2.20, hololive-api 1.2.13, hololive-alarm-worker 1.0.11. `iris-client-go` v6.1.0 갱신과 `SendMarkdown` 제거(#906)가 `hololive-shared`를 바꾸므로 alarm-worker image도 바뀝니다.

## r1.2.19 - 2026-11-27

- **방화벽 사전 검사의 jq 1.8 호환:** `scripts/deploy/lib/admin-bind.sh`의 jq 필터가 jq 1.8.1에서 문법 오류를 내던 문제를 고쳤습니다. 함수 선언과 입력 바인딩을 조건식 앞으로 옮겼고 허용·거부 조건은 같습니다. Fallback delta: none.
- **스크립트 동작 시험 분리:** 스크립트 시험을 제품 모듈 게이트와 분리해 패키지 병렬도 1, 시험 병렬도 4로 실행하고, PR CI에서 전체를 실행하며 skip과 누락된 결과를 실패로 판정합니다. 호출되지 않던 `go-lint-matrix.py`·`resolve-go-tool.py` 사본과 Dockerfile 문자열·Trivy 인자 자기검사를 지웠습니다.
- **재사용 일정 시각을 payload 밖으로(migration 011):** live snapshot에서 helper가 재사용한 예정 시각의 확인 슬롯(`schedule_observed_at`)을 payload 세션이 아니라 관측 행의 새 열 `source_observations.schedule_observed_at`(video ID별 시각)에 싣습니다. 이 값은 `evidence_sha256`에 포함되고, 값이 없는 관측의 hash는 이전과 같습니다. 운영 하루치 `live_snapshot` payload 17,457개가 이 값만 빼면 1,986개로 줄어듭니다(-89%). API는 두 모양을 모두 읽고, 같은 시각을 두 곳에 싣거나 목록에 없는 영상·UPCOMING이 아닌 세션·재사용 창 밖 시각을 실은 관측은 거부합니다. collector가 새 모양으로 발행하려면 이 릴리스의 collector로 바꿔야 하며, 반드시 API를 먼저 수용한 뒤 교체합니다. 새 collector가 발행한 관측이 남은 채 API를 r1.2.18 이하로 되돌리면 그 관측은 `invalid_payload`로 dead letter가 됩니다. Fallback delta: none.
- **`source_observations.created_at` 삭제(migration 011):** 항상 `received_at`과 같던 열을 지웠습니다(운영에서 다른 행 0건). 어느 코드도 읽거나 쓰지 않았습니다. migration 011은 대기 migration이므로 "대기 migration 적용 창"에서 적용합니다. 실행 중인 collector는 두 열 이름을 쓰지 않으므로 그대로 둡니다. 새 열은 비어 있지 않은 4096바이트 이하 객체만 받습니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.19, hololive-api 1.2.12, hololive-alarm-worker 1.0.10(공유 계약 변경으로 image가 바뀜).
- **프로필 같은 값 `REPLAY` 행 생략:** r1.2.18이 남겨 둔 프로필 필드별 `REPLAY`(이미 같은 값인 필드를 다시 관측한 경우)도 감사 행을 만들지 않습니다. r1.2.18 배포 뒤 8분 동안 15행이 생겼습니다. Fallback delta: none.
