# Hololive Bot 레포 AI 냄새 및 문서 정리 보고서

작성일: 2026-04-14  
범위: 기능 변경 제외, 문서 정리와 유지보수 관점의 "AI 냄새" 정리만 수행

## 1. 결론

이번 정리는 코드 기능을 바꾸지 않고도 레포의 신뢰도를 높이는 데 집중했다.

핵심 판단은 다음과 같다.

1. 이 레포의 문제를 단순히 "AI가 작성한 코드 같다"로 보는 것은 부정확하다.
2. 대신 실제로는 **agentic accretion**으로 보는 편이 맞다. 즉, 작업 단위별 패치와 계획 문서가 누적되면서 구조가 과잉 세분화되고 현재 SSOT가 흐려진 상태다.
3. 가장 큰 운영 리스크는 코드보다도 **문서 계층의 혼합**이었다. 현재 문서, 과거 문서, 작업용 plan/spec, 모듈 로컬 문서가 명확히 구분되지 않아 신규 작업자나 운영자가 잘못된 문서를 따라갈 위험이 컸다.
4. 따라서 이번 패치는 기능 수정 없이 다음 두 가지를 해결했다.
   - 현재 문서와 과거/작업 문서의 경계를 명시적으로 고정
   - 실제로 읽다가 막히는 끊긴 링크와 잘못된 진입점을 제거

## 2. 여기서 말하는 "AI 냄새"의 의미

이번 보고서에서 "AI 냄새"는 저자 추정이 아니라 **유지보수 냄새**를 뜻한다. 즉, 사람이 썼든 AI가 썼든 아래 패턴이 강하면 결과적으로 비슷한 문제가 생긴다.

- 지나치게 잘게 쪼개진 파일과 naming 계층
- 동일 책임을 얇게 감싼 mirror/helper/view 타입의 누적
- 기존 구조를 정리하지 않은 채 새로운 규칙과 문서를 계속 덧붙인 흔적
- 현재 문서와 작업 로그의 경계가 사라져 운영 SSOT가 흐려지는 현상
- 테스트와 문서가 "현재 구조 설명"보다 "그 시점의 변경 흔적 보존"에 치우친 상태

이 레포는 특히 **문서와 부트스트랩 레이어**에서 이 냄새가 강하게 나타난다.

## 3. 정량적으로 본 현재 상태

정리 작업 시점 기준 정적 집계는 아래와 같다.

- Markdown 문서: **121개**
- 루트 `docs/` 아래 Markdown: **64개**
- `hololive/hololive-kakao-bot-go/docs/` 아래 Markdown: **23개**
- `docs/superpowers/` 아래 Markdown: **28개**
  - `plans/`: 18개
  - `specs/`: 9개
- `hololive/hololive-kakao-bot-go/internal/app` 파일 수: **59개**
- 그중 `bootstrap_*` 파일 수: **30개**
- 레포 전체 `*additional_test.go`: **22개**
- 그중 `internal/app` 아래 `*additional_test.go`: **12개**

이 숫자만 봐도 문서와 조립 레이어가 실제 도메인 복잡도보다 빠르게 증식한 흔적이 보인다.

## 4. AI 냄새가 강하게 보이는 지점

### 4.1 문서 계층이 실제 사용 계층과 일치하지 않음

가장 큰 문제는 문서 구조였다.

루트에는 `docs/current`, `docs/history`, `docs/design`이 이미 존재하지만, 실제 사용자는 여전히 루트 `README.md`, `docs/README.md`, 모듈 로컬 `docs/`, 그리고 `docs/superpowers/`를 섞어 보게 된다. 즉 디렉터리 이름은 분리되어 있는데, **진입 문서가 분리를 강제하지 못하고 있었다.**

그 결과 아래 같은 문제가 있었다.

