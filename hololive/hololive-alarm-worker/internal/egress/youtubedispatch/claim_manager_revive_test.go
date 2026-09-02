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

package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

// reviveTestClaimManager는 정규화된 canonical lifecycle 구성을 사용합니다.
func reviveTestClaimManager(db *deliveryTestDB) *ClaimManager {
	return NewDispatcher(
		db,
		cachemocks.NewLenientClient(),
		&testSender{failRoom: map[string]bool{}},
		nil,
		slog.New(slog.DiscardHandler),
		&dispatchstate.Config{
			BatchSize:            50,
			MaxRetries:           3,
			RetryBackoff:         time.Minute,
			LockTimeout:          5 * time.Minute,
			ClaimFreshnessWindow: 2 * time.Hour,
		},
	).claim
}

type reviveStaleFixture struct {
	oldNextAttempt   time.Time
	freshVideoID     int64
	freshLiveID      int64
	freshMilestoneID int64
	zeroDeliveryID   int64
	freshCommunityID int64
	staleVideoID     int64
	deliveredVideoID int64
	lockedVideoID    int64
}

func TestReviveStaleFailedOutbox_RevivesFreshNeverSentAndPreservesDelivered(t *testing.T) {
	db := newDeliveryPool(t)
	cm := reviveTestClaimManager(db)
	ctx := t.Context()
	fixture := seedReviveStaleFailedOutboxFixture(t, db)

	revived, err := cm.reviveStaleFailedOutbox(ctx, 60*time.Minute, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(5), revived, "fresh fanout 실패 4건과 미전송 room 논리 그룹 1건 revive")

	assertReviveOutboxStatuses(t, db, fixture)

	// per-room dedup: SENT 행 불변, FAILED 행만 PENDING.
	var sentDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &sentDelivery, "outbox_id = ? AND room_id = ?", fixture.freshVideoID, "room-sent").Error)
	assert.Equal(t, domain.OutboxStatusSent, sentDelivery.Status, "이미 발송된 room은 재발송 금지")
	require.NotNil(t, sentDelivery.SentAt)

	var failedDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &failedDelivery, "outbox_id = ? AND room_id = ?", fixture.freshVideoID, "room-failed").Error)
	assert.Equal(t, domain.OutboxStatusPending, failedDelivery.Status, "미전송 room 논리 그룹은 재시도 대상")
	assert.Zero(t, failedDelivery.AttemptCount)
}

func seedReviveStaleFailedOutboxFixture(t *testing.T, db *deliveryTestDB) reviveStaleFixture {
	t.Helper()

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
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, Error: testSendFailedMessage,
	}).Error)

	deliveredVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-delivered", freshCreatedAt)
	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, map[string]any{"sent_at": sentAt}, "id = ?", deliveredVideo.ID).Error)

	lockedVideo := newFailedOutbox(domain.OutboxKindNewVideo, "video-locked", freshCreatedAt)
	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, map[string]any{"locked_at": recentLock}, "id = ?", lockedVideo.ID).Error)

	return reviveStaleFixture{
		oldNextAttempt:   oldNextAttempt,
		freshVideoID:     freshVideo.ID,
		freshLiveID:      newFailedOutbox(domain.OutboxKindLiveStream, "live-fresh", freshCreatedAt).ID,
		freshMilestoneID: newFailedOutbox(domain.OutboxKindMilestone, "ms-fresh", freshCreatedAt).ID,
		zeroDeliveryID:   newFailedOutbox(domain.OutboxKindNewVideo, "video-nodelivery", freshCreatedAt).ID,
		freshCommunityID: newFailedOutbox(domain.OutboxKindCommunityPost, "post-fresh", freshCreatedAt).ID,
		staleVideoID:     newFailedOutbox(domain.OutboxKindNewVideo, "video-stale", staleCreatedAt).ID,
		deliveredVideoID: deliveredVideo.ID,
		lockedVideoID:    lockedVideo.ID,
	}
}

