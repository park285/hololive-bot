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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
)

// reviveTestClaimManager는 reviveStaleFailedOutbox만 행사하는 최소 ClaimManager를 만든다.
func reviveTestClaimManager(db *deliveryTestDB) *ClaimManager {
	return &ClaimManager{
		db:     store.AsDeliveryDB(db),
		config: Config{MaxRetries: 3, LockTimeout: 5 * time.Minute},
		logger: slog.Default(),
	}
}

func TestReviveStaleFailedOutbox_RevivesFreshNeverSentAndPreservesDelivered(t *testing.T) {
	db := newDeliveryPool(t)
	cm := reviveTestClaimManager(db)
	ctx := context.Background()

	now := time.Now().UTC()
	staleCreatedAt := now.Add(-2 * time.Hour)
	freshCreatedAt := now.Add(-5 * time.Minute)
	oldNextAttempt := now.Add(-30 * time.Minute)
	sentAt := now.Add(-20 * time.Minute)
	recentLock := now.Add(-1 * time.Minute)

	newFailedOutbox := func(kind domain.OutboxKind, contentID string, createdAt time.Time) *domain.YouTubeNotificationOutbox {
		row := &domain.YouTubeNotificationOutbox{
			Kind: kind, ChannelID: "ch-1", ContentID: contentID,
			Payload: `{"id":"` + contentID + `"}`, Status: domain.OutboxStatusFailed,
			AttemptCount: 3, NextAttemptAt: oldNextAttempt, CreatedAt: createdAt, Error: "failed",
		}
		require.NoError(t, insertDeliveryTestRows(db, row).Error)
		return row
	}

	freshVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-fresh", freshCreatedAt)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: freshVideo.ID, RoomID: "room-sent", Status: domain.OutboxStatusSent,
		AttemptCount: 1, NextAttemptAt: oldNextAttempt, SentAt: &sentAt,
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: freshVideo.ID, RoomID: "room-failed", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, Error: "send failed",
	}).Error)

	freshLive := newFailedOutbox(domain.OutboxKindLiveStream, "live-fresh", freshCreatedAt)

	freshMilestone := newFailedOutbox(domain.OutboxKindMilestone, "ms-fresh", freshCreatedAt)

	zeroDeliveryVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-nodelivery", freshCreatedAt)

	staleVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-stale", staleCreatedAt)

	deliveredVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-delivered", freshCreatedAt)
	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, map[string]any{"sent_at": sentAt}, "id = ?", deliveredVideo.ID).Error)

	lockedVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-locked", freshCreatedAt)
	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, map[string]any{"locked_at": recentLock}, "id = ?", lockedVideo.ID).Error)

	freshCommunity := newFailedOutbox(domain.OutboxKindCommunityPost, "post-fresh", freshCreatedAt)

	revived, err := cm.reviveStaleFailedOutbox(ctx, 60*time.Minute, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(4), revived, "video+live+milestone+zero-delivery video 4건만 revive")

	assertRevived := func(id int64, label string) {
		var row domain.YouTubeNotificationOutbox
		require.NoError(t, firstDeliveryTestRowWhere(db, &row, "id = ?", id).Error)
		assert.Equal(t, domain.OutboxStatusPending, row.Status, label+" → PENDING")
		assert.Zero(t, row.AttemptCount, label+" attempt 리셋")
		assert.Empty(t, row.Error, label+" error clear")
		assert.True(t, row.NextAttemptAt.After(oldNextAttempt), label+" next_attempt 전진")
		assert.Nil(t, row.LockedAt)
	}
	assertNotRevived := func(id int64, label string) {
		var row domain.YouTubeNotificationOutbox
		require.NoError(t, firstDeliveryTestRowWhere(db, &row, "id = ?", id).Error)
		assert.Equal(t, domain.OutboxStatusFailed, row.Status, label+" → FAILED 유지")
	}

	assertRevived(freshVideo.ID, "freshVideo")
	assertRevived(freshLive.ID, "freshLive")
	assertRevived(freshMilestone.ID, "freshMilestone")
	assertRevived(zeroDeliveryVideo.ID, "zeroDeliveryVideo")
	assertNotRevived(staleVideo.ID, "staleVideo")
	assertNotRevived(deliveredVideo.ID, "deliveredVideo")
	assertNotRevived(lockedVideo.ID, "lockedVideo")
	assertNotRevived(freshCommunity.ID, "freshCommunity(스코프 밖)")

	// per-room dedup: SENT 행 불변, FAILED 행만 PENDING.
	var sentDelivery domain.YouTubeNotificationDelivery
	require.NoError(t, firstDeliveryTestRowWhere(db, &sentDelivery, "outbox_id = ? AND room_id = ?", freshVideo.ID, "room-sent").Error)
	assert.Equal(t, domain.OutboxStatusSent, sentDelivery.Status, "이미 발송된 room은 재발송 금지")
	require.NotNil(t, sentDelivery.SentAt)

	var failedDelivery domain.YouTubeNotificationDelivery
	require.NoError(t, firstDeliveryTestRowWhere(db, &failedDelivery, "outbox_id = ? AND room_id = ?", freshVideo.ID, "room-failed").Error)
	assert.Equal(t, domain.OutboxStatusPending, failedDelivery.Status, "실패한 room은 재시도 대상")
	assert.Zero(t, failedDelivery.AttemptCount)
}

