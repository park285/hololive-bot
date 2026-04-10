# Hololive Improved Version Page TODO

Source review: `hololive_improved_version_review.md`

이 문서는 리뷰 결과를 우선순위가 아니라 실행 표면(page/surface) 기준으로 재정리한 후속 TODO다.
각 페이지는 `owning seam`, `boundary`, `contract risk`, `proof map`을 함께 적어 이후 구현 범위를 바로 고정할 수 있게 한다.

## Recommended Order

1. Page 1. Alarm Runtime and Settings Propagation
2. Page 2. Scraper Scheduler Core
3. Page 3. YouTube Scraper Deploy/Config Page
4. Page 4. Workspace and Deploy Hermeticity Page
5. Page 5. Architecture Gate Page
6. Page 6. Admin Backend Holo Contract Page
7. Page 7. Dashboard Alarms Page
8. Page 8. Dashboard Members Page
9. Page 9. Dashboard Rooms Page
10. Page 10. Dashboard Stats Page
11. Page 11. Dashboard Streams Page
12. Page 12. Dashboard Milestones Page
13. Page 13. Dashboard Settings Page
14. Page 14. Shared Cleanup Page

## Execution Board

| Page | Surface | Priority | Depends On |
|------|---------|----------|------------|
| 1 | Alarm runtime/settings | P0 | none |
| 2 | Scraper scheduler core | P0 | none |
| 3 | YouTube scraper deploy/config | P0 | Page 2 |
| 4 | Workspace/deploy hermeticity | P1 | none |
| 5 | Architecture gate | P1 | none |
| 6 | Admin backend holo contract | P1 | none |
| 7 | Dashboard alarms | P1 | Page 6, Page 1 |
| 8 | Dashboard members | P2 | Page 6 |
| 9 | Dashboard rooms | P2 | Page 6 |
| 10 | Dashboard stats | P2 | Page 6 |
| 11 | Dashboard streams | P2 | Page 6 |
| 12 | Dashboard milestones | P2 | Page 6 |
| 13 | Dashboard settings | P1 | Page 1, Page 3, Page 6 |
| 14 | Shared cleanup | P3 | Page 1-6 이후 권장 |

## Dashboard Route Coverage

Actual dashboard route entrypoints:

- `/dashboard/stats` -> `admin-dashboard/frontend/src/components/StatsTab.tsx`
- `/dashboard/streams` -> `admin-dashboard/frontend/src/components/StreamsTab.tsx`
- `/dashboard/members` -> `admin-dashboard/frontend/src/components/MembersTab.tsx`
- `/dashboard/milestones` -> `admin-dashboard/frontend/src/components/MilestonesTab.tsx`
- `/dashboard/alarms` -> `admin-dashboard/frontend/src/components/AlarmsTab.tsx`
- `/dashboard/rooms` -> `admin-dashboard/frontend/src/components/RoomsTab.tsx`
- `/dashboard/settings` -> `admin-dashboard/frontend/src/components/SettingsTab.tsx`

이 문서의 dashboard page TODO는 위 실제 route entry를 기준으로 적었다.

## Page 1. Alarm Runtime and Settings Propagation

- Scope: runtime `alarm_advance_minutes` 변경이 `RuntimeScheduler`, `YouTubeChecker`, `dedup.Service`까지 재주입되게 만들고 target minute normalization을 single source로 올린다.
- Owning seam: `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler`
- Boundary:
  `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go`
  `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`
  `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go`
  `hololive/hololive-shared/pkg/server/settings/settings_applier_local.go`
- Contract risk: 운영 중 설정 변경 이후에도 중복 발송이나 누락 없이 같은 target minute 정책을 유지해야 한다.
- TODO:
  `normalizeTargetMinutes` / `buildTargetMinutes` 중복 제거
  scheduler가 mutable target minute source를 읽도록 seam 정리
  settings apply / pubsub apply 이후 런타임 루프에 새 값이 반영되도록 연결
  기존 dedup category 판단이 새 target minute와 일관되게 움직이도록 확인