func assertReviveOutboxStatuses(t *testing.T, db *deliveryTestDB, fixture reviveStaleFixture) {
	t.Helper()

	assertReviveOutboxProjectedPending(t, db, fixture.freshVideoID, fixture.oldNextAttempt, "freshVideo")
	assertReviveOutboxRevived(t, db, fixture.freshLiveID, fixture.oldNextAttempt, "freshLive")
	assertReviveOutboxRevived(t, db, fixture.freshMilestoneID, fixture.oldNextAttempt, "freshMilestone")
	assertReviveOutboxRevived(t, db, fixture.zeroDeliveryID, fixture.oldNextAttempt, "zeroDeliveryVideo")
	assertReviveOutboxRevived(t, db, fixture.freshCommunityID, fixture.oldNextAttempt, "freshCommunity")
	assertReviveOutboxStillFailed(t, db, fixture.staleVideoID, "staleVideo")
	assertReviveOutboxStillFailed(t, db, fixture.deliveredVideoID, "deliveredVideo")
	assertReviveOutboxStillFailed(t, db, fixture.lockedVideoID, "lockedVideo")
}

func assertReviveOutboxRevived(t *testing.T, db *deliveryTestDB, id int64, oldNextAttempt time.Time, label string) {
	t.Helper()

	var row domain.YouTubeNotificationOutbox

	require.NoError(t, firstDeliveryTestRowWhere(db, &row, "id = ?", id).Error)
	assert.Equal(t, domain.OutboxStatusPending, row.Status, label+" → PENDING")
	assert.Zero(t, row.AttemptCount, label+" attempt 리셋")
	assert.Empty(t, row.Error, label+" error clear")
	assert.True(t, row.NextAttemptAt.After(oldNextAttempt), label+" next_attempt 전진")
	assert.Nil(t, row.LockedAt)
}

func assertReviveOutboxProjectedPending(t *testing.T, db *deliveryTestDB, id int64, oldNextAttempt time.Time, label string) {
	t.Helper()

	var row domain.YouTubeNotificationOutbox

	require.NoError(t, firstDeliveryTestRowWhere(db, &row, "id = ?", id).Error)
	assert.Equal(t, domain.OutboxStatusPending, row.Status, label+" → PENDING")
	assert.Equal(t, 3, row.AttemptCount, label+" fanout attempt는 불변")
	assert.WithinDuration(t, oldNextAttempt, row.NextAttemptAt, time.Microsecond, label+" fanout next_attempt는 불변")
	assert.Nil(t, row.LockedAt)
}

func assertReviveOutboxStillFailed(t *testing.T, db *deliveryTestDB, id int64, label string) {
	t.Helper()

	var row domain.YouTubeNotificationOutbox

	require.NoError(t, firstDeliveryTestRowWhere(db, &row, "id = ?", id).Error)
	assert.Equal(t, domain.OutboxStatusFailed, row.Status, label+" → FAILED 유지")
}

// TestReviveStaleFailedOutbox_RevivedRowIsActuallyRedelivered는 revive가 "theater"가 아님을 증명한다:
// revive 전에는 dispatcher가 FAILED 행을 재전달하지 않지만, revive 후 ProcessOnce가 실제로 실패했던
// room에 메시지를 발송하고 delivery 행이 SENT로 전이된다(end-to-end revive→dispatch 경로 검증).
func TestReviveStaleFailedOutbox_RevivedRowIsActuallyRedelivered(t *testing.T) {
	db := newDeliveryPool(t)
	ctx := t.Context()

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil,
		slog.New(slog.DiscardHandler), &dispatchstate.Config{
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
		AttemptCount: 3, NextAttemptAt: now.Add(-10 * time.Minute), Error: testSendFailedMessage,
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

func TestReviveStaleFailedOutbox_RevivesCommunityAndShorts(t *testing.T) {
	db := newDeliveryPool(t)
	cm := reviveTestClaimManager(db)
	fixture := seedCommunityShortReviveFixture(t, db)

	revived, err := cm.reviveStaleFailedOutbox(t.Context(), 60*time.Minute, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(2), revived, "shorts와 미전송 community room 논리 그룹 revive")
	assertCommunityShortReviveFixture(t, db, fixture)
}

type communityShortReviveFixture struct {
	shortID        int64
	communityID    int64
	oldNextAttempt time.Time
}

func seedCommunityShortReviveFixture(t *testing.T, db *deliveryTestDB) communityShortReviveFixture {
	t.Helper()

	now := time.Now().UTC()
	freshCreatedAt := now.Add(-5 * time.Minute)
	oldNextAttempt := now.Add(-30 * time.Minute)
	sentAt := now.Add(-20 * time.Minute)
	short := insertFailedReviveOutbox(t, db, domain.OutboxKindNewShort, "short-fresh", freshCreatedAt, oldNextAttempt)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: short.ID, RoomID: "room-short-failed", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, Error: testSendFailedMessage,
	}).Error)

	community := insertFailedReviveOutbox(t, db, domain.OutboxKindCommunityPost, "post-fresh", freshCreatedAt, oldNextAttempt)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: community.ID, RoomID: "room-comm-sent", Status: domain.OutboxStatusSent,
		AttemptCount: 1, NextAttemptAt: oldNextAttempt, SentAt: &sentAt,
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: community.ID, RoomID: "room-comm-failed", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, Error: testSendFailedMessage,
	}).Error)

	return communityShortReviveFixture{shortID: short.ID, communityID: community.ID, oldNextAttempt: oldNextAttempt}
}

