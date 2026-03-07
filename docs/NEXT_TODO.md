# 향후 작업 TODO

> 최종 갱신: 2026-03-07
> 아키텍처: Go 단일 런타임 (bot + dispatcher-go + llm-scheduler + stream-ingester)
> 운영 기준: Docker Compose 단일 호스트 배포

---

## 1. 2026-03-06 코드베이스 리팩토링 (우선순위: HIGH)

**상세 문서**
- 감사 보고서: `docs/20260306/CODEBASE_REFACTOR_AUDIT_20260306.md`
- 실행 TODO: `docs/20260306/CODEBASE_REFACTOR_TODO_20260306.md`

### 즉시 착수 권장
- [x] 알람 구독 경로 전체 플랫폼 재동기화 제거
- [x] 알람 목록 batch read API 도입
- [x] YouTube scheduler latest stats / milestone 배치화
- [x] Outbox 상태 업데이트와 전송 경로 배치화
- [x] Auth 세션 발급 원자화(`SetNX`) 및 중복 서비스 정리 시작

### 2026-03-06 반영 메모
- auth 세션 발급을 `SetNX`로 원자화
- 알람 플랫폼 매핑을 요청 경로 증분 동기화로 전환
- 알람 목록 조회를 shared contract/API/client의 batch view 경로로 승격하고 legacy fallback 제거
- scheduler latest stats / milestone 조회를 batch화
- outbox per-room delivery 실패/aggregate/sending 경로를 batch + bounded parallelism으로 개선
- `!live` CHZZK 조회를 batch OpenAPI 전용 경로로 정리하고 legacy fallback 제거
- YouTube/Holodex primary fallback fan-out을 공통 `FetchPlan`/`RunPrimary`로 정리
- LLM summarizer fallback 결과 타입(`primary`/`fallback`/`empty`) 명시화 완료
- YouTube scraper parser를 extractor/parser/scorer 계층으로 분리하고 legacy ytInitialData 패턴 제거 + bounded scan 제한 추가
- Outbox dispatcher를 claim/load·send·retry/aggregate seam으로 분리하고 legacy `PerRoomMode` 토글 제거
- outbox enqueue/dispatch, alarm queue drain, fallback execution에 Prometheus metric을 추가
- alarm add/remove/clear/list-view 경로에 latency metric을 추가해 읽기/쓰기 지연을 직접 관측 가능하게 정리
- 사용처가 사라진 `/internal/alarm/member-name/:id` fallback API와 `AlarmCRUD` 계약의 legacy 멤버명 조회 메서드를 제거
- alarm dedup notified read 경로에서 legacy JSON 호환 fallback을 제거하고 현재 hash/typed payload만 허용
- 메시지 어댑터에서 configured prefix 외 `/`, `！` legacy 명령 prefix 호환을 제거
- Holodex channel schedule 경로에서 빈 YouTube scraper 결과에 대한 과도한 공식 스케줄 fallback을 제거하고, scraper 오류일 때만 fallback 하도록 축소
- Holodex `GetChannels` batch fallback을 retryable 오류 전용으로 축소하고, 개별 `/channels/{id}` fallback에서 nested scraper fan-out을 제거
- Holodex `GetChannel` fallback을 retryable 오류 전용으로 축소하고, primary+fallback 모두 실패할 때 명시적 에러를 반환하도록 정리
- Holodex `GetChannelsLiveStatus` fallback에서 공식 스케줄 2차 조회를 제거하고 YouTube scraper 1차 경로만 사용하도록 축소
- Holodex proxy toggle 경로를 concrete service 결합 대신 최소 인터페이스 의존으로 축소하고 관련 문서/주석을 현재 정책 기준으로 동기화

### 후속 구조 개선
- MemberMatcher snapshot index화 완료 (snapshot + singleflight refresh)
- LLM summarizer budget mode 1차 완료 (cache-hit search skip + conditional review + fallback result type)
- YouTube scraper parser 분리 완료 (channel/upcoming/recent parser 재배선 + parser 중복 제거)
- Outbox dispatcher 역할 분리 완료 (항상 per-room dispatch 경로 사용)
- 관측성 보강 1차 완료 (alarm latency + queue throughput + fallback hit ratio)
- 알람 member-name 조회/중복방지 경로의 legacy compatibility 제거 완료
- 메시지 파서의 legacy prefix 호환 제거 완료
- Holodex channel schedule fallback 조건 축소 완료
- Holodex channel/list/live-status fallback 구조 정리 완료 (retryable-only + nested fan-out 제거)
- Holodex proxy/public surface 축소 완료

