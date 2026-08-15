# YouTube producer convergence status — 2026-08-14

## 목적

이 문서는 `youtube-producer`의 수집 책임을 `youtube-collector`로, canonical 처리 책임을 `hololive-api`로 옮기는 작업의 현재 상태를 사실 기준으로 기록한다. 목표 구조와 실행 순서는 `youtube-three-provider-convergence-contract-v2-20260814.md`가 함께 소유한다.

production migration, deploy, restart, data change는 이 문서의 범위가 아니다.

## 저장소 스냅샷

- Branch: `feat/schedule-api-and-community-observation`
- Task 8 HEAD: `3008d3988`. Task 9 local retirement는 이 commit 위의 uncommitted worktree다.
- 주요 선행 commit:
  - `4c6faafcc feat(schedule): replace official HTML scraper with API-only source`
  - `e073d3896 Document community source observation vertical slice`
  - `0d68ad2b0 Add source observation PostgreSQL infrastructure`
  - `5248898cd Add source observation contract and repository`
- 2026-08-14 read-only evidence 기준 production에는 migration `144`와 `youtube-collector`가 적용되지 않았다. 따라서 rollout 전 SSOT인 manifest의 `144`–`174` 전체를 순서대로 적용해야 한다.
- 2026-08-14 read-only 관측 당시 `hololive-api`, `alarm-worker`, producer `a/b/c/d`는 healthy였고 중앙 `youtube-collector`는 배포되지 않았다. 이 관측은 production 상태를 가리키며 현재 worktree identity가 아니다.
- 2026-08-14 통합 contract v2.1의 Task 1–8은 로컬 구현과 targeted validation을 완료했다. Task 9 local retirement도 같은 branch worktree에서 완료했다. 2026-08-15 `PRE_PUSH_MODE=full`과 retention policy 승인은 완료했다. `build-all.sh --no-bump` 절체는 reviewed commit 없이 `revision=unknown` artifact를 만들 수 없어 보류했고 production apply도 수행하지 않았다.
- worktree에서 standalone `hololive-youtube-producer` 모듈/binary/compose/systemd/current runbook은 삭제됐다. AP a/b/c/d identity는 `youtube-collector`다. `members.photo`는 hololive-api admin PhotoSync, YouTube channel photos는 API `channel_photo` reducer다.
- source observation identity는 Go `encoding/json` 관례 대신 `source-observation-canonical-json-v1` safe-integer JCS subset과 language-neutral fixture로 고정했다. collector runtime은 계속 Go다.

## 현재 진척

| 작업 영역 | 현재 증거 | 판정 | 목표까지 남은 일 |
|---|---|---|---|
| Official Schedule API-only | commit `4c6faafcc`, collector `official_schedule` adapter와 fixture | collector observation 로컬 검증 완료 | production collector fleet apply |
| Observation 저장 기반 | migration `144`–`155`, contract/repository v2.1, Canonical JSON v1 fixture와 replay·golden test | Task 1 및 identity 후속 로컬 검증 완료 | production migration apply |
| Target projection/job fence | generation rebuild, DB lease owner, set-based publish fence와 stale-holder regression. `viewer_sample`은 video ID roster | Task 2 로컬 검증 완료 | production apply |
| Community domain processor | `pkg/service/youtube/community` | API YouTube plane consumer | production apply |
| 독립 collector module | typed registry, `YouTubeCollector` config, Holodex/Official/YouTube.js adapters, Community registry 흡수. Compose가 `HOLODEX_API_KEY`를 전달 | Task 3 로컬 검증 완료 | AP fleet production 배포 |
| Community observation consume | API YouTube plane claim/finalize | Task 4 로컬 검증 완료 | production apply |
| Videos/Shorts | API content reducer가 `video_list`/`shorts_list`를 consume | Task 5 로컬 검증 완료 | production apply |
| Live/Viewer/Schedule | API live/viewer/schedule reducer와 due-finalizer | Task 6 로컬 검증 완료 | production apply |
| Stats/Profile/Photo | API stats/profile/`channel_photo` reducer; `members.photo`는 admin PhotoSync | Task 7 로컬 검증 완료 | production apply |
| Retention/replay | API YouTube plane | Task 8 로컬 검증 완료 | production apply |
| Collector AP 병렬화 | PostgreSQL subject lease와 duplicate-publish fence | runtime foundation 구현. production 배포 미수행 | 동일 collector binary를 AP fleet에 배포하고 Task 3 adapter job을 등록 |
| Collector fleet identity | `youtube-collector` module/binary/compose/systemd/docs, AP a/b/c/d. standalone producer 모듈 삭제 | Task 9 로컬 완료, Task 10 full pre-push 통과. production apply 금지 | clean reviewed revision의 build gate와 승인된 deploy |

Community부터 photo까지의 canonical consume는 API YouTube plane이다. standalone producer module은 worktree에서 삭제됐다. Task 9 local retirement, Task 10 full pre-push와 retention policy 승인은 완료됐다. clean reviewed revision의 build gate와 collector fleet apply가 남아 있다.

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
- target projection staging verification은 PostgreSQL locale과 무관하게 Go canonical byte-order와 일치하도록 text identity를 `COLLATE "C"`로 읽는다.

### 목표 설계와 충돌하는 내용

