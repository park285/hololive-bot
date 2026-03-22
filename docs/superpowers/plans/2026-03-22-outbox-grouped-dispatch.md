# Outbox Grouped Dispatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bundle same-room, same-channel, same-kind YouTube outbox delivery rows into a single message at dispatch time.

**Architecture:** Modify `dispatchDeliveryRows()` in `dispatcher_send.go` to group delivery rows before dispatch. Single-item groups use existing per-outbox formatting. Multi-item groups use the existing (currently dead) `formatGroupedMessage()`. Fallback to individual dispatch on grouped format failure.

**Tech Stack:** Go, GORM, errgroup, existing outbox dispatcher infrastructure

**Spec:** `docs/superpowers/specs/2026-03-22-outbox-grouped-dispatch-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go` | Modify | Add `deliveryGroup` type, `groupDeliveryRows()`, `validateOutboxPayload()`, rewrite `dispatchDeliveryRows()` to dispatch per-group |
| `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send_test.go` | Create | All tests for grouping, grouped dispatch, fallback, payload validation |

Existing files used but NOT modified:
- `dispatcher_format.go` — `formatGroupedMessage()`, `getMemberName()` called as-is
- `dispatcher.go` — `Dispatcher` struct, `deliveryParallelism()`
- `domain/youtube_content.go` — `OutboxKind` constants

---

### Task 1: Add `deliveryGroup` type and `groupDeliveryRows()` function

**Files:**
- Modify: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go`
- Create: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send_test.go`

- [ ] **Step 1: Write the failing test for `groupDeliveryRows`**

Create `dispatcher_send_test.go` with a table-driven test:

