# 비유튜브 방송 제공자 실행 경로 제거

**Decisions:** `DEC-20260926-youtube-only-stream-providers` (governing), `DEC-20260926-hololive-live-query-read-model` (context)

## Execution capsule

**Goal:** Chzzk·Twitch 방송 제공자 지원을 제거하고 YouTube 조회·알림을 유지한다.
**Context:** 사용자가 2026-09-26 비유튜브 제공자 미지원과 로직 정리를 명시했다. 기존 읽기 모델 계획의 Chzzk adapter 제안은 실행하지 않는다.
**Constraints:** 배포·운영 데이터·secret 변경 없음. 외부 Stream API의 기존 직렬화 필드와 저장된 멤버 플랫폼 식별자는 보존한다. X Spaces는 별도의 알림 기능이며 이번 방송 제공자 제거 대상이 아니다. 이 제거 작업은 YouTube writer·알림 admission을 변경하지 않으며, 승인된 조회 scope 변경은 별도 `DEC-20260926-hololive-live-query-read-model`이 소유한다.
**Evidence:** `docs/review/live-query-read-model-20260926.md`의 차집합 조사와 command/DI, shared clients/settings/alarmservice, worker scheduler/formatter 제거 및 실제 영향 검증. 로컬 DB 조회 구현과 운영 coverage 전환 조건을 구분한다.
**Success:** Chzzk·Twitch 원천 클라이언트와 주기 조회가 없고 명령·알림은 YouTube 경로만 사용한다. 영향 모듈의 실제 회귀·race 검사가 통과한다.
**Output:** API/shared/worker 코드 정리, YouTube 범위 조사 기록, 검증 근거와 운영 전환 전 확인 조건.

## 작업

### T01 조회 범위와 제거 경로 조사

Holodex 조직 채널과 운영 등록 채널을 bounded read로 비교한다. 실제 provider 필터와 활성/졸업/공유 채널 차이를 기록한다. 제거할 client·DI·scheduler·formatter를 확인한다. AC01/V01.

### T02 비유튜브 실행 경로 제거

T01 뒤 shared `service/chzzk`, `service/twitch`, notification platform mapping과 API/worker 생성·주기 조회·명령 합성 경로를 제거한다. 설정 로더의 해당 의존성을 제거한다. 사용되지 않는 streamfeed/streamschedule도 참조 확인 후 제거한다. AC02/V02.

### T03 표시와 계약 경계 정리

T02 뒤 formatter의 비유튜브 링크 합성을 제거하고 관련 회귀 검사를 YouTube 동작에 맞춘다. 기존 Stream JSON 필드·멤버 저장 데이터는 삭제하지 않는다. 구형 비유튜브 알림은 YouTube 대상으로 오인하지 않도록 기존 오류 경계에서 거부한다. AC03/V02.

### T04 통합 검증과 후속 조건 기록

shared/API/worker 영향 패키지의 compile·회귀·race 검사와 해당 정적 검사, decision gate를 실행한다. DB cutover·운영 활성화와 분리해 전달한다. AC02/AC03/V02/V03.

## 수용 기준

### AC01 조회 대상 차이 구체화

공개 채널 identity 기준 추가/제외 대상을 기록하고 운영 시점과 코드값을 구분한다. Holodex inactive를 현행 명령이 걸러낸다고 추정하지 않는다.

### AC02 비유튜브 제공자 호출 없음

Chzzk/Twitch 클라이언트·스케줄러 루프·bootstrap·명령 요청이 제거되고 자격 증명 설정 없이 YouTube 기능이 구성된다.

### AC03 YouTube 계약 보존

기존 파서·멤버 해석·예정/일정·Stream API·canonical writer·X Spaces 및 unrelated work를 유지한다. 방송 조회 실패를 빈 목록으로 바꾸지 않는다. 저장 데이터/운영 queue를 수정하지 않는다.

## 검증

### V01 원천 차집합 실측

Holodex 채널 pagination과 read-only guard를 증명한 운영 DB의 제한된 공개 멤버 채널 조회로 비교한다. 실행 시점, 원천 수, 필터 한계와 결과를 기록한다.

### V02 영향 Go 검증

고정된 기존 Go 1.27.1 바이너리, `GOEXPERIMENT=jsonv2`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `-mod=readonly`로 shared/API/worker 영향 패키지의 회귀·race 검사를 실행한다. lint/NilAway는 저장소 실행 경계를 따른다. 새 의존성이나 toolchain 변경 없음.

### V03 문서와 경계 검증

`git diff --check`, `bash tools/checks/check-decision-catalog.sh` 및 영향 architecture gate. Fallback delta: none. API 응답 필드나 runtime activation은 이 작업의 부수 효과로 제거하지 않는다.
