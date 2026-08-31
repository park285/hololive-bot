# YouTube Three-Provider Convergence — Task 2 Implementation Handoff

> Historical implementation handoff. Do not use as a current execution contract. See `docs/current/PROJECT_MAP.md`.

> **문서 상태 — 2026-08-14 완료:** Task 2와 별도 승인된 Canonical JSON v1 identity 후속 작업은 로컬 구현·targeted validation을 완료했습니다. 아래 원문은 실행 당시의 write boundary와 판단 근거를 보존하는 historical implementation record입니다. 새 세션에서 Task 2를 다시 실행하지 말고 canonical contract와 status 문서의 현재 상태를 우선하십시오.

## 완료 업데이트

Outcome: `complete`

Task 2는 다음 소유 영역을 구현했습니다.

- `hololive/hololive-api/internal/planes/youtube/targetprojection/**`: failure last-good, same-hash heartbeat, reason-only 교체, changed/valid-empty generation 전환
- `hololive/hololive-youtube-collector/internal/runtime/joblease/**`: PostgreSQL acquisition/takeover epoch, missed-slot coalescing, renew/cancel/join, Complete/Defer/Release
- `hololive/hololive-shared/pkg/service/youtube/sourceobservation/**`: compile-time subject/bundle contract, set-based target fence와 stale-holder atomic rollback
- `hololive/hololive-youtube-collector/internal/runtime/collectorruntime/**`: Community production path의 lease-backed scheduler와 `PollWithLease` 연결, projection 전환 뒤 미참조된 alarm/member channel resolver 제거

Task 2 완료 뒤 migration `144` 적용 전 identity 규칙을 고정하는 별도 후속 범위가 승인되어 다음을 추가했습니다.

- `hololive/hololive-shared/pkg/contracts/sourceobservation/canonical_json.go`
- `hololive/hololive-shared/pkg/contracts/sourceobservation/canonical_json_test.go`
- `hololive/hololive-shared/pkg/contracts/sourceobservation/testdata/canonical_json_v1.json`
- `docs/current/contracts/source-observation-canonical-json-v1.md`

`source-observation-canonical-json-v1`은 RFC 8785/JCS 출력과 일치하는 safe-integer strict subset입니다. UTF-16 property ordering, JCS string escaping, IEEE-754 변환 전 number-token 검증, 1 MiB input/output, 128단계 nesting과 invalid Unicode/duplicate member fail-closed를 fixture로 고정했습니다. collector는 Go로 유지하며 TypeScript collector는 8개 YouTube.js kind 활성화 뒤 helper RPC pressure가 측정상 material bottleneck이고 Go/TypeScript 양쪽이 fixture를 통과할 때만 검토합니다.

production migration, deploy, restart, live data 변경, dependency 추가, commit과 remote write는 수행하지 않았습니다.

### 최종 검증 증거

2026-08-14 최종 상태에서 다음 검증이 통과했습니다.

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

`TestPublishTargetVerificationQueryCountIsConstantAtMaxBatch`는 별도 실행에서도 통과했으며 max batch fence query count를 3회로 고정합니다. Canonical JSON fixture의 6개 success case는 Go test와 Node.js reference serialization에서 같은 bytes를 만들었습니다. rejection case는 Go contract test가 duplicate name, invalid Unicode, fractional/unsafe number, IEEE-754 rounding/underflow, trailing value와 depth overflow를 거부함을 검증합니다.

### NFR gate — 2026-08-14

Task 2의 로컬 완료 판정은 유지하지만 production readiness 판정은 내리지 않습니다.

