# Hololive 2026-04-14 정본 기준 이슈 레지스터

작성 위치:
- worktree: `.worktrees/close-canonical-issues-20260414`

정본 기준:
- `hololive_20260414_ai_smell_docs_cleanup_complete.patch`
- `hololive_20260414_ai_smell_docs_cleanup_report.md`
- `hololive_20260414_remaining_followups.md`

## 0. 한눈에 보는 요약

| 구분 | 요약 |
|---|---|
| 닫힌 축 | 문서 SSOT 혼선, 즉시성 운영 버그 |
| 열린 축 | durability/operability 보강, interface 명시화, 조립 레이어 축소, 테스트/문서 거버넌스 정리 |
| 최우선 후속 | dispatcher durable retry/DLQ, alarm persistence durability model |
| 정본 source 역할 | patch=닫힌 범위 증거, report=구조 냄새 정본, followups=운영 후속 과제 정본 |

## 1. 정본별 역할과 해석 규칙

### 1.1 정본별 역할

| 정본 | 이 문서에서 맡는 역할 |
|---|---|
| `hololive_20260414_ai_smell_docs_cleanup_complete.patch` | 문서 정리 패치가 실제로 무엇을 닫으려 했는지 보여주는 증거 아티팩트 |
| `hololive_20260414_ai_smell_docs_cleanup_report.md` | 문서 정리 이후에도 남는 구조적 AI 냄새와 문서/테스트/조립 레이어 후속 과제의 정본 |
| `hololive_20260414_remaining_followups.md` | 최근 클로저 이후에도 남아 있는 운영/백엔드 후속 과제의 정본 |

### 1.2 해석 규칙

이 문서는 현재 워크트리 diff 자체를 다시 판정하는 문서가 아니라, 위 세 문서를 정본으로 삼아 **무엇이 이미 닫혔고 무엇이 아직 열려 있는지**를 운영/구조 관점에서 재정리한 레지스터입니다.

판단 규칙은 다음과 같습니다.

1. `hololive_20260414_remaining_followups.md` 는 최근 클로저 이후에도 남아 있는 **운영/백엔드 후속 과제의 정본**으로 취급합니다.
2. `hololive_20260414_ai_smell_docs_cleanup_report.md` 는 문서 정리 이후에도 남는 **구조적 AI 냄새/유지보수 냄새의 정본**으로 취급합니다.
3. `hololive_20260414_ai_smell_docs_cleanup_complete.patch` 는 무엇을 실제로 닫으려 했는지 보여주는 **증거 아티팩트**로 취급합니다.
4. 다만 patch 파일은 `git apply --stat --summary` 기준 `corrupt patch at line 306` 이므로, patch 관련 결론은 **보이는 hunk와 보고서가 함께 지지하는 범위까지만** 확정합니다.
5. 이 문서에서 `P1/P2` 는 **중요도**, `## 4. 권장 실행 순서` 는 **실행 순서**를 뜻합니다.

## 2. 이미 닫힌 이슈

### 2.1 문서 계층/SSOT 혼선

문서 정리 패치와 보고서 기준으로 아래는 닫힌 것으로 봅니다.

- source:
  - `hololive_20260414_ai_smell_docs_cleanup_report.md` §5~§6
  - `hololive_20260414_ai_smell_docs_cleanup_complete.patch`

- 루트 `README.md`, `docs/README.md`, `docs/current/README.md` 재작성
- `docs/current/PROJECT_MAP.md` 중심의 현재 모듈/서비스 맵 고정
- `docs/current/DOCUMENTATION_STATUS.md` 신설을 통한 current/supplemental/historical 구분
- `docs/superpowers/README.md` 를 통한 작업용 문서 저장소의 위상 격리
- 모듈 로컬 docs 의 supplemental/historical 재분류
- 과거 문서 오용 방지 배너 추가
- 문서 링크 정리와 broken markdown link 0건 달성

즉, **문서 계층 혼합 자체는 이번 정리 범위에서 우선 닫혔고**, 앞으로의 핵심은 재발 방지 규칙과 구조 후속 작업입니다.

### 2.2 즉시성 운영 버그

`hololive_20260414_remaining_followups.md` 기준으로 아래 즉시성 이슈는 이미 닫혔습니다.

