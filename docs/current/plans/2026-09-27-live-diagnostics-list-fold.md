# 라이브 진단 누락과 목록 접기 연결

**Decisions:** `DEC-20260926-hololive-live-absence-evidence` (governing), `DEC-20260926-hololive-list-reply-fold-default` (governing)

## Execution capsule

**Goal:** cross-channel ENDED pending 진단 누락을 해소하고 긴 목록·묶음 알림이 머리 문단 뒤 전체보기로 접히게 한다.
**Context:** 이전 리뷰의 조건부 진단 누락은 SQL로 재현됐다. 사용자 첨부 쇼츠 10개는 OUTBOX_SHORTS_GROUP의 긴 본문이 노출되고 라이브 목록은 머리만 남는다.
**Constraints:** 로컬 수정·검증만 수행한다. 운영 발송·배포·DB 쓰기·Git publication·secret 접근 금지. D2 비차단·pending 보존, 사용자 지정 template/override, 짧은 메시지와 단일 알림 보존.
**Evidence:** 2026-09-27 사용자 잔여 이슈 수정 지시와 image copy.png/image.png. MESSAGE_STYLE_GUIDE §8의 기존 알림 fold-out과 outbox 경로의 접기 미적용을 확인했다.
**Success:** AC01~AC03과 V01~V03을 충족한다.
**Output:** 진단 SQL/회귀, 목록 경로 접기 연결/렌더 회귀, 계약·운영 문서·versionable 실행 근거.

## 적용 범위

D2의 session=ENDED 비차단은 유지하되 pending 채널과 canonical session 채널 각각의 조회 범위에서 보존 근거가 진단에 남게 한다. 같은 채널·같은 영상은 중복 집계하지 않는다.

기존 250 rune·머리 문단·ZWSP 500개 방식과 disable switch를 재사용한다. 긴 묶음 영상·쇼츠·커뮤니티 및 여러 항목 알림은 목록으로 접고, 단일 알림·상태·확인·오류는 전문을 유지한다. bot/llm/worker의 목록 소비 경로를 확인해 누락을 연결하며 무조건 모든 텍스트를 접는 전송층 처리는 금지한다. 사용자 지정 본문은 저장값과 펼친 가시 문자를 바꾸지 않는다. 새 기술 계약이나 별도 pad 알고리즘을 만들지 않는다.

사용자의 이번 요청이 기존 알림 fold-out 중 여러 항목 목록의 예외를 명시한다. `DEC-20260926-hololive-list-reply-fold-default`의 머리 문단 접기 규칙을 이 목록에도 적용하고 가이드의 소비 경계를 갱신한다. 실제 카카오톡 UI 변경 검증은 운영 발송 승인이 없어 불가하므로 실제 DB template→최종 payload를 실행해 확인하고 그 한계를 보고한다.

## 작업

### T01 보존 진단 범위 수정

LiveQuery SQL과 DB 회귀를 수정한다. session 채널이 조회 밖인 ENDED pending도 pending 채널 진단에 남기되 차단하지 않는다. 담당: 진단 agent.

### T02 목록 접기 연결

worker outbox 묶음 알림 및 bot/llm 목록 소비 경로를 점검·수정한다. 기존 fold toggle을 해당 소비 경로까지 연결하고 single/status/error를 보존한다. 부모가 문서를 통합한다. 담당: 접기 agent.

### T03 실행 검증과 문서 정리

통합 담당자가 실제 renderer/egress payload와 진단 DB smoke, 영향 lint·NilAway·race/build·workspace gate를 실행하고 문서와 근거를 정리한다.

## 수용 기준

### AC01 조회 채널별 진단 유지

pending X/session ENDED Y에서 X 단독·Y 단독·양쪽 전체 조회 모두 해당 보존 근거를 표시한다. 같은 채널은 한 번만 집계하며 pending 삭제·D2 차단 복귀가 없다.

### AC02 긴 묶음 알림 접기

실제 DB의 쇼츠 10개 묶음은 최종 text payload에서 머리 문단 직후 접기 패딩이 정확히 한 번 존재하고 모든 제목·URL이 펼친 본문에 보존된다. grouped video/community 및 발견된 다른 목록 경로에도 같은 정책을 적용한다.

### AC03 기존 표시와 설정 보존

짧은 목록·단일 알림·상태·오류, fold disable, 기존 접힌 본문의 멱등성이 유지된다. 사용자 지정 template/채널 override의 저장값과 가시 문자를 변경하지 않는다.

## 검증

### V01 진단 DB 회귀

hololive-dbtest fixture의 cross-channel/out-of-roster/동일채널/ENDED·LIVE 경계를 실제 LiveQuery로 실행하고 영향 패키지 race를 통과한다.

### V02 최종 렌더 smoke

실제 template renderer 및 최종 kakaoformat/worker 경로를 실행하여 머리·padding 위치·펼친 본문·toggle·제외 경계를 확인한다. 운영 카카오톡에 메시지를 보내지 않는다.

### V03 결합 gate

영향 lint·NilAway·race와 build-only, SQL ownership·structure 및 영향 workspace gate/selected plan gate를 통과한다. 전체 catalog의 사전 고지된 범위 밖 결함은 재확인·수정하지 않는다.