- `NFR — Reliability/concurrency`: PostgreSQL epoch fence, stale-holder atomic rollback, renew-loss cancellation/join, bounded queue/worker/retry와 targeted race가 통과했습니다. 이 범위에서 추가 gap은 발견하지 못했습니다.
- `NFR — Security/input`: strict JSON/Unicode/duplicate validation, safe-integer bound, payload/depth bound와 bounded structured log field를 확인했습니다. raw payload, credential, token 또는 provider response logging은 발견하지 못했습니다.
- `NFR — Performance/capacity`: target verification은 max batch에서도 query 3회로 고정되고 target/reason/worker/queue/payload가 bounded입니다. 다만 1 MiB canonicalization의 CPU/allocation benchmark는 아직 없어 8개 YouTube.js kind 활성화 전 동일 workload 증거가 필요합니다.
- `NFR — Observability`: bounded failure log는 있으나 canonical contract Section 16의 lease acquire/loss, queue saturation, publish/freshness metric 구현과 contract test는 아직 없습니다. Task 3/10에서 production readiness 전에 채워야 합니다.
- `Readiness — Configuration/portability`: existing scraper worker/timeout/backoff override는 runtime에 도달하고 invalid lease budget은 startup에서 fail closed합니다. collector-specific lease TTL/renew/cadence와 provider budget의 config owner는 아직 없으므로 Task 3 multi-provider runtime config에서 결정해야 합니다.
- `Readiness — Supply chain/release`: Canonical JSON 후속은 standard library만 사용하며 새 dependency를 추가하지 않았습니다. 현재 module 전체가 dirty untracked WIP라 HEAD diff만으로 Task 1/2 dependency provenance를 독립 분리할 수 없고, full pre-push·NilAway·repository-wide perf/security와 production rollout은 Task 10 및 별도 승인이 소유합니다.

### Tech-debt review — 2026-08-14

1. `RESOLVED` — obsolete `collectorruntime/channels.go`
   - `channelResolver`, `newChannelResolver`, `Resolve`, `enabledMemberChannelIDs`만 소유하던 미참조 파일을 삭제했습니다.
   - authoritative projection-backed candidate discovery만 target source로 남겼습니다.
   - repository reference scan과 canonical Task 2 race command가 삭제 후 통과했습니다.

2. `TRACK` — `collectorruntime/scheduler.go`의 local lease/budget constants
   - Artifact: `collectorNormalizationBudget`, `collectorPublishBudget`, `collectorLeaseTTL`, `collectorRenewInterval`, release jitter와 acquisition cadence가 Community scheduler local constant입니다.
   - Compromise: existing worker/timeout/backoff config와 달리 multi-provider lease budget에는 아직 단일 config owner가 없습니다.
   - Trigger/interest: Task 3에서 Holodex/Official/8개 YouTube.js kind의 timeout·request budget이 달라지면 code edit 없이 조정할 수 없고, 기존 poll-timeout override가 fixed TTL budget을 넘으면 collector startup이 fail closed합니다.
   - Next: Task 3 collector config contract가 provider/shared budget ownership을 정하고 non-default override runtime test를 추가합니다. 현재 fail-closed validation과 Community-only 범위 때문에 deferral은 bounded합니다.

3. `ACCEPT` — Community-specific lease scheduler
   - Artifact: `leaseScheduler`는 현재 `communitycollector.PollerName`, `currentCommunityContractGeneration`과 `PollWithLease`에 직접 결합됩니다.
   - Compromise: generic PostgreSQL lease repository 위에 Task 2가 요구한 최소 production integration만 연결했습니다.
   - Trigger/interest: Task 3 provider adapter를 같은 conditional branch로 누적하면 scheduler가 provider/kind 조건문과 중복 failure handling을 소유하게 됩니다.
   - Exit condition: Task 3에서 typed job runner/registry로 일반화하고 Community compatibility dual path를 남기지 않습니다. Task 2의 명시적 scope 제한과 다음 task owner가 있어 현재 비용은 수용합니다.

별도 debt ledger, suppression, compatibility wrapper나 enforcement hook은 만들지 않았습니다.

---

이하 내용은 완료 전 구현 세션의 원본 prompt입니다.

## 역할과 목표

`/home/kapu/work/iris-stack/hololive-bot`에서 YouTube Three-Provider Convergence의 두 번째 foundation task를 구현하십시오.

이번 세션의 유일한 완료 목표는 다음과 같습니다.

> `Task 2 — Target projection과 PostgreSQL job fence`를 구현하고, generation-based target rebuild, monotonically fenced job lease와 publish-time fence가 concurrency·rollback regression을 통과하게 한다.

Task 3의 three-provider adapter·pagination semantics, Task 4의 API YouTube plane/Community canonical ownership 이전, Task 5 이후 kind reconciler, Task 9의 producer 제거와 deployment 전환은 구현하지 마십시오.

## 시작 시 적용할 workflow

