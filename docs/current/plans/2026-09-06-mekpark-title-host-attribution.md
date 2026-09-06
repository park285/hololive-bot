# mekPark 제목 기반 방송자 표시

**Decisions:** `DEC-20260906-hololive-mekpark-title-host-attribution` (governing)

## Execution capsule
**Goal:** 공유 채널을 쓰는 UNIT B와 ACHRORA의 제목에서 확인된 방송자를 알림과 방송 조회에 표시한다.
**Context:** 운영 DB의 두 채널 방송 세션 140건 중 128건에 이름·개인 태그 단서가 있으며 기존 자료는 검토용 JSON뿐이다.
**Constraints:** 채널·구독·수집 계약과 원본 제목을 유지한다. 운영 쓰기·배포·새 의존성·모델의 추측 확정은 범위 밖이다.
**Evidence:** pkg/domain/mekparkhost의 기존 제목 149건, 공유 outbox formatter, alarm-worker dispatchrun, bot 방송 formatter를 확인했다.
**Success:** 진행자·게스트·합방을 구분하고 근거가 부족하면 유닛 표시를 유지하며 대표 렌더링과 회귀 검증이 통과한다.
**Output:** 공유 판별기와 규칙·검증 표본, 표시 경로 연결, 검증 기록을 로컬 변경으로 제공한다.

## 범위와 순서

### T01 제목 판별기와 표본 정리

소유자는 `hololive/hololive-shared/pkg/domain/mekparkhost`다. 기존 JSON에서 이름·개인 태그·시리즈 진행자 규칙만 실행 가능한 정본으로 정리한다. 두 채널 ID가 유닛을 결정한다. NFKC·해시 뒤 공백·제로폭 문자를 정규화하고, 이름 일부는 확인된 시점/파트/이름 쌍에서만 사용한다. 시리즈 진행자 외 인물은 같은 유닛이어도 게스트로 처리한다. 근거 토큰을 결과에 남기며, 단체/미상 제목을 특정 개인에게 할당하지 않는다. 기존 표본의 검토 지적을 반영하고 현재 DB의 새로운 사례를 읽기 전용으로 보완한다. AC01과 V01을 충족한다.

### T02 알림과 방송 조회에 연결

T01 이후 공유 outbox formatter, `hololive-alarm-worker/internal/service/dispatchrun`, bot의 방송 표시 경로에 같은 판별기를 연결한다. 라이브·방송 전·영상·쇼츠의 텍스트 및 카카오링크 표시와 라이브/예정/채널 일정/방송 이력 조회를 다룬다. 여러 영상 묶음은 항목별 방송자를 표시하고 묶음 전체에 한 사람을 붙이지 않는다. 수집 데이터, 구독 대상, 채널 이름 캐시, 원본 제목, 외부 payload 스키마를 변경하지 않는다. AC02·AC03과 V02를 충족한다.

### T03 결과 검증과 기록

T01·T02 이후 수정된 패키지의 race test, 빌드, lint와 NilAway를 수행한다. 판별 범위와 실제 정답률을 구분해 기록한다. 제목 자료는 사람이 읽어 확인 가능한 기대값에 한정하며 독립 검수된 학습 정답으로 주장하지 않는다. 모델 학습은 이름이 없는 표본의 정답과 독립 평가 자료가 확보된 뒤 규칙 대비 개선 여부로 판단한다. 운영 배포는 이 계획의 완료 조건에 포함하지 않는다. V03을 충족한다.

## 수용 기준

### AC01 출연자 구분과 보류

여섯 멤버의 성명·고유 방송 태그, 전각 해시·공백, 시리즈 진행자와 같은/다른 유닛 게스트, 명시적 합방 이름 쌍을 처리한다. `ミラクル`, 일반 본문의 이름 일부, 다른 채널, `?`, 단체 제목만으로 개인을 추정하지 않는다.

### AC02 사용자 표시 일치

단독 방송은 `유닛 B · 미라`, 시리즈 게스트는 `아크로라 · 사야나 (게스트: 미라)` 형태로 표시된다. 합방은 확인된 진행자들을 표시한다. 단일/묶음 알림과 방송 조회가 같은 제목에 같은 사람을 표시한다.

### AC03 기존 채널 계약 보존

다른 채널과 커뮤니티·구독자 달성 알림의 출력은 유지한다. 묶음의 채널 표시는 유지하며 개별 항목만 구분한다. 호출자의 Stream·Channel·payload를 수정하지 않으며 DB·네트워크 호출과 새로운 runtime dependency를 추가하지 않는다.

## 검증

### V01 제목 판별 회귀

`go test -race ./hololive/hololive-shared/pkg/domain/mekparkhost`로 검토한 corpus와 오탐·정규화 사례를 확인한다. corpus 출력의 식별/보류 건수는 정확도와 구분한다.

### V02 표시 경로 회귀

`go test -race ./hololive/hololive-shared/pkg/service/youtube/outbox/format ./hololive/hololive-alarm-worker/internal/service/dispatchrun ./hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter ./hololive/hololive-api/internal/planes/bot/internal/command/handlers`로 각 경로의 기존 테스트와 추가한 표시 회귀를 수행한다.

### V03 정적 검사와 최종 확인

수정한 패키지를 대상으로 `go build`, `golangci-lint run`, `go vet -vettool=$(command -v nilaway)`를 실행한다. `git diff --check`, 최종 diff 검토와 `bash ../tools/checks/check-decision-catalog.sh check --submodules`를 완료한다. 실패가 기존 부채이면 원인과 영향을 기록하고 무관한 변경이나 우회로 숨기지 않는다.

## 인계와 경계

실행은 사용자 요청 `진행해보자 ㄱㄱ`의 로컬 구현·검증 범위다. 배포, 재시작, 운영 DB 쓰기는 별도 대상 승인이 필요하다. 입력 제목만으로 모호한 인물을 확정할 수 없으면 유닛 표시로 완료하며, 추측을 늘리거나 데이터 모델·개인 구독 기능으로 범위를 넓히지 않는다. Fallback delta: none.
