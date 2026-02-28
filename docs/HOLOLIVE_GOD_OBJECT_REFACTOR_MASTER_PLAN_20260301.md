# HOLOLIVE God Object Refactor Master Plan

작성일: 2026-03-01 (KST) / 2026-02-28 (UTC)
문서 상태: v1.0 (문서화 1차)
범위: hololive-bot monorepo 내 God Object(또는 진행형) 7개 후보

---

## 1) 배경 / 문제 정의

최근 backend modernize/fail-fast 정비 과정에서, 일부 타입/파일이 다중 도메인 책임을 흡수하면서 **변경 영향 반경이 과도하게 커진 구조**가 확인되었다.

핵심 문제:
- 단일 타입에 책임이 집중되어 회귀 위험 증가
- Admin/Kakao 간 중복 구조로 동기화 비용 상승
- 신규 기능 추가 시 기존 거대 타입에 조건 분기만 누적
- 테스트가 기능 경계가 아닌 구현 묶음 단위로 비대화

본 문서는 **코드 변경 없이** 아래를 확정한다.
1. God Object 진단 근거(정량/정성)
2. 분리 대상 경계(interfaces/types)
3. 실행 우선순위(P0/P1/P2)
4. 검증/롤백 기준

---

## 2) 식별 기준 (정량 + 정성)

### 2.1 정량 기준
다음 지표를 함께 본다.
- LOC (파일 크기)
- 메서드/함수 수
- 필드 수
- import 수
- 파일 분산도(한 타입 메서드가 몇 개 파일에 흩어져 있는지)

### 2.2 정성 기준
- 도메인 경계(멤버/알람/스트림/설정/템플릿 등) 혼재 여부
- orchestration + domain logic + infra handling 동시 포함 여부
- 변경 시 ripple effect (파급 범위) 크기
- 테스트가 책임 경계가 아닌 묶음 구조를 강제하는지 여부

### 2.3 판정 레벨
- **Strong**: 즉시 분해 필요 (P0)
- **Medium**: 단기 분해 필요 (P1)
- **Growing**: 진행형 비대화, 선제적 분해 권장 (P2)

---

## 3) 후보 7개 진단 요약표

| ID | 대상 | 정량 근거 | 판정 | 우선순위 |
|---|---|---|---|---|
| A | `hololive-kakao-bot-go/internal/server/APIHandler` | 50 methods / 26 fields / method LOC 1442 / 12 files | Strong | P0 |
| B | `hololive-admin/internal/server/APIHandler` | 47 methods / 19 fields / method LOC 1391 / 12 files | Strong | P0 |
| C | `hololive-shared/pkg/service/youtube/stats_repository.go` | 1157 LOC / 28 methods | Medium (God Repository 경향) | P2 |
| D | `hololive-kakao-bot-go/internal/bot/Bot` | 12 methods / 25 fields / imports 26 | Medium-Strong | P1 |
| E | `hololive-shared/pkg/providers/providers.go` | 761 LOC / 44 funcs / 31 imports | Growing (God File) | P1 |
| F | `hololive-shared/pkg/adapter/message.go` | 622 LOC / 30 methods | Growing (Parser 비대화) | P2 |
| G | `hololive-scraper-rs/crates/alarm/service/src/scheduler.rs` | 852 LOC / 48 fn | Growing | P2 |

---

## 4) 후보별 상세 진단

## A. Kakao `APIHandler`
- 파일: `hololive/hololive-kakao-bot-go/internal/server/api.go`
- 근거:
  - 필드 26개
  - 메서드 50개가 12개 파일(`api_member.go`, `api_stream.go`, `api_settings.go`, `api_template.go` 등)에 분산
  - 멤버/알람/룸/ACL/스트림/통계/설정/템플릿/OAuth/마일스톤 책임 동시 보유
- 문제:
  - handler 단일 타입 수정이 다수 도메인 회귀로 연결
  - endpoint 추가 시 동일 타입 확장만 반복
