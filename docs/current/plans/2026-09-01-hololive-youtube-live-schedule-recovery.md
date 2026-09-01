# Hololive YouTube live schedule recovery and parser ownership

## Execution capsule
**Goal:** YouTube 대기방이 존재하면 예정 시각을 수집해 5분 전 알림을 복구하고, 방송 후 catch-up은 사전 대기방을 실제로 관측하지 못한 경우에만 남깁니다.
**Context:** 2026-09-01 20:00 방송은 YouTube.js가 18:55부터 UPCOMING을 33회 보았지만 모두 scheduled_at이 없었고, 20:01:08 첫 LIVE 뒤 20:02 catch-up이 발생했습니다. Holodex 예정 시각은 20:04:50에야 도착했습니다.
**Constraints:** 표시 문자열을 시각으로 파싱하거나 새 fallback·재시도·provider 우선순위를 만들지 않습니다. premiere 알림 소유권, live-start admission, source-observation generation과 DB schema를 바꾸지 않습니다.
**Evidence:** pinned youtubei.js 18.0.0의 LockupView에는 예정 시각이 없지만 raw player microformat에는 startTimestamp가 있고, 기존 content path도 상세 영상 조회에서 같은 값을 사용합니다. 현재 활성 예정 방송은 62개 채널 100개, 채널당 최대 5개입니다.
**Success:** 누락 시각이 raw player 조회로 채워지고, 끝까지 불완전한 UPCOMING은 관측·checkpoint를 남기지 않으며, 저장된 예정 시각이 기존 checker의 5분 target을 통과합니다. 처음부터 LIVE인 방송과 premiere 동작은 유지됩니다.
**Output:** collector 로컬 raw metadata adapter와 fixture, YouTube.js/Go fail-closed 경계, 요청 상한, focused 검증, 운영 데이터 판정 및 전체 AP fleet rollout 절차를 남깁니다.

## Plan controls

**Decisions:** `DEC-20260901-hololive-youtube-live-metadata-adapter-ownership` (governing), `DEC-20260814-hololive-youtube-three-provider-convergence-v2` (constraint), `DEC-20260826-hololive-youtubejs-bounded-read-retry` (constraint), `DEC-20260829-hololive-live-start-evidence-admission` (constraint), `DEC-20260830-hololive-premiere-content-owned-notifications` (constraint)

- 기준 source revision은 `hololive-bot@751f614bd63e0fe24267e139b61c7c3edbe5814d`입니다. 실행 직전에 branch, HEAD, worktree, active Git writer와 lock을 다시 확인하고 기준이 달라졌으면 연결 경로와 fixture를 재검토합니다.
- 깨진 불변조건은 `YouTube.js live_snapshot의 status=UPCOMING 행은 collector 보강을 마친 시점에 scheduled_at이 있어야 한다`입니다. provider-neutral generation 1/2의 optional field 계약은 바꾸지 않고 YouTube.js adapter에서만 강화합니다.
- `youtubei.js@18.0.0` 설치 면적은 약 23MB, 2,092개 파일, JavaScript/TypeScript 약 175,891줄입니다. 직접 사용 면적은 Innertube 생성, channel/video/community 조회, 일부 node type과 transport뿐이므로 전체 source fork는 이 결함의 최소 소유 경계가 아닙니다.
- 공식 upstream은 2025-07-18부터 2026-08-13까지 10개 release를 냈습니다. 방치된 dependency로 간주하지 않고 pinned 기반층으로 유지하되, 알람에 필요한 raw player field 해석과 회귀 fixture는 로컬에서 소유합니다.
- 상세 조회는 목록 자체에 예정 시각이 없는 UPCOMING video ID에만 순차적으로 한 번 수행합니다. 한 channel collection에서 최대 32개를 허용하고 초과, identity mismatch, malformed timestamp 또는 미복구 예정 시각은 partial success가 아니라 `parser_drift`로 종결합니다.
- `/youtubei/v1/player`의 transient network 및 HTTP 500/502/503/504에는 기존 총 2회 transport 시도만 적용합니다. 429, parser/protocol failure, 잘못된 응답에는 추가 retry나 alternate provider 호출을 만들지 않습니다.
- 기존 행이 목록에서 기계가독 시각을 제공하면 상세 조회를 하지 않습니다. 처음 관측된 상태가 LIVE이면 예정 시각을 발명하지 않고 기존 live catch-up 경로를 유지합니다.
- 수동 DB backfill은 수행하지 않습니다. 수정된 collector의 새 observation이 canonical row를 자연스럽게 보강하며, 별도 repair가 필요하다는 증거가 생기면 exact 대상과 rollback을 제시한 새 승인을 받습니다.
- 로컬 구현과 검증은 현재 요청 범위입니다. commit, push, artifact publication, AP fleet deploy/restart, production DB 변경과 rollback은 각각 exact target과 revision에 대한 별도 승인이 필요합니다.
- Fallback delta: none.