- 현재 운영 문서와 과거 migration 문서가 같은 위상처럼 보임
- 모듈 로컬 문서가 현재 SSOT인지 단순 참고인지 알기 어려움
- 작업용 plan/spec 문서가 현재 운영 문서처럼 보이기 쉬움
- 과거 구조를 설명하는 문서가 여전히 상단 진입 경로에 남아 있음

이건 전형적인 "문서를 계속 추가했지만 문서 계층 설계는 닫지 못한 상태"다.

### 4.2 계획 문서 저장소가 현재 문서 계층을 침범함

`docs/superpowers/`는 작업 계획, 설계, 체크리스트 저장소인데, 운영 문서와 같은 루트 층위에 노출돼 있다. 문제는 단순히 디렉터리가 있다는 점이 아니라, **루트 문서가 이 디렉터리를 현재 SSOT와 충분히 구분하지 않았다는 점**이다.

이 패턴은 AI/agent 기반 작업에서 자주 보인다. 구현 과정에서 만들어진 작업용 산출물이 오래 남는데, 이후 사람이 볼 때는 "이게 현재 기준인지, 작업 로그인지"를 판별하기 어려워진다.

### 4.3 과도한 부트스트랩/뷰 타입 누적

코드 쪽에서 가장 대표적인 냄새는 `hololive/hololive-kakao-bot-go/internal/app/`이다.

- 파일 수 59개
- `bootstrap_*` 계열 30개
- `*additional_test.go` 12개

예시:

- `internal/app/bootstrap_bot_dependency_views.go`
- `internal/app/bootstrap_services_types.go`
- `internal/app/container.go`
- `internal/app/container_accessors.go`

문제의 본질은 "파일이 많다"가 아니다. 실제 문제는 아래와 같다.

- 조립 레이어가 도메인 계층을 설명하기보다 **의존성 전달 경로를 잘게 래핑**하는 데 많은 파일을 소비함
- `botWebhookRuntimeDependencies`, `botConfigSubscriberDependencies`, `botAdminRuntimeDependencies` 같은 타입이 실제 정책 경계라기보다 조립 편의용 뷰 타입으로 누적됨
- `Build`, `Initialize`, `Provide`, `build*`, `*Dependencies`, `*Modules`, `*Views` naming 층이 동시에 존재해 진입점 파악 비용이 커짐

이건 기능 오작동보다는 **이해 비용을 계속 높이는 냄새**다.

### 4.4 동일한 사소한 유틸리티의 중복

아래 두 파일은 거의 같은 clone helper를 각각 보유한다.

- `hololive/hololive-kakao-bot-go/internal/app/command_builder_clone.go`
- `hololive/hololive-kakao-bot-go/internal/bot/command_builder_clone.go`

이 정도 크기의 중복은 성능 문제가 아니라, 구조를 정리하는 대신 작업 단위마다 필요한 코드를 옆에 덧붙인 흔적으로 보는 편이 맞다.

### 4.5 README와 실제 운영 모델의 톤 차이

`hololive/hololive-kakao-bot-go/README.md`는 설명 방식이 과도하게 마케팅/홍보 톤으로 기울어 있었고, 현재 compose 운영 구조와 문서 체계에 비해 정보 밀도가 낮았다. 이런 README는 작성 당시에는 보기 좋지만, 시간이 지나면 가장 먼저 드리프트한다.

즉 현재 구조를 설명하는 문서라기보다, 한 시점의 소개문에 가까웠다.

## 5. 이번 문서 정리에서 실제로 바꾼 것

이번 패치는 총 **25개 파일**을 수정하거나 추가했다.

### 5.1 루트 진입점 재작성

다음 문서를 현재 운영 기준으로 다시 썼다.

- `README.md`
- `docs/README.md`
- `docs/current/README.md`
- `docs/current/PROJECT_MAP.md`
- `docs/current/runbooks/README.md`
- `docs/current/architecture/README.md`
- `docs/history/README.md`
- `docs/design/README.md`

핵심 목적은 하나다.

