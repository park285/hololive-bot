# YouTube Three-Provider Convergence — Task 4 Implementation Handoff

이 문서 전체를 다음 구현 세션의 prompt로 사용하십시오.

> **선행 상태 — 2026-08-14:** Task 1–3과 Task 4 진입 차단 이슈(H1/F1, M1, M2, M4, M3)는 로컬 구현·targeted validation을 완료했습니다. 새 세션에서 그 수정을 다시 하지 마십시오. 이 문서의 완료 목표는 Task 4뿐입니다.

## 역할과 단일 완료 목표

`/home/kapu/work/iris-stack/hololive-bot`에서 canonical contract의 다음 범위만 구현하십시오.

> `Task 4 — API YouTube plane과 Community ownership 이전`: dedicated pool과 lifecycle-owned worker supervisor를 `hololive-api`에 두고, Community observation claim/reconcile/finalize를 API plane이 소유하게 하며, producer Community consumer wiring을 삭제한다.

Task 5–8 reconciler, Task 9 producer 모듈 삭제·AP collector fleet, Task 10 full gate는 구현하지 마십시오. live end due-finalizer와 retention/replay worker의 **lifecycle hook와 config**는 plane에 둘 수 있지만, Task 6/8이 소유하는 reducer/삭제 정책 본문은 구현하지 마십시오.

## 시작 workflow

1. `$executing-plans`로 이 handoff만 실행합니다.
2. `$nfr-guardrails`와 `$tech-debt-guardrails`가 있으면 적용합니다. 없으면 canonical contract Sections 14, 20–21을 같은 blocking rule로 직접 적용합니다.
3. 완료 주장 직전에 `$verification-before-completion`을 적용합니다.
4. 첫 commentary에서 dirty worktree 보존, Task 4 범위와 첫 discovery pass를 알립니다.
5. subagent는 사용자가 해당 실행 세션에서 명시적으로 승인하지 않으면 사용하지 않습니다.

## Worktree identity

- Absolute worktree: `/home/kapu/work/iris-stack/hololive-bot`
- Expected branch: `feat/schedule-api-and-community-observation`
- Task 1–3 commit: `fe9e7028a`
- Task 4 진입 수정은 이 handoff와 같은 커밋에 포함됩니다.
- Repository type: Go monorepo with central and AP runtimes
- Migration `144`–`155`와 `youtube-collector`는 production에 적용되지 않았습니다.

시작 직후 다음을 확인하십시오.

```bash
cd /home/kapu/work/iris-stack/hololive-bot
pwd
git branch --show-current
git rev-parse --short=9 HEAD
git status --short
```

worktree 또는 branch가 다르면 수정하지 말고 차이를 보고하십시오. HEAD가 달라졌으면 현재 code와 diff를 우선 조사하십시오. 관련 없는 사용자 WIP를 restore, reset, checkout 또는 삭제하지 마십시오.

## 반드시 읽을 문서

다음 순서로 읽고, 충돌하면 `AGENTS.md`와 canonical contract를 우선하십시오.

1. `/home/kapu/work/iris-stack/hololive-bot/AGENTS.md`
2. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md`
   - Sections 4.4, 7, 13.1 Community, 14, 16–18 Task 4, 19–23
3. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-producer-convergence-status-20260814.md`
4. `/home/kapu/work/iris-stack/hololive-bot/docs/current/contracts/source-observation-canonical-json-v1.md`
5. 이 문서의 「확정된 선행 상태」

Canonical source of truth는 두 번째 문서의 v2.1 contract입니다. 규범을 구현 편의를 위해 완화하지 마십시오.

## 확정된 선행 상태 (다시 구현하지 말 것)

다음 계약은 이미 코드와 테스트로 고정되어 있습니다.

- provider/kind typed envelope, Canonical JSON v1, immutable `source_observations` + mutable queue
- generation target projection, PostgreSQL job lease, set-based publish fence
- Holodex/Official/YouTube.js collector adapters와 typed registry
- helper env allowlist (M3): `youtubejs.helperProcessEnv`가 `POSTGRES_PASSWORD`/`API_SECRET_KEY`/`HOLODEX_API_KEY`를 상속하지 않음
- `viewer_sample` subject는 video ID만 (H1/F1)
  - `BuildPolicyTargets`는 operational channel에 `viewer_sample`을 심지 않음
  - `PolicyInputs.ViewerVideoIDs` / `InputReader.ViewerVideoIDs`
  - `UC`+22자 이상을 viewer video로 넘기면 `ErrInvalidProjection`
  - `LiveHeadViewerVideoIDs`가 `youtube_live_reconciliation_heads`의 `UPCOMING`/`LIVE`만 반환
  - Holodex는 channel roster를 viewer subject로 쓰면 sample을 만들지 않음
  - YouTube.js `ViewerRunner`는 channel subject를 video ID로 fetch하지 않음
