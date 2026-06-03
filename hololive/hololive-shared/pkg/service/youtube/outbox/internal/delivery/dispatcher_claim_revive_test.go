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
		db:     store.AsDeliveryDB(db.Pool),
		config: Config{MaxRetries: 3, LockTimeout: 5 * time.Minute},
		logger: slog.Default(),
	}
}

// TestReviveStaleFailedOutbox_RevivesFreshNeverSentAndPreservesDelivered는 stale-failed revival
// sweep의 핵심 계약을 고정한다. 전송 실패로 영구 FAILED된 fresh·미발송 알람을 PENDING으로 되살리되:
//   - 대상 kind는 per-post alarm-once 게이트를 우회하는 NEW_VIDEO/LIVE_STREAM/MILESTONE만(community/shorts는
//     되살려도 dispatch에서 alarm_sent_at 게이트로 skip되므로 제외 — 자체 ON CONFLICT 경로가 담당).
//   - 이미 SENT된 per-room delivery 행은 건드리지 않는다(중복 발송 방지).
//   - stale(freshness window 밖)·이미 발송 완료(sent_at NOT NULL)·in-flight(locked_at 미만료) 행은 제외.
//   - delivery 행이 없는 직접 FAILED outbox(구독자 조회/enqueue 실패 경로)도 되살린다.
func TestReviveStaleFailedOutbox_RevivesFreshNeverSentAndPreservesDelivered(t *testing.T) {
	db := newDeliveryTestDB(t)
	cm := reviveTestClaimManager(db)
	ctx := context.Background()

	now := time.Now().UTC()
	staleCreatedAt := now.Add(-2 * time.Hour)
	freshCreatedAt := now.Add(-5 * time.Minute)
	oldNextAttempt := now.Add(-30 * time.Minute)
	sentAt := now.Add(-20 * time.Minute)
	recentLock := now.Add(-1 * time.Minute) // LockTimeout(5m) 안 → in-flight로 간주

	newFailedOutbox := func(kind domain.OutboxKind, contentID string, createdAt time.Time) *domain.YouTubeNotificationOutbox {
		row := &domain.YouTubeNotificationOutbox{
			Kind: kind, ChannelID: "ch-1", ContentID: contentID,
			Payload: `{"id":"` + contentID + `"}`, Status: domain.OutboxStatusFailed,
			AttemptCount: 3, NextAttemptAt: oldNextAttempt, CreatedAt: createdAt, Error: "failed",
		}
		require.NoError(t, db.Create(row).Error)
		return row
	}

	// (1) fresh·미발송·FAILED video — 되살아나야 함. room-A는 SENT(중복 방지), room-B는 FAILED(재시도).
	freshVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-fresh", freshCreatedAt)
	require.NoError(t, db.Create(&domain.YouTubeNotificationDelivery{
		OutboxID: freshVideo.ID, RoomID: "room-sent", Status: domain.OutboxStatusSent,
		AttemptCount: 1, NextAttemptAt: oldNextAttempt, SentAt: &sentAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationDelivery{
		OutboxID: freshVideo.ID, RoomID: "room-failed", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, Error: "send failed",
	}).Error)

	// (2) fresh·미발송·FAILED live_stream — 대상 kind, 되살아나야 함.
	freshLive := newFailedOutbox(domain.OutboxKindLiveStream, "live-fresh", freshCreatedAt)

	// (3) fresh·미발송·FAILED milestone — 대상 kind, 되살아나야 함.
	freshMilestone := newFailedOutbox(domain.OutboxKindMilestone, "ms-fresh", freshCreatedAt)

	// (4) delivery 행 없는 직접 FAILED video(구독자 조회/enqueue 실패 경로) — 되살아나야 함.
	zeroDeliveryVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-nodelivery", freshCreatedAt)

	// (5) stale video — freshness window 밖이라 제외.
	staleVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-stale", staleCreatedAt)

	// (6) FAILED지만 이미 발송 완료(sent_at NOT NULL) — 제외(중복 방지).
	deliveredVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-delivered", freshCreatedAt)
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ?", deliveredVideo.ID).Updates(map[string]any{"sent_at": sentAt}).Error)

	// (7) in-flight video(locked_at 미만료) — 처리 중이라 제외.
	lockedVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-locked", freshCreatedAt)
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ?", lockedVideo.ID).Updates(map[string]any{"locked_at": recentLock}).Error)

	// (8) fresh·미발송·FAILED community_post — 스코프 밖(alarm-once 게이트 우회 불가)이라 제외.
	freshCommunity := newFailedOutbox(domain.OutboxKindCommunityPost, "post-fresh", freshCreatedAt)

	revived, err := cm.reviveStaleFailedOutbox(ctx, 60*time.Minute, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(4), revived, "video+live+milestone+zero-delivery video 4건만 revive")

	assertRevived := func(id int64, label string) {
		var row domain.YouTubeNotificationOutbox
		require.NoError(t, db.Where("id = ?", id).First(&row).Error)
		assert.Equal(t, domain.OutboxStatusPending, row.Status, label+" → PENDING")
		assert.Zero(t, row.AttemptCount, label+" attempt 리셋")
		assert.Empty(t, row.Error, label+" error clear")
		assert.True(t, row.NextAttemptAt.After(oldNextAttempt), label+" next_attempt 전진")
		assert.Nil(t, row.LockedAt)
	}
	assertNotRevived := func(id int64, label string) {
		var row domain.YouTubeNotificationOutbox
		require.NoError(t, db.Where("id = ?", id).First(&row).Error)
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
	require.NoError(t, db.Where("outbox_id = ? AND room_id = ?", freshVideo.ID, "room-sent").First(&sentDelivery).Error)
	assert.Equal(t, domain.OutboxStatusSent, sentDelivery.Status, "이미 발송된 room은 재발송 금지")
	require.NotNil(t, sentDelivery.SentAt)

	var failedDelivery domain.YouTubeNotificationDelivery
	require.NoError(t, db.Where("outbox_id = ? AND room_id = ?", freshVideo.ID, "room-failed").First(&failedDelivery).Error)
	assert.Equal(t, domain.OutboxStatusPending, failedDelivery.Status, "실패한 room은 재시도 대상")
	assert.Zero(t, failedDelivery.AttemptCount)
}

// TestReviveStaleFailedOutbox_RevivedRowIsActuallyRedelivered는 revive가 "theater"가 아님을 증명한다:
// revive 전에는 dispatcher가 FAILED 행을 재전달하지 않지만, revive 후 ProcessOnce가 실제로 실패했던
// room에 메시지를 발송하고 delivery 행이 SENT로 전이된다(end-to-end revive→dispatch 경로 검증).
func TestReviveStaleFailedOutbox_RevivedRowIsActuallyRedelivered(t *testing.T) {
	db := newDeliveryTestDB(t)
	ctx := context.Background()

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db.Pool, cachemocks.NewLenientClient(), sender, nil,
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
	require.NoError(t, db.Create(outboxRow).Error)
	deliveryRow := &domain.YouTubeNotificationDelivery{
		OutboxID: outboxRow.ID, RoomID: "room-x", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: now.Add(-10 * time.Minute), Error: "send failed",
	}
	require.NoError(t, db.Create(deliveryRow).Error)

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
	require.NoError(t, db.Where("id = ?", deliveryRow.ID).First(&updated).Error)
	assert.Equal(t, domain.OutboxStatusSent, updated.Status, "재전달 후 delivery 행은 SENT")
}

func senderMessages(s *testSender) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...)
}