## Ordered tasks

### T01 — Add the collector-owned raw player metadata adapter

Create `hololive-bot/hololive/hololive-youtube-collector/youtubejs/src/live-metadata.mjs` and its focused test. Add sanitized raw fixtures under `youtubejs/testdata/` for a current `LockupView` UPCOMING row and a `/player` response containing `videoDetails` plus `microformat.playerMicroformatRenderer.liveBroadcastDetails.startTimestamp`.

The adapter calls the pinned Innertube instance's public `actions.execute("/player", { videoId, racyCheckOk: true, contentCheckOk: true, parse: false })` boundary. It validates response success, exact requested `videoDetails.videoId`, boolean live/upcoming fields when present, and RFC3339 timestamp shape before returning a small local record. It must never expose or accept localized secondary text as a timestamp. HTTP status-bearing failures preserve the existing RPC classification; raw schema, identity and time failures use `parser_drift`.

The pure raw parser is independent from YouTube.js node classes so future upstream reverse-engineering changes can be selectively ported against our fixture without copying its session, player deciphering, browse parser or client matrix.

### T02 — Enrich missing UPCOMING schedules and share the critical parser

Modify `youtubejs/src/fetch-channel.mjs` so the list mapper remains a pure first pass, then sequentially enriches only sessions whose status is `UPCOMING` and whose `scheduled_at` is absent. Preserve a list-provided timestamp without a player call. Deduplicate candidate video IDs, reject more than 32 candidates before issuing detail requests, and require every UPCOMING item to have `scheduled_at` after enrichment.

Modify `youtubejs/src/fetch-content.mjs` to use the same raw live metadata adapter instead of `innertube.getInfo()` for upcoming premiere classification. Preserve the existing contract: `isUpcoming=true` with `isLiveContent=false` is premiere-owned content, while live content is not marked premiere. This removes a second product-critical interpretation path and reduces each existing premiere detail probe from player+next to the single raw player request without claiming a performance result until measured.

Extend `fetch-channel.test.mjs`, `fetch-content.test.mjs`, relevant declarations, and the response/error tests to cover list timestamp preservation, LockupView enrichment, multiple candidate ordering, exact 32/33 request boundary, player/list transition race, identity mismatch, malformed/missing timestamp, cancellation, and premiere separation. A valid but unresolved UPCOMING must return the typed 422 `parser_drift` RPC result rather than a 200 response with an omitted time.

### T03 — Reject incomplete YouTube.js live output before publication

Modify `hololive/hololive-youtube-collector/internal/runtime/youtubejscollector/validation.go` and `channel.go` so a live-enabled runner rejects any YouTube.js `UPCOMING` session whose `ScheduledAt` is nil or zero before building an envelope. Keep this provider adapter check out of the shared generation 1/2 decoder so stored historical observations and Holodex's separate nullable fields remain replayable.

Update `internal/runtime/youtubejscollector/testdata/channel.json` and `runner_test.go` to prove both directions: an enriched schedule reaches `contract.LiveSessionV1.ScheduledAt`, while an incomplete UPCOMING produces `collecterr.ParserDrift`, a zero `CollectResult`, no observation, and no checkpoint. Metadata-only channel jobs must retain their current output contract; if the shared helper call fails, they may fail explicitly but must not publish stale or fabricated live data.

Use the Modern Go Guidelines immediately before modifying the Go files. Do not add a compatibility field, generation alias, consumer fallback or alarm-side join.

### T04 — Verify the existing persistence and alarm path

