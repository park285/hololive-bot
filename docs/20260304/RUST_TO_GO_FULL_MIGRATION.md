# Rust → Go 전체 전환 + 서비스 통합 계획

> **날짜**: 2026-03-04
> **상태**: 완료 (Phase1~6 종료, Rust 소스/배포 참조 제거 완료)
> **목표**: 하이브리드(Rust 14 crates + Go 5 서비스) → Go 단일 언어, 바이너리 8개 → 4개

---

## 1. 배경

Rust 빌드/개발 속도 비효율 (증분 빌드 ~40s, 크로스 컴파일 복잡도)로 인해 전체 Go 단일 언어로 전환한다.
동시에 기능별 서비스 통합으로 운영 복잡도를 줄인다.

## 2. 현재 → 목표

| 현재 (8 바이너리) | 목표 (4 바이너리) | 변경 내역 |
|---|---|---|
| bot (Go, 30001) | **bot** (30001) | + admin + alarm-checker 통합 |
| admin (Go, 30002) | 제거 | bot에 흡수 |
| alarm-checker (Rust, 30011) | 제거 | bot에 흡수 |
| dispatcher (Rust, 30020) | **dispatcher-go** (30020) | Go 재구현 |
| scraper (Rust, 30010) | 제거 | llm-sched에 흡수 |
| llm-sched (Go, 30003) | **llm-sched** (30003) | + scraper 통합 |
| stream-ingester (Go, 30004) | **stream-ingester** (30004) | 변경 없음 |

## 3. Phase 의존성

```
Phase 1: Foundation (공유 패키지)
    │
    ├──→ Phase 2: Alarm-Checker → Bot 통합
    │         └──→ Phase 5: Bot + Admin 병합
    ├──→ Phase 3: Dispatcher Go 바이너리      (Phase 2와 병렬)
    └──→ Phase 4: Scraper → LLM-Sched 통합   (Phase 2,3과 병렬)

Phase 6: Rust 제거 + 배포 갱신               (Phase 2~5 전부 완료 후)
```

## 4. Phase 상세

### Phase 1: Foundation — 공유 패키지 확장 (~1,760 LOC)

| 구성요소 | 파일 위치 (hololive-shared 기준) | 포팅 원본 | LOC |
|---|---|---|---|
| Tier 상수 | `pkg/constants/alarm_tier.go` | `alarm-core::constants` | ~60 |
| TieredScheduler | `pkg/service/alarm/tier/` | `alarm-service::tier/` | ~650 |
| DedupService | `pkg/service/alarm/dedup/` | `alarm-service::dedup/` | ~650 |
| QueuePublisher | `pkg/service/alarm/queue/publisher.go` | `alarm-service::queue` | ~140 |
| QueueConsumer | `pkg/service/alarm/queue/consumer.go` | `dispatcher-notification::consumer` | ~220 |
| Checker Helpers | `pkg/service/alarm/checker/helpers.go` | checker_helpers | ~40 |

### Phase 2: Alarm-Checker → Bot 통합 (~2,430 LOC)

| 구성요소 | 파일 위치 (kakao-bot-go 기준) | 핵심 기능 |
|---|---|---|
| YouTubeChecker | `internal/service/alarm/checker/youtube_checker.go` | Holodex 폴링 → 알림 생성 |
| ChzzkChecker | `internal/service/alarm/checker/chzzk_checker.go` | Chzzk 라이브 상태 체크 |
| TwitchChecker | `internal/service/alarm/checker/twitch_checker.go` | Twitch 라이브 상태 체크 |
| Notifier | `internal/service/alarm/checker/notifier.go` | errgroup 병렬 claim + publish |
| AlarmScheduler | `internal/service/alarm/scheduler/` | 3 goroutine 루프 코디네이터 |
| Bot 통합 | `internal/app/runtime.go` 수정 | 라이프사이클 통합 |

### Phase 3: Dispatcher — 새 Go 바이너리 (~1,420 LOC)

| 구성요소 | 파일 위치 | 핵심 기능 |
|---|---|---|
| Go 모듈 | `hololive-dispatcher-go/` | 새 모듈, go.work 갱신 |
| ResponseRenderer | `internal/dispatch/render.go` | 알람 메시지 렌더링 |
| Dispatch Loop | `internal/dispatch/dispatcher.go` | BRPOP → group → render → Iris |
| Grouping | `internal/dispatch/grouping.go` | room_id 기준 그룹화 |
| Health/Ready | `internal/app/runtime.go` | `/health`, `/ready` + dispatch loop 상태 연동 |
| Bootstrap | `internal/app/` | config → Valkey → Iris → runtime |

### Phase 4: Scraper → LLM-Sched 통합 (~1,150 LOC)

| 구성요소 | 파일 위치 (llm-sched 기준) | 핵심 기능 |
|---|---|---|
| RSS Parser | `internal/service/majorevent/scraper/rss_parser.go` | gofeed 기반 RSS 파싱 |
| Feed Fetcher | `.../scraper/scraper.go` | HTTP fetch → parse → upsert |
| Feed Scheduler | `.../scraper/feed_scheduler.go` | KST 04:00 cron, retry 큐 |
| Link Checker | `.../scraper/link_checker.go` | HEAD 요청 + URL 안전성 검증 |
| Maintenance | `.../scraper/maintenance_scheduler.go` | 만료 이벤트/링크 재검증 |

