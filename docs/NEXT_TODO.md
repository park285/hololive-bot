# 향후 작업 TODO

> 최종 갱신: 2026-03-04
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

**상세 문서**: `docs/modularization/TODO.md`

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
