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

package dispatch

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type testSender struct {
	mu               sync.Mutex
	failRoom         map[string]bool
	messages         []string
	clientRequestIDs []string
}

type deliveryTestOutboxModel struct {
	ID            int64     `db:"id"`
	Kind          string    `db:"kind"`
	ChannelID     string    `db:"channel_id"`
	ContentID     string    `db:"content_id"`
	Payload       string    `db:"payload"`
	Status        string    `db:"status"`
	AttemptCount  int       `db:"attempt_count"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `db:"error"`
}

func (deliveryTestOutboxModel) TableName() string {
	return "youtube_notification_outbox"
}

type deliveryTestDeliveryModel struct {
	ID            int64     `db:"id"`
	OutboxID      int64     `db:"outbox_id"`
	RoomID        string    `db:"room_id"`
	Status        string    `db:"status"`
	AttemptCount  int       `db:"attempt_count"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `db:"error"`
}

func (deliveryTestDeliveryModel) TableName() string {
	return "youtube_notification_delivery"
}

func (s *testSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRoom[roomID] {
		return assert.AnError
	}
	s.messages = append(s.messages, roomID+":"+message)
	return nil
}

func (s *testSender) SendMessageWithClientRequestID(_ context.Context, roomID, message, clientRequestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRoom[roomID] {
		return assert.AnError
	}
	s.messages = append(s.messages, roomID+":"+message)
	s.clientRequestIDs = append(s.clientRequestIDs, clientRequestID)
	return nil
}

func TestEnqueueDeliveries_SubscriberLookupFailureSchedulesRetryBackoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	now := time.Now()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_lookup_fail",
		ContentID:     "test_lookup_fail",
		Payload:       `{"video_id":"vid1","title":"test-title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		LockedAt:      &now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, nil, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})

	dispatcher.claim.enqueueDeliveries(ctx, []domain.YouTubeNotificationOutbox{item}, map[string]channelAlarmRoomTargets{})

	var updated domain.YouTubeNotificationOutbox
	require.NoError(t, firstDeliveryTestRow(db, &updated, item.ID).Error)
	assert.Equal(t, domain.OutboxStatusPending, updated.Status)
	assert.Equal(t, 1, updated.AttemptCount)
	assert.Nil(t, updated.LockedAt)
	assert.WithinDuration(t, now.Add(time.Minute), updated.NextAttemptAt, time.Second)
	assert.Equal(t, "subscriber lookup failed", updated.Error)
}

func TestEnqueueDeliveries_NoSubscribersMarksSent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	now := time.Now()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_no_subscribers",
		ContentID:     "test_no_subscribers",
		Payload:       `{"video_id":"vid2","title":"test-title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		LockedAt:      &now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, nil, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})

	dispatcher.claim.enqueueDeliveries(ctx, []domain.YouTubeNotificationOutbox{item}, map[string]channelAlarmRoomTargets{
		item.ChannelID: {
			domain.AlarmTypeLive: {},
		},
	})

	var updated domain.YouTubeNotificationOutbox
	require.NoError(t, firstDeliveryTestRow(db, &updated, item.ID).Error)
	assert.Equal(t, domain.OutboxStatusSent, updated.Status)
}

func TestEnqueueDeliveries_UsesAlarmTypeSpecificRoomsForSameChannel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	now := time.Now()
	shortsItem := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_mixed_targets",
		ContentID:     "short-1",
		Payload:       `{"video_id":"short-1","title":"short-title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		LockedAt:      &now,
	}
	communityItem := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_mixed_targets",
		ContentID:     "post-1",
		Payload:       `{"post_id":"post-1","content_text":"community-title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		LockedAt:      &now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &shortsItem).Error)
	require.NoError(t, insertDeliveryTestRows(db, &communityItem).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, nil, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})

	dispatcher.claim.enqueueDeliveries(ctx, []domain.YouTubeNotificationOutbox{shortsItem, communityItem}, map[string]channelAlarmRoomTargets{
		shortsItem.ChannelID: {
			domain.AlarmTypeShorts:    {"room-shorts": true},
			domain.AlarmTypeCommunity: {"room-community": true},
		},
	})

	var rows []deliveryTestDeliveryModel
	require.NoError(t, findDeliveryTestRowsOrdered(db, &rows, "outbox_id ASC, room_id ASC").Error)
	require.Len(t, rows, 2)
	assert.Equal(t, shortsItem.ID, rows[0].OutboxID)
	assert.Equal(t, "room-shorts", rows[0].RoomID)
	assert.Equal(t, communityItem.ID, rows[1].OutboxID)
	assert.Equal(t, "room-community", rows[1].RoomID)
}

func TestDispatchDeliveryRows_CommunitySuccessSetsSentAtOnDeliveryAndOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	cacheClient := cachemocks.NewLenientClient()

	now := time.Now()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_community_sent_at",
		ContentID:     "post-community-sent-at",
		Payload:       `{"post_id":"post-community-sent-at","content_text":"community-title","published_at":"2026-04-10T01:11:12Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
		Kind:       string(item.Kind),
		ContentID:  item.ContentID,
		ChannelID:  item.ChannelID,
		DetectedAt: now,
	}).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-community",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cacheClient, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	result := dispatcher.send.dispatchDeliveryRows(ctx, []domain.YouTubeNotificationDelivery{delivery}, map[int64]domain.YouTubeNotificationOutbox{
		item.ID: item,
	})
	require.Equal(t, []int64{delivery.ID}, result.SuccessDeliveryIDs)
	require.Equal(t, []int64{item.ID}, result.TouchedOutboxIDs)
	require.NoError(t, dispatcher.claim.delivery.MarkSentBatch(ctx, result.SuccessDeliveryIDs))
	require.NoError(t, dispatcher.claim.delivery.UpdateOutboxAggregateStatuses(ctx, result.TouchedOutboxIDs))

	var updatedDelivery deliveryTestDeliveryModel
	require.NoError(t, firstDeliveryTestRow(db, &updatedDelivery, delivery.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedDelivery.Status)
	require.NotNil(t, updatedDelivery.SentAt)

	var updatedOutbox deliveryTestOutboxModel
	require.NoError(t, firstDeliveryTestRow(db, &updatedOutbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedOutbox.Status)
	require.NotNil(t, updatedOutbox.SentAt)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.messages[0], "room-community:📝 VTuber 커뮤니티 알림")
	assert.Contains(t, sender.messages[0], "community-title")
	assert.Contains(t, sender.messages[0], "https://www.youtube.com/post/post-community-sent-at")
}

func newDispatcherTestCache(t *testing.T) (*cache.Service, *miniredis.Miniredis) {
	t.Helper()

	mini := miniredis.RunT(t)
	host, rawPort, err := net.SplitHostPort(mini.Addr())
	require.NoError(t, err)

	port, err := strconv.Atoi(rawPort)
	require.NoError(t, err)

	service, err := cache.NewCacheService(context.Background(), cache.Config{
		Host:              host,
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	return service, mini
}