1. `$executing-plans`를 사용해 canonical contract의 Task 2만 실행하십시오.
2. 완료를 주장하기 직전에 `$verification-before-completion`을 사용하십시오.
3. `$nfr-guardrails`, `$tech-debt-guardrails`가 다음 세션에 설치되어 있으면 함께 적용하십시오. 사용할 수 없다면 canonical contract의 Section 20과 Section 21을 같은 blocking rule로 직접 적용하십시오.
4. 사용자에게 첫 commentary로 현재 목표, dirty worktree 보존 원칙과 첫 점검 단계를 짧게 보고하십시오.
5. subagent는 사용자가 새 세션에서 명시적으로 승인하지 않는 한 사용하지 마십시오.

## Worktree identity

- Absolute worktree: `/home/kapu/work/iris-stack/hololive-bot`
- Expected branch: `feat/schedule-api-and-community-observation`
- Handoff 작성 시 HEAD: `5248898cd`
- Repository type: Go monorepo with central and AP runtimes
- Migration `144`–`155`와 `youtube-collector`는 production에 적용되지 않았다.
- Task 1은 현재 dirty worktree에서 구현·리뷰·targeted validation을 완료했다. 이 handoff는 해당 미커밋 상태를 선행 foundation으로 사용한다.

세션 시작 직후 다음을 확인하십시오.

```bash
cd /home/kapu/work/iris-stack/hololive-bot
pwd
git branch --show-current
git rev-parse --short=9 HEAD
git status --short
```

branch나 worktree가 다르면 수정하지 말고 차이를 보고하십시오. HEAD가 달라졌다면 변경 내용을 먼저 조사하고 이 handoff의 사실을 현재 코드보다 우선하지 마십시오.

현재 worktree에는 migration, source observation, collector, producer, deployment와 documentation을 포함한 대규모 미커밋 WIP가 있습니다. 모두 사용자 소유 변경입니다. 관련 없는 수정, 삭제, restore, reset, checkout을 하지 마십시오.

## 반드시 읽을 문서

다음 순서로 읽고, 충돌하면 `AGENTS.md`와 canonical contract를 우선하십시오.

1. `/home/kapu/work/iris-stack/hololive-bot/AGENTS.md`
2. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md`
   - 특히 Sections 1–3, 7–10, 15–18의 Task 2, 19–23
3. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-collector-convergence-status-20260814.md`
4. `docs/history/youtube-convergence/handoffs/2026-08-14-youtube-convergence-task1-implementation-handoff.md`
   - Task 1의 이전 실행 지침과 boundary 기록이며 Task 2의 규범을 확장하지 않는다.
5. `docs/history/architecture/youtube-collector-observation-outbox-community-vertical-slice-20260813.md`
   - 목표가 아니라 폐기 예정인 intermediate WIP 기록이다.

Canonical source of truth는 두 번째 문서의 v2.1 contract입니다. handoff 요약이나 현재 WIP가 contract와 다르면 contract를 따르십시오.

## Task 1 확정 선행 상태

Task 1 final state에서 다음이 구현되어 있습니다.

- typed provider/kind envelope, strict JSON/Unicode validation과 canonical payload/scope/evidence hash
- `scheduled_for` snapshot identity와 viewer sample window identity
- immutable `source_observations`와 mutable `source_observation_queue`
- generation 1 contract seed, target projection/job lease schema, checkpoint와 audit schema
- collision batch audit-only fail-closed 처리
- current publish generation과 compile-time consumer support의 분리
- checkpoint와 observation의 semantic one-to-one binding
- queue terminal operation의 commit-time lease expiry 검증
- migration replay가 증가한 schema/generation을 되돌리지 않는 seed semantics
- actual table/sequence ACL test와 `hololive_scraper`의 pre-existing canonical 권한 회수

Task 1 final state에서 다음 command가 통과했습니다.

```bash
go test -count=1 \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-dbtest/...

bash scripts/architecture/check-migration-manifest.sh

go test -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/communitycollector \
  ./hololive/hololive-youtube-collector/internal/runtime/pollers

go test -race -count=1 \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation
```

Task 2 변경은 이 baseline을 회귀시키면 안 됩니다.

## 선행 상태 보고

수정 전에 한 번의 bounded `rg`/`rg --files` discovery pass로 다음을 확인하고 간단히 보고한 뒤 구현하십시오.