- 목표:
  - 도메인별 handler 타입 분리 + 라우터 조립단 분리

### 분리안 (결정)
- `MemberAPIHandler`
- `AlarmAPIHandler`
- `RoomACLAPIHandler`
- `StreamAPIHandler`
- `SettingsAPIHandler`
- `TemplateAPIHandler`
- `MilestoneAPIHandler`
- `OAuthHandler`

---

## B. Admin `APIHandler`
- 파일: `hololive/hololive-admin/internal/server/api.go`
- 근거:
  - 필드 19개
  - 메서드 47개, 12개 파일 분산
  - Kakao와 동일한 도메인 폭
- 문제:
  - Kakao와 구조 대칭이지만 구현은 이중 유지
  - 에러 계약/응답 포맷 drift 위험
- 목표:
  - 공통 handler contract + admin/kakao thin adapter 구조

### 분리안 (결정)
- Kakao와 동일한 handler 분해 축 유지
- 공통 endpoint contract를 `hololive-shared/pkg/server` 레벨로 문서화

---

## C. `StatsRepository`
- 파일: `hololive/hololive-shared/pkg/service/youtube/stats_repository.go`
- 근거:
  - 1157 LOC 단일 파일
  - 28 methods
  - 통계 저장/조회 + 마일스톤 + 접근 알림 + 랭킹 + 그래프 처리 집중
- 문제:
  - SQL 책임 과포화 (조회/기록/도메인 집계 혼합)
  - 테스트 범위가 지나치게 넓어짐
- 목표:
  - read/write + milestone + graph + notification으로 repo 분리

### 분리안 (결정)
- `StatsWriteRepository`
- `StatsReadRepository`
- `MilestoneRepository`
- `SubscriberGraphRepository`
- `ApproachingNotificationRepository`

---

## D. `Bot`
- 파일: `hololive/hololive-kakao-bot-go/internal/bot/bot.go`
- 근거:
  - 필드 25개
  - imports 26개
  - runtime orchestration + ingress filtering + command execution + transport + shutdown 동시 보유
- 문제:
  - 수신/실행/송신/종료 책임 혼재
  - 장애 분석 시 원인 경계가 불명확
- 목표:
  - lifecycle/orchestration과 command flow 분리

### 분리안 (결정)
- `MessageIngress` (self-filter, ACL, parse entry)
- `CommandExecutor` (registry execute, normalize)
- `CommandTransport` (sendMessage/sendImage/sendError)
- `BotLifecycle` (start/wait/shutdown/resource close)

---

## E. `providers.go` (God File 성향)
- 파일: `hololive/hololive-shared/pkg/providers/providers.go`
- 근거:
  - 761 LOC
  - 44 funcs
  - imports 31
- 해석:
  - composition root 특성상 폭이 넓은 것은 정상이나, 현재는 읽기/변경 비용이 임계치 초과
- 목표:
  - 기능군별 provider 파일 분리로 탐색성/변경성 개선

### 분리안 (결정)
- `providers/infra_providers.go` (cache/db/iris)
- `providers/member_providers.go`
- `providers/alarm_providers.go`
- `providers/llm_providers.go`
- `providers/youtube_providers.go`
- `providers/delivery_providers.go`

---

## F. `MessageAdapter` (진행형)
- 파일: `hololive/hololive-shared/pkg/adapter/message.go`
- 근거:
  - 622 LOC
  - 30 methods
  - command parser if-chain 누적 구조
- 문제:
  - 커맨드 추가 시 중앙 파일 수정 필수
  - parser 단위 테스트 경계가 약함
- 목표:
  - command별 parser registry 도입

### 분리안 (결정)
- 인터페이스:
  - `type CommandParser interface { Parse(cmd string, args []string, raw string) (*ParsedCommand, bool) }`
- registry:
  - 순차 체인 대신 parser 리스트 주입/등록 방식
- 파일 분해:
  - `message_parser_alarm.go`, `message_parser_member.go`, `message_parser_stats.go` 등

---