Add one focused assertion to the existing source-observation live persistence test that a YouTube.js UPCOMING schedule from the generation-2 payload becomes `youtube_live_sessions.scheduled_start_time` and survives the later LIVE sparse update. Reuse the existing alarm-worker tests that show a persisted ordinary stream crossing the 5-minute target emits `minutes_until=5`, that recent upcoming delivery suppresses live catch-up, and that confirmed premiere is excluded.

No production Go behavior in `hololive-api` or `hololive-alarm-worker` should change unless the focused test exposes a distinct defect in that connected path. If it does, stop and amend this plan and DEC before widening runtime ownership.

### T05 — Record the dependency boundary and operational signals

Update `docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md` and `docs/current/runbooks/youtube-collector.md` with the local raw field owner, 32-probe cap, terminal failure behavior, existing retry ceiling, and the rule that a first-seen LIVE remains eligible for normal catch-up. Keep `package.json` and `package-lock.json` pinned to `youtubei.js@18.0.0`; this work neither vendors upstream source nor removes the dependency.

Document the upgrade gate: inspect upstream release notes and only the locally used surface, run raw fixtures plus all helper tests/typecheck, and port a relevant parser finding into the local adapter when needed. Reconsider a full fork only on the DEC review trigger. Do not add a background updater, runtime remote-code fetch, compatibility shim or new dependency.

### T06 — Validate locally and prepare a guarded fleet rollout

Run V01-V04 from clean final inputs and inspect the final diff for unexpected dependency, schema, protocol generation, retry or alarm changes. Record the observed detail-call count in Node tests: zero for list-complete data, one per unique missing UPCOMING video, and no more than 32 per channel call.

After separate deployment approval, build once and deploy the exact collector artifact, bundled helper and lockfile to fleet members a/b/c/d through `hololive-bot-ops`; mixed old/new fleet state is not completion. Verify each member's exact revision and readiness, then perform V05 with the guarded read-only PostgreSQL route and bounded logs. Do not mutate rows or synthesize a Kakao notification for smoke testing.

## Acceptance criteria

### AC01 — Waiting-room schedules are machine-readable and complete

A current LockupView UPCOMING row without a list timestamp obtains `scheduled_at` from the exact matching raw player response. A list timestamp is preserved, all times are UTC RFC3339, and localized display text is never parsed.

### AC02 — Invalid schedule state cannot advance collection

More than 32 missing-schedule candidates, player identity mismatch, malformed/missing start timestamp, cancellation, or unresolved UPCOMING returns the existing typed terminal class and produces no live observation or checkpoint. No complete-empty, partial-success, cached default or invented time is emitted.

### AC03 — Request amplification remains bounded

Each unique missing UPCOMING video causes at most one player request per channel collection before the existing transport retry layer. List-complete, LIVE, ENDED and CANCELLED rows cause no schedule enrichment call. Total candidates are capped at 32 and calls are sequential and context-cancellable.

### AC04 — Premiere and real catch-up semantics are preserved

The shared raw adapter keeps premiere classification content-owned, confirmed premiere stays out of live upcoming/catch-up, and a broadcast first seen directly as LIVE remains eligible for normal catch-up. No new provider fallback, precedence or live-start evidence rule appears.

### AC05 — The ownership boundary is narrow and upgradeable

Product-critical raw live/premiere metadata interpretation and fixtures are local. `youtubei.js@18.0.0` remains pinned only as the Innertube session/request/browse/parser substrate, its full source is not copied, and upgrade validation fails when the used surface or raw fixtures drift.

### AC06 — The existing five-minute path receives the repaired data

The enriched timestamp reaches `youtube_live_sessions.scheduled_start_time`, later sparse LIVE evidence does not erase it, and the existing checker selects `minutes_until=5` at the target crossing. After rollout, bounded real observations contain no fresh YouTube.js UPCOMING row without `scheduled_at`; completion of the user-visible outcome additionally requires at least one real 5-minute target delivery sample.

## Validation

### V01 — Node metadata and helper contract

From `hololive-bot/hololive/hololive-youtube-collector/youtubejs`:

```bash
node --test --test-concurrency=1 \
  src/live-metadata.test.mjs \
  src/fetch-channel.test.mjs \
  src/fetch-content.test.mjs \
  src/rpc-validation.test.mjs
npm test
npm run typecheck
```