```go
package outbox

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestGroupDeliveryRows(t *testing.T) {
	t.Parallel()

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`},
		4: {ID: 4, ChannelID: "UCch2", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s3","title":"쇼츠3"}`},
		5: {ID: 5, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"100만"}`},
		6: {ID: 6, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"200만"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"}, // same room+channel+kind as 101 -> grouped
		{ID: 103, OutboxID: 3, RoomID: "room1"}, // same room, different kind -> separate group
		{ID: 104, OutboxID: 4, RoomID: "room1"}, // same room, different channel -> separate group
		{ID: 105, OutboxID: 1, RoomID: "room2"}, // different room -> separate group
		{ID: 106, OutboxID: 99, RoomID: "room1"}, // orphan (outbox not in map)
		{ID: 107, OutboxID: 5, RoomID: "room1"}, // milestone -> always single
		{ID: 108, OutboxID: 6, RoomID: "room1"}, // milestone -> always single (not grouped with 107)
	}

	groups, orphans := groupDeliveryRows(rows, outboxByID)

	// orphan check
	if len(orphans) != 1 || orphans[0].ID != 106 {
		t.Fatalf("orphans = %+v, want [{ID:106}]", orphans)
	}

	// group count: (room1+UCch1+SHORT=2), (room1+UCch1+VIDEO=1), (room1+UCch2+SHORT=1), (room2+UCch1+SHORT=1), (room1+milestone1), (room1+milestone2)
	if len(groups) != 6 {
		t.Fatalf("group count = %d, want 6", len(groups))
	}

	// find the grouped shorts
	var shortsGroup *deliveryGroup
	for i := range groups {
		if groups[i].roomID == "room1" && groups[i].channelID == "UCch1" && groups[i].kind == domain.OutboxKindNewShort {
			shortsGroup = &groups[i]
			break
		}
	}
	if shortsGroup == nil {
		t.Fatalf("shorts group for room1+UCch1 not found")
	}
	if len(shortsGroup.rows) != 2 {
		t.Fatalf("shorts group row count = %d, want 2", len(shortsGroup.rows))
	}
	if len(shortsGroup.outboxes) != 2 {
		t.Fatalf("shorts group outbox count = %d, want 2", len(shortsGroup.outboxes))
	}

	// milestones are always single
	milestoneCount := 0
	for _, g := range groups {
		if g.kind == domain.OutboxKindMilestone {
			milestoneCount++
			if len(g.rows) != 1 {
				t.Fatalf("milestone group should be single-item, got %d rows", len(g.rows))
			}
		}
	}
	if milestoneCount != 2 {
		t.Fatalf("milestone group count = %d, want 2", milestoneCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd hololive/hololive-shared && go test -v -run TestGroupDeliveryRows ./pkg/service/youtube/outbox/`
Expected: FAIL — `groupDeliveryRows` undefined

- [ ] **Step 3: Implement `deliveryGroup` type and `groupDeliveryRows()`**

Add to `dispatcher_send.go` after the `deliveryDispatchResult` type:

```go
// deliveryGroup: dispatch 시점 동일 room+channel+kind delivery row 그룹
type deliveryGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	rows      []domain.YouTubeNotificationDelivery
	outboxes  []domain.YouTubeNotificationOutbox
}

// groupDeliveryRows: delivery row를 room+channel+kind 기준으로 그룹핑한다.
// milestone kind는 그룹핑 제외 (항상 단건 그룹).
// outbox를 찾을 수 없는 row는 orphanRows로 반환한다.
func groupDeliveryRows(
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (groups []deliveryGroup, orphanRows []domain.YouTubeNotificationDelivery) {
	if len(rows) == 0 {
		return nil, nil
	}

	index := make(map[string]int)
	groups = make([]deliveryGroup, 0, len(rows))

	for i := range rows {
		row := rows[i]
		outbox, ok := outboxByID[row.OutboxID]
		if !ok {
			orphanRows = append(orphanRows, row)
			continue
		}

		// milestone은 그룹 템플릿 미지원 -> 항상 단건 그룹
		if outbox.Kind == domain.OutboxKindMilestone {
			groups = append(groups, deliveryGroup{
				roomID:    row.RoomID,
				channelID: outbox.ChannelID,
				kind:      outbox.Kind,
				rows:      []domain.YouTubeNotificationDelivery{row},
				outboxes:  []domain.YouTubeNotificationOutbox{outbox},
			})
			continue
		}

		key := row.RoomID + "|" + outbox.ChannelID + "|" + string(outbox.Kind)
		if idx, exists := index[key]; exists {
			groups[idx].rows = append(groups[idx].rows, row)
			groups[idx].outboxes = append(groups[idx].outboxes, outbox)
			continue
		}

		index[key] = len(groups)
		groups = append(groups, deliveryGroup{
			roomID:    row.RoomID,
			channelID: outbox.ChannelID,
			kind:      outbox.Kind,
			rows:      []domain.YouTubeNotificationDelivery{row},
			outboxes:  []domain.YouTubeNotificationOutbox{outbox},
		})
	}

	return groups, orphanRows
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd hololive/hololive-shared && go test -v -run TestGroupDeliveryRows ./pkg/service/youtube/outbox/`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat(outbox): add deliveryGroup type and groupDeliveryRows function

Groups delivery rows by room+channel+kind for bundled dispatch.
Milestone kind is excluded from grouping (always single-item).
Orphan rows (missing outbox) returned separately.
```

---

### Task 2: Add `validateOutboxPayload()` function

**Files:**
- Modify: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go`
- Modify: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send_test.go`

- [ ] **Step 1: Write the failing test**

Append to `dispatcher_send_test.go`:

```go
func TestValidateOutboxPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		item    domain.YouTubeNotificationOutbox
		wantOK  bool
	}{
		{
			name:   "valid video",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"t"}`},
			wantOK: true,
		},
		{
			name:   "valid short",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"t"}`},
			wantOK: true,
		},
		{
			name:   "valid community",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindCommunityPost, Payload: `{"post_id":"p1","content_text":"c"}`},
			wantOK: true,
		},
		{
			name:   "invalid json",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewVideo, Payload: `{broken`},
			wantOK: false,
		},
		{
			name:   "milestone always valid",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"100만"}`},
			wantOK: true,
		},
		{
			name:   "unknown kind",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKind("UNKNOWN"), Payload: `{}`},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validateOutboxPayload(tt.item); got != tt.wantOK {
				t.Fatalf("validateOutboxPayload() = %v, want %v", got, tt.wantOK)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd hololive/hololive-shared && go test -v -run TestValidateOutboxPayload ./pkg/service/youtube/outbox/`
