# Hololive Bot Broadcast History Review Notes

Prepared for external review on 2026-07-05.

## Scope

This change adds bot-side features only. It does not add admin dashboard behavior.

- Ended broadcast history command with member, category/type, topic, date window, count, and all-history filters.
- Default date window is 7 days when no explicit date option is given.
- High-quality thumbnail download command for ended broadcasts.
- Runtime persistence of observed live `topic_id` and `thumbnail_url` for future sessions.
- Query-time fallback to historical `alarm_dispatch_events` payloads for older sessions that were recorded before the new columns existed.

## Why the implementation is shaped this way

YouTube ended/live session rows do not contain a stable first-class "broadcast type" field. The implementation therefore uses the best observed source first:

1. `topic_id` from the live stream payload when available.
2. Title-based classification from the embedded rule file when the topic is missing or unknown.
3. `unknown` when neither source gives enough evidence.

The stored type is not persisted as a separate enum because the classification rules are heuristic and may improve over time. The rule data is versioned in `broadcast_type_rules.json` and embedded at build time so classification is reviewable without adding runtime network/config dependencies. The classifier normalizes titles with NFKC, lowercasing, zero-width removal, and whitespace collapse before matching. The repository returns both the computed type and the source (`topic`, `title`, or `unknown`) so reviewers can see which evidence drove a result.

Topic classification is preferred for known non-game topics. Strong title signals such as members-only, watchalong, singing, ASMR, concrete event identity, horse racing, and news can override `game` or `other` topics because those cases represent access or format information that often matters more than the game category. Generic event words (`大会`, `リーグ`, `本配信`, `耐久`, etc.), generic `talk`, and generic `other` run after game evidence so ordinary game streams are not hidden by broad wording. Official event identity markers such as `ホロマリオテニス大会`, `ポケモンチャンピオンズ大会`, and `漢義リーグ` remain event-first policy cases.

Game matching no longer uses raw whole-title substring matching for ASCII aliases. ASCII aliases require token boundaries after normalization, while CJK and mixed title aliases can still match compact Japanese title text. This prevents short aliases such as `nte` from matching unrelated words like `CONTENT` while preserving titles such as `【NTE】`, `【バイオハザードRE4】`, and `【リトルナイトメア２】`.

The history repository uses keyset pagination over the whole requested date/member window. This is intentional: filtering by computed type after a single SQL `LIMIT` can miss valid broadcasts if the latest page is filled with non-matching rows. The command still limits the final response size to `maxBroadcastHistoryLimit`.

The old one-shot metadata backfill migration was removed. Existing ended rows can already be enriched from `alarm_dispatch_events` at query time, while future rows are populated by the live poller. This avoids a production backfill lock/volume risk while preserving old-data behavior for this command.

Thumbnail download tries the highest-resolution YouTube candidate first (`maxresdefault.jpg`), then falls back through known YouTube thumbnail sizes and the stored thumbnail URL. The downloader restricts hosts, schemes, ports, content types, redirects, and response size.

## User-facing command forms

Representative forms:

- `!방송이력`
- `!방송이력 페코라`
- `!방송기록 페코라 게임`
- `!방송기록 사쿠라 미코 게임`
- `!방송이력 카테고리:게임 멤버:페코라 7일`
- `!방송이력 경마 30`
- `!방송이력 type:멤버십 14일 10`
- `!방송이력 카테고리:게임 14일 개수:10`
- `!방송이력 topic:Forza all`
- `!방송이력 썸네일 AqxEw3kXcgU`
- `!썸네일 AqxEw3kXcgU`

Bare positive numbers are interpreted as days unless a days value is already present or a later token explicitly supplies days; in those cases they are interpreted as the response limit. Explicit `limit:`, `개수:`, and `갯수:` keep working regardless of order.

Supported category labels include: `게임`, `잡담`, `노래`, `ASMR`, `멤버십`, `이벤트`, `경마`, `동시시청`, `뉴스`, `기타`, `미분류` and English aliases.