- Proof map:
  설정 변경 전 알림 후보 1건
  설정 변경 후 새 minute 기준으로만 알림 후보가 나오는 경로 1건
  중복 발송 방지 경로 1건

## Page 2. Scraper Scheduler Core

- Scope: backlog 시 `+10s` 재스케줄링을 제거하고 worker count와 cadence 관련 정책을 다시 소유한다.
- Owning seam: `hololive/hololive-shared/pkg/service/youtube/poller`
- Boundary:
  `hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go`
  `hololive/hololive-shared/pkg/providers/youtube_providers.go`
- Contract risk: scheduler는 backlog 상황에서도 anchor를 임의로 깨지 말아야 하고, capacity는 hard-coded value에 묶이면 안 된다.
- TODO:
  `job.NextRunAt = now.Add(10 * time.Second)` 제거
  backlog 처리 시 다음 slot 계산이 wall clock anchor를 보존하도록 수정
  `ProvideScraperScheduler`의 `WorkerCount: 2`를 config/option으로 이동
  현재 default poll cadence와 global rate limit의 균형을 다시 계산하고 문서화
- Proof map:
  backlog 재현 테스트 1건
  worker count option 반영 테스트 1건
  기본 cadence 계산 근거 문서 업데이트

## Page 3. YouTube Scraper Deploy/Config Page

- Scope: 운영자가 compose만 보고도 scraper cadence와 capacity를 조정할 수 있게 env를 노출한다.
- Owning seam: deploy/config boundary
- Boundary:
  `docker-compose.prod.yml`
  `docs/runbook_execution/YOUTUBE_SCRAPER_RUNBOOK.md`
- Contract risk: 코드에서만 설정 가능하고 운영 compose에는 드러나지 않으면 runtime tuning 계약이 깨진다.
- TODO:
  `SCRAPER_VIDEOS_SECONDS`
  `SCRAPER_SHORTS_SECONDS`
  `SCRAPER_COMMUNITY_SECONDS`
  `SCRAPER_STATS_SECONDS`
  `SCRAPER_LIVE_SECONDS`
  필요 시 worker count env도 함께 노출
  runbook에 기본값과 조정 기준 추가
- Proof map:
  compose diff inspection
  runbook example 검토

## Page 4. Workspace and Deploy Hermeticity Page

- Scope: build/deploy/compose 기본 경로에서 `../llm/shared-go` 가정을 제거한다.
- Owning seam: repo build and deploy scripts
- Boundary:
  `build-all.sh`
  `scripts/deploy/compose-redeploy-service.sh`
  `docker-compose.prod.yml`
- Contract risk: 개발은 in-repo workspace를 쓰는데 배포는 외부 canonical path를 전제하면 self-contained 계약이 깨진다.
- TODO:
  기본 shared-go 경로를 repo 내부 기준으로 정렬
  compose `additional_contexts.shared_go_workspace`를 in-repo 기준으로 교체
  관련 README/runbook 문구 정리
- Proof map:
  스크립트 path resolution 확인 1건
  compose build context diff inspection 1건

## Page 5. Architecture Gate Page

- Scope: LOC gate가 실제로 동작하도록 threshold 파일을 채운다.
- Owning seam: architecture governance docs
- Boundary:
  `docs/architecture/go-module-loc-thresholds.txt`
  `scripts/architecture/check-go-module-loc.sh`
- Contract risk: gate input이 비어 있으면 governance check가 사실상 no-op가 된다.
- TODO:
  큰 Go 파일 후보를 추출해 threshold baseline 작성
  어떤 파일이 예외인지/축소 대상인지 설명 추가
  architecture README와 연결 상태 점검
- Proof map:
  threshold file populated 상태 확인
  check script 실행 1건

## Page 6. Admin Backend Holo Contract Page