- source:
  - `hololive_20260414_remaining_followups.md` §1

- 5분 전 알람 누락
- persistence saturation 시 request blocking / durable gap 의 즉시성 문제
- channel subscriber lookup 의 과도한 `SMembers` fan-out
- cache warm 의 per-record write amplification
- dispatcher 의 immediate requeue hot loop
- member adapter 의 cancellation 전파 상실
- stale alarm runtime 문서의 런타임 경로 불일치

따라서 이 레지스터에서 남은 항목은 **즉시 장애 대응 목록이 아니라 후속 구조/운영 보강 목록**입니다.

## 3. 현재 열린 이슈

### 3.1 P1 — dispatcher retry 를 durable retry/DLQ 모델로 승격해야 함

- 상태: 열림
- source: `hololive_20260414_remaining_followups.md` §3.1
- owning seam:
  - `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
  - `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher_test.go`
- boundary file:
  - `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- contract risk:
  - retry state 가 프로세스 메모리 내부에만 있어 restart 시 parked envelope 유실 가능
  - envelope 차원의 persisted retry metadata 부재
  - DLQ 부재
  - fixed backoff 로 operability 한계 존재
- proof map:
  - envelope-level `attempt/retry_after/next_visible_at` 보존 확인
  - restart 이후 retry visibility 유지 확인
  - max-attempt 초과 시 DLQ 또는 parking queue 이동 확인
  - jitter/ramp/backoff metric 노출 확인
- next action:
  - delayed retry queue/ZSET 또는 envelope persisted retry field 중 하나를 먼저 선택해 설계를 고정
- defer rationale:
  - immediate requeue hot loop 는 이미 제거됐고 bounded in-memory parking retry 로 tail risk 가 줄어든 상태

### 3.2 P1 — alarm persistence 를 durable outbox 또는 authoritative DB write 로 정리해야 함

- 상태: 열림
- source: `hololive_20260414_remaining_followups.md` §3.2
- owning seam:
  - `hololive/hololive-kakao-bot-go/internal/service/notification/striped_executor.go`
  - `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go`
  - `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persist_ordering_test.go`
- boundary file:
  - `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go`
- contract risk:
  - durable outbox/authoritative persistence 모델 미확정
  - process crash 중간 지점에서 cache/DB 완전 재구성 모델 부재
  - saturation 시 inline fallback 이 latency spike 를 유발할 수 있음
  - async retry/dead-letter/observability 경로 부재
- proof map:
  - authoritative DB write 또는 durable outbox 결정 문서
  - saturation/normal path ordering 보존 테스트
  - crash-recovery 또는 rebuild 기준 명시
  - retry/dead-letter/metric 경로 검증
- next action:
  - `DB authoritative + cache derived` 와 `durable command/outbox` 중 하나를 architecture decision 으로 먼저 고정
- defer rationale:
  - request blocking 과 silent loss 의 즉시성 문제는 이미 닫힘

### 3.3 P2 — `internal/app` 조립 레이어를 실제 정책 경계 기준으로 축소해야 함

- 상태: 열림
- source:
  - `hololive_20260414_ai_smell_docs_cleanup_report.md` §7.1
  - `hololive_20260414_ai_smell_docs_cleanup_report.md` §8
- owning seam:
  - `hololive/hololive-kakao-bot-go/internal/app/`
- boundary file:
  - `hololive/hololive-kakao-bot-go/internal/app/container.go`
  - `hololive/hololive-kakao-bot-go/internal/app/container_accessors.go`
  - `hololive/hololive-kakao-bot-go/internal/app/bootstrap_*.go`
- contract risk:
  - runtime assembly 이해 비용 지속 증가
  - `Build`/`Initialize`/`Provide`/`*Dependencies`/`*Views` naming 계층 중첩
  - mirror dependency view 가 실제 정책 경계가 아니라 조립 편의 view 로 누적
- proof map:
  - 리팩터 설계 문서 1건
  - naming 규칙 단일화 표
  - merged seam 이후 bootstrap 진입점 수/유형 감소 확인
  - app 조립 테스트와 도메인 테스트 책임 분리 확인
- next action:
  - `Provide*`, `Build*`, `Initialize*` 규칙을 레이어당 하나로 줄이는 별도 리팩터 설계 작성