- `youtube_collection_projection_generations`, targets, reasons와 job lease schema/ACL/index
- `sourceobservation.PublishBatch`의 fence, target, job completion query 순서
- `InitialJobContracts`의 provider/kind emission vocabulary와 subject/bundle 표현 한계
- collector runtime의 current shared scheduler, channel resolver와 Community `Poll`/`PollWithLease` 호출 관계
- target projection authoritative input owner와 기존 repository/query surface
- Task 2 소유 package 존재 여부와 direct imports
- Task 1 tests 중 stale generation, target disable, job completion과 rollback fixture

현재 확인된 사실은 다음과 같습니다.

- `hololive-api/internal/planes/youtube/targetprojection` package는 아직 없다.
- `hololive-youtube-collector/internal/runtime/joblease` package는 아직 없다.
- `collectorruntime`은 기존 shared scheduler와 in-process channel resolver를 사용한다.
- `communitycollector.Poll`은 lease proof가 없으면 fail closed하고, `PollWithLease`는 현재 test에서만 직접 호출된다.
- `sourceobservation`은 lease row, current projection, compile-time emission과 각 observation target을 확인하지만 Task 2가 소유할 subject/bundle fence semantics와 acquisition runtime은 아직 없다.
- Task 1 repository의 per-observation fence query는 bounded batch 안에서 선형 DB round trip을 수행한다. Task 2가 fence를 재작성할 때 이 amplification을 더 키우지 말고 set-based 검증 또는 측정 가능한 bounded query shape로 고정해야 한다.

현재 코드가 이 요약과 다르면 현재 코드와 canonical contract를 근거로 판단하십시오.

## Primary write boundary

Canonical Task 2 production code와 test는 다음 경계 안에서만 수정하십시오.

```text
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-api/internal/planes/youtube/targetprojection/**
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-youtube-collector/internal/runtime/joblease/**
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-shared/pkg/service/youtube/sourceobservation/**
```

검증 성공 뒤 진척 증거만 다음 문서에 갱신할 수 있습니다.

```text
/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-collector-convergence-status-20260814.md
```

Canonical contract는 구현 진행표가 아닙니다. 구현 편의를 위해 규범을 완화하거나 임의 변경하지 마십시오. contract 자체의 오류가 발견되면 code로 우회하지 말고 file/section 근거와 최소 수정안을 보고하십시오.

Migration `144`–`155`, schema golden, manifest와 Task 1 contract vocabulary는 Task 2 write boundary가 아닙니다. 현재 schema로 Task 2 correctness를 구현할 수 없다면 migration을 임의 변경하지 말고 grounded blocker로 보고하십시오.

## Conditional runtime-integration boundary

Task 2의 canonical validation은 `collectorruntime`을 포함하고, 현재 production-reachable scheduler는 lease proof를 전달하지 않습니다. 다음 파일에는 lease-backed scheduler path를 연결하는 데 필요한 최소 integration change만 허용됩니다.

```text
hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler.go
hololive/hololive-youtube-collector/internal/runtime/collectorruntime/runtime.go
hololive/hololive-youtube-collector/internal/runtime/collectorruntime/infrastructure.go
hololive/hololive-youtube-collector/internal/runtime/collectorruntime/runtime_test.go
hololive/hololive-youtube-collector/internal/runtime/communitycollector/poller.go
hololive/hololive-youtube-collector/internal/runtime/communitycollector/poller_test.go
```

이 조건부 경계에는 다음 제한을 모두 적용하십시오.

- lease acquisition/renewal owner와 fetch context cancellation을 production path에 연결하는 변경만 허용한다.
- fake lease, in-memory owner fallback, hard-coded always-success fence나 test-only production bypass를 만들지 않는다.
- 기존 shared scheduler를 억지로 compatibility wrapper로 유지하지 않는다. 실제 lease-backed path에서 사용되지 않으면 Task 2 범위 안에서 최소 제거한다.
- `communitycollector.Poll` hard-fail이 production scheduler에서 계속 호출되는 상태로 Task 2 complete를 주장하지 않는다.
- Community fetch result의 pagination/exhaustion 추론을 변경하지 않는다. 이는 Task 3 adapter 계약이다.
- Holodex/Official/YouTube.js provider adapter, parser, canonical writer, notification path를 추가하지 않는다.
- config/settings, deployment, Compose, systemd 또는 API plane wiring이 필요하면 이 경계를 확장하지 말고 blocker를 보고한다.
- legacy scheduler adaptation과 lease-owned scheduler replacement가 materially different behavior를 만들고 contract가 선택 기준을 제공하지 않으면 임의 결정하지 말고 중단한다.

