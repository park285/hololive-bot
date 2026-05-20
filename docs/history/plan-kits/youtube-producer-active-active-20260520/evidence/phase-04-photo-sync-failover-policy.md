# Phase 04 Evidence: Photo Sync Failover Policy

## Header

- Phase: 04 - Photo Sync Failover Policy
- Date/time: 2026-05-20T02:13:10Z
- Host: kapu
- Branch/commit: main / b1c65a400e473721171e87786c4ba34e26e77779
- Operator: Codex
- Live system touched: no
- Approval evidence if live mutation occurred: n/a

## Decision

Option A: AP-A-Owned PhotoSync.

Rationale: `docker-compose.osaka.yml` already sets `PHOTO_SYNC_ENABLED=true` for `youtube-producer-a` and `PHOTO_SYNC_ENABLED=false` for `youtube-producer-b`. AP-B scraping failover is in scope, but PhotoSync failover is not required without an explicit operator request and live failover approval.

## Changes

- Kept `docker-compose.osaka.yml` unchanged.
- Updated `docs/current/services/youtube-producer.md` to state AP-A-only PhotoSync and no AP-B PhotoSync failover.
- Updated `docs/current/runbooks/youtube-producer.md` to state the Osaka policy and diagnosis note.

## Commands

```bash
rg -n "PhotoSync|photo sync|PHOTO_SYNC|AP-B|failover|youtube-producer-a|youtube-producer-b" docker-compose.osaka.yml docs/current/services/youtube-producer.md docs/current/runbooks/youtube-producer.md hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/photo_sync_guard.go
```

Exit code: 0

Important output:

```text
docker-compose.osaka.yml: youtube-producer-a has PHOTO_SYNC_ENABLED: "true"
docker-compose.osaka.yml: youtube-producer-b has PHOTO_SYNC_ENABLED: "false"
docs/current/services/youtube-producer.md and docs/current/runbooks/youtube-producer.md now document AP-A-only PhotoSync policy.
```

```bash
git diff --check
```

Exit code: 0

Important output:

```text
No output.
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| compose unchanged | pass | no `docker-compose.osaka.yml` diff |
| AP-A PhotoSync enabled | pass | `docker-compose.osaka.yml` has `PHOTO_SYNC_ENABLED: "true"` under `youtube-producer-a` |
| AP-B PhotoSync disabled | pass | `docker-compose.osaka.yml` has `PHOTO_SYNC_ENABLED: "false"` under `youtube-producer-b` |
| service doc aligned | pass | service doc states AP-A-only PhotoSync and no AP-B failover |
| runbook aligned | pass | runbook states AP-B is scraping/polling failover only |
| docs formatting | pass | `git diff --check` exited 0 |

## Findings

- Completed: Option A policy is documented and compose remains unchanged.
- Blocked: none for docs-only Option A.
- Inconclusive: live PhotoSync runtime evidence is not proven here; Phase 02 remains blocked by Osaka runtime drift.
- Follow-up: If AP-B PhotoSync failover is later required, use Option B with explicit compose change, rollout approval, and live failover test approval.

## Completion Claim

This phase is complete for Option A. Evidence: docs were updated, compose remains unchanged, and `git diff --check` exited 0.
