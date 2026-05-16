# Repo-Wide Remaining Risks Closure Report (2026-04-15)

상태: **CLOSED**

이 파일은 원래 `hololive_20260414_repo_wide_review_master.md` 이후 남아 있던 리스크를 추적하기 위한 실행 가이드였다.
2026-04-15 마감 기준으로, 당시 열려 있던 P0/P1 항목은 모두 닫혔고 이 문서는 **종료 기록**으로 유지한다.

---

## 1. 닫힌 머지 hygiene 항목

아래 축은 서로 분리된 커밋 단위로 정리됐다.

1. `hololive-shared` YouTube correctness + poller / outbox / tracking seam 분리
2. `hololive-stream-ingester` ops session 재사용 + continuous observation collector / closeout 분리
3. `hololive-kakao-bot-go` ACL atomic swap + `internal/app` http/runtime/wiring seam 정리
4. `hololive-llm-sched` summarizer prompt asset 분리 + semi-golden 회귀 테스트 추가
5. governance / docs / thresholds 정리
6. `admin-dashboard` generated output 정리와 독립 검증

즉, 이번 라운드의 변경은 더 이상 “한 덩어리 워킹트리” 상태가 아니라 **리뷰 가능한 축별 커밋**으로 분리됐다.

---

## 2. 닫힌 기술 리스크

### R1-1. ACL room set atomicity
닫힘.

- `syncRoomsToValkey` 는 temp key + Lua swap 기반으로 수렴했다.
- temp write 실패 / swap 실패 / 성공 경로 테스트가 추가됐다.
- mock-only seam 에서는 raw Valkey client 미구성 시 안전한 fallback 경로를 유지한다.

### R1-2. `internal/app` runtime / wiring 경계
닫힘.

- `internal/app/http/` 구현 분리 완료
- `internal/app/runtime/` helper seam 도입 완료
- `internal/app/wiring/` helper seam 도입 완료
- 루트 `internal/app` 파일은 façade / orchestration 위주로 얇아졌다.

### R1-3. `*_additional_test.go`
닫힘.

- `internal/app` 하위 `*_additional_test.go` 는 0개다.
- 테스트 파일명은 책임 중심으로 재배치됐다.

### R1-4. `delivery_timelines.go`
닫힘.

- query / build / classification 책임이 분리됐다.
- `delivery_timelines.go` 는 더 이상 모든 책임을 한 파일에 쌓아두지 않는다.

### R1-5. `tracking/repository.go`
닫힘.

- identity / source post / delivery state 책임이 분리됐다.
- sent-state 변경이 source-post 조회 로직과 한 파일에 뒤섞여 있지 않다.

### R1-6. stream-ingester continuous observation orchestration
닫힘.

- collector wiring 과 closeout policy 가 분리됐다.
- session 재사용은 유지된다.
- collector seam 전용 proof 테스트가 추가됐다.

### R1-7. summarizer prompt semi-golden
닫힘.

- weekly / monthly semi-golden 테스트가 추가됐다.
- 아래 anchor 누락을 회귀로 잡는다.
  - `<scope_fence priority="HARD">`
  - `<date_authority priority="HARD">`
  - `<member_filter>`
  - `<translation_guide>`
  - weekly / monthly `<example>` 블록

---

### R2-1. `hololive-shared/pkg/service/youtube/service.go` / `scheduler.go`
닫힘.

- `scheduler.go` 는 alert / watcher helper seam 으로 분리됐다.
- `service.go` 는 upcoming / channel-statistics / read / retry / policy seam 으로 분리됐다.
- 두 파일 모두 composition-root 성격만 남기고 read-side / dispatch-side helper 가 전용 파일로 이동했다.

### R2-2. current docs SSOT sync
닫힘.

- `docs/current/architecture/app-bootstrap-boundary-guide.md` 는 현재 경계 상태를 반영한다.
- `docs/current/README.md` 는 종료 기록 문서를 현재 SSOT 목록으로 유지한다.

## 3. 검증 증거