이 경계 밖의 producer, API plane app/runtime, provider client, config, deployment, CI와 migration은 수정하지 마십시오.

## 구현 계약

### Generation-based target projection

- API policy input을 collector-facing `TargetSpec`과 diagnostic `TargetReason`으로 분리한다.
- target source mapping을 유지한다.
  - notification target: `community_page`, `video_list`, `shorts_list`
  - operational roster: `live_snapshot`, `viewer_sample`, `channel_stats`, `channel_profile`, `channel_photo`
  - fixed global target: `schedule_snapshot`, `subject_key=global:hololive-schedule`
- authoritative input read가 하나라도 실패하면 current generation과 last-good targets를 그대로 보존한다.
- `TargetSpec`을 `(subject_key, observation_kind)` 기준으로 trim/validate/deterministic sort한다. 완전히 동일한 duplicate만 dedup하고 같은 key의 scheduling field가 충돌하면 fail closed한다.
- projection hash에는 subject, observation kind, priority, poll interval과 enabled만 포함한다.
- `valid_until` heartbeat와 reason tuple은 collector-facing projection hash에 넣지 않는다.
- same hash + same row count refresh는 generation을 회전시키지 않고 generation/target validity만 연장한다.
- reason-only change는 같은 generation의 diagnostic reason만 transactionally 교체한다.
- changed content는 `STAGING` insert, target/reason bulk insert와 count/hash 확인 뒤 old `CURRENT -> RETIRED`, new `STAGING -> CURRENT`를 한 transaction에서 수행한다.
- 성공적으로 계산된 empty target set은 zero-row `CURRENT` generation으로 활성화한다.
- input failure에서 나온 empty slice와 valid empty projection을 구분한다.
- refresh 성공 뒤에만 in-memory cache/metric을 갱신한다. Task 4 API plane lifecycle을 이번 task에서 만들지 않는다.
- target/reason count, subject, reason key, priority, poll interval, validity와 input batch를 bounded validation한다.

### PostgreSQL monotonically fenced job lease

- PostgreSQL만 stale holder publish를 차단하는 correctness fence다. Valkey는 이번 task에서 필수가 아니며, 추가하더라도 optional contention optimization일 뿐이다.
- acquire 전에 DB current projection과 job이 대표하는 enabled/valid target set을 검증한다. caller의 generation만 신뢰하지 않는다.
- concurrent insert/upsert 뒤 반드시 row lock과 `fence_epoch + 1`을 거친다.
- `IDLE` acquisition만 `date_bin`과 `next_due_at` origin으로 missed slots를 최신 due boundary 하나로 coalesce한다.
- expired takeover와 `DEFERRED` retry는 기존 `scheduled_for`를 유지한다.
- `Complete`와 known collision completion은 slot을 `IDLE`로 만들고 `next_due_at = scheduled_for + poll_interval`로 전진한다.
- `Defer`는 bounded retry 시각과 error code를 기록하면서 같은 scheduled slot을 유지한다.
- shutdown `Release`는 성공으로 처리하지 않고 같은 slot을 bounded jitter 뒤 재획득할 수 있게 한다.
- `Renew`는 동일 job key, owner, epoch와 active slot에만 성공한다.
- lease TTL은 provider timeout + normalization + publish budget보다 커야 하고 renew interval은 TTL의 1/3 이하여야 한다.
- renewal loss는 in-flight fetch context를 즉시 cancel하고 run owner가 renew loop를 join한다.
- worker 수, acquisition batch, retry delay/jitter, lease TTL과 polling cadence를 bounded validation한다.
- global job은 fleet-wide active holder가 하나뿐이어야 한다. YouTube.js subject jobs는 여러 collector instance에 분산되되 한 slot의 duplicate publish를 허용하지 않는다.

### Subject/bundle emission contract