Expected: FAIL — `validateOutboxPayload` undefined

- [ ] **Step 3: Implement `validateOutboxPayload()`**

Add to `dispatcher_send.go`. Uses the same payload structs from `dispatcher_format.go` (same package):

```go
// validateOutboxPayload: outbox payload가 정상 파싱 가능한지 검증한다.
// grouped format 전에 호출하여 빈 Title/URL 방지.
func validateOutboxPayload(item domain.YouTubeNotificationOutbox) bool {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		var p videoPayload
		return json.Unmarshal([]byte(item.Payload), &p) == nil
	case domain.OutboxKindCommunityPost:
		var p communityPayload
		return json.Unmarshal([]byte(item.Payload), &p) == nil
	default:
		return true
	}
}
```

Add the `json` import to the existing import block in `dispatcher_send.go`:

```go
import (
	"context"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd hololive/hololive-shared && go test -v -run TestValidateOutboxPayload ./pkg/service/youtube/outbox/`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat(outbox): add validateOutboxPayload for pre-group payload check

Validates that outbox payload can be unmarshalled before grouped
formatting. Prevents buildGroupedTemplateData from silently producing
empty Title/URL/ContentText fields.
```

---

### Task 3: Rewrite `dispatchDeliveryRows()` for grouped dispatch

**Files:**
- Modify: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go`

- [ ] **Step 1: Replace `dispatchDeliveryRows()` with grouped version**

Replace the existing `dispatchDeliveryRows` function body. Keep the existing `dispatchDeliveryRow`, `recordDeliveryFailure`, `preFormatMessages`, and `deliveryParallelism` functions unchanged.

```go
func (d *Dispatcher) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) deliveryDispatchResult {
	result := deliveryDispatchResult{
		successDeliveryIDs: make([]int64, 0, len(rows)),
		touchedOutboxIDs:   make([]int64, 0, len(rows)),
		failureBuckets:     make(map[string][]int64),
	}
	var mu sync.Mutex

	formattedMessages, formatFailures := d.preFormatMessages(ctx, outboxByID)

	groups, orphanRows := groupDeliveryRows(rows, outboxByID)

	// orphan row 처리
	for i := range orphanRows {
		d.recordDeliveryFailure(&result, &mu, "outbox row not found", orphanRows[i].ID, orphanRows[i].OutboxID)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.deliveryParallelism())

	for i := range groups {
		group := groups[i]
		eg.Go(func() error {
			d.dispatchGroup(egCtx, group, formattedMessages, formatFailures, &result, &mu)
			return nil
		})
	}
	_ = eg.Wait()

	return result
}

func (d *Dispatcher) dispatchGroup(
	ctx context.Context,
	group deliveryGroup,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	// 단건 그룹: 기존 개별 dispatch 경로
	if len(group.rows) == 1 {
		d.dispatchDeliveryRow(ctx, group.rows[0], formattedMessages, formatFailures, result, mu)
		return
	}

	// 복수건: payload 검증 -> 유효 항목만 그룹 포맷
	var validOutboxes []domain.YouTubeNotificationOutbox
	var validRows []domain.YouTubeNotificationDelivery
	var invalidRows []domain.YouTubeNotificationDelivery

	for i := range group.outboxes {
		if validateOutboxPayload(group.outboxes[i]) {
			validOutboxes = append(validOutboxes, group.outboxes[i])
			validRows = append(validRows, group.rows[i])
		} else {
			invalidRows = append(invalidRows, group.rows[i])
		}
	}

	// payload 검증 실패 항목 -> 개별 dispatch
	for i := range invalidRows {
		d.dispatchDeliveryRow(ctx, invalidRows[i], formattedMessages, formatFailures, result, mu)
	}

	// 검증 후 1건 이하 -> 개별 dispatch
	if len(validRows) <= 1 {
		for i := range validRows {
			d.dispatchDeliveryRow(ctx, validRows[i], formattedMessages, formatFailures, result, mu)
		}
		return
	}

	// 그룹 포맷 시도
	memberName, err := d.formatter.getMemberName(ctx, group.channelID)
	if err != nil || memberName == "" {
		memberName = "VTuber"
	}

	message, err := d.formatter.formatGroupedMessage(ctx, memberName, group.channelID, group.kind, validOutboxes)
	if err != nil {
		d.logger.Warn("Grouped format failed, falling back to individual dispatch",
			slog.String("room_id", group.roomID),
			slog.String("channel_id", group.channelID),
			slog.String("kind", string(group.kind)),
			slog.Int("count", len(validRows)),
			slog.Any("error", err))
		for i := range validRows {
			d.dispatchDeliveryRow(ctx, validRows[i], formattedMessages, formatFailures, result, mu)
		}
		return
	}

	// 그룹 메시지 전송
	if sendErr := d.sender.SendMessage(ctx, group.roomID, message); sendErr != nil {
		d.logger.Warn("Failed to send grouped delivery",
			slog.String("room_id", group.roomID),
			slog.String("kind", string(group.kind)),
			slog.Int("count", len(validRows)),
			slog.Any("error", sendErr))
		for i := range validRows {
			d.recordDeliveryFailure(result, mu, "send message", validRows[i].ID, validRows[i].OutboxID)
		}
		return
	}

	// 성공: 그룹 내 모든 delivery ID 성공 처리
	mu.Lock()
	for i := range validRows {
		result.successDeliveryIDs = append(result.successDeliveryIDs, validRows[i].ID)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, validRows[i].OutboxID)
	}
	mu.Unlock()
}
```

