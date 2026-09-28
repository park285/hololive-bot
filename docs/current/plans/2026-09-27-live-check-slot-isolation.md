# 라이브 확인 슬롯과 판정 시각 분리

**Decisions:** `DEC-20260927-live-check-slot-isolation` (governing), `DEC-20260926-hololive-live-absence-evidence` (constraint), `DEC-20260814-hololive-youtube-three-provider-convergence-v2` (constraint)

## Execution capsule

**Goal:** 방송 탭 재시도가 성공한 채널 확인을 만료시키는 문제와 refresh 시각 캡처 이후 positive의 stale 오인을 고친다.
**Context:** 적대적 리뷰에서 DEFERRED 슬롯 300초 유지 및 fresh 응답의 incomplete, positive 커밋 전후 시각에 따른 stale 선택 차이를 실제 격리 DB SQL로 재현했다.
**Constraints:** 로컬 수정·검증만 허용한다. 운영·Git publication·secret 접근 금지. 기존 terminal 계획을 재개하지 않는다. D1~D6·pending 보존·원시 판정·absence 종료는 유지한다.
**Evidence:** 2026-09-27 사용자 적대적 리뷰 후 수정 지시와 부모 세션의 격리 DB 재현. 이전 구현 근거는 docs/review/live-absence-evidence-20260926.md다.
**Success:** AC01~AC02를 회귀와 실제 DB 실행으로 입증하고 V01~V02를 통과한다.
**Output:** 독립 job/runner와 모든 caller·계측·전환 문서, DB 시계 stale query, 검증 근거.

## 수정 경계

채널 확인은 `youtubejs_channel_live_check` exact-subject job이 `channel_live_check`만 방출한다. 기존 `youtubejs_channel_live`는 `live_snapshot`만 방출한다. 양쪽 lease key와 COMPLETE/DEFERRED 진행은 독립이며 같은 채널 확인을 두 job에서 발행하지 않는다. 새 kind/schema/generation 변경은 없다. stale LIVE 선택은 쿼리의 `statement_timestamp()`를 사용하고 사용하지 않는 외부 AsOf 입력을 제거한다. API의 projection validity 시계 자체는 이번 수정 범위가 아니다.

미배포 로컬 구현을 clean cutover한다. 운영 도입에는 기존 공유-job collector drain, API job 계약 선행, 새 collector 순서가 필요하며 자동 운영 전환·호환 shim은 추가하지 않는다. 조건부 cross-channel ENDED 진단 누락과 입증되지 않은 성능 위험은 이 두 확정 이슈의 수정 범위가 아니다.

## 작업

### T01 채널 확인 작업 분리

shared job 계약, collector registry/runner/acquire/publish, API job 수요 계측과 관련 caller를 전환한다. 필요 없어진 공유-result 처리를 제거한다. 실패한 snapshot 재시도와 성공한 check의 새 슬롯 진행을 검증한다. 담당: 수집 변경 agent.

### T02 stale 조회 시각 일치

DB statement clock으로 stale query를 바꾸고 외부 AsOf 필드와 caller를 정리한다. 기존 제어 시계 테스트는 DB 상대 시각 fixture로 전환하며 freshness·회복·종료 전이와 캡처 이후 positive 경합을 검증한다. 담당: projection 변경 agent. T01과 파일 소유권을 분리한다.

### T03 통합 검증과 계약 정리

통합 담당자가 변경을 결합하고 모든 검증을 foreground 실행한다. 실제 수집·발행·DB smoke와 회귀 결과를 versionable 근거 문서에 기록하고 계약·서비스·runbook·AP manifest를 일치시킨다.

## 수용 기준

### AC01 채널 확인의 독립 진행

snapshot 작업이 같은 슬롯에서 반복 실패해도 성공한 check는 자체 슬롯을 완료하고 다음 cadence에서 새 scheduled_for/key로 발행·canonical 갱신된다. 해당 실패 때문에 신선한 음성 coverage가 만료되지 않는다. snapshot 작업은 check를 방출하지 않는다.

### AC02 조회 시점 positive 보존

refresh 외부 시각 캡처 후 DB에 커밋된 신선한 positive는 stale target이 아니다. 진짜 만료·missing head·미래 DB 시각은 계속 대상이며 신선한 positive 회복과 ENDED는 다음 refresh에서 제외된다.

## 검증

### V01 동작 검증

영향 Go 패키지 `GOEXPERIMENT=jsonv2 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=readonly -race -count=1` 및 새 반례 경계 회귀, 실제 DB/runner/publisher smoke를 수행한다. 영구 wording·배선 복사 테스트는 만들지 않는다.

### V02 정적 계약 검증

영향 lint·NilAway, SQL ownership·structure·AP manifest·DB/retry/projection/worker gate 및 selected plan gate를 통과한다. 전체 catalog의 사전 고지된 범위 밖 결함은 임의 수정·재확인하지 않는다. 운영 검증은 수행하지 않는다.