- defer rationale:
  - 동작 오작동보다 변경 비용과 이해 비용을 높이는 구조 냄새이기 때문

### 3.4 P2 — member adapter 를 explicit error-return 모델로 전환해야 함

- 상태: 열림
- source: `hololive_20260414_remaining_followups.md` §3.3
- owning seam:
  - `hololive/hololive-shared/pkg/service/member/adapter.go`
  - 관련 caller 전반
- boundary file:
  - `hololive/hololive-shared/pkg/service/member/adapter.go`
- contract risk:
  - `GetAllMembers()` 와 검색 계열이 아직 explicit error-return 계약이 아님
  - empty result 와 lookup failure 를 완전히 분리하지 못함
- proof map:
  - `MemberDataProvider` 계열 인터페이스 개편안
  - empty/failure 분리 테스트
  - caller/UI degraded mode 처리 검증
- next action:
  - `([]*domain.Member, error)` 중심 계약으로 재정의하고 caller 전파까지 함께 정리
- defer rationale:
  - 가장 위험한 cancellation/error masking 은 이미 완화됨

### 3.5 P2 — alarm 문서를 운영 카탈로그 수준까지 확장해야 함

- 상태: 열림
- source: `hololive_20260414_remaining_followups.md` §3.4
- owning seam:
  - `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`
  - 필요 시 runbook/ops docs
- boundary file:
  - `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`
- contract risk:
  - runtime path 정합성은 회복됐지만 운영자가 필요한 key/TTL/retry/rebuild 카탈로그는 아직 부족
- proof map:
  - key/TTL 표
  - claim key / logical event / schedule transition key 카탈로그
  - retry/parking 절차
  - cache warm/rebuild runbook 추가
- next action:
  - 현재 runtime 설명 문서를 운영 카탈로그/runbook 으로 분리 확장
- defer rationale:
  - 최소 런타임 경로 설명은 이미 현재 구조와 맞음

### 3.6 P2 — target minute wrapper surface 를 policy object 중심으로 수렴해야 함

- 상태: 열림
- source: `hololive_20260414_remaining_followups.md` §3.5
- owning seam:
  - target-minute helper/policy 호출부 전반
- boundary file:
  - `hololive/hololive-shared/pkg/service/alarm/checker/helpers.go`
  - `hololive/hololive-shared/pkg/service/alarm/checker/target_policy.go`
  - settings/dedup/alarm service 관련 호출부
- contract risk:
  - behavior split-brain 위험은 줄었지만 compatibility wrapper surface 가 계속 남아 있음
- proof map:
  - wrapper 호출부 inventory
  - 신규 코드의 policy object 직접 사용 전환
  - wrapper 축소 후 회귀 테스트 유지
- next action:
  - `NormalizeTargetMinutes`, `BuildRuntimeTargetMinutes`, `ResolveConfiguredTargetMinutes`, `ResolvePersistedTargetMinutes` 축소 계획 수립
- defer rationale:
  - 현재 wrapper 는 compatibility adapter 이고 의미론 단일화는 이미 달성됨

### 3.7 P2 — clone helper 중복을 제거해야 함

- 상태: 열림
- source:
  - `hololive_20260414_ai_smell_docs_cleanup_report.md` §7.2
  - `hololive_20260414_ai_smell_docs_cleanup_report.md` §8
- owning seam:
  - `hololive/hololive-kakao-bot-go/internal/app/command_builder_clone.go`
  - `hololive/hololive-kakao-bot-go/internal/bot/command_builder_clone.go`
- boundary file:
  - 위 두 helper 파일
- contract risk:
  - 사소한 중복이 구조 단순화 대신 패치 누적 습관을 고착
- proof map:
  - helper 단일 소유화 또는 삭제 후 호출부 동일성 확인
  - builder slice 불변 정책 문서화
- next action:
  - `internal/bot` 단일 소유 또는 helper 제거 중 하나를 선택
- defer rationale:
  - 기능 오류보다 구조 단순화 과제이기 때문

### 3.8 P2 — `*additional_test.go` 패턴을 책임 기준으로 정리해야 함

- 상태: 열림
- source:
  - `hololive_20260414_ai_smell_docs_cleanup_report.md` §7.3
  - `hololive_20260414_ai_smell_docs_cleanup_report.md` §8
