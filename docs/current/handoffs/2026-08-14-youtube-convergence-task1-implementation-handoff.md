# YouTube Three-Provider Convergence — Task 1 Implementation Handoff

이 문서 전체를 다음 구현 세션의 prompt로 사용하십시오.

## 역할과 목표

`/home/kapu/work/iris-stack/hololive-bot`에서 YouTube Three-Provider Convergence의 첫 번째 foundation task를 구현하십시오.

이번 세션의 유일한 완료 목표는 다음과 같습니다.

> `Task 1 — Migration 144와 source observation contract v2.1 재작성`을 구현하고, 새 immutable evidence/queue 계약과 migration replay가 targeted validation을 통과하게 한다.

Task 2의 target projection/job lease runtime, Task 3의 provider adapter, Task 4의 API YouTube plane ownership 이전은 이번 세션에서 구현하지 마십시오. 다만 Task 1 변경으로 직접 영향을 받는 기존 Community WIP의 compile dependency는 아래 경계에 따라 확인해야 합니다.

## 시작 시 적용할 workflow

1. `$executing-plans`를 사용해 canonical contract의 Task 1만 실행하십시오.
2. 완료를 주장하기 직전에 `$verification-before-completion`을 사용하십시오.
3. `$nfr-guardrails`, `$tech-debt-guardrails`가 다음 세션에 설치되어 있으면 함께 적용하십시오. 사용할 수 없다면 canonical contract의 Section 20과 Section 21을 동일한 blocking rule로 직접 적용하십시오.
4. 사용자에게 첫 commentary로 현재 목표, dirty worktree 보존 원칙, 첫 점검 단계를 짧게 보고하십시오.
5. subagent는 사용자가 새 세션에서 명시적으로 승인하지 않는 한 사용하지 마십시오.

## Worktree identity

- Absolute worktree: `/home/kapu/work/iris-stack/hololive-bot`
- Expected branch: `feat/schedule-api-and-community-observation`
- Handoff 작성 시 HEAD: `5248898cd`
- Repository type: Go monorepo with central and AP runtimes
- Migration `144`와 `youtube-collector`는 production에 적용되지 않았다는 현재 합의에 따라 direct rewrite한다.

세션 시작 직후 다음을 확인하십시오.

```bash
cd /home/kapu/work/iris-stack/hololive-bot
pwd
git branch --show-current
git rev-parse --short=9 HEAD
git status --short
```

branch나 worktree가 다르면 수정하지 말고 차이를 보고하십시오. HEAD가 달라졌다면 변경 내용을 먼저 조사하고 이 handoff의 사실을 현재 코드보다 우선하지 마십시오.

현재 worktree에는 migration, source observation, collector, producer, deployment, documentation을 포함한 대규모 미커밋 WIP가 있습니다. 이는 사용자 소유 변경입니다. 관련 없는 수정, 삭제, restore, reset, checkout을 하지 마십시오.

## 반드시 읽을 문서

다음 순서로 읽고, 서로 충돌하면 `AGENTS.md`와 canonical contract를 우선하십시오.

1. `/home/kapu/work/iris-stack/hololive-bot/AGENTS.md`
2. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md`
   - 특히 Sections 1–6, 8–12, 18 Task 1, 19–23
3. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-producer-convergence-status-20260814.md`
4. `/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-collector-observation-outbox-community-vertical-slice-20260813.md`
   - 이것은 목표가 아니라 폐기 예정인 intermediate WIP 기록이다.
5. `/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-api/scripts/migrations/CONVENTIONS.md`

Canonical source of truth는 두 번째 문서의 v2.1 contract입니다. handoff의 요약이 contract와 다르면 contract를 따르십시오.

## 선행 상태 보고

수정 전에 다음을 read-only로 확인하고 간단히 보고하십시오.

- migration `144`의 현재 table, constraint, seed, grant와 manifest 위치
- current `sourceobservation` contract/repository의 authority, checkpoint, publish, claim, finalize dependency
- provider/observation kind 확장에 필요한 현재 public symbol과 SQL query inventory
- migration replay/schema golden test의 현재 구조
- Task 1 변경이 직접 깨뜨릴 수 있는 collector/producer import 목록

