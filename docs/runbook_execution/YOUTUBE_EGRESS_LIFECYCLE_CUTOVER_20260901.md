# YouTube egress lifecycle cutover evidence (2026-09-01)

## Scope

- Target: `hololive-osaka` (`100.100.1.8`), central Compose service `hololive-alarm-worker` only.
- Change: replace distributed YouTube delivery lifecycle mutations with the alarm-worker-owned, ledger-first, version-fenced transition path.
- The approved operation covered the source merge, ARM64 image transfer, `hololive-alarm-worker` recreate, live verification, and guarded PostgreSQL reads.
- No migration, production data mutation, secret sync, remote build, or other service restart was performed in this cutover.

## Source and validation

- PR: `https://github.com/park285/hololive-bot/pull/456`.
- Reviewed head: `b4c04d29aaafbfff9a847155ec1edd10ef28b4a7`.
- Squash merge on `main`: `4301ae7ff666cfadb51dba9f1b32fbe4cb31c5c4` at `2026-09-01T12:53:05Z`.
- The reviewed head and merge commit both resolve to tree `851315e8af9535f6add2293c598e98d00d1ff7e5`.
- Targeted unit, fault-injection, integration, and race checks passed. `bash scripts/ci/local-ci.sh`, `./build-all.sh --build-only`, the notification-egress architecture gate, the hot-path snapshot contract, the pre-push gate, all PR module jobs, GitGuardian, policy, and `fast-gate` passed.
- The clean merge checkout produced `hololive-alarm-worker:candidate-4301ae7f-arm64` with image ID `sha256:8e36703cbdea111f1ff2b617ceff30c26939540026effc1238901d5115673aa1`, `arch=arm64`, `version=3.1.2`, and the full merge revision label.
- Runtime Compose, deploy scripts, and migration inputs were unchanged from the previously deployed revision, so the existing reviewed deploy tree remained in place.

## Cutover and rollback point

- Before the cutover, `hololive-alarm-worker:prod` and the running container both used image ID `sha256:bb22bcdf60c73498535f30ecac944aeece8ea39a3fd6fd6c9ee87cc6aae9500d`, revision `eab17a288b8880a103abbaf0c2dd32bda1f103d5`, `arch=arm64`, and `version=3.1.2`.
- That exact image remains available as `hololive-alarm-worker:rollback-20260901T125408Z`.
- The transferred candidate was rechecked on the runtime host before retagging. Remote Compose `config --quiet` passed.
- `change_started_at` was `2026-09-01T12:54:42Z`. The service was recreated with `up -d --no-build --no-deps --force-recreate hololive-alarm-worker`; the repository health gate passed.
- The final container started at `2026-09-01T12:54:47.277461292Z`, used the candidate image ID, remained `running/healthy`, and had `RestartCount=0`.

## Runtime observation

- H3 `/health`, public `/ready`, and authenticated `/internal/ready` passed after recreation and again at final observation.
- The authenticated metrics-plane `/diagnostics/workers` response was complete and reported exactly three enabled executors: `youtube_delivery` 4/4 running, `alarm_dispatch` 1/1 running, and `notification_delivery` 4/4 running. All three queue depths and all reported `outcomeUnknown` totals were zero.
- Filtered logs since `change_started_at` contained zero error-level, panic, fatal, atomicity, outcome-unknown, logical-invariant, ledger-mismatch, permission, X.509, missing-file, or OOM markers.
- Exported lifecycle risk counter samples summed to zero. Counter-vector families without observations do not emit a series, so runtime logs and the database audit below remain the primary negative evidence.

## Guarded PostgreSQL audit

- The authoritative Hololive database was read through the local socket in `holo-postgres` with `PGOPTIONS=-c default_transaction_read_only=on`; `show transaction_read_only` returned `on` before substantive queries.
- Ledger state: one singleton row, schema version 1, `completed_at` present, and delivery, verification, and outbox cursors equal to their fixed high-water marks.
- Retained state counts at observation: 45 delivery rows, 43 outbox rows, and 37 logical ledger rows.
- Delivery shape violations, ledger shape violations, terminal deliveries without ledger evidence, ledger/source mismatches, impossible mixed logical groups, outbox terminal-shape violations, and aggregate projection mismatches were all zero.
- The cutover initially retained three `COMMUNITY_POST` rows from `2026-05-17` that lacked the current canonical payload field. They were nonterminal, unlocked, and explicitly parked until `2099-01-01`; due, locked, active-send, and terminal-invalid counts were zero. They were not rewritten or reclassified during the cutover. The separately approved cleanup below removed this residue, so the current retained count is zero.
- Aggregate TLS state reported 27 network sessions using TLS 1.3, one local inspection session, and no plaintext TCP session.

## Approved post-cutover legacy cleanup

- A read-only audit identified exactly three parked outboxes, their three `PENDING` delivery children, and three matching `DETECTED` alarm-state rows as test residue. The rows had no telemetry, terminal ledger, tracking, authorization, successful-send, active-lock, or downstream-dispatch evidence.
- The source `youtube_community_posts` rows referenced by the stale payloads had different channels and were deliberately excluded from the cleanup. No canonical source, observation, tracking, telemetry, or ledger row was changed.
- After explicit approval of the exact target and irreversible no-backup operation, one guarded PostgreSQL transaction deleted the three alarm-state rows and three outboxes. The outbox foreign key cascaded deletion to exactly three delivery rows. Expected-count, state-shape, and no-terminal-evidence guards ran under row locks; any mismatch would have rolled back the transaction.
- The observed cleanup window was `2026-09-01T13:40:27Z` through `2026-09-01T13:42:22Z`. The transaction committed with `deleted_alarm_states=3` and `deleted_outboxes=3`.
- Postconditions reported zero legacy test outboxes, zero pending Community rows missing `canonical_post_id`, zero matching legacy alarm states, zero orphan deliveries, zero invalid current identities, zero delivery/ledger/outbox shape violations, zero terminal deliveries without ledger evidence, and zero impossible mixed logical groups. The retained totals were 40 outboxes, 42 deliveries, and 37 ledger rows; the completed ledger high-water state remained intact.
- H3 `/health`, public `/ready`, authenticated `/internal/ready`, and authenticated worker diagnostics passed. All three queues, in-flight counts, and `outcomeUnknown` totals were zero. The alarm-worker remained on image `sha256:8e36703cbdea111f1ff2b617ceff30c26939540026effc1238901d5115673aa1`, `running/healthy`, with `RestartCount=0`; filtered risk-log matches since the cleanup start were zero.
- The follow-up inspection used a read-only session and observed 19 TLS 1.3 network sessions, one local inspection session, and no plaintext TCP session. No service restart, deploy, migration, secret change, or fallback path was introduced.

## Result

The reviewed lifecycle owner is live on the single production alarm-worker instance, and the invalid parked test residue has been removed. All publish, runtime, readiness, ledger, projection, cleanup, and no-resend ambiguity checks passed. The rollback image and prior deploy tree remain available.

Fallback delta: none.
