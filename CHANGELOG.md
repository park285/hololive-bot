# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **구형 `SendMarkdown` 전달 경로 정리:** `iris-client-go`를 `v6.1.0`으로 올리고 퇴역한 `RuntimeIrisClient.SendMarkdown`과 테스트 스텁을 제거했습니다. Markdown 응답은 기존과 같이 `SendRichText`(`type:"richtext"`) 경로만 사용합니다. Fallback delta: none.

## r1.2.19 - 2026-11-27

- **방화벽 사전 검사의 jq 1.8 호환:** `scripts/deploy/lib/admin-bind.sh`의 jq 필터가 jq 1.8.1에서 문법 오류를 내던 문제를 고쳤습니다. 함수 선언과 입력 바인딩을 조건식 앞으로 옮겼고 허용·거부 조건은 같습니다. Fallback delta: none.
- **스크립트 동작 시험 분리:** 스크립트 시험을 제품 모듈 게이트와 분리해 패키지 병렬도 1, 시험 병렬도 4로 실행하고, PR CI에서 전체를 실행하며 skip과 누락된 결과를 실패로 판정합니다. 호출되지 않던 `go-lint-matrix.py`·`resolve-go-tool.py` 사본과 Dockerfile 문자열·Trivy 인자 자기검사를 지웠습니다.
- **재사용 일정 시각을 payload 밖으로(migration 011):** live snapshot에서 helper가 재사용한 예정 시각의 확인 슬롯(`schedule_observed_at`)을 payload 세션이 아니라 관측 행의 새 열 `source_observations.schedule_observed_at`(video ID별 시각)에 싣습니다. 이 값은 `evidence_sha256`에 포함되고, 값이 없는 관측의 hash는 이전과 같습니다. 운영 하루치 `live_snapshot` payload 17,457개가 이 값만 빼면 1,986개로 줄어듭니다(-89%). API는 두 모양을 모두 읽고, 같은 시각을 두 곳에 싣거나 목록에 없는 영상·UPCOMING이 아닌 세션·재사용 창 밖 시각을 실은 관측은 거부합니다. collector가 새 모양으로 발행하려면 이 릴리스의 collector로 바꿔야 하며, 반드시 API를 먼저 수용한 뒤 교체합니다. 새 collector가 발행한 관측이 남은 채 API를 r1.2.18 이하로 되돌리면 그 관측은 `invalid_payload`로 dead letter가 됩니다. Fallback delta: none.
- **`source_observations.created_at` 삭제(migration 011):** 항상 `received_at`과 같던 열을 지웠습니다(운영에서 다른 행 0건). 어느 코드도 읽거나 쓰지 않았습니다. migration 011은 대기 migration이므로 "대기 migration 적용 창"에서 적용합니다. 실행 중인 collector는 두 열 이름을 쓰지 않으므로 그대로 둡니다. 새 열은 비어 있지 않은 4096바이트 이하 객체만 받습니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.19, hololive-api 1.2.12, hololive-alarm-worker 1.0.10(공유 계약 변경으로 image가 바뀜).
- **프로필 같은 값 `REPLAY` 행 생략:** r1.2.18이 남겨 둔 프로필 필드별 `REPLAY`(이미 같은 값인 필드를 다시 관측한 경우)도 감사 행을 만들지 않습니다. r1.2.18 배포 뒤 8분 동안 15행이 생겼습니다. Fallback delta: none.

## r1.2.18 - 2026-11-27

- **변화 없는 결정의 감사 행 생략:** `source_observation_applications`에 정본 상태를 바꾼 결정만 남깁니다. 값이 같은 더 새 영상 목록 관측(누락 해제가 없을 때), 바뀐 것이 없는 영상·프로필·사진 `REPLAY`, 프로필·사진의 `RAW_RETAINED`는 더 이상 행을 만들지 않습니다. 운영 감사 323만 행 중 `youtube_video APPLIED` 82만 행이 영상 472개에서 나왔고 대부분 값이 같은 반복 관측이었습니다. `youtube_live_session` 결정은 보존 보호의 최신 근거라 모두 남기고, 커뮤니티 창 `CANONICALIZED`도 그대로입니다. migration은 없습니다. Fallback delta: none.

## r1.2.17 - 2026-11-27

- **관측 보존 인덱스 축소(migration 010):** 보존 함수가 LIVE·UPCOMING 세션의 최신 적용 관측을 찾으려고 쓰던 `idx_source_application_entity_latest`(운영 212MB)를 지웠습니다. 대신 새 표 `youtube_live_session_application_heads`가 세션별 최대 observation ID를 들고, `write_finalize_applications.sql`이 감사 행과 같은 문장에서 이 값을 올립니다. 늦게 도착한 이전 관측은 값을 되돌리지 않고, 관측이 지워지면 FK CASCADE로 함께 지워집니다. migration은 기존 감사에서 세션별 최대 ID를 채운 뒤 인덱스를 지웁니다.
- **관측 identity unique 축소(migration 010):** `uq_source_observation_identity`를 여섯 열에서 `(observation_key, schema_version, contract_generation)`으로 줄였습니다. `observation_key`는 provider·kind·subject·scope·예정 시각의 SHA-256이고 collector가 envelope 검증에서 다시 계산하므로 유일성 의미는 같습니다. 운영 감사에서 세 열 기준 중복은 0건이었습니다. 운영 규모 합성 데이터에서 인덱스는 176MB에서 69MB로 줄었고 migration 전체는 약 1.1초였습니다. migration 010은 대기 migration이므로 `docs/current/runbooks/release.md`의 "대기 migration 적용 창"에서 적용합니다. Fallback delta: none.
- **PostgreSQL `checkpoint_timeout` 30분:** `holo-postgres.container`의 `checkpoint_timeout`을 900s에서 1800s로 올렸습니다. 운영 WAL의 67%가 full-page image이고, 체크포인트 주기마다 페이지의 첫 수정만 full-page image를 남기므로 이 비율을 줄이려는 변경입니다. crash recovery 재생량은 늘어납니다. unit 설치 뒤 `holo-postgres.service`를 재시작해야 적용됩니다. Fallback delta: none.