검색은 한 번의 bounded `rg`/`rg --files` discovery pass로 묶고, 그 결과의 관련 파일을 병렬로 읽으십시오. 상태 보고 뒤 바로 구현을 진행하며, 이미 승인된 로컬 구현을 다시 질문하지 마십시오.

## Primary write boundary

다음 경계 안에서만 Task 1 production code와 test를 수정하십시오.

```text
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-api/scripts/migrations/144_source_observation_outbox.sql
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-api/scripts/migrations/manifest.txt
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-dbtest/migrations_p1_test.go
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-shared/pkg/contracts/sourceobservation/**
/home/kapu/work/iris-stack/hololive-bot/hololive/hololive-shared/pkg/service/youtube/sourceobservation/**
```

검증이 성공한 뒤 진척 증거만 다음 문서에 갱신할 수 있습니다.

```text
/home/kapu/work/iris-stack/hololive-bot/docs/current/architecture/youtube-producer-convergence-status-20260814.md
```

Canonical contract는 구현 진행표가 아니므로 구현 결과 때문에 규범 내용을 완화하거나 임의 변경하지 마십시오. 계약 자체의 오류가 발견되면 code로 우회하지 말고 file/section 근거와 최소 수정안을 보고하십시오.

## Conditional compile-repair boundary

Task 1은 `AuthorityMode`, `SourceKindYouTubeCommunity`, `LoadAuthority`, `TransitionAuthority`, `source_authority_fences`를 제거합니다. 현재 Community WIP가 이 API를 import하므로 다음 파일이 직접 영향을 받을 수 있습니다.

```text
hololive/hololive-youtube-collector/internal/runtime/communitycollector/poller.go
hololive/hololive-youtube-collector/internal/runtime/communitycollector/poller_test.go
hololive/hololive-youtube-producer/internal/runtime/pollers/community_observation_consumer.go
hololive/hololive-youtube-producer/internal/runtime/pollers/community_observation_consumer_test.go
hololive/hololive-youtube-producer/internal/runtime/pollers/community_poller_observation_test.go
```

이 파일에는 다음 조건을 모두 만족하는 최소 compile repair만 허용됩니다.

- authority compatibility type, wrapper, alias, dual path를 만들지 않는다.
- provider adapter, DB job lease, target projection, API plane 또는 새로운 runtime ownership을 구현하지 않는다.
- 기존 production registration을 새 임시 bypass로 바꾸지 않는다.
- 단순 signature repair로 해결되지 않고 Task 2 또는 Task 4 behavior가 필요하면 해당 파일을 수정하지 않고 grounded blocker로 보고한다.
- Task 1 완료를 주장하려면 primary packages뿐 아니라 이 direct dependent package들의 compile 상태도 확인한다. 해결 불가능하면 worktree를 의도적으로 깨진 상태로 두지 말고 마지막 coherent state까지 되돌린 뒤 blocker를 보고한다.

이 경계 밖의 collector, producer, API plane, deployment, CI, config, docs는 수정하지 마십시오.

## 구현 계약

다음 결과를 모두 구현하십시오.

### Contract v2.1

- provider vocabulary: `holodex`, `youtubejs`, `hololive_official`
- observation kind vocabulary:
  - `community_page`
  - `video_list`
  - `shorts_list`
  - `live_snapshot`
  - `viewer_sample`
  - `channel_stats`
  - `channel_profile`
  - `channel_photo`
  - `schedule_snapshot`
- `scheduled_for` 기반 snapshot identity와 `viewer_sample` window identity
- strict typed payload/coverage V1 validation
- canonical JSON 기반 `scope_sha256`, `payload_sha256`, `evidence_sha256`
- `source_event_at` future skew validation과 `scheduled_for` fallback
- `collection_job_kind`와 observation kind의 분리
- `AuthorityMode`, `legacy/shadow/authoritative`, authority transition API 제거
- unknown fields, malformed values, unbounded identifiers/payload를 fail closed

### Migration 144 direct rewrite

