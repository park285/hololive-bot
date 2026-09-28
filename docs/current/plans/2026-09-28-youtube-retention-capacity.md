# YouTube 관측 보존 기간과 DB 증가 제어

**Decisions:** `DEC-20260928-youtube-retention-capacity` (governing), `DEC-20260824-hololive-dependent-retention` (constraint), `DEC-20260926-hololive-live-absence-evidence` (context), `DEC-20260908-infrastructure-efficiency-with-retention` (context)

## Execution capsule

**Goal:** 증설 없이 Hololive 운영 DB의 YouTube 관측·적용 이력 증가를 제어한다.
**Context:** 2026-09-28 운영 DB는 25 GB, 호스트 여유는 45 GiB이다. 원본 14 GB, 적용 이력 6205 MB, 종료 증거 673 MB이며 종료 증거는 계속 증가한다.
**Constraints:** queue·pending replay·live-head 보호와 적용 이력 orphan 조건을 유지한다. 호스트 비밀을 출력하지 않고, 중앙 API 재시작·운영 설정·migration 적용은 각 승인 범위에서 진행한다. in-flight live evidence 작업과 파일 충돌을 피한다.
**Evidence:** `docs/review/2026-09-28-youtube-retention-capacity.md`의 guarded read-only SQL, 실제 container retention 값, repository retention SQL/Go와 migration 192를 사용한다.
**Success:** 승인된 보존 기간이 적용되고 현재 image·readiness를 확인한다. 삭제 처리량과 WAL·dead tuple·DB 크기를 관측하여 적체가 줄고 증가율이 안정되는지 판정한다. 종료 증거의 무기한 증가에는 별도 안전한 수명 계약을 확정한다.
**Output:** 보존 정책 DEC, exact 운영 변경과 rollback 지점, 관측 기록, 종료 증거 30일 수명 구현과 APPLIED 이력 절감 검토이다.

## 작업

### T01 보존 단축의 후보·처리량 기준선을 확정한다

Owner: stack-platform-ops. 읽기 전용 guard와 15초 statement timeout을 사용한다. 표본 후보·최근 유입, 현재 환경변수, DB·테이블·WAL·호스트 여유, dead tuple·autovacuum, retention 오류·실제 삭제량을 기록한다. 전체 행 count가 DB에 부담을 주면 재시도하지 않는다. `docs/review/2026-09-28-youtube-retention-capacity.md`를 근거로 삼고, 운영 변경 직전에 짧은 최신 상태를 다시 확인한다.

### T02 보존 기간을 검증하고 중앙 API에 적용한다

Owner: hololive-bot-ops, 정적 설정은 stack-platform-ops 경계. 승인된 정책으로 `/home/kapu/work/stack-secrets/hosts/hololive-osaka/hololive-bot/compose.env`의 여섯 값만 바꾼다: LIVE_SNAPSHOT/COMMUNITY_PAGE/VIDEO_LIST/SHORTS_LIST 14, SCHEDULE_SNAPSHOT 30, APPLICATION_AUDIT_GRACE 14일. 키 접두사는 모두 `YOUTUBE_PLANE_RETENTION_`이다. `PROJECTION_RETIRED_DAYS=30`, `INTERVAL_SECONDS=120`, `BATCH_SIZE=1000`, 나머지 보존값은 유지한다. master 원본의 제한된 권한·복구 사본을 보존하고 비밀을 출력하지 않는다. sync dry-run에서 삭제·예상 밖 stack 변경이 없는지 확인한 뒤 master→`hololive-osaka:/etc/stack-secrets/hololive-bot/compose.env`로 적용하고 mode·in-place match를 검증한다. `compose config --quiet`와 실제 API image ID/revision/architecture를 확인한 뒤 검증 이미지로 `hololive-api`만 `up -d --no-build --no-deps` 한다. readiness·새 container 설정값·retention error를 검증한다.

### T03 삭제 적체와 공간을 추적한다

Owner: stack-platform-ops와 hololive-bot-ops. 첫 24시간은 6시간 간격으로 deleted total의 증가율, retention 오류·tick duration, `n_dead_tup`과 autovacuum, DB/`pg_wal`/호스트 여유를 기록한다. 이후 적체가 줄어드는 동안 매일 관측한다. 여유 공간 20 GiB 미만, 6시간에 5 GiB 이상 감소, 반복 transaction timeout 또는 autovacuum이 24시간 동안 dead tuple 증가를 따라잡지 못하는 경우 속도 상향을 보류하고 원인을 조사한다. `INTERVAL_SECONDS=60`은 실제 순처리 부족과 WAL·vacuum 여력을 확인한 뒤 별도 승인된 변경으로만 적용한다. 일반 `VACUUM` 자동 처리와 빈 공간 재사용을 우선하며 물리적 테이블 재작성은 별도 작업이다.

