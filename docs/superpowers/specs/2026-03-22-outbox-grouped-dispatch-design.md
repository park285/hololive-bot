# Outbox Grouped Dispatch Design

Date: 2026-03-22

## Problem

YouTube scraping notifications (Shorts, Video, Community) are dispatched individually per delivery row. When multiple items of the same kind arrive in the same batch for the same room, each triggers a separate `SendMessage()` call. The LIVE alarm pipeline (`hololive-dispatcher-go`) has `GroupEnvelopes()` for bundling, but the outbox pipeline has no equivalent in its production path.

Grouping logic (`groupOutboxItems`) and group formatting (`formatGroupedMessage`) already exist but are dead code — never called from the production dispatch path.

## Goal

Bundle same-room, same-channel, same-kind delivery rows into a single message at dispatch time, using the existing dead code.

## Scope

- **In scope**: `dispatcher_send.go` — group delivery rows and dispatch grouped messages
- **Out of scope**: enqueue logic, delivery row schema, aggregate status logic, template content changes

## Architecture

### Current Flow

```
FetchAndLock() -> delivery rows
loadOutboxItemsByIDs() -> outboxByID map
preFormatMessages() -> per-outbox_id message cache
for each row -> SendMessage(row.RoomID, messages[row.OutboxID])
```

### New Flow

```
FetchAndLock() -> delivery rows
loadOutboxItemsByIDs() -> outboxByID map
preFormatMessages() -> per-outbox_id message cache (kept for single/fallback)
groupDeliveryRows(rows, outboxByID) -> []deliveryGroup, orphanRows
  orphanRows -> fail individually (existing behavior)
for each group:
  if len == 1 -> use preFormatMessages cache + SendMessage()
  if len >  1 -> formatGroupedMessage() + SendMessage()
                 on format error -> fall back to individual dispatch per row
  success/fail -> bulk mark all delivery IDs in group
```

## Data Structures

```go
// deliveryGroup: dispatch-time grouping of delivery rows targeting the same room+channel+kind.
type deliveryGroup struct {
    roomID    string
    channelID string
    kind      domain.OutboxKind
    rows      []domain.YouTubeNotificationDelivery
    outboxes  []domain.YouTubeNotificationOutbox
}
```

Group key: `roomID + "|" + channelID + "|" + kind`

Key safety: YouTube `channel_id` is always `UC`-prefixed alphanumeric, `room_id` is numeric, and `kind` is an enum constant (`NEW_VIDEO`, `NEW_SHORT`, `COMMUNITY_POST`, `MILESTONE`). None of these contain `|`, so the delimiter is collision-free.

## Grouping Function

```go
func groupDeliveryRows(
    rows []domain.YouTubeNotificationDelivery,
    outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (groups []deliveryGroup, orphanRows []domain.YouTubeNotificationDelivery)
```

- Iterates delivery rows, looks up outbox to get `channelID` and `kind`
- Rows whose outbox is missing from the map are returned as `orphanRows` (fail individually)
- **Milestone kind excluded from grouping**: `OutboxKindMilestone` has no group template, no group header, and `buildGroupedTemplateData()` does not populate milestone fields. Milestone rows are always placed in their own single-item group regardless of batch contents.
- Groups remaining rows by `roomID|channelID|kind`

## Dispatch Logic Changes

`dispatchDeliveryRows()` changes from per-row dispatch to per-group dispatch:

1. Call `preFormatMessages()` as before (used for single-item groups and fallback)
2. Build groups via `groupDeliveryRows()`
3. Fail orphan rows individually (existing `recordDeliveryFailure`)
4. For each group, run in parallel (reuse existing `errgroup` with `DeliveryParallelism` limit):
   - **Single item**: use `preFormatMessages()` cache (no change from current behavior)
   - **Multiple items**: call `formatter.formatGroupedMessage(ctx, memberName, channelID, kind, outboxItems)`
     - On format error -> fall back to individual dispatch (see Fallback Strategy)
   - `sender.SendMessage(ctx, roomID, message)` once per group
   - On success: all delivery IDs in group -> `successDeliveryIDs`
   - On failure: all delivery IDs in group -> `failureBuckets`

