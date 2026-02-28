# Hololive Scraper RS - Conventions

Reference for code generation and modification. Read before writing code.

---

## Workspace Structure

| Crate | Path | Role |
|-------|------|------|
| `scraper-core` | `crates/scraper/core` | Domain models, error types |
| `scraper-service` | `crates/scraper/service` | RSS parsing, scraping, scheduling |
| `scraper-infra` | `crates/scraper/infra` | DB (SeaORM), config, repository |
| `scraper-app` | `crates/scraper/app` | CLI, HTTP server, observability |
| `alarm-core` | `crates/alarm/core` | Domain models, error, constants, key builders |
| `alarm-service` | `crates/alarm/service` | Alarm checkers, dedup, notifier, scheduler |
| `alarm-infra` | `crates/alarm/infra` | Valkey (fred), Holodex/Chzzk/Twitch clients, config |
| `alarm-app` | `crates/alarm/app` | CLI, HTTP server, observability |

---

## Error Handling

### ScraperError (`scraper-core`)
```rust
#[derive(Error, Debug)]
pub enum ScraperError {
    Http(String),
    HttpStatus { code: u16, message: String },
    XmlParse(String),
    Database(String),
    Config(String),
    AllFeedsFailed(String),
    LinkBlocked(String),
    LinkFailed(String),
    Io(#[from] std::io::Error),
}
// is_retryable() -> bool (502-504, transient connection errors)
```

### AlarmError (`alarm-core`)
```rust
#[derive(Error, Debug)]
pub enum AlarmError {
    Valkey(String),
    Http(String),
    Database(String),
    Api { platform: String, message: String },
    CircuitOpen { platform: String },
    Config(String),
    Serialization(#[from] serde_json::Error),
}
```

### Rules
- **App boundary** (main, bootstrap): `anyhow::Context`
- **Domain/infra**: `thiserror` enum (types above)
- Error message format: `"action: context: cause"`

---

## Config Keys

Pattern: `{APP}__{SECTION}__{KEY}` (double underscore separator). Apps: `SCRAPER`, `ALARM`.

**SecretString fields** (MUST use `SecretString`): `DATABASE__PASSWORD`, `TWITCH__CLIENT_SECRET`, `IRIS__BOT_TOKEN`

---

## Constants (`alarm-core::constants`)

All constants use `SCREAMING_SNAKE_CASE`.

| Constant | Value | Purpose |
|----------|-------|---------|
| `TIER1_WINDOW` | 45min | Tier 1 check window |
| `TIER1_INTERVAL` | 1min | Tier 1 polling interval |
| `TIER2_WINDOW` | 3h | Tier 2 check window |
| `TIER2_INTERVAL` | 3min | Tier 2 polling interval |
| `TIER3_WINDOW` | 12h | Tier 3 check window |
| `TIER3_INTERVAL` | 10min | Tier 3 polling interval |
| `TIER4_INTERVAL` | 15min | Tier 4 (fallback) polling |
| `NO_UPCOMING_INTERVAL` | 5min | Interval when no upcoming streams |
| `FULL_REFRESH_INTERVAL` | 5min | Force-refresh all channels |
| `RECENTLY_NOTIFIED_WINDOW` | 15min | High-frequency polling after notification |
| `LIVE_CATCHUP_SUPPRESS_WINDOW` | 15min | Suppress catch-up after upcoming alert |
| `NOTIFICATION_SENT_TTL` | 24h | Notification history TTL |
| `NEXT_STREAM_INFO_TTL` | 1h | Next stream cache TTL |
| `TWITCH_NOTIFICATION_TTL` | 7d | Twitch notification TTL |
| `LOCAL_FALLBACK_DEDUP_TTL` | 10min | Local dedup fallback TTL (Valkey outage) |
| `LOCAL_FALLBACK_CLEANUP_MAX_KEYS` | 4096 (usize) | Max local dedup map entries |
| `DEFAULT_TARGET_MINUTES` | `&[5, 3, 1]` | Default alert target minutes |

---