- owning seam:
  - 레포 전반
  - 특히 `hololive/hololive-kakao-bot-go/internal/app/`
- boundary file:
  - `*additional_test.go` 계열 전반
- contract risk:
  - 회귀 보강 맥락이 파일명에만 누적돼 테스트 책임 경계가 흐려짐
- proof map:
  - 도메인별 테스트 재통합
  - app 조립 테스트와 도메인 동작 테스트 분리
  - 추가 배경은 commit/문서로 이관
- next action:
  - `internal/app` 부터 시작해 `additional_test.go` 를 도메인 기준 파일로 재배치
- defer rationale:
  - 단기 회귀 대응용 구조가 장기 유지보수성 문제로 남아 있는 상태

### 3.9 P2 — 문서 거버넌스 재발 방지 규칙을 추가해야 함

- 상태: 열림
- source: `hololive_20260414_ai_smell_docs_cleanup_report.md` §8
- owning seam:
  - root docs governance
  - PR/review policy
- boundary file:
  - `README.md`
  - `docs/README.md`
  - 향후 리뷰 규칙 문서 또는 AGENTS/CONVENTIONS 계열
- contract risk:
  - current SSOT 승격 없이 신규 운영 문서가 다시 추가되면 같은 혼선이 재발 가능
- proof map:
  - 리뷰 규칙 1건 추가
  - 신규 운영 문서가 `docs/current/` 또는 `DOCUMENTATION_STATUS` 에 반영되는지 점검하는 체크리스트
- next action:
  - **“current SSOT 승격 없는 신규 운영 문서 금지”** 규칙을 리뷰 정책에 추가
- defer rationale:
  - 이번 정리로 경계는 세워졌고, 남은 것은 재발 방지 장치

### 3.10 P2 — cache warm 최적화를 full batching 단계까지 확장할지 결정해야 함

- 상태: 열림
- source: `hololive_20260414_remaining_followups.md` §3.6
- owning seam:
  - `hololive/hololive-shared/pkg/service/alarm/cache_warm.go`
  - `hololive/hololive-shared/pkg/service/cache/*`
  - 관련 strict mock/test helper
- boundary file:
  - `hololive/hololive-shared/pkg/service/alarm/cache_warm.go`
- contract risk:
  - full cross-key batching 부재
  - batch size/chunk/error metric 튜닝 부재
  - strict mock/harness 정합성 추가 정리 필요
- proof map:
  - cross-key batched write 지원 여부 설계
  - strict mock 의 `B()/DoMulti()` 와 `HMSet` fallback 모델링 강화
  - batch/chunk/error metric 검증
- next action:
  - cache abstraction 의 cross-key batched write 허용 범위를 먼저 정리
- defer rationale:
  - 현재 단계는 안전한 집계 최적점까지 도달했고 즉시 병목으로 남아 있지는 않음

## 4. 권장 실행 순서

1. dispatcher durable retry / DLQ 설계 고정
2. alarm persistence authoritative DB vs durable outbox 결정
3. `internal/app` 조립 레이어 축소 설계 작성
4. member adapter explicit error-return 모델 전환
5. alarm 운영 카탈로그/runbook 확장
6. target minute wrapper surface 축소
7. 문서 거버넌스 재발 방지 규칙 추가
8. clone helper 중복 제거 + `*additional_test.go` 정리
9. cache warm full batching / chunk tuning 검토

## 5. 최종 판단

정본 문서 3종 기준으로 보면, **급한 불은 이미 꺼졌고 남은 것은 구조와 운영 내구성을 높이는 후속 과제**입니다.

가장 중요한 구분은 다음과 같습니다.

- 문서 SSOT 혼선은 이번 정리 범위에서 닫혔습니다.
- 즉시성 운영 버그도 대부분 닫혔습니다.
- 아직 열린 이슈는 durable retry/outbox, interface 명시화, 조립 레이어 축소, 테스트/문서 거버넌스 정리처럼 **다음 단계 품질 투자 항목**입니다.

즉, 지금 우선순위는 “무엇이 아직 고장났는가”보다 “어디를 정리해야 다음 장애와 구조 악화를 막을 수 있는가”에 맞춰야 합니다.