## Implementation files to review

Bot command and parser:

- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_broadcast_history.go`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/broadcast_history_repository.go`
- `hololive/hololive-api/internal/planes/bot/internal/broadcasttype/broadcasttype.go`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/broadcast_type.go`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/broadcast_type_rules.json`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/broadcast_thumbnail_downloader.go`
- `hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/message_parser_broadcast.go`
- `hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter/formatter_broadcast_history.go`
- `hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/bot_command_init_views.go`
- `hololive/hololive-shared/pkg/domain/command.go`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/handlercore/command.go`

Metadata persistence and fallback:

- `hololive/hololive-shared/pkg/domain/youtube_content.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/live_poller_sessions.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_live_session_source.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_persisted_live.go`

Migrations:

- `hololive/hololive-api/scripts/migrations/100_add_youtube_live_session_metadata.sql`
- `hololive/hololive-api/scripts/migrations/101_add_youtube_live_session_sort_index.sql`
- `hololive/hololive-api/scripts/migrations/102_add_youtube_live_session_channel_sort_index.sql`
- `hololive/hololive-api/scripts/migrations/103_add_youtube_live_session_topic_index.sql`
- `hololive/hololive-api/scripts/migrations/manifest.txt`
- `hololive/hololive-shared/pkg/dbtest/testdata/schema_snapshot.golden.sql`

Tests:

- `hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/message_parser_broadcast_test.go`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_broadcast_history_test.go`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/broadcast_type_test.go`
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/broadcast_thumbnail_downloader_test.go`
- `hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/bot_command_init_views_test.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/live_poller_test.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_live_session_source_test.go`

## Review result

The read-only review passes all returned `qualified/disagree` against the earlier implementation. Main findings:

- Type/topic filters were applied after a bounded SQL `LIMIT`, so valid sparse matches could be missed.
- Bare `멤버` must remain available for member-name parsing; membership category filtering should use `멤버십` or explicit membership aliases.
- Topic-first classification could hide strong title evidence such as members-only or watchalong.
- The removed `102` migration was a one-shot update and did not match the migration convention for backfills.

Resolution in this working tree:

- Replaced bounded prefetch with keyset pagination and added `TestPgBroadcastHistoryRepositoryListEndedBroadcastsScansPastFirstPageForTypeFilter`.
- Reserved bare `멤버` for member-name parsing, kept membership type filtering on `멤버십`, and added command-alias/member-name/type regression coverage.
- Added strong title override policy and source tests for members-only/watchalong while preserving game-topic priority over generic talk.
- Moved broadcast type rules into embedded `broadcast_type_rules.json`, added `경마`/`horse_racing`, and covered JRA G1/J-G1 race-name classification without matching bare `G1` or `的中` alone.
- Refined the title classifier with strict hard/generic rule phases, NFKC normalization, ASCII token-boundary matching, shared broadcast type aliases, and regression coverage for `nte` false positives, broad event overmatching, and observed game false negatives.
- Added per-rule `reject_keywords` so horse-racing game titles (`ウマ娘`, `ウイニングポスト`, `ダビスタ` families) classify as `game` even when they contain real race names such as `有馬記念`. The reject scope is the lead `【…】` tag when present (whole title otherwise), so a real prediction stream that merely mentions `ウマ娘` in its body stays `horse_racing`. Rejects match without ASCII token boundaries so compact spellings like `Winning Post10` are still caught.
- Split the event rule so the guard only covers race-name-ambiguous words (`記念`, `周年`, `anniversary`); personal event markers (`生誕`, `birthday`, `卒業`, `新衣装`, etc.) are never rejected and keep their topic-override strength.
- Extended `horse_racing` coverage with NAR dirt graded races (`帝王賞`, `東京大賞典`, `かしわ記念`, JBC races, `羽田盃`, `東京ダービー`, etc.), major international races (`凱旋門賞`, Dubai, Breeders' Cup, Hong Kong G1s, `ケンタッキーダービー`), and `ステークス` long forms for existing `S` abbreviations.
- Deduplicated keywords after NFKC normalization and removed now-redundant full-width JSON entries (`天皇賞（春）`/`（秋）`, `スト６`); regression tests keep the full-width titles classifying correctly.
- Applied NFKC to `broadcasttype.NormalizeToken` so full-width user category input (`ＡＳＭＲ`, `ｇａｍｅ`, `＃競馬`) parses to the same aliases.
- Removed the one-shot backfill migration and kept query-time event fallback for historical rows.

## Read-only production data evidence

Commands were run against `holo-postgres` using container environment variables only; raw secrets were not read or printed.

Observed before deploying these migrations:

- `youtube_live_sessions` does not yet have `topic_id` or `thumbnail_url` columns in the running DB.
- Updated app runtimes must run after `hololive-db-migrate`; `scripts/deploy/compose-redeploy-service.sh` now executes the migration job before app runtime cutover.
- Ended sessions: `4724`.
- Ended-session metadata recoverable from latest LIVE alarm event payloads: topic `327`, thumbnail `345`.
- Current rows with `status='LIVE'`: `72`.
- LIVE rows with latest LIVE event metadata: topic `3`, thumbnail `3`.
- Observed stream payload keys include:
  `channel, channel_id, channel_name, duration, id, link, start_actual, start_scheduled, status, thumbnail, title, topic_id, viewer_count`.

Top observed ended topics from event payloads:

```text
Forza 28
membersonly 27
talk 24
News_Show 21
minecraft 18
residentevil 13
Rhythm_Heaven 13
singing 13
Power_Pros 11
Mario_64 10
MECCHA_CHAMELEON 10
```

This supports using `notification.stream.topic_id` and `notification.stream.thumbnail` as the historical fallback path.

## External YouTube thumbnail evidence

The high-resolution URL for a known video was checked:

```text
https://i.ytimg.com/vi/AqxEw3kXcgU/maxresdefault.jpg
HTTP/2 200
content-type: image/jpeg
content-length: 258324
```

The command uses that max-resolution candidate first and falls back to lower-resolution candidates when it is unavailable.

## Verification run

Passed:

```bash
go test ./hololive/hololive-api/internal/planes/bot/internal/adapter/messaging ./hololive/hololive-api/internal/planes/bot/internal/command/handlers ./hololive/hololive-api/internal/planes/bot/internal/bot/orchestration ./hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers ./hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking
scripts/architecture/check-migration-manifest.sh
go test ./hololive/hololive-api/... ./hololive/hololive-shared/... ./hololive/hololive-alarm-worker/...
curl -fsSI --max-time 10 https://i.ytimg.com/vi/AqxEw3kXcgU/maxresdefault.jpg
```

## Known limits

- Type classification is heuristic. It is grounded in observed topics and title markers, not a YouTube-authoritative type taxonomy.
- Horse-racing classification uses concrete race/project names (`大阪杯`, `有馬記念`, `ホロ的中バトル`, etc.) and the general `競馬` marker; generic `G1` or `的中` alone is intentionally not enough evidence.
- The horse-racing game guard scopes to the lead `【…】` tag when one exists and to the whole title otherwise. A bracket-less real prediction title that mentions `ウマ娘` still downgrades to `game`; this residual trade-off favors the far more common case where race names appear inside horse-racing game titles.
- The command still returns one primary type. Game-centered official events are handled by explicit policy markers; broader analytics such as `secondary_types` or confidence scores are intentionally outside this change.
- Historical `topic_id` coverage is limited to rows that have compatible alarm event payloads. Future rows improve because the poller now persists metadata directly.
- No live deploy, restart, production schema migration, or production data mutation was performed in this task.
- A thumbnail can still be unavailable at `maxresdefault.jpg`; fallback candidates handle that case.