## Valkey Key Patterns (Go `alarm_types.go` 1:1 parity)

### Key Structure
| Pattern | Type | Purpose |
|---------|------|---------|
| `alarm:{room_id}` | HASH | Per-room alarm subscriptions |
| `alarm:registry` | SET | Active room set |
| `alarm:channel_registry` | SET | Active channel set |
| `alarm:channel_subscribers:{channel_id}` | SET | Channel subscriber rooms (LIVE default) |
| `alarm:channel_subscribers:COMMUNITY:{channel_id}` | SET | Community alarm subscribers |
| `alarm:channel_subscribers:SHORTS:{channel_id}` | SET | Shorts alarm subscribers |
| `alarm:next_stream:{channel_id}` | STRING | Next stream info cache |
| `notified:{stream_id}` | HASH | Notification history |
| `notified:chzzk:live:{id}:{bucket}` | STRING | Chzzk live dedup (10min bucket) |
| `notified:integrated:{id}:{bucket}` | STRING | Integrated dedup (1min bucket) |
| `notified:claim:{room}:{stream}:{ts}:{cat}` | STRING | Notification claim lock |
| `notified:claim:event:{room}:{ch}:{ts}:{fp}:{cat}` | STRING | Logical event claim (stream_id change tolerant) |
| `notified:upcoming:event:{room}:{ch}:{ts}:{fp}` | STRING | Upcoming event marker |
| `notified:schedule:transition:{stream}:{old}:{new}` | STRING | Schedule change transition |
| `alarm:dispatch:queue` | LIST | Notification dispatch queue (LPUSH) |

### Key Builder Functions (`alarm-core::keys`)
```rust
fn alarm_key(room_id) -> String
fn channel_subscribers_key(channel_id) -> String
fn channel_subscribers_key_by_type(channel_id, AlarmType) -> String
fn notified_key(stream_id) -> String
fn next_stream_key(channel_id) -> String
fn chzzk_live_notified_key(chzzk_channel_id, DateTime<Utc>) -> String
fn integrated_notified_key(youtube_channel_id, DateTime<Utc>) -> String
fn build_notify_claim_key(room_id, stream_id, DateTime<Utc>, category) -> String
fn build_logical_event_claim_key(room_id, channel_id, stream_id, title, DateTime<Utc>, category) -> String
fn build_upcoming_event_key(room_id, channel_id, stream_id, title, DateTime<Utc>) -> String
fn build_schedule_transition_key(stream_id, old_minute, new_minute) -> String
fn build_title_fingerprint(title, stream_id) -> String  // SHA256[:8] hex
fn notification_category(target_minutes, minutes_until) -> String  // "live" | "target" | minutes
```

---

## Key Types

- **`MajorEvent`** -- scraper-core model with `MajorEventStatus`, `MajorEventType`, `MajorEventLinkStatus` enums. Entry: `Scraper::scrape_feeds()`, `Repository::upsert_event()`
- **`Stream`** -- alarm-core. `is_live()`, `is_upcoming()`, `minutes_until_start()`, `get_youtube_url()`
- **`Channel`** -- alarm-core. `display_name()` (english_name preferred), `is_hololive()`
- **`AlarmType`** -- `Live | Community | Shorts`. `display_name()` returns Korean ("방송"/"커뮤니티"/"쇼츠"), `as_str()`, `all()`
- **`StreamStatus`** -- `Live | Upcoming | Past` (serde lowercase)
- **`ValkeyClient`** trait -- `get`, `set`, `set_nx`, `del(&self, keys: &[&str])`, `hget`, `hset`, `hget_all`, `hmset`, `smembers`, `smembers_multi`, `expire`, `lpush`, `ping`

---

## Health Contract

| Endpoint | Role | Success | Failure |
|----------|------|---------|---------|
| `GET /health` | Liveness | 200 + JSON (version, status) | - |
| `GET /ready` | Readiness | 200 "ok" | 503 "degraded" |

**Readiness conditions:**
- Scraper: DB connection + FeedScheduler active
- Alarm: Valkey + DB connection + Scheduler active/healthy