- production 미적용 전제에 따라 compatibility migration이나 dual writer 없이 `144`를 직접 재작성
- contract generation table과 generation 1 seed
- generation target projection, target reason, collection job lease schema
- collection checkpoint
- immutable `source_observations`
- mutable `source_observation_queue`
- collision audit
- consumer offset, replay request, application audit
- reconciliation conflict audit
- `youtube_live_reconciliation_heads`
- canonical contract에 명시된 indexes, constraints, FK, vocabulary, bounds, grants와 sequence privileges
- migration runner semantics에 맞는 idempotency
- manifest entry 정합성
- schema golden 갱신

Migration SQL은 `/hololive/hololive-api/scripts/migrations/CONVENTIONS.md`를 준수해야 합니다. `CREATE INDEX CONCURRENTLY`를 transaction block 안에 넣지 말고, 재실행 가능한 DDL과 명시적인 conflict arbiter를 사용하십시오.

### Repository v2.1

- immutable evidence와 queue state를 별도 repository path로 소유
- `PublishBatch`의 batch validation, fence precondition interface, identity preflight, duplicate/collision 분류
- collision batch는 audit만 보존하고 observation/queue/checkpoint는 변경하지 않는 fail-closed contract
- checkpoint는 continuity/health이며 content duplicate suppression의 SSOT가 아님
- current generation publish validation과 compile-time supported contract consumption을 분리
- bounded claim with `FOR UPDATE SKIP LOCKED`
- per-item claim/finalize API가 이후 API plane에서 transactionally 사용할 수 있는 형태
- replay/application audit와 retention query surface
- `fmt.Errorf("action: context: %w", err)` wrapping, `context.Context` first, bounded input validation

Task 2의 실제 acquisition/renewal scheduler나 target projection builder를 구현하지 마십시오. Task 1 repository에는 Task 2가 구현할 fence/target verification을 명확한 interface 또는 transaction precondition으로 둘 수 있지만, correctness를 우회하는 fake success path는 금지합니다.

## 반드시 고정할 regression test

최소 다음 behavior를 test로 고정하십시오.

```text
same identity + same evidence -> one observation, one queue row
same identity + different payload -> collision audit, no second observation
same identity + completeness change -> collision audit
same payload + next scheduled slot -> two observations
viewer equal value + next sample window -> two observations
unknown field / malformed payload -> validation reject
stale schema/generation -> publish reject
current generation bump does not strand old supported queue rows
unsupported contract -> item DLQ with bounded audit
source_event_at future skew -> scheduled_for fallback and observable result
checkpoint + observation rollback atomicity
migration replay twice
role grants and schema golden
```

정상, 실패, 경계 경로를 함께 구현하십시오. skipped test, test-only production bypass, broad exclusion을 추가하지 마십시오.

## NFR and tech-debt blocking rules

- queue, claim limit, payload, identifier, replay count, retry detail은 모두 bounded
- DB transaction/rows/resource lifetime은 owner가 close/rollback/commit
- collision과 malformed payload는 bounded detail만 저장하고 raw payload를 log하지 않음
- SQL identifier runtime interpolation 금지
- scraper role은 least privilege이며 canonical/replay/claim-finalize 권한을 갖지 않음
- provider precedence, authority compatibility, universal `map[string]any` payload 금지
- unbounded goroutine/retry/queue와 detached lifecycle 금지
- 새 dependency, lockfile/toolchain upgrade, lint/race/NilAway suppression 금지
- unrelated cleanup과 existing user WIP 변경 금지

Migration/schema 변경은 high risk입니다. 구현 편의를 위해 contract, security, resource bound 또는 gate를 낮추지 마십시오.

## 구현 순서

1. current schema/contract/repository/test gap을 contract v2.1 항목별로 표로 정리한다.
2. contract types, canonicalization, typed validation과 unit tests를 먼저 구현한다.
3. migration `144`, manifest, DB test expectation을 재작성한다.
4. generic repository query/API를 immutable evidence + queue 모델로 교체한다.
5. authority symbol/query/test를 제거하고 repository regression test를 완성한다.
6. direct dependent compile impact를 확인하고 conditional boundary 규칙을 적용한다.
7. `gofmt`와 필요한 generated schema golden update를 수행한다.
8. 아래 validation을 final state에서 실행한다.
9. 통과한 증거만 status report에 기록한다.
10. Task 2를 시작하지 않고 결과를 사용자에게 보고한다.