- `collection_job_kind`와 observation kind를 동일시하지 않는다.
- compile-time job contract가 provider와 허용 observation kind 집합을 계속 소유한다.
- `job_class=SUBJECT`는 channel 또는 namespaced subject bundle이고 `GLOBAL`도 stable namespaced subject를 가진다.
- 단순한 `lease.subject_key == observation.SubjectKey` predicate를 추가하지 않는다. viewer/video/global batch처럼 lease bundle subject와 emitted observation subject가 다를 수 있다.
- acquisition에서 job이 대표하는 target set을 명시적으로 확정하고, publish transaction은 lease row의 subject/bundle과 batch의 각 target membership을 검증해야 한다.
- bundle membership을 job key string parsing, provider precedence 또는 unchecked caller data로 추론하지 않는다.
- current schema와 canonical contract만으로 subject/bundle membership을 하나로 정의할 수 없다면 임시 equality/allow-all path를 만들지 말고 grounded contract blocker로 보고한다.

### Publish-time fence integration

`PublishBatch` transaction은 observation/checkpoint/collision write 전에 다음을 검증해야 합니다.

1. lease row `FOR UPDATE`
2. job key, owner instance, fence epoch, active state와 wall-clock expiry
3. scheduled slot과 projection generation
4. DB current projection generation/status/validity
5. lease job subject/bundle membership
6. compile-time provider/job-kind emission contract
7. batch의 모든 `(generation, subject_key, observation_kind)` target enabled/validity
8. current schema/contract generation

추가 invariant:

- global job도 generation-only bypass를 사용하지 않는다.
- target 검증은 set-based 또는 명시적으로 bounded된 query shape로 수행하고 max batch에서 query amplification을 regression test로 고정한다.
- 한 observation이라도 undeclared/disabled/out-of-bundle이면 observation, queue, checkpoint, collision audit와 job state를 모두 변경하지 않는다.
- stale holder가 돌아왔을 때 `ErrCollectionFenceLost`를 원인 보존 wrapping으로 반환한다.
- target generation/validity failure는 `ErrProjectionStale`, disabled/emission mismatch는 `ErrTargetDisabled`로 구분한다.
- duplicate와 known collision completion도 동일한 live fence를 통과한 holder만 job state를 전진시킨다.
- job completion query도 owner/epoch/generation/scheduled slot/active state와 commit-time lease validity를 재검증한다.
- transaction, rows와 connection lifetime은 owner가 rollback/commit/close한다.

## 반드시 고정할 regression test

최소 다음 정상·실패·경계 behavior를 test로 고정하십시오.

```text
projection input failure preserves current generation and targets
same hash/row count refresh preserves generation and extends validity
reason-only change preserves generation and atomically replaces reasons
changed projection atomically retires old and activates new
successful empty projection activates zero-row current generation
invalid/duplicate/unbounded targets fail before activation

first acquisition increments epoch and returns DB-owned proof
only one global holder is active
expired holder takeover increments epoch and preserves scheduled_for
long outage coalesces missed slots to one latest due boundary
Defer preserves scheduled_for and uses bounded retry_not_before
Release preserves scheduled_for and permits bounded reacquisition
projection valid_until expiry blocks acquisition
renew only succeeds for current owner/epoch
renew failure cancels fetch and renew loop joins

A epoch 1 acquire
A fetch blocks
lease expires
B epoch 2 acquire
B publish succeeds
A resumes and returns ErrCollectionFenceLost
A changes no observation, queue, checkpoint, collision or next_due_at state

target disable during fetch blocks every emitted observation
multi-kind subject bundle accepts declared enabled kinds
multi-kind subject bundle rejects one undeclared/disabled/out-of-bundle emission atomically
global batch verifies every emitted target without bypass
duplicate/collision completion cannot be performed by stale holder
publish target verification has bounded query count at MaxPublishBatchSize
YouTube.js subject jobs distribute without duplicate publish
```

Race regression은 clock sleep에만 의존하지 말고 transaction/lock/channel barrier를 사용해 interleaving을 통제하십시오. 실제 DB `NOW()`/`clock_timestamp()`가 correctness를 소유해야 하며 Go clock으로 fence 성공을 위조하지 마십시오.

skipped test, test-only production bypass, broad exclusion이나 always-success fake는 추가하지 마십시오.

## NFR and tech-debt blocking rules