## G. Rust Alarm Scheduler (진행형)
- 파일: `hololive/hololive-scraper-rs/crates/alarm/service/src/scheduler.rs`
- 근거:
  - 852 LOC
  - 48 fn
  - loop 실행/metrics/health/mapping fetch/test helper 집중
- 문제:
  - loop 변경 시 scheduler 전역 영향
  - 모듈 경계 약화로 리뷰 난이도 상승
- 목표:
  - loop/mapping/health/metrics를 모듈 분리

### 분리안 (결정)
- `scheduler/youtube_loop.rs`
- `scheduler/chzzk_loop.rs`
- `scheduler/twitch_loop.rs`
- `scheduler/mapping.rs`
- `scheduler/health.rs`
- `scheduler/metrics.rs`

---

## 5) 타겟 아키텍처 / 공개 인터페이스 변경 계획

## 5.1 Go API Layer
### 기존
- `APIHandler` 1개가 다중 도메인 메서드 보유

### 목표
- 도메인별 핸들러 분리 + route registration 분리

```text
api_router
 ├─ registerMemberRoutes(MemberAPIHandler)
 ├─ registerAlarmRoutes(AlarmAPIHandler)
 ├─ registerRoomRoutes(RoomACLAPIHandler)
 ├─ registerStreamRoutes(StreamAPIHandler)
 ├─ registerSettingsRoutes(SettingsAPIHandler)
 ├─ registerTemplateRoutes(TemplateAPIHandler)
 └─ registerMilestoneRoutes(MilestoneAPIHandler)
```

## 5.2 Repository Layer
- `StatsRepository` 분해 후 public contract를 read/write/cross-cut으로 나눈다.
- 주의: 외부 호출자 입장에선 API endpoint contract가 변하면 안 됨.

## 5.3 Bot Runtime Layer
- `Bot` 외부 시작점(`Start`, `Shutdown`, `HandleMessage`)은 유지
- 내부 구현만 ingress/executor/transport/lifecycle로 위임

## 5.4 Parser Layer
- `MessageAdapter.ParseMessage` 시그니처는 유지
- 내부 파싱 로직만 registry 방식으로 교체

## 5.5 Rust Scheduler Layer
- `AlarmScheduler::run` public entry는 유지
- 내부 loop 실행 책임만 모듈 분리

---

## 6) 단계별 실행 로드맵

## P0 (즉시)
### 목표
- APIHandler 두 개의 책임 분리를 설계/구현 가능한 수준으로 확정

### 작업 단위
1. Kakao/API route 그룹별 핸들러 타입 정의
2. Admin 동일 축으로 핸들러 분리
3. 공통 에러 응답 helper contract 재확인
4. Admin/Kakao endpoint parity 검증 테스트 정의

### 산출물
- handler 분해 설계서
- route registration 분해 명세
- parity test matrix

---

## P1
### 목표
- Bot 및 providers.go 구조적 비대화 완화

### 작업 단위
1. `Bot` 내부 책임 분해(ingress/executor/transport/lifecycle)
2. providers 기능군별 파일 분해
3. dependency wiring 가시화

### 산출물
- runtime 책임도
- provider 분할 맵

---

## P2
### 목표
- 진행형 비대화 선제 정리

### 작업 단위
1. StatsRepository 분해
2. MessageAdapter parser registry 도입
3. Rust scheduler 모듈 분해

### 산출물
- repo split map
- parser plugin contract
- scheduler module map

---

## 7) 테스트/검증 시나리오

## 7.1 API 회귀
- 기존 endpoint/HTTP status/body 계약 불변
- Admin/Kakao 동일 endpoint 동작 parity 유지

## 7.2 에러 계약
- Go 에러 포맷: `"action: context: cause"`
- wrapping: `fmt.Errorf("...: %w", err)` 준수
- handler에서 `context.Context` 전달 경로 유지

## 7.3 성능/운영
- p95 latency 악화 없음
- scheduler heartbeat/readiness 판정 유지
- startup/shutdown 동작 동일