**"현재 운영 기준은 어디서 시작해야 하는가"를 1분 안에 알 수 있게 만든 것**이다.

### 5.2 문서 상태 분류 문서 신설

신규 문서:

- `docs/current/DOCUMENTATION_STATUS.md`

이 문서는 현재 레포에서 가장 자주 헷갈리는 문서를 아래 세 가지로 분류한다.

- Current SSOT
- Supplemental module-local references
- Historical documents that are easy to misuse

이 문서가 생기면서, 예를 들어 아래 문서가 현재 기준이 아닌 것을 명시적으로 알 수 있게 됐다.

- `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`
- `hololive/hololive-kakao-bot-go/docs/GO_RS_DOMAIN_BOUNDARY.md`
- `hololive/hololive-kakao-bot-go/docs/SERVICE_DECOMPOSITION_ROADMAP.md`
- `hololive/hololive-kakao-bot-go/docs/MULTIMODULE_MIGRATION_P3_PLAN.md`
- `hololive/hololive-kakao-bot-go/docs/MULTIMODULE_MIGRATION_PHASE3.md`

### 5.3 `docs/superpowers/`의 위상 격리

신규 문서:

- `docs/superpowers/README.md`

이제 `docs/superpowers/`는 현재 운영 SSOT가 아니라는 점을 진입 문서 수준에서 명확히 고정했다. 이 조치는 작지만 중요하다. 왜냐하면 plan/spec/worklog 저장소를 계속 둘 거라면, 최소한 **현재 문서 계층을 침범하지 않도록 경계 문서를 먼저 세워야 하기 때문**이다.

### 5.4 모듈 로컬 docs 재분류

다음 문서를 재작성 또는 보완했다.

- `hololive/hololive-kakao-bot-go/README.md`
- `hololive/hololive-kakao-bot-go/docs/README.md`
- `hololive/hololive-kakao-bot-go/docs/DISTRIBUTED_RATE_LIMITING.md`
- `hololive/hololive-kakao-bot-go/docs/LLM_SCHEDULER_RUNBOOK.md`
- `hololive/hololive-kakao-bot-go/docs/STREAM_INGESTER_RUNBOOK.md`

목표는 두 가지였다.

1. 모듈 로컬 문서를 "보조 참고 자료"로 정확히 위치시키기
2. obsolete runtime 이름, 잘못된 링크, 현재 구조와 안 맞는 설명을 제거하기

특히 `DISTRIBUTED_RATE_LIMITING.md`는 단순 historical 표기 대신, **현재 구조에 맞는 보조 참고 문서**로 다시 정리했다. 이 문서는 현재 구현을 설명할 가치가 있었기 때문이다.

### 5.5 오용 위험이 큰 과거 문서에 배너 추가

아래 문서에는 historical/supplemental 배너를 추가했다.

- `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`
- `hololive/hololive-kakao-bot-go/docs/GO_RS_DOMAIN_BOUNDARY.md`
- `hololive/hololive-kakao-bot-go/docs/SERVICE_DECOMPOSITION_ROADMAP.md`
- `hololive/hololive-kakao-bot-go/docs/MULTIMODULE_MIGRATION_P3_PLAN.md`
- `hololive/hololive-kakao-bot-go/docs/MULTIMODULE_MIGRATION_PHASE3.md`
- `hololive/hololive-kakao-bot-go/docs/LLM_SCHEDULER_RUNBOOK.md`
- `hololive/hololive-kakao-bot-go/docs/STREAM_INGESTER_RUNBOOK.md`

이 조치는 문서를 삭제하지 않고도 오용 확률을 크게 낮춘다.

### 5.6 끊긴 링크 정리

초기 정리 후 남아 있던 문제까지 마무리했다.

- `docs/superpowers/plans/2026-04-09-r7-root-docs-split-and-governance.md`
  - 잘못된 상대 링크 수정
