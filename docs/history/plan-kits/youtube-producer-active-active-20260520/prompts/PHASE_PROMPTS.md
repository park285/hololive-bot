# Phase Prompts

## Phase 00 Prompt

```text
Use docs/history/plan-kits/youtube-producer-active-active-20260520/phases/phase-00-state-freeze-and-evidence.md.

Perform read-only evidence collection for current `youtube-producer` active-active implementation. Do not edit code. Confirm the naming canon, implementation paths, config env names, and compose AP layout. Write the result as a concise evidence note.
```

## Phase 01 Prompt

```text
Use docs/history/plan-kits/youtube-producer-active-active-20260520/phases/phase-01-local-regression-gates.md.

Run local active-active regression gates. Classify failures as change-caused, pre-existing, environmental, or inconclusive. Only make minimal fixes if the failure is directly in active-active code/tests and safe to fix in this phase.
```

## Phase 02 Prompt

```text
Use docs/history/plan-kits/youtube-producer-active-active-20260520/phases/phase-02-osaka-smoke-and-operational-evidence.md.

Collect read-only Osaka operational evidence for `youtube-producer-a` and `youtube-producer-b`. Do not deploy, restart, stop, rollback, or write secrets. Record `/ready`, health, metrics, log scan, and duplicate SQL results. If access or approval is missing, report that as the blocker.
Do not deploy, restart, stop, rm, rollback, modify env, write secrets/OpenBao KV, or read/use secret-backed metrics credentials without explicit approval. Record metrics only if accessible without additional authentication; otherwise record the 401/auth blocker.
```

## Phase 03 Prompt

```text
Use docs/history/plan-kits/youtube-producer-active-active-20260520/phases/phase-03-proactive-valkey-readiness.md.

Decide whether to keep reactive readiness or implement proactive Valkey readiness for active-active. If implementing, write tests first, keep the probe lightweight, avoid real job identity collisions, and preserve fail-closed semantics.
```

## Phase 04 Prompt

```text
Use docs/history/plan-kits/youtube-producer-active-active-20260520/phases/phase-04-photo-sync-failover-policy.md.

Decide and implement/document the PhotoSync policy. If keeping AP-A-only PhotoSync, update docs only. If enabling AP-B failover, update compose and tests, but do not run live failover without explicit approval.
```

## Phase 05 Prompt

```text
Use docs/history/plan-kits/youtube-producer-active-active-20260520/phases/phase-05-two-scheduler-regression-test.md.

Add a shared fake claimer regression test proving two scheduler instances with the same due `poller + channel` perform exactly one Poll inside the cooldown window. Keep it local and deterministic; do not require Valkey or Postgres.
```

## Phase 06 Prompt

```text
Use docs/history/plan-kits/youtube-producer-active-active-20260520/phases/phase-06-readiness-metrics-and-ttl-docs.md.

Close the readiness counter and lease TTL documentation gap. Prefer metrics-only documentation unless HTTP counters are explicitly required. Do not add high-cardinality fields. Document the current TTL calculation and why a hard clamp is risky.
```
