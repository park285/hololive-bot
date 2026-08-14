# YouTube producer convergence status — 2026-08-14

## 목적

이 문서는 `youtube-producer`의 수집 책임을 `youtube-collector`로, canonical 처리 책임을 `hololive-api`로 옮기는 작업의 현재 상태를 사실 기준으로 기록한다. 목표 구조와 실행 순서는 `youtube-three-provider-convergence-contract-v2-20260814.md`가 함께 소유한다.

production migration, deploy, restart, data change는 이 문서의 범위가 아니다.

## 저장소 스냅샷

- Branch: `feat/schedule-api-and-community-observation`
- Task 5 baseline: `bebd0b9bf`; Task 6 live/viewer/schedule reducer는 이 baseline 위의 local worktree
- 주요 선행 commit:
  - `4c6faafcc feat(schedule): replace official HTML scraper with API-only source`
  - `e073d3896 Document community source observation vertical slice`
  - `0d68ad2b0 Add source observation PostgreSQL infrastructure`
  - `5248898cd Add source observation contract and repository`
- 2026-08-14 read-only evidence 기준 production에는 migration `144`와 `youtube-collector`가 적용되지 않았다. 따라서 rollout 전 manifest `144`–`161` 전체를 순서대로 적용해야 한다.
- 현재 worktree는 Task 4 config/shutdown/readiness hardening을 포함하며 publish·deploy 판단은 별도 gate 소유다.
- 2026-08-14 read-only 관측 당시 `hololive-api`, `alarm-worker`, producer `a/b/c/d`는 healthy였고 중앙 `youtube-collector`는 배포되지 않았다.
- 2026-08-14 통합 contract v2.1의 Task 1–6은 로컬 구현과 targeted validation을 완료했다. Task 7–8 reconciler와 Task 9 producer 제거는 시작하지 않았다.
- source observation identity는 Go `encoding/json` 관례 대신 `source-observation-canonical-json-v1` safe-integer JCS subset과 language-neutral fixture로 고정했다. collector runtime은 계속 Go다.

## 현재 진척

| 작업 영역 | 현재 증거 | 판정 | 목표까지 남은 일 |
|---|---|---|---|
| Official Schedule API-only | commit `4c6faafcc`, collector `official_schedule` adapter와 fixture | collector observation 로컬 검증 완료 | producer/API 내부 Official 호출 제거는 Task 9 |
| Observation 저장 기반 | migration `144`–`155`, contract/repository v2.1, Canonical JSON v1 fixture와 replay·golden test | Task 1 및 identity 후속 로컬 검증 완료 | Task 4 API가 collector observation을 consume |
| Target projection/job fence | generation rebuild, DB lease owner, set-based publish fence와 stale-holder regression. `viewer_sample`은 video ID roster | Task 2 로컬 검증 완료 | Task 4 API lifecycle에서 `LiveHeadViewerVideoIDs`로 viewer roster를 채운다 |
| Community domain processor | `pkg/service/youtube/community` WIP | 로컬 구현 | consumer ownership을 API로 이동 |
| 독립 collector module | typed registry, `YouTubeCollector` config, Holodex/Official/YouTube.js adapters, Community registry 흡수. Compose가 `HOLODEX_API_KEY`를 전달 | Task 3 로컬 검증 완료 | AP fleet 배포, Task 4 ownership 이전 |
| Community observation consume | API YouTube plane claim/finalize, producer production wiring 삭제 | Task 4 로컬 검증 완료 | Task 5–8 reducer와 production apply |
| Videos/Shorts | API content reducer가 `video_list`/`shorts_list`를 consume; producer videos/shorts/backfill 등록 삭제 | Task 5 로컬 검증 완료 | live/stats/profile/photo 전환과 producer 모듈 삭제 |
| Live/Viewer/Schedule | API live/viewer/schedule reducer와 due-finalizer; producer live 등록 삭제 | Task 6 로컬 검증 완료 | stats/profile/photo 전환과 producer 모듈 삭제 |
| Profile/Photo | YouTube.js `youtubejs_channel` adapter 로컬; Holodex live API는 profile 미발행; producer sync 잔존 | collector 일부 로컬 완료 | variant 보존 및 API projection 규칙 구현 |
| Collector AP 병렬화 | PostgreSQL subject lease와 duplicate-publish fence, producer `a/b/c/d` 배포 | runtime foundation 구현·배포 미수행 | 동일 collector binary를 AP fleet에 배포하고 Task 3 adapter job을 등록 |
| Producer 제거 | module, binary, Compose, systemd, scripts, docs 존재 | 미착수 | 모든 kind 전환 후 같은 branch에서 완전 삭제 |

Community vertical slice와 Task 3 collector adapters는 로컬에서 구현되었지만 최종 구조 기준으로는 중간 단계다. `youtube-producer` 제거, API canonical ownership, AP collector fleet 배포는 완료되지 않았다.

## 문서와 구현의 정합도

### 현재 구현과 맞는 내용