- `docs/superpowers/specs/2026-04-09-repo-wide-masterplan-closeout-design.md`
  - 번들에 존재하지 않는 절대 경로 링크 제거
- `docs/history/settlement/2026-03-18-settlement-go-separation.md`
  - 없는 companion spec을 현재 링크처럼 보이지 않도록 수정
- `hololive/hololive-kakao-bot-go/docs/local/member-news-v4-implementation-complete.md`
  - 로컬 handoff 문서 경로와 template 참조 표현 정리

## 6. 검증 결과

문서 정리 후 로컬 markdown 링크 검사를 수행했고, **남아 있는 broken markdown link는 0건**이었다.

즉 현재 패치 기준으로는 최소한 아래 수준은 충족한다.

- 현재 문서 진입 경로가 분명함
- 현재 문서와 작업 문서의 위상이 구분됨
- 문서 안에서 실제로 따라갈 수 없는 끊긴 링크가 남아 있지 않음

## 7. 이번 패치가 해결하지 않은 AI 냄새

이번 범위는 문서와 유지보수 냄새 정리였기 때문에, 아래 항목은 보고만 하고 코드 수정은 하지 않았다.

### 7.1 `internal/app` 조립 레이어 축소

권장 방향은 다음과 같다.

- mirror dependency view 타입을 실제 정책 경계 기준으로 통합
- `Provide*`, `Build*`, `Initialize*` naming을 한 레이어당 한 방식으로 축소
- accessors와 builders를 묶어 "runtime assembly" 단위로 재배치

### 7.2 clone helper 중복 제거

아래 둘 중 하나로 통합하는 편이 맞다.

- `internal/bot`의 helper를 단일 소유로 두고 `app`에서 재사용
- 아예 clone helper 없이 builder slice 불변 정책을 상위에서 정리

### 7.3 `*additional_test.go` 패턴 정리

현재는 작은 회귀 보강이 빠르게 누적되면서 `additional_test.go`가 많이 늘었다. 이 패턴은 단기 대응에는 유용하지만, 장기적으로는 테스트 구조를 흐린다.

권장 방향은 다음과 같다.

- 도메인별 테스트 파일로 다시 합치기
- "왜 추가되었는지"를 commit/문서에 남기고 파일명은 책임 기준으로 정리
- app 조립 테스트와 실제 도메인 동작 테스트를 분리

## 8. 적용 우선순위

문서만 놓고 보면 이번 패치로 우선순위가 높은 문제는 대부분 닫혔다.

이후 순서는 아래가 적절하다.

1. 이번 문서 패치 반영
2. PR/리뷰 규칙에 "current SSOT 승격 없는 신규 운영 문서 금지" 추가
3. `internal/app` 조립 레이어 축소 리팩터 설계 별도 작성
4. `*additional_test.go` 정리와 helper 중복 제거

## 9. 최종 판단

이번 레포는 코드 자체보다도 **문서와 조립 계층에서 AI 냄새가 강했다.**

정확히 말하면, AI가 썼는지보다 **AI/agent식 작업 산출물이 장기간 누적되면서 현재 기준 문서와 작업용 문서의 경계가 흐려진 상태**였다. 이 상태를 그대로 두면 신규 작업자는 과거 migration 문서나 작업 plan을 현재 SSOT로 오해하기 쉽고, 운영 판단도 흔들리게 된다.

이번 패치는 그 문제를 기능 변경 없이 가장 낮은 리스크로 줄이는 정리다. 즉,

- 현재 문서 계층을 고정했고,
- 과거 문서에 경고 배너를 달았고,
- 작업용 문서 저장소를 격리했고,
- 끊긴 링크를 정리했고,
- README와 모듈 문서를 현재 구조 기준으로 다시 맞췄다.

문서 관점에서는 이제 "읽다가 틀린 구조를 믿게 되는 레포"에서 "현재 기준과 과거 기록을 구분해서 읽을 수 있는 레포"로 한 단계 올라간 상태라고 판단한다.
