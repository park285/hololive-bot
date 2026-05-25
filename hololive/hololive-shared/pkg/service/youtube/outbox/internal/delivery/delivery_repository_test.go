// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package delivery

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUniqueStrings(t *testing.T) {
	t.Parallel()

	in := []string{"room1", "room2", "room1", "room3", "room2"}
	got := uniqueStrings(in)
	want := []string{"room1", "room2", "room3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueStrings() = %#v, want %#v", got, want)
	}

	if out := uniqueStrings(nil); out != nil {
		t.Fatalf("uniqueStrings(nil) = %#v, want nil", out)
	}
}

func TestResolveOutboxStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pending int64
		sent    int64
		failed  int64
		want    domain.OutboxStatus
	}{
		{name: "pending has priority", pending: 1, sent: 10, failed: 10, want: domain.OutboxStatusPending},
		{name: "failed when mixed sent and failed", pending: 0, sent: 1, failed: 9, want: domain.OutboxStatusFailed},
		{name: "sent when only sent", pending: 0, sent: 1, failed: 0, want: domain.OutboxStatusSent},
		{name: "failed when only failed", pending: 0, sent: 0, failed: 2, want: domain.OutboxStatusFailed},
		{name: "empty fallback pending", pending: 0, sent: 0, failed: 0, want: domain.OutboxStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveOutboxStatus(tt.pending, tt.sent, tt.failed)
			if got != tt.want {
				t.Fatalf("resolveOutboxStatus() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseStatusCounts(t *testing.T) {
	t.Parallel()

	counts := []deliveryStatusCount{
		{Status: domain.OutboxStatusPending, Count: 3},
		{Status: domain.OutboxStatusSent, Count: 5},
		{Status: domain.OutboxStatusFailed, Count: 1},
	}

	pending, sent, failed := parseStatusCounts(counts)
	if pending != 3 || sent != 5 || failed != 1 {
		t.Fatalf("parseStatusCounts() = (%d,%d,%d), want (3,5,1)", pending, sent, failed)
	}
}

func TestGroupOutboxIDsByAggregateStatus(t *testing.T) {
	t.Parallel()

	grouped := groupOutboxIDsByAggregateStatus([]int64{1, 2, 3}, []deliveryStatusCount{
		{OutboxID: 1, Status: domain.OutboxStatusSent, Count: 2},
		{OutboxID: 2, Status: domain.OutboxStatusPending, Count: 1},
		{OutboxID: 2, Status: domain.OutboxStatusSent, Count: 1},
		{OutboxID: 3, Status: domain.OutboxStatusFailed, Count: 1},
	})

	if !reflect.DeepEqual(grouped[domain.OutboxStatusSent], []int64{1}) {
		t.Fatalf("sent group = %#v, want [1]", grouped[domain.OutboxStatusSent])
	}
	if !reflect.DeepEqual(grouped[domain.OutboxStatusPending], []int64{2}) {
		t.Fatalf("pending group = %#v, want [2]", grouped[domain.OutboxStatusPending])
	}
	if !reflect.DeepEqual(grouped[domain.OutboxStatusFailed], []int64{3}) {
		t.Fatalf("failed group = %#v, want [3]", grouped[domain.OutboxStatusFailed])
	}
}

func TestDispatchDeliveryRows_CapturesSuccessAndFailureBuckets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, mini := newDispatcherTestCache(t)
	defer mini.Close()
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close cache service: %v", err)
		}
	}()

	dispatcher := NewDispatcher(nil, cache, &testSender{
		failRoom: map[string]bool{"room-fail": true},
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		DeliveryParallelism: 1,
	})

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 1, OutboxID: 100, RoomID: "room-ok"},
		{ID: 2, OutboxID: 100, RoomID: "room-fail"},
		{ID: 3, OutboxID: 999, RoomID: "room-missing"},
	}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		100: {
			ID:            100,
			Kind:          domain.OutboxKindNewVideo,
			ChannelID:     "UC_dispatch_delivery",
			ContentID:     "video_dispatch_delivery",
			Payload:       `{"video_id":"vid1","title":"dispatch test"}`,
			Status:        domain.OutboxStatusPending,
			AttemptCount:  0,
			NextAttemptAt: time.Now(),
		},
	}

	result := dispatcher.send.dispatchDeliveryRows(ctx, rows, outboxByID)

	if !reflect.DeepEqual(result.successDeliveryIDs, []int64{1}) {
		t.Fatalf("successDeliveryIDs = %#v, want []int64{1}", result.successDeliveryIDs)
	}
	if result.failedDeliveries != 2 {
		t.Fatalf("failedDeliveries = %d, want 2", result.failedDeliveries)
	}
	if !reflect.DeepEqual(result.failureBuckets["send message"], []int64{2}) {
		t.Fatalf("send message failures = %#v, want []int64{2}", result.failureBuckets["send message"])
	}
	if !reflect.DeepEqual(result.failureBuckets["outbox row not found"], []int64{3}) {
		t.Fatalf("outbox row not found failures = %#v, want []int64{3}", result.failureBuckets["outbox row not found"])
	}
	wantTouched := []int64{100, 100, 999}
	gotTouched := make([]int64, len(result.touchedOutboxIDs))
	copy(gotTouched, result.touchedOutboxIDs)
	slices.Sort(gotTouched)
	slices.Sort(wantTouched)
	if !reflect.DeepEqual(gotTouched, wantTouched) {
		t.Fatalf("touchedOutboxIDs (sorted) = %#v, want %#v", gotTouched, wantTouched)
	}
}