- target count, reason count, acquisition batch, worker count, local queue, retry, TTL, renew interval, jitter와 error detail은 모두 bounded
- target normalization과 projection hash는 input order와 map iteration order에 독립적
- DB transaction, rows, connection, timer, ticker, renew loop와 worker는 owner가 close/rollback/commit/cancel/join
- renewal loss 뒤 provider request나 publish가 계속 진행되지 않음
- raw payload, token, DB credential, full provider response와 unbounded error를 log하지 않음
- `slog` field는 job key, provider, job kind, bounded subject, epoch, generation, error code와 phase를 사용
- SQL identifier runtime interpolation 금지
- Valkey/in-memory state를 PostgreSQL correctness fallback으로 사용하지 않음
- provider precedence, authority compatibility, dual path와 universal `map[string]any` payload 금지
- `PublishBatch`의 현재 bounded O(N) query amplification을 악화시키지 않고 Task 2 fence query count를 test/measurement로 고정
- unbounded goroutine, retry, target expansion과 detached renewer 금지
- 새 dependency, lockfile/toolchain upgrade, lint/race/NilAway suppression 금지
- unrelated cleanup과 기존 사용자 WIP 변경 금지

성능 개선을 주장하려면 같은 workload의 query count 또는 benchmark 전후 evidence가 있어야 합니다. full perf gate는 Task 10이 소유하지만, Task 2가 직접 변경한 fence/projection/lease query amplification은 이번 task에서 bounded evidence를 남겨야 합니다.

## 구현 순서

1. current projection schema, target source, lease/publish query와 runtime call graph의 gap을 contract 항목별 표로 정리한다.
2. target projection types, normalization/hash와 transaction algorithm을 정상·실패·empty test와 함께 구현한다.
3. job lease repository/API와 acquire/renew/complete/defer/release DB regression을 구현한다.
4. subject/bundle emission contract를 명시하고 publish fence를 set-based current target validation으로 교체한다.
5. stale-holder race와 atomic rollback tests를 구현한다.
6. conditional boundary가 필요하면 lease-backed runtime path와 cancellation/join만 최소 연결한다.
7. Task 1 regression과 direct dependent compile/test를 확인한다.
8. `gofmt` 후 consolidated diff를 검사한다.
9. 아래 validation을 final state에서 실행한다.
10. 통과한 증거만 status report에 기록한다.
11. Task 3을 시작하지 않고 결과를 보고한다.

작은 수정마다 diff를 반복 출력하지 말고 coherent batch로 편집한 뒤 consolidated diff를 검사하십시오. 파일 수정에는 `apply_patch`를 사용하십시오.

## Validation

먼저 canonical Task 2 command를 실행하십시오.

```bash
go test -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/joblease \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime \
  ./hololive/hololive-api/internal/planes/youtube/targetprojection \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation
```

Task 1 foundation과 conditional dependent를 회귀 검사하십시오.

```bash
go test -count=1 \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-dbtest/... \
  ./hololive/hololive-youtube-collector/internal/runtime/communitycollector
```

DB/concurrency 변경을 targeted race로 검증하십시오.

```bash
go test -race -count=1 \
  ./hololive/hololive-youtube-collector/internal/runtime/joblease \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime \
  ./hololive/hololive-api/internal/planes/youtube/targetprojection \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation
```

마지막으로 touched scope를 검사하십시오.

```bash
git diff --check -- \
  hololive/hololive-api/internal/planes/youtube/targetprojection \
  hololive/hololive-youtube-collector/internal/runtime/joblease \
  hololive/hololive-youtube-collector/internal/runtime/collectorruntime \
  hololive/hololive-youtube-collector/internal/runtime/communitycollector \
  hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  docs/current/architecture/youtube-collector-convergence-status-20260814.md
```

현재 전체 worktree에는 범위 밖 `scripts/deploy/ap-rsync-files.txt`의 pre-existing blank-line hygiene finding이 기록되어 있습니다. Task 2에서 이를 수정하지 말고 scoped diff check와 실제 Task 2 command 결과를 분리해 보고하십시오.

이번 Task 2 세션에서는 full pre-push, full NilAway와 repository-wide perf gate를 실행하지 마십시오. canonical contract가 Task 10 final-state blocking gate로 소유합니다. 다만 targeted test/race/query-count evidence가 드러낸 Task 2 root cause를 suppression이나 gate 완화로 우회하지 마십시오.