- Scope: `/admin/api/holo/*` blind proxy를 backend-owned contract 또는 명시적 proxy boundary로 정리한다.
- Owning seam: `admin-dashboard/backend`
- Boundary:
  `admin-dashboard/backend/src/routes.rs`
  `admin-dashboard/backend/src/proxy/bot_proxy.rs`
  `admin-dashboard/docs/openapi-pipeline.md`
- Contract risk: dashboard backend가 holo contract를 소유하지 않으면 generated client와 수동 wrapper가 계속 공존한다.
- TODO:
  proxy 유지 vs backend-owned contract 중 하나를 명시적으로 선택
  선택한 모델에 맞춰 OpenAPI source of truth를 고정
  frontend holo client 정리의 선행 boundary를 확정
- Proof map:
  route contract test 1건
  generated client or proxy contract smoke 1건

## Page 7. Dashboard Alarms Page

- Scope: alarms 화면을 feature 단위와 data boundary 단위로 재분해한다.
- Owning seam: `admin-dashboard/frontend/src/features/alarms`
- Boundary:
  `admin-dashboard/frontend/src/components/AlarmsTab.tsx`
  `admin-dashboard/frontend/src/features/alarms/api.ts`
  `admin-dashboard/frontend/src/features/alarms/types.ts`
- Contract risk: giant tab가 query, mutation, filter, rendering을 함께 가지면 contract 변경 시 회귀 범위가 커진다.
- TODO:
  query/mutation 로직을 feature layer로 이동
  탭 entry는 orchestration만 남기기
  holo manual wrapper와 generated contract 정렬
- Proof map:
  alarms route render smoke 1건
  API mutation success path 1건

## Page 8. Dashboard Members Page

- Scope: members 화면의 data fetch, filter, presentational split을 진행한다.
- Owning seam: `admin-dashboard/frontend/src/features/members`
- Boundary:
  `admin-dashboard/frontend/src/components/MembersTab.tsx`
- Contract risk: 멤버 목록/검색/상세 편집이 한 파일에 얽히면 state와 transport 회귀가 커진다.
- TODO:
  route entry와 presenter 분리
  query state와 local UI state 분리
  필요한 경우 feature-local hooks로 이동
- Proof map:
  members route smoke 1건
  filter/search interaction 1건

## Page 9. Dashboard Rooms Page

- Scope: rooms 화면의 table/action/state를 분해한다.
- Owning seam: `admin-dashboard/frontend/src/features/rooms`
- Boundary:
  `admin-dashboard/frontend/src/components/RoomsTab.tsx`
- Contract risk: room action과 table state가 결합된 giant component는 CRUD 회귀를 만들기 쉽다.
- TODO:
  data table / action panel / modal state 분리
  room-related API access 경로를 feature boundary로 모으기
- Proof map:
  rooms route smoke 1건
  room action path 1건

## Page 10. Dashboard Stats Page

- Scope: stats 화면의 dashboard composition과 heavy child component를 분리한다.
- Owning seam: `admin-dashboard/frontend/src/features/stats`
- Boundary:
  `admin-dashboard/frontend/src/components/StatsTab.tsx`
  `admin-dashboard/frontend/src/components/dashboard/ChannelStatsTable.tsx`
- Contract risk: stats page가 chart/table/lazy load orchestration을 한곳에서 다 가지면 렌더링 회귀가 커진다.
- TODO:
  page shell과 data sections 분리
  chart/table fetch boundary를 feature folder로 이동
  stats contract가 generated 쪽으로 옮길 수 있는지 점검
- Proof map:
  stats route smoke 1건
  chart/table render path 1건

## Page 11. Dashboard Streams Page

- Scope: streams 화면의 query/filter/rendering을 분리한다.
- Owning seam: `admin-dashboard/frontend/src/features/streams`
- Boundary:
  `admin-dashboard/frontend/src/components/StreamsTab.tsx`
- Contract risk: stream list filters와 rendering이 한 파일에 모이면 live/upcoming 조건 회귀를 찾기 어렵다.
- TODO:
  filter state 분리
  list presenter 분리
  stream API boundary 정리