All commands must exit 0. Boundary tests must assert exact upstream call counts, the 32/33 cap, 422 `parser_drift`, cancellation propagation, and no localized-text parse.

### V02 — Go collector publication boundary

From `hololive-bot`:

```bash
go test ./hololive/hololive-youtube-collector/internal/runtime/youtubejs \
  ./hololive/hololive-youtube-collector/internal/runtime/youtubejscollector
```

The focused incomplete-UPCOMING case must return `collecterr.ParserDrift` with a zero result. The valid fixture must contain the exact UTC scheduled time in its live snapshot envelope.

### V03 — Persistence and alarm target path

From `hololive-bot`:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  -run 'TestLiveConsumer.*(Schedule|Metadata)' -count=1
go test ./hololive/hololive-alarm-worker/internal/service/alarm/checker/checking \
  -run 'TestYouTubeChecker.*(Upcoming|Catchup|Premiere)' -count=1
```

Both commands must exit 0 and cover schedule persistence, five-minute selection, catch-up suppression after an upcoming delivery, and premiere exclusion. If the regex selects no test, treat the validation as failed and use the exact discovered test names rather than broadening to a full repository suite.

### V04 — Dependency, scope and documentation boundary

From the helper directory and then `hololive-bot`:

```bash
npm ls --all --omit=dev
git diff --check
git diff -- package.json package-lock.json
```

The dependency tree must resolve `youtubei.js@18.0.0` with no extraneous package. The lockfile diff must be empty, and the final source diff must contain no copied upstream tree, source-observation generation/schema change, new retry/fallback, or alarm-worker production change.

### V05 — Approved fleet and data observation

After an approved a/b/c/d rollout, use the `hololive-bot-ops` completion checks for all four fleet members. Then use `stack-platform-ops` against `hololive-osaka` / `holo-postgres` / `hololive` with `default_transaction_read_only=on`, a 5-second statement timeout, rollout-time predicate and bounded rows to verify:

```sql
SELECT count(*) AS invalid_upcoming
FROM source_observations AS observation
CROSS JOIN LATERAL jsonb_array_elements(observation.payload->'sessions') AS session
WHERE observation.provider = 'youtubejs'
  AND observation.observation_kind = 'live_snapshot'
  AND observation.received_at >= $ROLLOUT_AT
  AND session->>'status' = 'UPCOMING'
  AND NULLIF(session->>'scheduled_at', '') IS NULL;
```

`invalid_upcoming` must be zero, and at least one real post-rollout UPCOMING row must exist before marking the production parser outcome observed. For the next eligible ordinary broadcast, verify a matching `alarm_dispatch_events` row has `category='target'`, `(payload->'notification'->>'minutes_until')::int=5`, and at least one corresponding `alarm_dispatch_deliveries.status='sent'`. A later `live_catchup` for already-sent rooms is a failure. No eligible real sample means V05 remains inconclusive rather than passed.

## Failure behavior and approval boundaries

- Raw player lookup failure, cap excess, identity mismatch, malformed time or unresolved UPCOMING ends the current collection with typed failure. It does not publish an incomplete observation, advance its checkpoint, call another provider, parse display text or turn unknown into success.
- The existing one-retry transport contract is unchanged. A second player attempt is possible only for the already accepted read-only transient classes; every other failure is terminal for the collection.
- A first-seen LIVE row is not relabeled UPCOMING and receives no invented scheduled time. It remains the valid unannounced/early-start catch-up case.
- Local implementation does not authorize commit, push, dependency upgrade, artifact publication, fleet deploy/restart, DB repair or rollback. Each requires the repository, exact revision/artifact, target fleet member and impact to be named before execution.
- If the rollout produces 429/timeout growth, repeated `parser_drift`, any fresh incomplete UPCOMING, missing 5-minute target delivery or duplicate catch-up, stop completion. Rollback is the exact prior collector revision across binary, bundled helper and lockfile; DB rollback is none.

## Handoff

Execute this plan with `executing-plans`. Complete T01-T05 and V01-V04 locally first. T06/V05 starts only after exact fleet deployment approval and remains open until a real post-rollout upcoming plus five-minute delivery sample exists. Report upstream request amplification and `Fallback delta: none` in the final handoff.