Room-level ordering: different kind groups targeting the same room may be dispatched in parallel, so message arrival order between groups (e.g., VIDEO group vs COMMUNITY group) is non-deterministic. This matches the current per-row behavior where ordering was already non-deterministic.

## Fallback Strategy

`formatGroupedMessage()` differs from single-item `formatMessage()` in error handling:
- Single `formatMessage()` has an internal fallback (`formatMessageFallback`) when renderer is nil or template render fails
- `formatGroupedMessage()` returns an error immediately if renderer is nil or template render fails
- `buildGroupedTemplateData()` silently produces empty `Title`/`URL` fields if payload JSON unmarshal fails

Payload validation before grouped formatting:
- Before calling `formatGroupedMessage()`, validate each outbox item's payload can be unmarshalled
- Items that fail payload validation are ejected from the group and dispatched individually via `preFormatMessages()` cache
- This prevents `buildGroupedTemplateData()` from silently producing empty `Title`/`URL`/`ContentText` fields
- If ejection reduces the group to 1 item, dispatch it as single-item (no grouped format needed)

Fallback behavior when `formatGroupedMessage()` returns an error (after payload validation):
1. Fall back to dispatching each item in the group individually using `preFormatMessages()` cache
2. Each individual item succeeds/fails independently
3. This ensures the feature is non-breaking even if group templates are missing from a channel's DB

## Template Convention

Existing group templates in migration `014-add-outbox-group-templates.sql`:

```
OUTBOX_VIDEO_GROUP:
{{range $idx, $item := .Items}}{{if gt $idx 0}}

{{end}}{{add $idx 1}}. {{$item.Title | truncate 40}}
   {{$item.URL}}{{end}}

OUTBOX_SHORTS_GROUP:  (same structure)
OUTBOX_COMMUNITY_GROUP: (uses $item.ContentText instead of $item.Title)
```

Header is prepended by `getGroupedTemplateKeyAndHeader` in `dispatcher_format.go`:
- Video: `📺 {MemberName} 새 영상 ({Count}개)`
- Shorts: `📱 {MemberName} 쇼츠 알림 ({Count}개)`
- Community: `📝 {MemberName} 커뮤니티 알림 ({Count}개)`

No template changes needed — templates and headers already exist.

## Files Changed

| File | Change |
|------|--------|
| `outbox/dispatcher_send.go` | Add `deliveryGroup` type, `groupDeliveryRows()` (milestone exclusion + orphan handling), `validateGroupPayloads()`, modify `dispatchDeliveryRows()` to dispatch per-group with fallback |
| `outbox/dispatcher_send_test.go` | Add grouped dispatch tests |

## Files NOT Changed

| File | Reason |
|------|--------|
| `outbox/dispatcher.go` | `groupOutboxItems()` remains as-is (may remove later as dead code cleanup) |
| `outbox/dispatcher_format.go` | `formatGroupedMessage()` used as-is |
| `outbox/dispatcher_claim.go` | enqueue path unchanged |
| `outbox/delivery_repository.go` | delivery row schema unchanged |
| Templates / migrations | Group templates already seeded |

## Success / Failure Semantics

- Group succeeds (send OK) -> all delivery IDs marked SENT
- Group fails (send error) -> all delivery IDs marked FAILED with retry
- Group format fails -> fallback to individual dispatch; each item succeeds/fails independently
- Orphan rows (outbox not found) -> fail individually (existing behavior)

## Testing Plan

1. **Unit test**: `groupDeliveryRows` — verify grouping by room+channel+kind, orphan handling, milestone kind always single-item
2. **Unit test**: `dispatchDeliveryRows` with grouped input — verify single `SendMessage` call per group, all delivery IDs in result
3. **Unit test**: fallback path — `formatGroupedMessage` fails, verify items fall back to individual send via `preFormatMessages` cache
4. **Unit test**: mixed batch — single-item groups + multi-item groups in same batch
5. **Unit test**: `successDeliveryIDs` and `touchedOutboxIDs` contain all rows/outboxes from group
6. **Unit test**: payload validation — item with broken JSON ejected from group, dispatched individually
7. **Existing tests**: must continue passing (single-item dispatch unchanged)

## Follow-up (out of scope)

- `groupOutboxItems()` in `dispatcher.go` is now fully superseded by `groupDeliveryRows()` — consider removal