- provider와 observation kind가 분리된 typed payload·coverage contract가 있다.
- scheduled slot과 viewer sample window identity, canonical payload/scope/evidence hash를 검증한다.
- canonical JSON은 UTF-16 property ordering, JCS string escaping, safe-integer normalization과 invalid/duplicate/depth bounds를 문서와 공용 fixture로 검증한다.
- immutable `source_observations`와 mutable `source_observation_queue`가 분리되어 있다.
- collision audit-only fail-closed publish, generation fence, bounded claim/finalize와 least-privilege scraper role이 있다.
- migration `144`는 table·constraint·seed·grant를 소유하고 required concurrent index는 runner 규칙에 맞춰 `145`–`155`의 single-statement migration으로 분리되어 있다.
- target projection은 input failure의 last-good 보존, same-hash heartbeat, reason-only 교체, changed/valid-empty generation 전환을 원자적으로 처리한다.
- collector production scheduler는 typed job registry와 하나의 `PublishBatch`를 사용하며 Community-only `PollWithLease` path는 제거됐다.
- Holodex/Official/YouTube.js collector adapter가 fixture-backed observation을 발행한다. Official `isLive`는 schedule evidence only다.
- publish fence는 job class/subject bundle, epoch, scheduled slot, current generation과 batch의 모든 enabled target을 검증하고 target query 수를 batch 크기와 무관하게 고정한다.

### 목표 설계와 충돌하는 내용

1. 현재 collector 문서는 중앙 Community singleton을 규정하지만 목표는 AP collector fleet이다.
2. stats/profile/photo canonical write는 아직 producer가 소유한다. Community/videos/shorts/live/viewer/schedule consume는 API YouTube plane이다.
3. collector Holodex/Official adapter는 로컬 observation publisher지만 production 수집과 canonical write는 아직 producer/API 내부 provider 호출에 남아 있다.
4. producer의 Community direct-persist 코드가 registration 제거 뒤에도 남아 있어 최종 owner 경계가 깨끗하지 않다.
5. retention/replay worker 본문은 Task 8 소유라 plane config만 있고 loop는 시작하지 않는다. live-end due-finalizer는 Task 6에서 API plane이 소유한다.
6. `scripts/deploy/ap-rsync-files.txt`는 삭제된 authority/community 경로를 제거하고 현재 youtube-producer `go list -deps` 누락 파일을 보강했다. `scripts/deploy/check-ap-rsync-manifest.sh`와 scoped `git diff --check`는 통과한다.

## 확정된 목표 전제

- 개인 프로젝트이며 아직 `main` merge 전이므로 compatibility rollout 대신 branch 내 완전 전환을 수행한다.
- rollback 단위는 runtime authority mode가 아니라 merge 전 branch/commit이다.
- evidence provider는 `holodex`, `youtubejs`, `hololive_official` 세 개다.
- provider 사이에 primary/fallback/authority 우선순위를 두지 않는다.
- API만 canonical domain authority다. provider 표시는 provenance, freshness, health, conflict 분석에만 쓴다.
- Holodex와 Official은 각각 fleet-wide lease를 가진 singleton collection job으로 실행한다.
- YouTube.js는 모든 AP collector가 channel/kind 단위 lease로 분산 수집한다.
- 동일 collector instance가 global job과 YouTube.js shard를 함께 맡을 수 있다.
- collector는 Go로 유지한다. 8개 YouTube.js kind가 실제 활성화되고 helper RPC pressure가 동일 workload 측정에서 material bottleneck일 때만 TypeScript collector를 검토하며, 그 전에 Go/TypeScript fixture conformance가 필요하다.
- 최종 egress는 계속 `alarm-worker`만 소유한다.
- 최종 branch에는 `legacy/shadow/authoritative` mode와 standalone `youtube-producer`를 남기지 않는다.

## 검증 증거와 한계

복원된 Community WIP에서 다음 targeted check가 통과했다.

```text
go test ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/community \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-youtube-collector/internal/runtime/... \
  ./hololive/hololive-youtube-producer/internal/runtime/pollers \
  ./hololive/hololive-youtube-producer/internal/runtime/producerruntime

(cd hololive/hololive-youtube-collector/youtubejs && npm test)
```

Go package와 Node test 17개가 통과했다. 이는 Community 중간 구현만 검증하며 목표 아키텍처, migration replay, producer 제거, production readiness를 증명하지 않는다.

Task 1 최종 상태에서 다음 targeted check가 통과했다.

```text
go test ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-dbtest/...

bash scripts/architecture/check-migration-manifest.sh

go test ./hololive/hololive-youtube-collector/internal/runtime/communitycollector \
  ./hololive/hololive-youtube-producer/internal/runtime/pollers
```

Migration `144`–`155` replay twice, schema golden, role grant, contract/repository regression과 direct dependent compile/test가 통과했다. 이는 Task 1 foundation만 검증하며 Task 2의 target projection/lease scheduler, Task 3 provider adapter, Task 4 API YouTube plane ownership이나 production readiness는 증명하지 않는다.

