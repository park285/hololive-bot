# PR-12 compatibility shim inventory

> Historical verification capture. Do not use as a current CI or runtime contract.

Working tree is dirty on `HEAD` `81883b7fad0483a7b1563779b294073d450fac76`. This is an inventory, not a deletion PR.

| Shim | Production usage | Test usage | Residual action | Next-release plan |
|---|---|---|---|---|
| `collecterr.WrapClass` / `Code` / `Class` / `Detail` | Removed. Production and tests use `Wrap` / `CodeOf` / `ClassOf` / `DiagnosticOf`. | None | Closed. HC-014 has no allowlist. | None. |
| `YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES` / HC-013 dual-render | Removed. Loader and Compose read canonical env only. | Retired-alias ignore tests remain | Closed. HC-013 row deleted. | None. |
| `collectorInstanceIDForTest` | 0 production callers | `readiness_test.go` | Moved from `runtime.go` to `readiness_test.go` | None. Test helper only. |
| Compose `x-scraper-env` / `SCRAPER_POLL_*` / `SCRAPER_FETCHER_ENGINE` on collector | Loader does not read these keys. Collector was the only Compose merge consumer. | Render tests now assert collector has none of those keys | Removed unused YAML anchor and collector merge | None for collector Compose. `SCRAPER_POLL_*` remains on the hololive-api `Load()` scraper path and `.env.example`. |
| host-native `SETTINGS_DIR` | Generator, completion-check, and remote-apply no longer create or require `/var/lib/hololive-bot/youtube-collector/settings`. Collector runtime loader never read it. `providers/modules/settings.go` still consumes `SETTINGS_DIR` for other runtimes. | `ap-host-native-deploy_test.sh` requires the collector generator line and settings `install -d` to be absent | Dropped from collector host-native path | Existing AP `youtube-collector-host.env` files may still contain leftover `SETTINGS_DIR`; leftover is unused. Do not fail live completion on leftover until those files are regenerated. |
| `PublishBatch` | `COMPLETE` terminal path remains. `PARTIAL` uses additive `PublishBatchAndDefer`. | Existing PublishBatch tests remain | Keep. Do not delete. | None this release. Additive API; mixed-version writers still need `PublishBatch`. |
