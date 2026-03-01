# Go → Rust 전면 전환 TODO LIST (Active)

> 생성일: 2026-03-01
> 정리일: 2026-03-01
> 진행 상황: [PROGRESS](GO_TO_RUST_MIGRATION_PROGRESS_20260301.md)
> 원칙: **완료([x]) 항목은 본 문서에서 제거**

---

## Phase 1: dispatcher (Go alarm-dispatcher 대체)

### P1-3. 검증 — **DONE**

- [x] **P1-3.4** p95 queue latency < 1s, error rate < 0.1%
  - 50개 batch × 20회 반복, p95 < 1s 및 error rate 0% 확인
- [x] **P1-3.5** 장애 주입: Valkey 단절 5분 → degraded + 재연결 복구
  - 300회 연속 brpop 실패(5분 압축 시뮬) → degraded 유지 → 복구 후 정상 dispatch 확인

---

## 하이브리드 아키텍처 확정 (2026-03-01)

네트워크 복잡도 분석 결과, bot/ingester/admin/llm-sched는 Go net/http 생태계(h2c 양방향, SOCKS5 런타임 토글, HTTP/2 선택적 비활성화, per-host 커넥션 풀)에 강하게 의존하여 Rust 전환 ROI가 낮음.

### Rust 서비스 (compute 집약)
- alarm-checker: 멀티 플랫폼 폴링 → dedup → 큐 발행
- scraper-rss: RSS 피드 → major_events DB
- dispatcher: 큐 소비 → 렌더 → Iris 발송 (검증 완료, cutover 대기)

### Go 서비스 (네트워크 집약)
- bot: Iris h2c 양방향 웹훅 + 커맨드 라우팅
- stream-ingester: YouTube/Holodex/Chzzk/Twitch 폴링 + SOCKS5 프록시
- admin: REST API + Auth + WebSocket
- llm-sched: OpenAI/Exa LLM 호출 + 스케줄링 + delivery outbox
- alarm-dispatcher: 큐 소비 주체 (Rust dispatcher cutover 별도 결정)

### 잔여 TODO
- [ ] Rust dispatcher cutover 타임라인 결정
- [ ] 교차언어 큐 계약 테스트 유지보수
