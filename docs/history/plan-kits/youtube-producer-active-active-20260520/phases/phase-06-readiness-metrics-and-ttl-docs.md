# Phase 06: Readiness Metrics And TTL Docs

## Goal

두 가지 운영 문서 gap을 닫습니다.

1. `/ready`에 recent lease counters를 넣을지, Prometheus metrics로 충분한지 결정합니다.
2. scheduler lease TTL 정책을 runbook에 명확히 기록합니다.

## Current State

Readiness has:

- `mode`
- `active_active`
- `instance_id`
- `job_lease_enabled`
- `valkey_available`
- `scraping_paused`

Metrics have:

- `youtube_poller_job_claim_total`
- `youtube_poller_job_lease_renew_total`
- `youtube_poller_job_mark_completed_total`
- `youtube_poller_job_release_total`
- `youtube_poller_outbox_insert_total`

Scheduler lease TTL:

- `leaseTTL = pollTimeout + 15s`
- minimum is 1 minute
- no max clamp

## Counter Decision

### Option A: Metrics-Only

Recommended unless operators explicitly need HTTP diagnostics.

Required changes:

- document metric names and example expected labels in `docs/current/runbooks/youtube-producer.md`
- state `/ready` is readiness state, not recent activity telemetry

### Option B: Add `/ready` Counters

Use only if needed.

Required changes:

- add bounded process-local counters
- no `channel_id` labels or high-cardinality fields
- document counters as hints, not global truth
- add tests

## TTL Decision

Recommended:

- document current calculation
- do not add a hard 5-minute clamp unless `pollTimeout` is also constrained
- optionally add startup validation or warning for excessive active-active `pollTimeout`

## Files

Option A docs:

- `docs/current/runbooks/youtube-producer.md`
- optional: `docs/current/services/youtube-producer.md`

Option B code:

- `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go`
- `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness_test.go`
- readiness wiring in `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime`

Optional TTL validation:

- `hololive/hololive-shared/pkg/config/internal/settings/config_validation.go`
- related config tests

## Verification

Docs-only:

```bash
git diff --check
```

Readiness code:

```bash
go test ./hololive/hololive-youtube-producer/internal/runtime/readiness \
  ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime
```

Config validation:

```bash
go test ./hololive/hololive-shared/pkg/config/internal/settings
```

## Stop Rules

Stop and report if:

- proposed `/ready` counters require shared mutable global state across packages
- labels or fields include channel IDs
- TTL clamp can expire a valid in-flight poll before its timeout