1. local current docs는 AP collector fleet을 규정한다. production은 2026-08-14 관측 기준 여전히 producer `a/b/c/d`다.
2. Community부터 photo까지 canonical consume는 API YouTube plane이다. `members.photo`는 admin PhotoSync, YouTube channel photos는 `channel_photo` reducer다. 이 경계는 worktree에서 맞춰졌다.
3. collector Holodex/Official adapter는 로컬 observation publisher다. production 수집은 아직 승인된 collector fleet deploy 전이다.
4. standalone producer 모듈과 Community direct-persist runtime은 worktree에서 삭제됐다.
5. retention/replay worker는 API YouTube plane이 소유한다. Task 9 local module retirement는 완료됐다.
6. `scripts/deploy/ap-rsync-files.txt`는 삭제된 authority/community 경로를 제거하고 현재 `youtube-collector` `go list -deps` 누락 파일을 보강했다. `scripts/deploy/check-ap-rsync-manifest.sh`와 scoped `git diff --check`는 통과한다.

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
  ./hololive/hololive-youtube-collector/internal/runtime/pollers \
  ./hololive/hololive-youtube-collector/internal/runtime/producerruntime

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
  ./hololive/hololive-youtube-collector/internal/runtime/pollers
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
  ./hololive/hololive-youtube-collector/internal/runtime/pollers \
  ./hololive/hololive-youtube-collector/internal/runtime/producerruntime \
  ./hololive/hololive-youtube-collector/internal/runtime/readiness

go test -race -count=1 \
  ./hololive/hololive-api/internal/planes/youtube/... \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation

bash scripts/ci/check-postgres-capacity.sh
```

API shutdown join, invalid-item isolation, transaction rollback, replay 비중복, viewer-empty projection, YouTube plane budget fail-closed, compose capacity `allocated=55 reserve=5`가 통과했다. producer production path에 Community consume type/wiring이 없다. 이는 Task 4의 로컬 invariant만 검증하며 Task 5–8 reducer, Task 9 producer 제거, production apply를 증명하지 않는다.

현재 알려진 hygiene failure는 없다. `scripts/deploy/ap-rsync-files.txt`의 삭제 경로·누락 의존성·EOF blank line은 보강했고 `scripts/deploy/check-ap-rsync-manifest.sh`가 통과한다.

Task 9 최종 상태에서 다음 targeted check가 통과해야 한다.

```text
gofmt -l hololive/hololive-shared hololive/hololive-api hololive/hololive-youtube-collector internal/workspace
GOTMPDIR=/home/kapu/work/iris-stack/.tmp go test ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-youtube-collector/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-dbtest ./internal/workspace -count=1
git diff --check
```

standalone producer 모듈은 worktree에 없다. compose/systemd/docs identity는 `youtube-collector` AP a/b/c/d다. 이는 Task 9 local retirement만 검증하며 Task 10 full pre-push와 production apply를 증명하지 않는다.

## 다음 승인 경계

Task 9 local retirement와 Task 10 full pre-push는 완료됐다. 2026-08-15 retention policy는 다음 초기값으로 승인했다. production apply는 여전히 별도 승인 경계다.

| 대상 | 보존 기간 |
|---|---:|
| processed queue / DLQ | 7일 / 90일 |
| collision / replay audit | 365일 / 365일 |
| retired projection | 30일 |
| community / videos / shorts | 각 30일 |
| channel stats / profile / photo | 각 180일 |
| live snapshot / viewer sample | 각 30일 |
| schedule snapshot | 90일 |

90채널과 기본 cadence를 모두 성공 관측으로 계산한 상한은 약 247,896 observations/day다. row, queue, TOAST와 index를 합쳐 observation당 4–8 KiB로 잡으면 steady-state storage estimate는 약 31–63 GB다. 2026-08-15 중앙 호스트의 가용 공간은 약 95 GB였고 기존 producer 시계열 3종은 약 117 MB였다. `BatchSize=1000`, `Interval=300s`는 processed queue 기준 시간당 최대 12,000행을 정리해 계산상 유입 상한 약 10,329행/시간을 상회한다. 실제 배포 후에는 kind별 row rate, relation size와 retention backlog를 관측해 이 추정치를 교정한다.

1. `viewer_sample` target은 live/upcoming video ID만 심는다. channel operational roster는 live/stats/profile/photo만 소유한다.
2. Holodex `live_snapshot` subject는 channel ID, 같은 `holodex_live` batch의 `viewer_sample`은 video ID이며 fence도 그 공간을 쓴다. Schedule과 metadata는 각 cadence별 global lease로 분리된다.
3. `MaxPublishBatchSize`/`MaxCheckpointCount`는 1024다. Hololive 규모(90채널×4 kind+schedule)는 한 응답 한 batch로 들어간다. 초과는 여전히 부분 drop 없이 fail closed다.
4. collector Compose는 `HOLODEX_API_KEY`/`HOLODEX_API_KEY_1`을 전달한다. 값이 비면 collect-time fail-closed다.
5. production migration manifest `144`–`174`, collector AP fleet deploy, restart, live data 변경은 별도 승인 전까지 수행하지 않는다.

통합 contract v2.1의 `PRE_PUSH_MODE=full`은 2026-08-15 통과했다. retention policy도 같은 날 승인했으며 clean reviewed commit의 `build-all.sh --no-bump`와 production apply는 별도 승인 경계로 남는다.