### 다음 세션 진입점
- `hololive-shared/pkg/service/youtube` fallback/compat 전역 스캔
- `hololive-shared/pkg/service/alarm` fallback/compat 전역 스캔
- `hololive-llm-sched` fallback/compat 전역 스캔
- Holodex `shouldUseFallback` / `GetChannelSchedule` 정책 최종 리뷰
- unrelated 변경이 많은 worktree 상태라 위 경로 중심으로 작업 범위를 고정

---

## 2. 교차언어 큐 계약 유지보수 (우선순위: MED)

**현재 상태**: Go/Rust 양측 계약 테스트 통과 (2026-03-02)

### 향후 필요 시점
- `AlarmQueueEnvelope` 스키마 변경 시
- envelope version 2 도입 시

### 작업
- Rust fixture (`hololive-rs/testdata/alarm_queue/`) 수정
- Go 계약 테스트 (`hololive-shared/pkg/domain/alarm_test.go`) 동기화
- Rust `queue.rs` 버전 맵 갱신

---

## 3. 레거시 코드 정리 (우선순위: MED)

### admin/kakao-bot-go 핸들러 중복 제거
- **상태**: 1차 완료 (2026-03-02) — `hololive-shared/pkg/server/`에 공통 로직 추출
- **잔여**: `api_response.go` 포함 나머지 핸들러 군 공통화

---

## 4. Codex 안티패턴 리팩토링 (우선순위: MED)

**상태**: 17/19 완료 (2026-03-02), 잔여 2개(대기 항목)
**상세 문서**: `docs/CODEX_ANTIPATTERN_REFACTORING.md`

### 대기 (2개, 외부 의존)
- M4: JSON 폴백 제거 (Valkey 데이터 마이그레이션 확인 후)
- L4: Formatter 트레이트 제거 (mock 의존 확인 후)

---

## 5. 모듈화 (우선순위: MED)

**Phase 0~10 완료**: `docs/modularization/TODO.md`, `docs/modularization/TODO_EXTENDED.md`
**Phase 11 (품질 강화)**: `docs/modularization/TODO_PHASE11.md`
- **2026-03-05 스냅샷**
  - 완료(코드 반영): A1/A2/A3, B1/B2/B3, C1/C2/C3/C4, D3, E2/E3/E4
  - 진행중: B8, D1, D4, D5, E1
  - 잔여 핵심: B4/B5/B6/B7/B9, D2

---

## 6. 품질 개선 (우선순위: LOW)

### 5-1. Rust str_to_string 점진적 전환
- **현재**: 잔여 약 56건 (2026-03-02 기준)
- **작업**: `.to_string()` → `.to_owned()` 전환

### 5-2. Rust wildcard_enum_match_arm 점진적 전환
- **현재**: `_ =>` 패턴 약 21건 잔여 (2026-03-02 기준)
- **작업**: 명시적 variant 나열로 변경

### 5-3. Go 테스트 커버리지 확대
- **대상**: hololive-shared 핵심 패키지 (adapter, service/notification, service/youtube)
- **상태**: 진행 중 (2026-03-02)
- **추가**: Phase 11-D 상세 계획 수립 완료 (알람 체커 0%, Auth 6/7 미테스트, Notification 핵심 미테스트)
- **상세**: `docs/modularization/TODO_PHASE11.md` P11-D 섹션

---

## 7. 후속 분리 작업 (2개)

- [x] 이번 작업 범위에서 후속 작업으로 분리 처리 (2026-03-05)
  - [x] **#15 envconfig 도입**
    - 사유: Config 구조체 전면 리팩토링 필요
    - 완료: dispatcher-go `LoadConfig`, shared `LoadAdminAPI`/`LoadLLMScheduler`, shared `buildConfig`/`loadRuntimeTokensAndCORS`/`loadValkeyConfig`/`loadPostgresConfig`/`loadTelemetryConfig`/`loadCliproxyConfig`/`loadLLMConfig`/`loadExaConfig` envconfig 전환 + CORS loose bool 파싱 정합성 테스트 보강 (2026-03-05)
  - [x] **#16 OTel Metrics 통합**
    - 사유: 기존 Prometheus와 점진적 전환 필요
    - 완료: 공용 telemetry 패키지 도입 + bot/dispatcher-go/llm-sched/stream-ingester metrics export 초기화 경로 연결 + bot telemetry 래퍼 단순화 + MetricsEnabled/ExportInterval 환경변수 경로 검증 (2026-03-05)
