# Hololive Kakao Bot Go - Conventions

Reference for code generation and modification. Read before writing code.

---

## Utility Functions (`internal/util/`)

```go
ToKST(t)                    // Convert time.Time to KST
FormatKST(t, layout)        // Format in KST
NowKST()                    // Current KST
MinutesUntilCeil(target, ref) // Minutes remaining (ceil, nil → -1)
FormatKoreanNumber(n)        // 10000 → "1만", 12345 → "1만 2345"
NormalizeSuffix(s)           // Strip Korean suffixes ("짱", "쨩")
ApplyKakaoSeeMorePadding(text, instruction) // "전체보기" padding
IsValkeyNil(err)             // Check Valkey nil error (including unwrap)
NewCircuitBreaker(threshold, resetTimeout, healthCheckInterval, fn, logger)
```

## shared-go Utilities

### `shared-go/pkg/stringutil/`
```go
TruncateString(s, maxRunes)  // Append "..." when exceeding limit
Normalize(s)                 // Lowercase + trim
NormalizeKey(s)              // Strip special chars for search key
Slugify(s)                   // URL-safe slug
ContainsString(slice, item)
StripLeadingHeader(text, header)
```

### `shared-go/pkg/errors/`
**Sentinels:** `ErrNotFound`, `ErrAlreadyExists`, `ErrInvalidInput`, `ErrUnauthorized`, `ErrForbidden`, `ErrTimeout`, `ErrRateLimited`, `ErrServiceDown`, `ErrInternalServer`, `ErrSessionNotFound`, `ErrSessionExpired`, `ErrToolExecution`, `ErrOAuthTokenExpired`, `ErrEncryption`

**Constructors:** `NewRedisError(op, key, err)`, `NewDatabaseError(op, table, err)`, `NewExternalAPIError(service, statusCode, err)`, `NewAPIError(api, statusCode, message, err)`, `NewToolError(toolName, err)`

## Key Constants (`internal/constants/`)

| Group | Field | Value | Purpose |
|-------|-------|-------|---------|
| CacheTTL | `LiveStreams` | 5m | Live stream list |
| | `UpcomingStreams` | 5m | Upcoming stream list |
| | `ChannelSchedule` | 5m | Channel schedule |
| | `ChannelInfo` | 20m | Channel info |
| | `NextStreamInfo` | 1h | Next stream info |
| | `NotificationSent` | 24h | Notification history |
| | `TwitchNotification` | 7d | Twitch notification history |
| StringLimits | `StreamTitle` | 100 | Stream title max runes |
| | `NextStreamTitle` | 40 | Truncated next stream title |
| RequestTimeout | `BotCommand` | 10s | Bot command processing |
| | `BotAlarmCheck` | 2m | Alarm check cycle |
| | `AlarmService` | 10s | Alarm service ops |

---

## Key Services (entry points)

- **Cache** (`internal/service/cache/`): KV (`Set/Get/Del/Exists/SetNX/MGet/MSet`), Hash (`HSet/HMSet/HGet/HGetAll`), Set (`SAdd/SRem/SMembers/SIsMember`), `Expire`, `ScanKeys`, `DoMulti`, `B()` (builder)
- **Holodex** (`internal/service/holodex/`): `GetLiveStreams`, `GetUpcomingStreams`, `GetChannelSchedule`, `GetChannelsLiveStatus`, `SearchChannels`, `GetChannel`, `GetChannels`
- **Formatter** (`internal/adapter/`): All user messages via `ResponseFormatter`. Key: `render(ctx, TemplateKey, data)` → fallback `ErrorMessage()`. Long responses: `splitTemplateInstruction()` → `ApplyKakaoSeeMorePadding()`
- **AlarmService** (`internal/service/notification/`): Room-based (room_id PRIMARY). `AddAlarm`, `RemoveAlarm`, `GetRoomAlarms`, `ClearRoomAlarms`, `CheckUpcomingStreams`, `MarkAsNotified`. Multi-platform: `MarkChzzkLiveAsNotified`, `MarkIntegratedAsNotified`, `MarkTwitchLiveAsNotified` + `Is*Notified` checks
- **Alarm Repository** (`internal/service/alarm/`): `Add` (upsert on room_id+channel_id), `Remove`, `ClearByRoom`, `FindByRoom`, `FindByChannel`, `LoadAll`, `GetAllChannelIDs`

---

## Valkey Flag Patterns (Notification)

- **Prefix**: `notified:` (YouTube, Chzzk, Twitch send history)
- **Simple flags**: `cache.Set(ctx, key, true, ttl)` / `cache.Exists(ctx, key)` — Chzzk Live, Twitch Live, Integrated
- **Compound flags**: `NotifiedData { StartScheduled string, SentAt map[int]bool }` — YouTube upcoming per-minute targets
- **Forbidden**: `"1"` string as flag value (use `true`), timestamps in flag values (log instead)

---

## Domain Model Summary

- **Stream**: `IsLive()`, `IsUpcoming()`, `IsPast()`, `StartScheduled`, `StartActual`, `IsChzzkOnly/IsIntegrated/IsTwitchOnly`, `GetYouTubeURL()`, `HasYouTubeInfo()`
- **Channel**: `GetDisplayName()` (EnglishName preferred), `Org` (Hololive/Nijisanji/VSPO/Indie/Stellive)
- **Alarm**: Room-based. DB unique: `(room_id, channel_id)`. Valkey: `alarm:{roomID}` SET. `RegistryKey()` → roomID
- **AlarmNotification**: `MinutesUntil` — 0 = live catchup, >0 = upcoming

---

## Deprecated / Forbidden Code

**Never reference** (completely removed): `internal/chess/*`, `internal/service/ai/*`, `internal/command/ask.go`, `internal/prompt/*`, OpenAI/NLU/clarification code