- [ ] **Step 2: Verify build compiles**

Run: `cd hololive/hololive-shared && go build ./pkg/service/youtube/outbox/`
Expected: success

- [ ] **Step 3: Run existing tests to verify no regression**

Run: `cd hololive/hololive-shared && go test -v ./pkg/service/youtube/outbox/`
Expected: all existing tests PASS

- [ ] **Step 4: Commit**

```
feat(outbox): rewrite dispatchDeliveryRows for grouped dispatch

Groups delivery rows by room+channel+kind before sending.
Single-item groups use existing per-outbox formatting.
Multi-item groups use formatGroupedMessage() with payload
validation and fallback to individual dispatch on failure.
```

---

### Task 4: Add grouped dispatch tests

**Files:**
- Modify: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send_test.go`

**Important context for tests:**
- `testSender` and `newDispatcherTestCache()` are defined in `dispatcher_partial_failure_test.go` (same package `outbox`)
- Use `NewDispatcher(db, cacheSvc, sender, renderer, logger, cfg)` — NOT direct struct init
- `getMemberName()` calls `cache.HGet("alarm:member_names", channelID)` — seed the cache in tests
- renderer=nil means `formatGroupedMessage()` returns error immediately (fallback path)
- For grouped success testing, a real renderer is needed but complex to set up; instead verify fallback correctness and grouped format error handling

- [ ] **Step 1: Add imports and test helper at top of `dispatcher_send_test.go`**

Add these imports to the existing import block in `dispatcher_send_test.go`:

```go
import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)
```

Add a helper to create a test dispatcher without DB (for unit-level dispatch tests):

```go
func newTestDispatcherForSend(t *testing.T, sender *testSender) *Dispatcher {
	t.Helper()

	cacheSvc, mini := newDispatcherTestCache(t)
	t.Cleanup(func() {
		mini.Close()
		_ = cacheSvc.Close()
	})

	return NewDispatcher(nil, cacheSvc, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})
}
```

- [ ] **Step 2: Add test for grouped fallback (renderer nil) — verifies all items dispatched individually**

```go
func TestDispatchDeliveryRows_GroupedFallback(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	// renderer nil -> grouped format fails -> fallback to individual dispatch
	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %d, want 2", len(result.successDeliveryIDs))
	}

	// sender receives 2 individual messages (fallback path)
	sender.mu.Lock()
	msgCount := len(sender.messages)
	sender.mu.Unlock()
	if msgCount != 2 {
		t.Fatalf("sender message count = %d, want 2 (fallback)", msgCount)
	}
}
```

- [ ] **Step 3: Add test for orphan rows**

```go
func TestDispatchDeliveryRows_OrphanRows(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 99, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, map[int64]domain.YouTubeNotificationOutbox{})

	if result.failedDeliveries != 1 {
		t.Fatalf("failedDeliveries = %d, want 1", result.failedDeliveries)
	}
	if ids, ok := result.failureBuckets["outbox row not found"]; !ok || len(ids) != 1 || ids[0] != 101 {
		t.Fatalf("unexpected failure buckets: %+v", result.failureBuckets)
	}
}
```

- [ ] **Step 4: Add test for payload validation ejection**

```go
func TestDispatchDeliveryRows_PayloadValidationEjection(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"ok"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{broken json`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s3","title":"ok"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
		{ID: 103, OutboxID: 3, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	// ID 102 has invalid payload -> ejected from group -> individual dispatch
	// preFormatMessages also fails for broken JSON -> formatFailures -> failure
	totalProcessed := len(result.successDeliveryIDs) + result.failedDeliveries
	if totalProcessed != 3 {
		t.Fatalf("total processed = %d, want 3", totalProcessed)
	}
	if result.failedDeliveries != 1 {
		t.Fatalf("failedDeliveries = %d, want 1 (broken payload)", result.failedDeliveries)
	}
	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs count = %d, want 2", len(result.successDeliveryIDs))
	}
}
```

- [ ] **Step 5: Add test for mixed batch (single + multi groups)**

```go
func TestDispatchDeliveryRows_MixedBatch(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`},
		4: {ID: 4, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"100만"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
		{ID: 103, OutboxID: 3, RoomID: "room1"},
		{ID: 104, OutboxID: 4, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 4 {
		t.Fatalf("successDeliveryIDs = %d, want 4", len(result.successDeliveryIDs))
	}
	if len(result.touchedOutboxIDs) != 4 {
		t.Fatalf("touchedOutboxIDs = %d, want 4", len(result.touchedOutboxIDs))
	}
}
```

- [ ] **Step 6: Add test for send failure (all items in group fail)**

```go
func TestDispatchDeliveryRows_SendFailure(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{"room1": true}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	// renderer nil -> fallback to individual -> both fail (room1 fail)
	if result.failedDeliveries != 2 {
		t.Fatalf("failedDeliveries = %d, want 2", result.failedDeliveries)
	}
	if len(result.successDeliveryIDs) != 0 {
		t.Fatalf("successDeliveryIDs = %d, want 0", len(result.successDeliveryIDs))
	}
}
```

- [ ] **Step 7: Run all tests**

Run: `cd /home/kapu/gemini/hololive-bot/hololive/hololive-shared && go test -v ./pkg/service/youtube/outbox/`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```
test(outbox): add grouped dispatch tests

Covers: grouped fallback (renderer nil), orphan rows, payload
validation ejection, mixed batch (single+multi groups), and
send failure propagation.
```

---

### Task 5: Run full test suite and verify

**Files:** None (verification only)

- [ ] **Step 1: Run full outbox package tests**

Run: `cd /home/kapu/gemini/hololive-bot/hololive/hololive-shared && go test -v -count=1 ./pkg/service/youtube/outbox/`
Expected: ALL PASS (existing + new tests)

- [ ] **Step 2: Run workspace-wide build**

Run: `cd /home/kapu/gemini/hololive-bot && go build ./...`
Expected: success

- [ ] **Step 3: Run workspace-wide tests**

Run: `cd /home/kapu/gemini/hololive-bot && go test ./...`
Expected: ALL PASS