- `MaxPublishBatchSize`와 `MaxCheckpointCount`는 1024 (M1). Hololive 규모 361건은 한 batch
- `holodex_global`은 kind별 poll interval이 달라도 `MIN(poll_interval)`로 acquire (live 2m / stats 6h 허용)
- collector `syncCandidates`는 GLOBAL job을 먼저 enqueue하고 queue full에서 다른 runner를 중단하지 않음 (M4)
- collector Compose는 `HOLODEX_API_KEY`/`HOLODEX_API_KEY_1` interpolation (M2). 값이 비면 collect-time fail-closed

아직 열려 있는 것 (Task 4가 닫음):

- canonical consumer가 producer `CommunityObservationConsumer`에 있음
- API YouTube plane lifecycle/pool/worker가 없음
- target projection refresh를 production runtime이 기동하지 않음
- `youtube_live_reconciliation_heads`는 schema만 있고 Task 6 전까지 비어 있을 수 있음. 빈 viewer roster는 정상 fail-closed

## 수정 전 bounded discovery

한 번의 `rg`/`rg --files` batch로 다음을 확인하고, 사실이 달라졌을 때만 계획을 조정하십시오.

- `hololive-api` bot/admin/llm plane bootstrap과 process-wide postgres pool/readiness
- `sourceobservation.Consumer` / `ClaimBatch` / `Finalize` / `BatchCanonicalWriter`
- producer `CommunityObservationConsumer`와 `producerruntime` wiring
- `targetprojection.PolicyBuilder`, `Refresher`, `LiveHeadViewerVideoIDs`
- `hololive_runtime` vs `hololive_scraper` grant (API는 runtime role)
- Community notification/outbox writer의 현재 owner

현재 확인된 사실:

- `hololive-api/internal/planes/youtube/`에는 `targetprojection`만 있음
- producer가 Community claim/canonical persist를 2s마다 수행함
- producer는 Community **fetch** poller를 등록하지 않음
- `InputReader`에 `ViewerVideoIDs`가 있으나 API runtime reader는 없음

## Primary write boundary

```text
hololive/hololive-api/internal/planes/youtube/**
hololive/hololive-shared/pkg/config/settings/config.go
hololive/hololive-shared/pkg/config/settings/config_validation.go
hololive/hololive-shared/pkg/config/settings/runtime_role_validation.go
hololive/hololive-shared/pkg/config/settings/*youtube*plane*
hololive/hololive-shared/pkg/service/youtube/sourceobservation/community_consumer.go
hololive/hololive-shared/pkg/service/youtube/sourceobservation/community_canonical_writer.go
hololive/hololive-shared/pkg/service/youtube/community/**
```

## Conditional producer-removal boundary

Community consume ownership을 API로 옮긴 뒤에만, live path가 남지 않게 다음을 삭제하거나 연결 해제할 수 있습니다.

```text
hololive/hololive-youtube-producer/internal/runtime/pollers/community_observation_consumer.go
hololive/hololive-youtube-producer/internal/runtime/pollers/community_observation_consumer_test.go
hololive/hololive-youtube-producer/internal/runtime/producerruntime/bootstrap_youtube_producer_youtube.go
hololive/hololive-youtube-producer/internal/runtime/producerruntime/youtube_producer_runtime_lifecycle.go
hololive/hololive-youtube-producer/internal/runtime/producerruntime/youtube_producer_runtime_runner.go
hololive/hololive-youtube-producer/internal/runtime/producerruntime/community_observation_consumer_test.go
```

제한:

- producer videos/shorts/live/stats poller와 binary는 Task 9 소유이므로 삭제하지 않습니다.
- Community **fetch**는 이미 collector입니다. 다시 producer에 넣지 않습니다.
- dual consumer(API+producer)를 남긴 채 Task 4 complete를 주장하지 않습니다.
- fake success, test-only production bypass, authority/legacy mode를 만들지 않습니다.

