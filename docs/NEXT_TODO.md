# 향후 작업 TODO

> 최종 갱신: 2026-03-05
> 아키텍처: 하이브리드 (Rust=compute, Go=network)

---

## 1. 교차언어 큐 계약 유지보수 (우선순위: MED)

**현재 상태**: Go/Rust 양측 계약 테스트 통과 (2026-03-02)

### 향후 필요 시점
- `AlarmQueueEnvelope` 스키마 변경 시
- envelope version 2 도입 시

### 작업
- Rust fixture (`hololive-rs/testdata/alarm_queue/`) 수정
- Go 계약 테스트 (`hololive-shared/pkg/domain/alarm_test.go`) 동기화
- Rust `queue.rs` 버전 맵 갱신

---

## 2. 레거시 코드 정리 (우선순위: MED)

### admin/kakao-bot-go 핸들러 중복 제거
- **상태**: 1차 완료 (2026-03-02) — `hololive-shared/pkg/server/`에 공통 로직 추출
- **잔여**: `api_response.go` 포함 나머지 핸들러 군 공통화

---

## 3. Codex 안티패턴 리팩토링 (우선순위: MED)

**상태**: 17/19 완료 (2026-03-02), 잔여 2개(대기 항목)
**상세 문서**: `docs/CODEX_ANTIPATTERN_REFACTORING.md`

### 대기 (2개, 외부 의존)
- M4: JSON 폴백 제거 (Valkey 데이터 마이그레이션 확인 후)
- L4: Formatter 트레이트 제거 (mock 의존 확인 후)

---

## 4. 모듈화 (우선순위: MED)

**Phase 0~10 완료**: `docs/modularization/TODO.md`, `docs/modularization/TODO_EXTENDED.md`
**Phase 11 (품질 강화)**: `docs/modularization/TODO_PHASE11.md`
- **2026-03-05 스냅샷**
  - 완료(코드 반영): A1/A2/A3, B1~B9, C1~C4, D1~D5, E1~E4
  - llm-sched 최종 동기화(phase11-15): memberNewsSearcherAdapter 변환 경로 단순화 + X allowlist 로딩 gosec 예외 사유 명시 + `go vet/lint/test` 검증 PASS
  - llm-sched round5 재검증(2026-03-05): `go test ./... -run TestDoesNotExist`, `go vet ./...`, `golangci-lint run ./...`, `go test -count=1 ./...` 전체 PASS
  - 진행중: 전체 모듈 `go vet + golangci-lint + go test` 통합 green 정리
  - 잔여 핵심: global lint/typecheck debt 분리 트랙 처리(shared/contracts, websocket test, youtube stats 계열)
  - 라운드4 검증 요약:
    - 타입체크 최신: shared/kakao/stream-ingester/llm-sched PASS (`go test ./... -run TestDoesNotExist`)
    - lint: 변경 영역 기준 PASS, 전체 모듈 통합 lint debt는 별도 트랙 유지
    - coverage 최신: kakao internal/app 65.7%, alarm/checker 89.9%, stream-ingester internal/app 63.9%, shared cache 42.0%

---

## 5. 품질 개선 (우선순위: LOW)

### 5-1. Rust str_to_string 점진적 전환
- **현재**: 잔여 약 56건 (2026-03-02 기준)
- **작업**: `.to_string()` → `.to_owned()` 전환

### 5-2. Rust wildcard_enum_match_arm 점진적 전환
- **현재**: `_ =>` 패턴 약 21건 잔여 (2026-03-02 기준)
- **작업**: 명시적 variant 나열로 변경

### 5-3. Go 테스트 커버리지 확대
- **대상**: hololive-shared 핵심 패키지 (adapter, service/notification, service/youtube)
- **상태**: 진행 중 (2026-03-02)
- **추가**: Phase 11-D 상세 계획 추적 중 (알람 체커/인증/ingester/scraper)

---

## 6. 후속 분리 작업 (2개)

- [x] 이번 작업 범위에서 후속 작업으로 분리 처리 (2026-03-05)
  - [x] **#15 envconfig 도입**
    - 사유: Config 구조체 전면 리팩토링 필요
    - 완료: dispatcher-go `LoadConfig`, shared `LoadAdminAPI`/`LoadLLMScheduler`, shared `buildConfig`/`loadRuntimeTokensAndCORS`/`loadValkeyConfig`/`loadPostgresConfig`/`loadTelemetryConfig`/`loadCliproxyConfig`/`loadLLMConfig`/`loadExaConfig` envconfig 전환 + CORS loose bool 파싱 정합성 테스트 보강 (2026-03-05)
  - [x] **#16 OTel Metrics 통합**
    - 사유: 기존 Prometheus와 점진적 전환 필요
    - 완료: 공용 telemetry 패키지 도입 + bot/dispatcher-go/llm-sched/stream-ingester metrics export 초기화 경로 연결 + bot telemetry 래퍼 단순화 + MetricsEnabled/ExportInterval 환경변수 경로 검증 (2026-03-05)