- Proof map:
  streams route smoke 1건
  filter transition path 1건

## Page 12. Dashboard Milestones Page

- Scope: milestones 화면은 이미 feature api를 쓰고 있으므로 giant-tab 회귀를 막는 선에서 shell/presenter/data section만 분리한다.
- Owning seam: `admin-dashboard/frontend/src/features/milestones`
- Boundary:
  `admin-dashboard/frontend/src/components/MilestonesTab.tsx`
  `admin-dashboard/frontend/src/features/milestones/api.ts`
- Contract risk: 현재는 상대적으로 깔끔하지만 stats/near/achieved 3개 query와 card/list rendering이 한 파일에 모여 있어 page growth가 다시 시작되기 쉽다.
- TODO:
  top summary cards와 list sections 분리
  query orchestration을 feature hook 또는 page shell로 이동
  milestone contract가 holo backend ownership과 어떻게 맞물리는지 점검
- Proof map:
  milestones route smoke 1건
  stats/near/achieved render path 1건

## Page 13. Dashboard Settings Page

- Scope: settings 화면을 실제 운영 설정 제어면으로 보고 alarm advance minutes, scraper tuning, docker control의 boundary를 명시한다.
- Owning seam: `admin-dashboard/frontend/src/features/settings`
- Boundary:
  `admin-dashboard/frontend/src/components/SettingsTab.tsx`
  `admin-dashboard/frontend/src/components/settings/SettingsForm`
  `admin-dashboard/backend/src/routes.rs`
- Contract risk: 설정 페이지가 SSR bootstrap과 form submit만 얇게 묶고 있어, Page 1과 Page 3에서 늘어나는 설정 항목이 합류할 때 contract drift가 생길 수 있다.
- TODO:
  alarm advance minutes가 실제 runtime 반영 결과까지 보여주는지 확인
  scraper cadence/worker count를 노출할 경우 settings contract에 포함할지 결정
  SSR initial data와 mutation success state를 동일 contract source로 정리
- Proof map:
  settings SSR load path 1건
  settings mutation success path 1건

## Page 14. Shared Cleanup Page

- Scope: duplicate helper/wrapper/provider와 `hololive-shared` monolith pressure를 별도 리팩터 축으로 관리한다.
- Owning seam: runtime app bootstrap packages and `hololive/hololive-shared`
- Boundary:
  `hololive/hololive-stream-ingester/internal/app/runtime_helpers.go`
  `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_proxy_toggle.go`
  `hololive/hololive-stream-ingester/internal/app/providers/infra_resources.go`
  `hololive/hololive-kakao-bot-go/internal/app/providers/infra_resources.go`
  `hololive/hololive-stream-ingester/internal/app/providers/youtube.go`
  `hololive/hololive-kakao-bot-go/internal/app/providers/youtube.go`
- Contract risk: P0가 닫히기 전에 대규모 dedupe를 섞으면 reviewable scope가 무너진다.
- TODO:
  `applyScraperProxyToggle` duplicate를 단일 helper로 올릴지 판단
  trivial `Close()` wrappers 공통화 또는 제거 기준 정하기
  `ProvideInfraResources` / `ProvideYouTubeStack` dedupe 우선순위 정하기
  `hololive-shared`에서 runtime orchestration을 밖으로 내보낼 후보 패키지 인벤토리 작성
- Proof map:
  duplicate body inventory 문서화
  smallest-safe refactor slice 정의

## Notes

- 이 TODO는 리뷰 문서를 실행 가능한 면으로 쪼갠 문서다. 구현 순서는 `Recommended Order`를 기본으로 하되, 한 번에 하나의 page만 plan/implementation으로 넘기는 것을 권장한다.
- Page 1, Page 2, Page 3은 운영 영향이 직접적이라 함께 움직일 수 있지만, Page 14와 섞지 않는 편이 안전하다.
- Page 6 이후 admin-dashboard 작업은 backend contract ownership 결정을 먼저 닫아야 frontend generated/manual 혼재를 줄일 수 있다.