### Phase 5: Bot + Admin 병합 (~300 LOC)

- Admin 핸들러를 bot 프로세스에 통합, 기존 `/api/holo/*` + `/api/auth/*` 라우트를 30001에서 제공
- Config에 `BOT_ADMIN_ENABLED`(`BotConfig.AdminEnabled`) 토글 추가
- 30002 포트 제거, 30001 단일 포트로 운영

### Phase 6: Rust 제거 + 배포 갱신 (파괴적 작업)

- `hololive/hololive-rs/` 전체 삭제 (14 crates) (**완료**)
- docker-compose: Rust 서비스 제거, dispatcher-go 추가 (**완료**)
- 배포 자산: alarm/scraper/rust-dispatcher/admin 삭제, dispatcher-go 경로 정리 (**완료**)
- pre-commit: Rust 검사 제거
- 운영/점검 스크립트 서비스 매핑 갱신 (`bot + dispatcher-go + llm-scheduler + stream-ingester`) (**완료**)
- 문서/README 최신 구조 반영 (**완료**)

## 5. 핵심 설계 결정

| 항목 | 결정 | 근거 |
|---|---|---|
| 큐 프로토콜 | Valkey `alarm:dispatch:queue` 유지 | bot↔dispatcher 프로세스 분리 유지 |
| alarm-checker IPC | Go channel (goroutine간) | 단일 프로세스 내부 |
| DedupService | Valkey SETNX + 로컬 sync.Map 폴백 | Rust 동일 전략 포팅 |
| TieredScheduler | sync.RWMutex + map (Rust DashMap 대체) | 동시성 안전, Go 관용적 |
| AlarmQueueEnvelope | v1 JSON 포맷 유지 | Go↔Go 호환, 기존 계약 테스트 유지 |

## 6. 검증 계획

| Phase | 검증 방법 |
|---|---|
| 1 | `go test ./hololive/hololive-shared/pkg/service/alarm/...` |
| 2 | Bot 기동 → alarm scheduler 시작 → Holodex/Chzzk/Twitch 폴링 → Valkey LPUSH |
| 3 | Dispatcher 기동 → BRPOP → 테스트 envelope 소비 → Iris 발송 |
| 4 | LLM-Sched 기동 → RSS fetch → MajorEvent upsert → 링크 검증 |
| 5 | Bot(30001) → `/api/holo/*`, `/api/auth/*` 접근 및 API Key/세션 인증 확인 |
| 6 | E2E: bot(alarm scheduler) → queue → dispatcher-go → Iris, 4개 바이너리 헬스체크 |

## 7. 총 규모

| Phase | 신규/수정 LOC | 파일 수 |
|---|---|---|
| 1: Foundation | ~1,760 | 12 |
| 2: Alarm→Bot | ~2,430 | 15 |
| 3: Dispatcher | ~1,420 | 14 |
| 4: Scraper→LLM | ~1,150 | 8 |
| 5: Bot+Admin | ~300 | 4 |
| 6: 정리 | 삭제 위주 | ~20 |
| **합계** | **~7,060** | **~73** |

제거 Rust 코드: ~8,000 LOC

## 8. 진행 상황

| Phase | 상태 | 비고 |
|---|---|---|
| 1.1 Tier 상수 | **완료** | `pkg/constants/alarm_tier.go` |
| 1.2 TieredScheduler | **완료** | `pkg/service/alarm/tier/` (18 tests) |
| 1.3 DedupService | **완료** | `pkg/service/alarm/dedup/` + `keys/` (21 tests) |
| 1.4 QueuePublisher | **완료** | `pkg/service/alarm/queue/publisher.go` |
| 1.5 QueueConsumer | **완료** | `pkg/service/alarm/queue/consumer.go` (12 tests) |
| 1.6 Checker Helpers | **완료** | `pkg/service/alarm/checker/helpers.go` (11 tests) |
| **Phase 1 전체** | **완료** | `cd hololive/hololive-shared && go test ./pkg/service/alarm/...` 통과 |
| **Phase 2 Alarm→Bot** | **완료** | `go test ./internal/service/alarm/... ./internal/app/...`, `go vet`, `golangci-lint run --fix` 통과 |
| **Phase 3 Dispatcher** | **완료** | `hololive-dispatcher-go` 신규 모듈 생성, `go test ./...`, `go vet ./...`, `golangci-lint run --fix ./...` 통과 |
| **Phase 4 Scraper→LLM** | **완료** | `internal/service/majorevent/scraper/` 통합, `go test ./internal/service/majorevent/scraper/... ./internal/app/...`, `go vet`, `golangci-lint run --fix` 통과 |
| **Phase 5 Bot+Admin** | **완료** | `BOT_ADMIN_ENABLED` 토글 기반 단일 프로세스 통합, `go test ./internal/service/trigger/... ./internal/app/...`, `go vet`, `golangci-lint run --fix` 통과 |
| 6 Rust 제거 | **완료** | `hololive-rs`/`hololive-admin`/`docker-compose.holo-rs.yml` 제거 및 배포 참조 정리 완료 |