### T04 종료 증거의 수명 계약을 설계하고 구현한다

Owner: hololive-bot code. 현재 `youtube_live_absence_slots`는 원본 FK 없이 과거 positive 복원에 사용되며 `effective_at` 하한이 없다. 30일보다 오래된 slot을 재처리에 사용할 수 없는 계약을 문서와 회귀 테스트에 고정한다. 30일 설정·bounded batch 삭제·권한을 코드와 새 migration에 추가하고 active/pending 상태에 이미 반영된 근거는 보존한다. migration과 새 image의 운영 적용은 검증된 clean revision과 별도 승인 범위에서 진행한다.

### T05 증가율과 복구 가능 기간을 검증한다

Owner: stack-platform-ops. 원본·적용 이력 적체가 줄어든 뒤 최소 7일간 유입·삭제량, relation/DB/호스트 크기, WAL·autovacuum을 함께 비교한다. DB 파일 크기가 즉시 줄지 않아도 재사용 가능한 공간과 호스트 여유 추세로 판단한다. 실제 삭제·restart·image identity·readiness 및 남은 무기한 성장 경로를 근거에 남긴다.

### T06 APPLIED 이력의 절감 경로를 검토한다

Owner: hololive-bot code. decision·entity_kind별 분포와 application 테이블 읽기 경로, replay/idempotency 및 감사 계약을 대조한다. 실제 canonical 변경인 APPLIED를 no-op으로 간주하지 않는다. 즉시 안전하게 생략할 수 있는 범위와 전체 생략의 행 수 절감 상한을 구분해 기록하고, 계약 변경이 필요한 후보는 별도 DEC와 회귀 검증 뒤 구현한다.

## 수용 기준

### AC01 기간과 보호 조건

실제 설정은 승인된 여섯 단축값과 기존 retired projection 30일, 신규 종료 증거 30일에 일치하고 production 양수·replay-audit 최대 evidence 제약을 만족한다. 원본 queue/replay/head 보호와 orphan-only 적용 이력 삭제는 유지된다.

### AC02 중앙 API 적용

`hololive-api`만 검증된 기존 이미지로 재생성되며 새 container가 설정을 읽고 건강하다. 설정 원본의 복구 사본이 남고 rollback 명령과 현재 image ID를 기록한다.

### AC03 적체·디스크 관측

실제 삭제량과 DB/WAL/호스트 추세를 기록하고, 순처리 속도가 신규 유입보다 큰지 판정한다. 오류·dead tuple 증가·용량 감소가 중단 기준에 걸리면 변경 범위를 넓히지 않는다.

### AC04 종료 증거 보존 안전성

과거 positive/replay와 active/pending 근거의 수명 계약이 테스트로 고정되기 전에는 기존 종료 증거를 삭제하지 않는다. 계약 뒤에는 종료 증거 증가에 유한한 경계를 제시한다.

### AC05 용량 안정

적체 해소 후 7일 관측에서 원본·적용 이력의 지속 증가가 멈추고, 종료 증거의 남은 증가 및 완화 일정이 명시된다.

### AC06 APPLIED 이력 사용처

실제 APPLIED 비중과 의존 조회를 확인하고, 무조건적인 기록 생략 없이 안전한 후보와 필요 계약 변경을 문서화한다.

## 검증

### V01 읽기 전용 기준선

모든 운영 SQL의 `transaction_read_only=on`, timeout·표본 가정·시각·대상 DB와 실제 container 설정을 확인한다.

### V02 설정과 runtime

`compose config --quiet`, 최종 여섯 단축값·retired projection 30일·종료 증거 30일, container image identity·StartedAt·health/readiness와 rollback 사본의 존재를 확인한다.

### V03 적체 관찰

첫 24시간 6시간 간격 및 후속 일일 집계가 삭제량·오류·tick 시간·dead tuple·autovacuum·DB/WAL/호스트 크기를 포함한다.

### V04 종료 증거 회귀

후속 코드에서 late positive/replay, active/pending, batch bound와 실제 DB migration 테스트를 통과하고 repository gate를 실행한다.

### V05 안정성 판정

적체 해소 뒤 7일간 크기·유입·삭제 추세와 runtime 상태로 증가율을 확인한다.

### V06 적용 이력 검토

bounded read-only 표본과 코드 검색 결과를 대조해 종류별 APPLIED 비중 및 소비 경로를 확인한다.

## 중단 조건과 전달

운영 설정·재시작·migration은 exact 대상과 효과의 승인 및 현재 image 검증 없이는 진행하지 않는다. 기존 DB 또는 호스트가 다르거나 read-only guard가 꺼지면 조회를 멈춘다. WAL/용량·timeout 중단 기준에 도달하면 interval 단축과 추가 데이터 삭제를 중단한다. 30일 이전 slot을 쓰는 재처리의 행동 변경은 T04 회귀 테스트와 문서에서 명시한다.