// TestReviveStaleFailedOutbox_RevivedRowIsActuallyRedelivered는 revive가 "theater"가 아님을 증명한다:
// revive 전에는 dispatcher가 FAILED 행을 재전달하지 않지만, revive 후 ProcessOnce가 실제로 실패했던
// room에 메시지를 발송하고 delivery 행이 SENT로 전이된다(end-to-end revive→dispatch 경로 검증).
func TestReviveStaleFailedOutbox_RevivedRowIsActuallyRedelivered(t *testing.T) {
	db := newDeliveryPool(t)
	ctx := context.Background()

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
			BatchSize:             10,
			LockTimeout:           time.Minute,
			PollInterval:          time.Second,
			MaxRetries:            3,
			RetryBackoff:          time.Minute,
			DeliveryParallelism:   1,
			ReviveEnabled:         true,
			ReviveInterval:        time.Minute,
			ReviveFreshnessWindow: time.Hour,
		})

	now := time.Now().UTC()
	outboxRow := &domain.YouTubeNotificationOutbox{
		Kind: domain.OutboxKindNewVideo, ChannelID: "UCe2e", ContentID: "video-e2e",
		Payload: `{"video_id":"video-e2e","title":"E2E"}`, Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: now.Add(-10 * time.Minute), CreatedAt: now.Add(-5 * time.Minute),
		Error: "all rooms failed",
	}
	require.NoError(t, insertDeliveryTestRows(db, outboxRow).Error)
	deliveryRow := &domain.YouTubeNotificationDelivery{
		OutboxID: outboxRow.ID, RoomID: "room-x", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: now.Add(-10 * time.Minute), Error: "send failed",
	}
	require.NoError(t, insertDeliveryTestRows(db, deliveryRow).Error)

	// revive 전: FAILED 행은 claim 대상이 아니므로 재전달 없음.
	dispatcher.ProcessOnceForTest(ctx)
	require.Empty(t, senderMessages(sender), "revive 전엔 FAILED 행이 재전달되지 않아야 함")

	// revive → dispatch.
	dispatcher.reviveOnce(ctx)
	dispatcher.ProcessOnceForTest(ctx)

	msgs := senderMessages(sender)
	require.Len(t, msgs, 1, "revive된 행이 실제로 재전달되어야 함(theater 아님)")
	assert.Contains(t, msgs[0], "room-x")

	var updated domain.YouTubeNotificationDelivery
	require.NoError(t, firstDeliveryTestRowWhere(db, &updated, "id = ?", deliveryRow.ID).Error)
	assert.Equal(t, domain.OutboxStatusSent, updated.Status, "재전달 후 delivery 행은 SENT")
}

func senderMessages(s *testSender) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...)
}