각 단계에서 작은 수정마다 diff를 반복 출력하지 말고 coherent batch로 편집한 뒤 consolidated diff를 검사하십시오. 파일 수정에는 `apply_patch`를 사용하십시오. schema golden은 repository가 지정한 generation command로만 갱신할 수 있습니다.

## Validation

먼저 canonical Task 1 check를 실행하십시오.

```bash
go test ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-dbtest/...
```

Migration 구조 위험을 직접 검증하십시오.

```bash
bash scripts/architecture/check-migration-manifest.sh
```

Task 1 API를 import하는 direct dependents의 compile/test 상태를 확인하십시오.

```bash
go test ./hololive/hololive-youtube-collector/internal/runtime/communitycollector \
  ./hololive/hololive-youtube-producer/internal/runtime/pollers
```

마지막으로 touched scope를 검사하십시오.

```bash
git diff --check -- \
  hololive/hololive-api/scripts/migrations/144_source_observation_outbox.sql \
  hololive/hololive-api/scripts/migrations/manifest.txt \
  hololive/hololive-dbtest \
  hololive/hololive-shared/pkg/contracts/sourceobservation \
  hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  hololive/hololive-youtube-collector/internal/runtime/communitycollector \
  hololive/hololive-youtube-producer/internal/runtime/pollers \
  docs/current/architecture/youtube-producer-convergence-status-20260814.md
```

`SCHEMA_SNAPSHOT_UPDATE=1`은 golden 갱신 단계에서만 사용하고, 그 뒤 환경변수 없이 같은 DB test가 통과하는지 확인하십시오.

이번 Task 1 세션에서는 full pre-push, full race/NilAway/perf gate를 실행하지 마십시오. canonical contract가 이를 Task 10 final-state blocking gate로 소유합니다. 다만 targeted command가 race/resource 문제를 드러내면 gate를 우회하지 말고 원인을 수정하십시오.

## 승인 및 금지 경계

현재 승인된 범위:

- local code/document edits within the write boundary
- non-destructive local tests and schema replay
- generated schema golden update

승인되지 않은 범위:

- production migration apply
- deploy, restart, live health mutation
- production/shared database write or repair
- raw secret access
- commit, push, PR, issue/review write
- new production dependency
- destructive Git operation

위 작업이 필요해지면 실행하지 말고 필요한 승인과 이유만 보고하십시오.

## Stop conditions

다음 중 하나면 범위를 확장하지 말고 중단·보고하십시오.

- migration `144`가 production 또는 shared non-test DB에 이미 적용됐다는 새 증거
- branch/worktree mismatch
- user-owned WIP와 겹쳐 안전하게 보존할 수 없는 conflict
- Task 1 완료에 Task 2/3/4의 실제 runtime behavior가 필수
- public/breaking contract, dependency, secret, deploy 또는 live DB 승인이 새로 필요
- canonical contract 자체의 상충 때문에 둘 이상의 materially different 구현이 가능

단순히 일이 크거나 test가 실패했다는 이유로 중단하지 말고, 먼저 범위 안에서 root cause를 해결하십시오.

## 완료 판정

다음 조건을 모두 충족해야 `Task 1 complete`라고 보고할 수 있습니다.

1. Task 1 contract, migration, repository와 필수 regression test가 구현됨
2. authority compatibility symbol/table/query가 Task 1 current surface에서 제거됨
3. migration replay, schema golden과 role grant test 통과
4. canonical Task 1 test command 통과
5. migration manifest check 통과
6. direct dependent compile/test 통과
7. touched-scope `git diff --check` 통과
8. production operation을 수행하지 않음

Task 1 범위로 해결할 수 없는 direct dependent blocker를 포함해 하나라도 충족하지 못하면 `partial` 또는 `blocked`로 보고하고, passing으로 과장하지 마십시오.

## 최종 보고 형식

한국어 합쇼체로 결론부터 다음만 보고하십시오.

- Outcome: `complete`, `partial`, 또는 `blocked`
- 변경한 소유 영역과 핵심 invariant
- 실제 실행한 validation command와 결과
- direct dependent 상태
- 남은 blocker 또는 Task 2 진입 전 확인사항
- production migration/deploy 미수행 확인

파일을 언급할 때는 절대경로의 clickable link를 사용하십시오.
