# PR-12 leftover-forbidden symbols inventory (contract v6 §28.4)

> Historical verification capture. Do not use as a current CI or runtime contract.

Record of production-path presence on the dirty tree at `HEAD` `81883b7fad0483a7b1563779b294073d450fac76`. This is not a claim that RC DoD is green.

| Forbidden leftover | Production path | Notes |
|---|---|---|
| `String(input)` proxy URL conversion | absent | HC-001 |
| per-request `ProxyAgent` construction | absent | HC-002; bootstrap construction remains in `fetch-transport.mjs` |
| collection RPC `proxy_url` field | absent | HC-003 |
| `YOUTUBEJS_SOCKET` production override | absent | HC-004 |
| kill-first `Helper.Close` | absent | `Close` sends SIGTERM, waits, then SIGKILL on timeout |
| Node socket unlink ownership | Go-owned | Node `server.mjs` documents Go-only unlink |
| arbitrary persisted error string | absent | `WrapClass`/`Code`/`Class`/`Detail` removed; HC-014 has no allowlist |
| unknown error => transient fallback | absent in current `Normalize` path | inventory only; not re-proven by PR-12 suite |
| missing target key => allow | absent | HC-005 |
| non-zero result + fatal error | absent | PR-07 contract; not re-run here |
| partial publish and lease defer in separate transactions | absent | `PublishBatchAndDefer`; not re-run here |
| publish result missing-row => inserted fallback | absent | `publishedOutcome` returns `("", false)` on missing index |
| leftover `index >= len(result.Results) { return outcomeInserted` | absent | HC-006 enabled; regex narrowed so the fail-closed bounds check is not a false match |
| application-clock absolute retry timestamp | residual clamp | `retryAt` still uses process `time.Now()` to clamp min/max. Recorded leftover, not a PR-12 behavior change. |
| GLOBAL candidate every cycle regardless due | absent | due-only `repository_candidates_global_0144_17.sql` |
| queue full => success | absent | explicit `EnqueueFull` |
| scheduler Stop 후 object reuse | absent | single-use scheduler; not re-run here |
| `firstPublishedObservationID` permanent latch | absent | replaced by bounded handoff tracker |
| nil DB => pending 0 success | absent | nil store fails closed; not re-run here |
| full pending `COUNT(*)` | absent | SQL is `COUNT(*)` over `LIMIT $1 + 1` subquery |
| provider status 확인 전 success-size body read | absent | PR-05; not re-run here |
| collector `ProvideCacheResources` / Valkey dependency | absent | HC-007 plus `service/cache` import rule on `collectorruntime/*.go` excluding tests |
| production build only default/`go_json` tested | unverified this session | sonic/image digest not executed |
| image-only rollback | forbidden in runbook | docs record only; production rollback not executed |