func insertFailedReviveOutbox(
	t *testing.T,
	db *deliveryTestDB,
	kind domain.OutboxKind,
	contentID string,
	createdAt time.Time,
	nextAttemptAt time.Time,
) *domain.YouTubeNotificationOutbox {
	t.Helper()

	payload := `{"video_id":"` + contentID + `"}`

	switch kind {
	case domain.OutboxKindNewShort:
		payload = `{"canonical_post_id":"short:` + contentID + `","video_id":"` + contentID + `"}`
	case domain.OutboxKindCommunityPost:
		payload = `{"canonical_post_id":"community:` + contentID + `","post_id":"` + contentID + `"}`
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
	}

	row := &domain.YouTubeNotificationOutbox{
		Kind: kind, ChannelID: "ch-cs", ContentID: contentID,
		Payload: payload, Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: nextAttemptAt, CreatedAt: createdAt, Error: "failed",
	}
	require.NoError(t, insertDeliveryTestRows(db, row).Error)

	return row
}

func assertCommunityShortReviveFixture(
	t *testing.T,
	db *deliveryTestDB,
	fixture communityShortReviveFixture,
) {
	t.Helper()

	assertOutboxPending := func(id int64, label string) {
		var row domain.YouTubeNotificationOutbox

		require.NoError(t, firstDeliveryTestRowWhere(db, &row, "id = ?", id).Error)
		assert.Equal(t, domain.OutboxStatusPending, row.Status, label+" → PENDING")
		assert.Equal(t, 3, row.AttemptCount, label+" fanout attempt는 불변")
		assert.WithinDuration(t, fixture.oldNextAttempt, row.NextAttemptAt, time.Microsecond, label+" fanout next_attempt는 불변")
		assert.Nil(t, row.LockedAt)
	}
	assertOutboxPending(fixture.shortID, "short")
	assertOutboxPending(fixture.communityID, testCaseNameCommunity)

	var shortDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &shortDelivery, "outbox_id = ? AND room_id = ?", fixture.shortID, "room-short-failed").Error)
	assert.Equal(t, domain.OutboxStatusPending, shortDelivery.Status, "FAILED shorts delivery 행은 재시도 대상")
	assert.Zero(t, shortDelivery.AttemptCount)

	var commFailedDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &commFailedDelivery, "outbox_id = ? AND room_id = ?", fixture.communityID, "room-comm-failed").Error)
	assert.Equal(t, domain.OutboxStatusPending, commFailedDelivery.Status, "미전송 community room 논리 그룹은 재시도 대상")

	var commSentDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &commSentDelivery, "outbox_id = ? AND room_id = ?", fixture.communityID, "room-comm-sent").Error)
	assert.Equal(t, domain.OutboxStatusSent, commSentDelivery.Status, "이미 발송된 room은 재발송 금지")
	require.NotNil(t, commSentDelivery.SentAt)
}