검증 성공 뒤 진척 증거만 다음 문서에 갱신할 수 있습니다.

```text
docs/current/architecture/youtube-producer-convergence-status-20260814.md
docs/current/handoffs/2026-08-14-youtube-convergence-task4-implementation-handoff.md
```

## 구현 계약

### 1. YouTube plane config

contract §14.1 `YouTubePlaneConfig`와 validation을 settings에 두고 non-default override가 runtime에 도달하는 test를 작성합니다. 권장 default:

```text
POSTGRES_POOL_MIN_CONNS = 1
POSTGRES_POOL_MAX_CONNS = 4
CONSUMER_WORKERS         = 2
DB_OPERATION_CONCURRENCY = 3
CLAIM_BATCH_SIZE         = 4
CLAIM_LEASE              = 60s
TRANSACTION_TIMEOUT      = 10s
```

- `DBOperationConcurrency < PostgresPoolMaxConns` (health/shutdown용 1 connection 예약)
- `ConsumerWorkers <= DBOperationConcurrency`
- `ClaimLease >= ClaimBatchSize*TransactionTimeout + 10s`
- existing bot/admin/llm pool과 합산한 process-wide connection budget을 deployment/compose test로 검증
- YouTube pool 때문에 PostgreSQL max connections를 근거 없이 올리지 않음

### 2. Dedicated pool과 lifecycle

API app owner가 §14.2 순서로 plane을 start/stop합니다.

Startup: config validate → dedicated pgxpool+ping → target projection state 확인 → worker supervisor → plane 공용 DB semaphore → claim loop 시작 → health component 등록.

Shutdown: 새 claim 중단 → worker cancel → in-flight item을 transaction timeout 안에 join → 미처리 claim explicit retry/release → loop join → pool close.

detached goroutine, leaked ticker/rows/tx를 남기지 않습니다. API는 `POSTGRES_USER=hololive_runtime`만 사용합니다. scraper role을 API consume에 쓰지 않습니다.

### 3. Target projection runtime

- `PolicyBuilder` + `Refresher`를 API plane lifecycle에서 주기적으로 기동
- `InputReader.NotificationChannelIDs` / `OperationalChannelIDs`는 기존 notification/operational roster를 읽음
- `InputReader.ViewerVideoIDs`는 `LiveHeadViewerVideoIDs`를 사용. 빈 목록은 정상 (viewer job 없음, Holodex viewer 미발행, 나머지 kind는 유지)
- input read 실패는 last-good generation 보존 (`ErrInputRead`)
- `viewer_sample`을 channel ID로 심지 않음

### 4. Community consume ownership

- `sourceobservation.Consumer`를 API worker가 claim/finalize
- detection/effective time에 `time.Now()`를 쓰지 않고 observation clocks를 전달
- per-item failure isolation: 한 invalid item은 DLQ/bounded error, 같은 batch의 이후 item은 처리
- Community canonical write, notification intent, application audit, queue completion은 한 transaction
- transaction failure는 네 가지를 모두 rollback
- replay가 notification intent를 중복 생성하지 않음
- 기존 Community artifact identity와 outbox ID 의미를 보존

### 5. Health

- invalid config, dedicated pool 실패, supervisor 비정상 종료, 필수 schema 불일치는 process readiness 실패
- pending age, freshness, last-good stale, DLQ/collision 증가는 `degraded`이지 readiness 자동 실패가 아님

### 6. Producer 경로 삭제

- `CommunityObservationConsumer` production wiring 삭제
- producer package에 live Community consume import/registration이 없어야 함
- 문서/runbook의 producer persist 문구는 Task 4 완료 갱신에서 맞춤

## 필수 regression

```text
API shutdown stops claim and joins workers
transaction failure rolls back canonical, notification, application, processed state
one invalid item DLQ while later batch item processes
replay does not duplicate notification intent
same evidence permutations yield same Community artifacts
producer package has no live Community consume registration/import
PolicyBuilder still plants viewer_sample only on video IDs
LiveHeadViewerVideoIDs returns UPCOMING/LIVE only
empty viewer roster does not fail projection refresh
YouTube plane invalid pool/worker/lease budget fails closed at startup
process-wide postgres connections stay within existing capacity
```

skipped test, test-only production bypass, dual consumer를 추가하지 마십시오.

## Validation