## 승인 및 금지 경계

현재 승인된 범위:

- local code/document edits within the write boundary
- non-destructive local tests, isolated DB transactions와 race tests
- generated test artifacts가 primary boundary 안에 있을 때의 갱신

승인되지 않은 범위:

- production/shared migration apply
- deploy, restart, live health mutation
- production/shared database write 또는 repair
- raw secret access
- commit, push, PR, issue/review write
- new production dependency
- destructive Git operation
- Task 3/4/5/9 implementation

위 작업이 필요하면 실행하지 말고 필요한 승인과 이유만 보고하십시오.

## Stop conditions

다음 중 하나면 범위를 확장하지 말고 중단·보고하십시오.

- migration `144`–`155`가 production 또는 shared non-test DB에 적용됐다는 새 증거
- branch/worktree mismatch
- 사용자 소유 WIP와 겹쳐 안전하게 보존할 수 없는 conflict
- current schema로 subject/bundle membership 또는 atomic projection/lease correctness를 표현할 수 없음
- lease-backed production path를 연결하려면 Task 3 provider adapter 또는 Task 4 API plane runtime이 필수
- canonical Task 2 ownership과 `collectorruntime` integration 사이에 둘 이상의 materially different architecture가 가능함
- config/settings, migration, public/breaking contract, dependency, secret, deploy 또는 live DB 승인이 새로 필요함
- canonical contract 자체의 상충 때문에 둘 이상의 materially different 구현이 가능함

단순히 일이 크거나 test가 실패했다는 이유로 중단하지 말고, 먼저 범위 안에서 root cause를 해결하십시오.

## 완료 판정

다음 조건을 모두 충족해야 `Task 2 complete`라고 보고할 수 있습니다.

1. generation rebuild가 failure, same-hash, reason-only, changed와 valid-empty path를 모두 원자적으로 처리함
2. acquisition이 DB current projection/target을 확인하고 every acquisition/takeover에서 epoch를 단조 증가시킴
3. missed-slot coalescing, Defer, Release와 takeover가 scheduled slot identity를 보존함
4. renew loss가 fetch context를 cancel하고 renewer/worker lifetime이 join됨
5. publish transaction이 lease subject/bundle, epoch, slot, current generation과 batch의 모든 enabled target을 검증함
6. stale holder race가 observation, queue, checkpoint, collision과 job schedule을 전혀 변경하지 못함
7. global single holder와 distributed YouTube.js subject job regression이 통과함
8. `communitycollector.Poll` hard-fail이 production-reachable scheduler path에 남지 않고 fake proof/compatibility path도 없음
9. canonical Task 2 test, Task 1 regression, targeted race와 scoped `git diff --check`가 통과함
10. Task 3/4/producer/deployment 범위를 구현하지 않았고 production operation을 수행하지 않음

하나라도 충족하지 못하면 `partial` 또는 `blocked`로 보고하고 passing으로 과장하지 마십시오.

## Task 2 이후에 남겨야 할 명시적 경계

- Task 3: provider adapter, true pagination/exhaustion metadata, Holodex/Official/YouTube.js collection implementation
- Task 4: API YouTube plane lifecycle, Community claim/reconcile/finalize ownership과 producer consumer 제거
- Task 9 또는 production rollout 전: alarm-worker가 공유 `hololive_runtime` DB credential을 쓰는 현재 identity를 별도 least-privilege role로 분리하거나 canonical contract 문구를 명확히 해야 함
- Task 10: full build, pre-push, NilAway, full race/perf/security와 production readiness

위 항목을 Task 2의 임시 compatibility path로 해결하지 마십시오.

## 최종 보고 형식

한국어 합쇼체로 결론부터 다음만 보고하십시오.

- Outcome: `complete`, `partial`, 또는 `blocked`
- 변경한 소유 영역과 핵심 projection/lease/fence invariant
- 실제 실행한 validation command와 결과
- stale-holder race와 direct runtime integration 상태
- 남은 blocker 또는 Task 3 진입 전 확인사항
- production migration/deploy 미수행 확인

파일을 언급할 때는 절대경로의 clickable link를 사용하십시오.