func TestReviveStaleFailedOutbox_ExcludesAllQuarantinedOutbox(t *testing.T) {
	db := newDeliveryPool(t)
	cm := reviveTestClaimManager(db)
	ctx := t.Context()

	now := time.Now().UTC()
	freshCreatedAt := now.Add(-5 * time.Minute)
	oldNextAttempt := now.Add(-30 * time.Minute)

	outbox := &domain.YouTubeNotificationOutbox{
		Kind: domain.OutboxKindNewVideo, ChannelID: "ch-q", ContentID: "video-all-quarantined",
		Payload: `{"id":"video-all-quarantined"}`, Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, CreatedAt: freshCreatedAt, Error: "per-room delivery failed",
	}
	require.NoError(t, insertDeliveryTestRows(db, outbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: outbox.ID, RoomID: "room-q", Status: store.DeliveryStatusQuarantined,
		AttemptCount: 1, NextAttemptAt: oldNextAttempt, Error: "mid-send crash",
	}).Error)

	revived, err := cm.reviveStaleFailedOutbox(ctx, 60*time.Minute, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(0), revived, "전량 QUARANTINED outbox는 revive 대상이 아님(flap 종료)")

	var gotOutbox domain.YouTubeNotificationOutbox

	require.NoError(t, firstDeliveryTestRowWhere(db, &gotOutbox, "id = ?", outbox.ID).Error)
	assert.Equal(t, domain.OutboxStatusFailed, gotOutbox.Status, "outbox는 FAILED 유지(PENDING으로 flap 안 함)")

	var gotDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &gotDelivery, "outbox_id = ?", outbox.ID).Error)
	assert.Equal(t, store.DeliveryStatusQuarantined, gotDelivery.Status, "QUARANTINED delivery는 불변")
}

func TestReviveStaleFailedOutbox_MixedFailedAndQuarantinedResetsFailedLogicalGroup(t *testing.T) {
	db := newDeliveryPool(t)
	cm := reviveTestClaimManager(db)
	ctx := t.Context()

	now := time.Now().UTC()
	freshCreatedAt := now.Add(-5 * time.Minute)
	oldNextAttempt := now.Add(-30 * time.Minute)

	outbox := &domain.YouTubeNotificationOutbox{
		Kind: domain.OutboxKindNewVideo, ChannelID: "ch-mix", ContentID: "video-mixed",
		Payload: `{"id":"video-mixed"}`, Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, CreatedAt: freshCreatedAt, Error: "per-room delivery failed",
	}
	require.NoError(t, insertDeliveryTestRows(db, outbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: outbox.ID, RoomID: "room-failed", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, NextAttemptAt: oldNextAttempt, Error: testSendFailedMessage,
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: outbox.ID, RoomID: "room-quarantined", Status: store.DeliveryStatusQuarantined,
		AttemptCount: 1, NextAttemptAt: oldNextAttempt, Error: "mid-send crash",
	}).Error)

	revived, err := cm.reviveStaleFailedOutbox(ctx, 60*time.Minute, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(1), revived, "FAILED room 논리 그룹은 별도로 revive")

	var gotOutbox domain.YouTubeNotificationOutbox

	require.NoError(t, firstDeliveryTestRowWhere(db, &gotOutbox, "id = ?", outbox.ID).Error)
	assert.Equal(t, domain.OutboxStatusPending, gotOutbox.Status, "미전송 room revive 후 outbox는 PENDING")
	assert.Equal(t, 3, gotOutbox.AttemptCount, "delivery revive에서 fanout attempt는 불변")
	assert.Nil(t, gotOutbox.LockedAt)

	var failedDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &failedDelivery, "outbox_id = ? AND room_id = ?", outbox.ID, "room-failed").Error)
	assert.Equal(t, domain.OutboxStatusPending, failedDelivery.Status, "FAILED room 논리 그룹은 재시도 대상으로 리셋")
	assert.Zero(t, failedDelivery.AttemptCount)

	var quarantinedDelivery domain.YouTubeNotificationDelivery

	require.NoError(t, firstDeliveryTestRowWhere(db, &quarantinedDelivery, "outbox_id = ? AND room_id = ?", outbox.ID, "room-quarantined").Error)
	assert.Equal(t, store.DeliveryStatusQuarantined, quarantinedDelivery.Status, "QUARANTINED delivery는 리셋하지 않고 유지")
	assert.Equal(t, 1, quarantinedDelivery.AttemptCount, "QUARANTINED delivery attempt 불변")
}

func senderMessages(s *testSender) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.messages...)
}