```bash
go test -count=1 \
  ./hololive/hololive-api/internal/planes/youtube/... \
  ./hololive/hololive-shared/pkg/service/youtube/community \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-shared/pkg/config/settings \
  ./hololive/hololive-youtube-producer/internal/runtime/pollers \
  ./hololive/hololive-youtube-producer/internal/runtime/producerruntime

go test -race -count=1 \
  ./hololive/hololive-api/internal/planes/youtube/... \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation

rg -n "CommunityObservationConsumer|community_observation_consumer" \
  hololive/hololive-youtube-producer/internal/runtime

git diff --check -- \
  hololive/hololive-api/internal/planes/youtube \
  hololive/hololive-shared/pkg/config/settings \
  hololive/hololive-shared/pkg/service/youtube \
  hololive/hololive-youtube-producer/internal/runtime \
  docs/current/architecture/youtube-producer-convergence-status-20260814.md \
  docs/current/handoffs/2026-08-14-youtube-convergence-task4-implementation-handoff.md
```

첫 `rg`는 완료 상태에서 production-reachable consumer type/wiring이 없어야 합니다. test-only 파일의 삭제 검증 match는 문맥을 직접 읽고 허용할 수 있습니다.

이번 Task 4에서 full pre-push, NilAway, repository-wide perf는 실행하지 마십시오. Task 10 소유입니다. targeted test/race가 드러낸 root cause를 suppression으로 우회하지 마십시오.

## NFR blocking rules

- queue, worker, claim batch, lease, transaction timeout, pool, semaphore는 bounded
- claim/finalize/projection/replay/retention DB op는 `DBOperationConcurrency` semaphore를 통과
- shutdown이 in-flight transaction과 worker를 join
- raw payload, token, credential, full description을 log하지 않음
- scraper role로 canonical consume를 하지 않음
- `time.Now()`로 Community detection/effective time을 만들지 않음
- 새 dependency, lockfile/toolchain upgrade, lint/race/NilAway suppression 금지

## Tech-debt blocking rules

- producer+API dual consumer
- authority/legacy/shadow mode
- untyped payload map
- temporary duplicate canonical writer
- `viewer_sample` channel-ID plant 재도입
- TODO/FIXME, skipped test, test-only production bypass
- broad gate exclusion

의도적으로 남기는 항목은 owner, trigger, exit를 이 handoff 완료 업데이트에 기록하십시오.

## 명시적 비범위

- Task 5 videos/shorts reducer
- Task 6 live/viewer/schedule canonical reducer와 due-finalizer 본문
- Task 7 stats/profile/photo canonical selection
- Task 8 retention 정책 본문
- Task 9 producer binary/Compose/systemd 삭제와 AP collector fleet
- production migration apply, deploy, restart, secret sync, live DB write

## Stop conditions

- migration `144`–`155`가 production/shared non-test DB에 적용됐다는 새 증거
- Community consume correctness에 Task 5–8 reducer가 필수
- 새 runtime dependency 또는 public contract 변경이 필요
- dual consumer 없이는 cutover할 수 없고 contract가 선택 기준을 주지 않음
- 사용자 WIP와 write boundary가 충돌

## 완료 판정

다음을 모두 만족해야 `Task 4 complete`라고 보고할 수 있습니다.

1. YouTube plane config/validation과 dedicated pool이 API lifecycle에 있음
2. claim worker가 shutdown에서 join되고 새 claim이 멈춤
3. Community canonical/notification/application/queue가 한 transaction이며 실패 시 전부 rollback
4. per-item isolation과 replay 비중복이 test로 고정됨
5. target projection refresh가 API에서 기동되고 viewer roster는 video ID/`LiveHeadViewerVideoIDs`
6. producer production path에 Community consume가 없음
7. canonical Task 4 test, producer compile/test, targeted race, scoped `git diff --check` 통과
8. Task 5–10과 production operation을 수행하지 않음

## 승인 경계

승인됨: write boundary 안 local edit, 비파괴 local test.

승인되지 않음: production/shared migration apply, deploy/restart, live DB write, secret raw access, push/PR, 새 production dependency, destructive git.

## 최종 보고 형식

한국어 합쇼체로 결론부터:

- Outcome: `complete` 또는 concrete blocker
- plane/lifecycle/consume 이전 요약
- 실제 실행한 validation command와 결과
- producer consume 삭제 상태
- NFR/tech-debt와 남은 Task 5 진입 조건
- production migration/deploy 미수행 확인