func TestDeliveryRepositoryMarkFailedRetryBatchIfLockedSkipsRowsRelockedByAnotherWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteDeliveryModel{}))

	staleLockedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	currentLockedAt := staleLockedAt.Add(time.Minute)
	row := domain.YouTubeNotificationDelivery{
		OutboxID:      10,
		RoomID:        "room-relocked",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now().UTC(),
		LockedAt:      &currentLockedAt,
	}
	require.NoError(t, db.Create(&row).Error)

	repository := NewDeliveryRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err = repository.MarkFailedRetryBatchIfLocked(ctx, []deliveryLockToken{{id: row.ID, lockedAt: &staleLockedAt}}, 3, time.Minute, "stale failure")
	require.NoError(t, err)

	var got domain.YouTubeNotificationDelivery
	require.NoError(t, db.First(&got, row.ID).Error)
	require.Equal(t, domain.OutboxStatusPending, got.Status)
	require.Equal(t, 0, got.AttemptCount)
	require.NotNil(t, got.LockedAt)
	require.True(t, got.LockedAt.Equal(currentLockedAt), "locked_at = %s, want %s", got.LockedAt, currentLockedAt)
	require.Empty(t, got.Error)
}

func TestDeliveryRepositoryMarkFailedRetryBatchIfLockedSkipsRowsCompletedByAnotherWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteDeliveryModel{}))

	staleLockedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	sentAt := time.Now().UTC()
	row := domain.YouTubeNotificationDelivery{
		OutboxID:      11,
		RoomID:        "room-sent",
		Status:        domain.OutboxStatusSent,
		AttemptCount:  0,
		NextAttemptAt: time.Now().UTC(),
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&row).Error)

	repository := NewDeliveryRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err = repository.MarkFailedRetryBatchIfLocked(ctx, []deliveryLockToken{{id: row.ID, lockedAt: &staleLockedAt}}, 3, time.Minute, "stale failure")
	require.NoError(t, err)

	var got domain.YouTubeNotificationDelivery
	require.NoError(t, db.First(&got, row.ID).Error)
	require.Equal(t, domain.OutboxStatusSent, got.Status)
	require.Equal(t, 0, got.AttemptCount)
	require.Nil(t, got.LockedAt)
	require.NotNil(t, got.SentAt)
	require.Empty(t, got.Error)
}
