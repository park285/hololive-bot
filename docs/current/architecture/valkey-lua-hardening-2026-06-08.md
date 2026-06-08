# Valkey Lua hardening follow-up (2026-06-08)

## Scope

This patch narrows the Lua hardening work to the two reviewed high-risk surfaces:

1. YouTube producer global budget reservation/release scripts.
2. ACL room cache atomic swap script.

The intent is not to remove Lua. Lua remains appropriate where Valkey needs single-command atomicity across multiple keys. The hardening goal is to make the existing Lua usage bounded, cacheable, key-declared, and easier to operate.

## Changes

- Global budget reserve/release now execute through `valkey.NewLuaScript`, so script execution uses the client-side SHA path (`EVALSHA` first, `EVAL` on `NOSCRIPT`) instead of sending long script bodies for every call.
- Expired reservation cleanup is bounded by `GlobalBudgetLimiterConfig.CleanupLimit`; production runtime config loads it from `YOUTUBE_PRODUCER_BUDGET_CLEANUP_LIMIT`, and non-positive values fall back to `defaultGlobalBudgetCleanupLimit`.
- If bounded cleanup leaves an expired backlog and stale counters still block the source/class, the reserve script returns `budget_cleanup_incomplete` instead of ordinary `budget_exhausted`.
- Reservation ZSET members and new reservation hash keys now encode the burst class (`class|ownerToken`). New writes use the encoded member shape only; owner-token-only legacy reservations are retired by bounded expired-entry cleanup as they age out.
- Shared counter/ZSET TTLs are extended only when the requested TTL is greater than the current TTL. The script deliberately avoids shortening a key that may still represent a longer-lived reservation.
- Reservation hashes also receive a TTL, but stale cleanup no longer depends exclusively on that hash for new reservations because the ZSET member carries the class.
- Release uses only the reservation class and encoded member for reservations created by the current code path. It does not reach into owner-token-only legacy reservations; those fade out through the reserve-side cleanup pass.
- Readiness payloads expose `budget_cleanup_incomplete` separately from ordinary `budget_exhausted` and `source_cooldown` pressure without changing HTTP readiness.
- ACL room-cache swap Lua declares touched keys via `KEYS` and `Numkeys(2)` instead of passing key names through `ARGV` with `Numkeys(0)`. The temporary set key now preserves the target key's cluster hash slot by either reusing an existing hash tag or wrapping the target key as the temp key hash tag.
- Regression tests cover bounded cleanup, hashless encoded stale reservations, encoded reservation hash placement, legacy reservation fadeout, shared no-TTL preservation, class-aware release, readiness reporting, and ACL key declaration.

## Intentional non-goals

- Weighted `SourceUnits` accounting is not introduced in this patch. `SourceUnits` remains metadata for now; changing capacity semantics from reservation-count to unit-weighted budget should be a separate compatibility-reviewed change.
- Multi-source profiles remain source-scoped atomic reservations with compensation rollback. A single cross-source Lua transaction would conflict with the current source hash-slot design.
- Sliding-window and small compare-and-delete/expire scripts are not changed here; they are short single-purpose scripts and did not carry the same cleanup/counter drift risk as the global budget scripts.

## Validation target

```bash
go test ./hololive/hololive-youtube-producer/internal/runtime/polling -count=1
go test ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime -run TestBuildIngestionRuntimeGlobalBudgetWiringPassesCleanupLimit -count=1
go test ./hololive/hololive-shared/pkg/service/acl -count=1
go test ./hololive/hololive-shared/pkg/config/internal/settings -run 'TestLoadYouTubeProducerGlobalBudgetConfig(Defaults|EnvOverrides|CleanupLimitDefault)' -count=1
go test ./hololive/hololive-youtube-producer/internal/runtime/readiness -count=1
```
