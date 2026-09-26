# LiveQuery 적대적 리뷰 수정과 운영 반영

**Decisions:** `DEC-20260926-hololive-live-query-read-model` (governing), `DEC-20260926-youtube-only-stream-providers` (constraint)

## Execution capsule

**Goal:** 독립 적대적 리뷰의 결함을 수정·검증하고 이번 변경을 커밋·푸시한 뒤 중앙 API/worker에 반영한다.
**Context:** 사용자가 서브에이전트 리뷰, 발견 이슈 수정, 커밋·푸시·라이브 반영을 승인했다. 기존 구현 계획의 로컬 완료와 구분하는 후속 실행이다.
**Constraints:** unrelated work 보존. 중앙 hololive-osaka에 검증된 arm64 이미지와 migration 211만 반영한다. 새 fallback·writer/coverage 계약 변경·데이터 정정·secret 변경·AP/collector 재배포·정리 작업은 하지 않는다.
**Evidence:** 두 독립 리뷰와 기존 구현 검증, guarded 운영 pending 집계. ENDED/ENDED 보존 pending 2370건과 positive보다 오래된 종료 증거의 오판을 확인했다.
**Success:** 보존 pending이 fresh LIVE/complete-empty를 숨기지 않고 미해결 종료는 미확인으로 유지한다. 필수 publish/build gate와 운영 revision·health·migration 검증을 통과한다.
**Output:** 수정 코드·회귀·비용 근거, 원격 커밋, 검증된 배포 revision/image, 운영 확인 및 보존한 rollback 지점.

## 작업

### T01 독립 리뷰와 증거 우선순위 수정

LiveQuery와 provider/cache를 별도 reviewer가 검토한다. terminal 또는 최신 positive가 이긴 pending은 현재 종료 확인에서 제외한다. 상태 불일치·더 최신 종료·시작 전 종료는 보수적으로 유지한다. 보존 pending을 포함한 격리 비용을 측정한다. AC01/V01.

### T02 커밋과 출판 및 이미지 준비

T01 뒤 task-owned diff만 커밋한다. unrelated work가 없는 source worktree에서 필수 pre-push gate를 실행해 configured remote로 푸시하고 kapu에서 API/worker arm64 이미지를 빌드한다. 정확한 40자리 SHA·architecture·이미지 ID를 검증한다. AC02/V02.

### T03 중앙 migration과 순차 전환

T02 뒤 이전 이미지·deploy tree를 보존하고 검증된 bundle/image를 hololive-osaka에 전달한다. repository one-shot으로 migration 211을 먼저 적용하고 API/worker만 `--no-build --no-deps`로 전환한다. 시작 시각·revision·readiness·새 오류·실제 DB query 진단과 비용을 확인한다. 실패하면 승인된 이전 이미지와 deploy tree로 복구하며 additive index는 유지한다. AC03/V03.

## 수용 기준

### AC01 정확한 종료 증거 읽기

ENDED/ENDED 반복 종료, LIVE positive 이전·동일 시각 종료, 더 새로운 종료, session 없는 종료와 상태 불일치 반례를 검증한다. provider/cache reviewer의 결과와 수정 재검토를 기록한다.

### AC02 검증한 revision 출판

task-only commit, 필수 publish gate, clean source의 local CI/image build, remote commit과 candidate SHA/architecture 일치를 확인한다. hook과 검사 범위를 우회하지 않는다.

### AC03 운영 반영 증명

migration 성공, 새 API/worker 시작 시각과 image SHA, health/ready, 새로운 치명 오류 부재와 rollback 자산 보존을 확인한다. 미확인 coverage/기존 상태 불일치는 명시하고 방송 없음으로 바꾸지 않는다.

## 검증

### V01 리뷰 회귀와 비용

Go 1.27.1/jsonv2, repository·command·reducer focused race, 보존 pending 포함 장기 이력 benchmark, 수정 SQL 및 lint/NilAway.

### V02 출판과 빌드

`scripts/ci/pre-push-gate.sh`, local CI, 관련 topology/deploy contract, image inspect 및 commit diff. 금지된 Iris 경로를 읽는 meta 전체 gate는 우회하지 않고 별도로 한계를 보고한다.

### V03 운영 완료 확인

hololive-bot-ops/stack-platform-ops의 승인 범위·read-only guard와 배포 순서를 준수한다. migration/Compose 설정 검증·running identity·health·bounded log/DB aggregate를 기록하고 DEC/PLN을 실제 결과와 맞춘다.
