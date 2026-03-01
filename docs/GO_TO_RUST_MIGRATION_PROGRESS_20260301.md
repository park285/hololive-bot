# Go → Rust 전환 진행 상황 (Active)

> 최종 갱신: 2026-03-01 (Session 6 — Phase 1 DONE, P1-3.4/P1-3.5 검증 완료)
> 관련 문서: [TODO LIST](GO_TO_RUST_MIGRATION_TODO_20260301.md)
> 원칙: **상세 완료 내역은 축약하고, 현재 진행/잔여 항목 중심으로 유지**

---

## 완료 상태 요약

| Phase | 상태 | 비고 |
|-------|------|------|
| Pre-Phase (Contract Freeze) | **DONE** | 완료 항목 정리됨 |
| Phase 0 (shared crates) | **DONE** | 완료 항목 정리됨 |
| Phase 0.5 (workspace 리팩터링) | **DONE** | 완료 항목 정리됨 |
| Phase 1 (dispatcher) | **DONE** | 10 tests pass, clippy clean |
| Phase 2 (ingester) | N/A (Go 유지) | 하이브리드 아키텍처 확정 |
| Phase 3 (llm-sched) | N/A (Go 유지) | 하이브리드 아키텍처 확정 |
| Phase 4 (admin) | N/A (Go 유지) | 하이브리드 아키텍처 확정 |
| Phase 5 (bot) | N/A (Go 유지) | 하이브리드 아키텍처 확정 |
| Phase 6 (통합/배포) | N/A (Go 유지) | 하이브리드 아키텍처 확정 |

---

## Phase 1 (dispatcher) 현재 상태

### 전체 완료
- P1-2.2: `notification_templates` DB lookup 연동
- P1-3.1: dequeue → Iris 발송 E2E (성공/실패 + claim key release)
- P1-3.2: Kakao alarm golden fixture 비교 테스트
- P1-3.3: Valkey 오류 시 degraded 전환 + 회복 테스트
- P1-3.4: 50개 batch × 20회 반복 — p95 < 1s, error rate 0%
- P1-3.5: 300회 연속 brpop 실패(5분 압축) → degraded 유지 → 복구 후 정상 dispatch
- health/ready + graceful shutdown

### 최종 검증 결과
- `cargo test -p dispatcher-app` → **10 passed**
- `cargo clippy -p dispatcher-app --all-targets --all-features -- -D warnings` → **PASS**

---

## 다음 우선순위
- Rust dispatcher cutover 타임라인 결정
- 교차언어 큐 계약 테스트 유지보수

---

## 최종 결정: 하이브리드 아키텍처 확정 (2026-03-01)

| 영역 | 언어 | 근거 |
|------|------|------|
| alarm-checker, scraper-rss, dispatcher | Rust | compute 집약, 순수 데이터 처리 |
| bot, ingester, admin, llm-sched, alarm-dispatcher | Go | h2c/SOCKS5/HTTP2 네트워크 복잡도, Go net/http 생태계 의존 |

Phase 2~6 (ingester/llm-sched/admin/bot/통합배포)은 Go 유지로 결정, Rust 전환 불요.