## 7.4 테스트 계층
- unit: 분해된 handler/service/repo/parser 단위
- integration: API/DB/Valkey 경계
- parity: admin vs kakao API contract

---

## 8) 리스크 / 롤백 / 운영 영향

## 주요 리스크
1. 핸들러 분해 과정에서 route wiring 누락
2. admin/kakao 동등성 깨짐
3. repository 분해 후 트랜잭션/쿼리 경계 변화
4. parser 순서 변경으로 명령 매칭 우선순위 drift

## 완화책
- parity matrix 선작성 후 구현
- route별 golden test 유지
- 작은 PR 단위(도메인별 분리)
- feature flag 없이 구조만 바꾸고 external contract 고정

## 롤백 전략
- 단계별 PR 분리(P0-1, P0-2 ...)
- 문제 발생 시 해당 분리 PR 단독 revert 가능하게 유지

---

## 9) 완료 정의 (Definition of Done)

다음을 모두 만족해야 완료로 간주한다.
1. 후보 7개 각각에 대해
   - 정량 근거
   - 분리 경계
   - 테스트 기준
   - 리스크/롤백 기준
   가 문서에 명시됨
2. P0/P1/P2 작업이 **구현자가 의사결정 없이 착수 가능한 수준**으로 쪼개짐
3. public contract(외부 API/호출 시그니처) 변경 여부가 명확히 표시됨

---

## 10) 부록 A — 측정 근거 스냅샷

- A Kakao APIHandler: methods 50 / fields 26 / method LOC 1442
- B Admin APIHandler: methods 47 / fields 19 / method LOC 1391
- C StatsRepository: LOC 1157 / methods 28
- D Bot: methods 12 / fields 25 / imports 26
- E providers.go: LOC 761 / funcs 44 / imports 31
- F MessageAdapter: LOC 622 / methods 30
- G Rust Scheduler: LOC 852 / fn 48

(측정 시점: 2026-03-01 KST, repo HEAD 기준)

---

## 11) 부록 B — 파일/타입 매핑

- Kakao API handler root: `hololive/hololive-kakao-bot-go/internal/server/`
- Admin API handler root: `hololive/hololive-admin/internal/server/`
- Stats repo: `hololive/hololive-shared/pkg/service/youtube/stats_repository.go`
- Bot: `hololive/hololive-kakao-bot-go/internal/bot/bot.go`
- Providers: `hololive/hololive-shared/pkg/providers/providers.go`
- Message adapter: `hololive/hololive-shared/pkg/adapter/message.go`
- Rust scheduler: `hololive/hololive-scraper-rs/crates/alarm/service/src/scheduler.rs`

---

## 12) 부록 C — 우선순위 백로그 (실행 단위)

## P0 backlog
- [ ] P0-1 Kakao APIHandler 분해: 타입 선언 및 route registration 분리
- [ ] P0-2 Admin APIHandler 동일 분해
- [ ] P0-3 Admin/Kakao parity 테스트 추가
- [ ] P0-4 에러 응답 helper 공통 contract 검증

## P1 backlog
- [ ] P1-1 Bot ingress/executor/transport/lifecycle 분해
- [ ] P1-2 providers.go 기능군별 파일 분리
- [ ] P1-3 dependency wiring 검증 테스트

## P2 backlog
- [ ] P2-1 StatsRepository read/write/milestone/graph 분리
- [ ] P2-2 MessageAdapter parser registry 도입
- [ ] P2-3 Rust scheduler loop/mapping/health/metrics 모듈 분리

---

## 명시적 가정/기본값

- 본 문서는 **문서화 단계**이며 repo-tracked 코드 변경은 포함하지 않음
- 구현 단계에서도 external API contract는 기본적으로 불변으로 유지
- 우선순위는 P0(APIHandler) → P1(Bot/providers) → P2(나머지)
- 새로운 하드코딩/비구조 로깅(fmt/println) 금지, 기존 AGENTS 규칙 준수