아래 검증이 모두 통과했다.

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/... -count=1
cd hololive/hololive-kakao-bot-go && go test ./internal/app/... ./internal/service/acl -count=1
cd hololive/hololive-llm-sched && go test ./internal/service/majorevent/summarizer -count=1
cd hololive/hololive-stream-ingester && go test ./internal/communityshorts ./internal/runtime ./internal/ops -count=1
./scripts/architecture/check-tracked-local-artifacts.sh
./scripts/architecture/check-file-loc.sh
./scripts/architecture/check-go-module-loc.sh
git diff --check
cd admin-dashboard/backend && make test
cd admin-dashboard/frontend && npm run lint && npm run build
```

---

## 4. 남겨둔 비차단 메모

아래는 현재 기준으로 **열린 리스크가 아니라 후속 개선 메모**다.

- ACL atomic swap 은 raw Valkey seam 을 직접 쓰므로, 장래 cluster-aware abstraction 이 필요해지면 별도 공용 seam 으로 승격할 수 있다.
- summarizer regression 은 semi-golden 이므로 비핵심 문구 drift 는 허용한다.
- stream-ingester continuous observation 은 seam-level proof 는 있지만 DB-backed end-to-end 전용 테스트는 아직 없다.

이 항목들은 현 머지 차단 사유가 아니다.

## 4A. 비차단 후속 cleanup 후보

2026-04-15 후속 라운드에서 C1 / C2 / C3 / C4 는 모두 마감됐다.
현재 이 섹션은 “무엇이 더 남았는가?”보다 “어떤 축이 어떻게 닫혔는가?”를 기록하는 종료 메모다.

### C1. `hololive-shared/pkg/service/youtube/scheduler.go`

닫힘.

- `scheduler.go` 는 생성자 / 상태 / 상수 중심으로 축소됐다.
- `scheduler_batch.go`
  - `Start`, `Stop`, `runBatch`
  - batch rotation / recent video batch orchestration
- `scheduler_tracking.go`
  - `trackAllSubscribers`
  - `prepareWorkItems`
  - `processAndRecordChanges`
  - `processMilestones`
- `scheduler_milestones.go`
  - milestone calculation / formatting / persistence helper
- `Stop()` 은 idempotent 하게 정리돼 lifecycle guard 가 더 단단해졌다.

### C2. `hololive-shared/pkg/service/youtube/service_upcoming.go`

닫힘.

- `service_upcoming.go` 는 orchestrator / cache gate 위주로 축소됐다.
- `service_upcoming_scrape.go`
  - scrape phase
  - scraped event conversion
- `service_upcoming_fallback.go`
  - API fallback
  - cache/store decision
- upcoming 전용 회귀 테스트가 추가돼 cache hit / quota-blocked fallback / scraped conversion edge 가 고정됐다.

### C3. `hololive-kakao-bot-go/internal/app/bootstrap_*`

닫힘.

- `internal/app/bootstrap/` 전용 디렉터리 도입
- provider / core / service / bot helper implementation 은 `internal/app/bootstrap/` 로 이동했다.
- 루트 `internal/app` 의 `bootstrap_*.go` / `providers_*.go` 는 thin wrapper / orchestration / local shape adapter 성격으로 축소됐다.
- public import path 와 테스트 진입점은 유지됐다.

### C4. stream-ingester continuous observation end-to-end proof

닫힘.

- `collectCommunityShortsContinuousObservationReportWithSession(...)` seam 을 추가해 session-backed 통합 proof 를 고정했다.
- DB-backed integration-style test 로 아래 경로를 실제 sqlite-backed session 에서 검증한다.
  - active observation
  - finalized observation
  - dataset unavailable fallback
- report assembly 전체 경로가 collector split 이후에도 유지됨을 fresh proof 로 확인했다.

## 4B. 권장 실행 순서

현재 기준으로 남은 후속 순서는 없다.

## 4C. 종료 기준

후속 cleanup 라운드를 “완료”로 부르려면 최소한 아래를 만족한다.

- 각 대상 파일이 책임 기준으로 더 작은 seam 으로 나뉜다.
- 기존 public import path / behavior 는 유지된다.
- 관련 패키지 테스트가 fresh run 으로 통과한다.
- architecture LOC threshold 가 새 구조에 맞게 다시 tightened 된다.

---

## 5. 최종 요약

2026-04-15 기준 이 문서가 추적하던 “남은 리스크”의 본질은 마지막 구조 마감과 머지 hygiene 였다.
그 마감은 이번 라운드에서 완료됐다.

- 변경은 축별 커밋으로 분리됐다.
- atomicity / large-file seam / orchestration 분리가 끝났고, `youtube/service_upcoming.go` / `youtube/scheduler.go` / `internal/app/bootstrap/` / stream-ingester continuous observation proof 보강까지 마감됐다.
- 테스트와 current docs 는 새 경계에 맞게 갱신됐다.

따라서 이 문서는 더 이상 실행 대기 가이드가 아니라 **종료 기록**이다.
