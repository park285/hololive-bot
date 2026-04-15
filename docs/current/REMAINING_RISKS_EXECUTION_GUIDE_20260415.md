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

- `docs/current/APP_BOOTSTRAP_BOUNDARY_GUIDE.md` 는 현재 경계 상태를 반영한다.
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

아래는 “더 정리는 가능한가?”에 대한 현재 기준의 명시적 답변이다.
가능하지만, 이제부터는 **필수 리스크 제거 단계**가 아니라 **장기 cleanup / 유지보수 비용 절감 단계**다.

### C1. `hololive-shared/pkg/service/youtube/scheduler.go`

현재 상태:
- `scheduler.go` 는 alert / watcher helper 분리 이후에도 orchestration 책임이 남아 있다.
- 현재 파일은 batch loop / subscriber tracking / milestone persistence 를 함께 가진다.

후속 후보:
- `scheduler_batch.go`
  - `Start`, `Stop`, `runBatch`
  - batch rotation / recent video batch orchestration
- `scheduler_tracking.go`
  - `trackAllSubscribers`
  - `prepareWorkItems`
  - `processAndRecordChanges`
  - `processMilestones`

권장 조건:
- scheduler 관련 테스트를 behavior lock 으로 먼저 재실행한다.
- 새 public abstraction 은 만들지 않고 file-level seam 분리만 한다.

기대 효과:
- batch orchestration 수정과 subscriber-tracking 수정이 같은 파일에서 충돌하지 않는다.
- `scheduler.go` 는 최종적으로 composition root 수준까지 더 줄일 수 있다.

### C2. `hololive-shared/pkg/service/youtube/service_upcoming.go`

현재 상태:
- `service.go` 는 축소됐지만 `service_upcoming.go` 가 새 최대 hotspot 후보가 됐다.
- 이 파일은 scraper phase / API fallback / cache path / event conversion 을 함께 가진다.

후속 후보:
- `service_upcoming_scrape.go`
  - scrape phase
  - scraped event conversion
- `service_upcoming_fallback.go`
  - API fallback
  - cache/store decision
- 또는 최소한 conversion helper 를 별도 파일로 이동

권장 조건:
- `service_test.go` 와 `service_policy_test.go` 외에 upcoming-specific proof 를 더 추가할지 먼저 판단한다.
- public method shape 는 그대로 둔다.

기대 효과:
- scraper 정책 수정과 fallback 정책 수정이 분리된다.
- `service_upcoming.go` LOC drift 를 threshold 아래에서 더 안정적으로 유지할 수 있다.

### C3. `hololive-kakao-bot-go/internal/app/bootstrap_*`

현재 상태:
- `http / runtime / wiring` seam 은 정리됐다.
- 하지만 bootstrap orchestration 파일들은 아직 루트 `internal/app` 에 남아 있다.

후속 후보:
- `internal/app/bootstrap/` 전용 디렉터리 도입
- 이동 1차 후보:
  - `bootstrap_bot*.go`
  - `bootstrap_core*.go`
  - `bootstrap_services*.go`
  - `providers_alarm_consumers.go`
  - `providers_single_consumer.go`

권장 조건:
- façade 유지 원칙을 지켜 import churn 을 만들지 않는다.
- bootstrap 전용 이동은 http/runtime/wiring 만큼의 직접 효익이 있는지 먼저 확인한다.

기대 효과:
- startup/wiring와 provider bootstrap churn 이 루트 패키지에 다시 쌓이지 않는다.
- `APP_BOOTSTRAP_BOUNDARY_GUIDE.md` 의 마지막 장기 과제를 실제 코드 구조로 수렴시킬 수 있다.

### C4. stream-ingester continuous observation end-to-end proof

현재 상태:
- collector / closeout seam-level proof 는 존재한다.
- DB-backed end-to-end 전용 proof 는 아직 없다.

후속 후보:
- `CollectCommunityShortsContinuousObservationReport(...)` 경로를 위한 DB-backed integration-style test 추가
- 최소한 다음 경로를 고정:
  - active observation
  - finalized observation
  - dataset unavailable fallback

기대 효과:
- seam split 이후에도 report assembly 전체 경로가 보존되는지 더 강하게 보장한다.

## 4B. 권장 실행 순서

후속 cleanup 을 실제로 다시 잡는다면 아래 순서가 효율적이다.

1. `service_upcoming.go`
   - 현재 hotspot 집중 완화
2. `scheduler.go` orchestration 재분리
   - batch / tracking seam 정리
3. `internal/app/bootstrap/`
   - import churn 을 최소화하는 장기 구조 정리
4. stream-ingester DB-backed e2e proof 보강
   - 유지보수 안정성 강화

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
- atomicity / large-file seam / orchestration 분리가 끝났고, `youtube/service.go` / `youtube/scheduler.go` P2 seam 분리까지 마감됐다.
- 테스트와 current docs 는 새 경계에 맞게 갱신됐다.

따라서 이 문서는 더 이상 실행 대기 가이드가 아니라 **종료 기록**이다.