Task 2 최종 상태에서 다음 targeted check가 통과했다.

```text
go test -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/joblease \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime \
  ./hololive/hololive-api/internal/planes/youtube/targetprojection \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation

go test -count=1 \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-dbtest/... \
  ./hololive/hololive-youtube-collector/internal/runtime/communitycollector

go test -race -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/joblease \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime \
  ./hololive/hololive-api/internal/planes/youtube/targetprojection \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation
```

Projection failure/same-hash/reason-only/changed/valid-empty, lease epoch/takeover/coalescing/Defer/Release/renew loss, global singleton/subject distribution과 stale-holder atomic rollback regression이 통과했다. `MaxPublishBatchSize` target fence는 3개 query로 고정됐다. Projection 전환 뒤 미참조된 alarm/member channel resolver도 제거해 collector target source를 하나로 정리했다. 이는 Task 2의 로컬 invariant만 검증하며 Task 4 API lifecycle, AP deployment와 production readiness는 증명하지 않는다.

Canonical JSON v1 후속 상태에서 다음 targeted check가 통과했다.

```text
go test -count=1 ./hololive/hololive-shared/pkg/contracts/sourceobservation

go test -count=1 \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-youtube-collector/internal/runtime/communitycollector \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime
```

fixture의 canonical bytes/SHA-256, rejection과 idempotence, repository 및 Community collector/runtime 회귀가 통과했다. 이는 Go 구현을 검증하며 아직 존재하지 않는 TypeScript collector나 helper RPC 성능 병목을 증명하지 않는다.

Task 3 최종 상태에서 다음 targeted check가 통과했다.

```text
go test -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/... \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-shared/pkg/config/settings

(cd hololive/hololive-youtube-collector/youtubejs && npm test)

go test -race -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/...

go test -run '^$' -bench '^BenchmarkCanonicalizeJSON1MiB$' -benchmem \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation
```

Go runtime/settings/sourceobservation package와 Node helper test 42개가 통과했다. race도 collector `internal/runtime/...`에서 통과했다. `BenchmarkCanonicalizeJSON1MiB-12`는 `25874544 ns/op`, `11532227 B/op`, `56 allocs/op`이다. 성능 개선 주장은 하지 않는다. YouTube.js process lifetime는 `youtubejs.Helper`, helper HTTP는 `youtubejs.RPC`이다. Community-only scheduler reference는 제거됐고 adapter는 producer/canonical/notification ownership을 import하지 않는다. 이는 Task 3 collector adapter만 검증하며 Task 4 API YouTube plane, producer 제거, AP deployment와 production readiness는 증명하지 않는다.

Task 4 최종 상태에서 다음 targeted check가 통과했다.

```text
go test -count=1 \
  ./hololive/hololive-api/internal/planes/youtube/... \
  ./hololive/hololive-shared/pkg/service/youtube/community \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-shared/pkg/config/settings \
  ./hololive/hololive-youtube-producer/internal/runtime/pollers \
  ./hololive/hololive-youtube-producer/internal/runtime/producerruntime \
  ./hololive/hololive-youtube-producer/internal/runtime/readiness

go test -race -count=1 \
  ./hololive/hololive-api/internal/planes/youtube/... \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation

bash scripts/ci/check-postgres-capacity.sh
```

API shutdown join, invalid-item isolation, transaction rollback, replay 비중복, viewer-empty projection, YouTube plane budget fail-closed, compose capacity `allocated=55 reserve=5`가 통과했다. producer production path에 Community consume type/wiring이 없다. 이는 Task 4의 로컬 invariant만 검증하며 Task 5–8 reducer, Task 9 producer 제거, production apply를 증명하지 않는다.

현재 알려진 hygiene failure는 없다. `scripts/deploy/ap-rsync-files.txt`의 삭제 경로·누락 의존성·EOF blank line은 보강했고 `scripts/deploy/check-ap-rsync-manifest.sh`가 통과한다.

## 다음 승인 경계

Task 4 진입 전 확인할 조건:

1. `viewer_sample` target은 live/upcoming video ID만 심는다. channel operational roster는 live/stats/profile/photo만 소유한다. Task 4는 `LiveHeadViewerVideoIDs`로 그 roster를 채운다.
2. Holodex `live_snapshot` subject는 channel ID, 같은 `holodex_global` batch의 `viewer_sample`은 video ID이며 fence도 그 공간을 쓴다.
3. `MaxPublishBatchSize`/`MaxCheckpointCount`는 1024다. Hololive 규모(90채널×4 kind+schedule)는 한 응답 한 batch로 들어간다. 초과는 여전히 부분 drop 없이 fail closed다.
4. collector Compose는 `HOLODEX_API_KEY`/`HOLODEX_API_KEY_1`을 전달한다. 값이 비면 collect-time fail-closed다.

통합 contract v2.1에 따라 로컬 구현과 비파괴 검증을 진행할 수 있다. production migration 적용, deploy, restart, live data 변경은 별도 승인 전까지 수행하지 않는다.
