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

func TestProcessOnce_RetriesPersistedDeliveriesWithoutNewOutboxClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	now := time.Now().UTC()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_restart_retry",
		ContentID:     "short-restart-retry",
		Payload:       `{"video_id":"short-restart-retry","title":"restart title","published_at":"2026-04-10T01:11:12Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&deliveryTestTrackingModel{
		Kind:       string(item.Kind),
		ContentID:  item.ContentID,
		ChannelID:  item.ChannelID,
		DetectedAt: now,
	}).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-shorts",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, db.Create(&delivery).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db.Pool, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.ProcessOnceForTest(ctx)

	var updatedDelivery deliveryTestDeliveryModel
	require.NoError(t, db.First(&updatedDelivery, delivery.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedDelivery.Status)
	require.NotNil(t, updatedDelivery.SentAt)

	var updatedOutbox deliveryTestOutboxModel
	require.NoError(t, db.First(&updatedOutbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedOutbox.Status)
	require.NotNil(t, updatedOutbox.SentAt)

	sender.mu.Lock()
	messages := append([]string(nil), sender.messages...)
	sender.mu.Unlock()
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], "room-shorts:📱 VTuber 쇼츠 알림")
	assert.Contains(t, messages[0], "restart title")
}

func TestProcessOnce_ReconcilesOutboxStatusFromPersistedDeliveryRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	now := time.Now().UTC()
	sentAt := now.Add(-30 * time.Second)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_restart_reconcile",
		ContentID:     "post-restart-reconcile",
		Payload:       `{"post_id":"post-restart-reconcile","content_text":"community body","published_at":"2026-04-10T01:11:12Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, db.Create(&item).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-community",
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: now,
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&delivery).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db.Pool, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.AggregateSyncForTest(ctx)

	var updatedOutbox deliveryTestOutboxModel
	require.NoError(t, db.First(&updatedOutbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedOutbox.Status)
	require.NotNil(t, updatedOutbox.SentAt)

	sender.mu.Lock()
	messageCount := len(sender.messages)
	sender.mu.Unlock()
	assert.Equal(t, 0, messageCount)
}

func TestProcessOnce_DoesNotResendAlreadySentCommunityShortsPostAfterDispatcherRestart(t *testing.T) {
	fixedSentAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	withFixedSentAtNow(t, fixedSentAt)

	testCases := []struct {
		name                  string
		kind                  domain.OutboxKind
		channelID             string
		contentID             string
		roomID                string
		payload               string
		detectedAt            time.Time
		actualPublishedAt     time.Time
		expectedMessageMarker string
	}{
		{
			name:                  "community",
			kind:                  domain.OutboxKindCommunityPost,
			channelID:             "UC_restart_sent_community",
			contentID:             "post-restart-sent",
			roomID:                "room-community",
			payload:               `{"canonical_post_id":"community:post-restart-sent","post_id":"post-restart-sent","content_text":"community restart sent body","published_at":"2026-04-10T01:12:00Z"}`,
			detectedAt:            time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
			actualPublishedAt:     time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
			expectedMessageMarker: "community restart sent body",
		},
		{
			name:                  "shorts",
			kind:                  domain.OutboxKindNewShort,
			channelID:             "UC_restart_sent_shorts",
			contentID:             "short-restart-sent",
			roomID:                "room-shorts",
			payload:               `{"canonical_post_id":"short:short-restart-sent","video_id":"short-restart-sent","title":"short restart sent title","published_at":"2026-04-10T01:12:00Z"}`,
			detectedAt:            time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
			actualPublishedAt:     time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
			expectedMessageMarker: "short restart sent title",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newRecoveryInputFixtureDB(t, "restart_does_not_resend_"+tc.name)

			item := domain.YouTubeNotificationOutbox{
				Kind:          tc.kind,
				ChannelID:     tc.channelID,
				ContentID:     tc.contentID,
				Payload:       tc.payload,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: tc.detectedAt,
				CreatedAt:     tc.detectedAt,
			}
			require.NoError(t, db.Create(&item).Error)

			postID := store.CanonicalDeliveryPostID(item.Kind, item.ContentID)
			require.NoError(t, db.Create(&deliveryTestTrackingModel{
				Kind:               string(item.Kind),
				ContentID:          item.ContentID,
				CanonicalContentID: postID,
				ChannelID:          item.ChannelID,
				ActualPublishedAt:  new(tc.actualPublishedAt),
				DetectedAt:         tc.detectedAt,
				DeliveryStatus:     string(domain.YouTubeContentAlarmDeliveryStatusPending),
			}).Error)

			delivery := domain.YouTubeNotificationDelivery{
				OutboxID:      item.ID,
				RoomID:        tc.roomID,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: tc.detectedAt,
				CreatedAt:     tc.detectedAt,
			}
			require.NoError(t, db.Create(&delivery).Error)

			firstSender := &testSender{failRoom: map[string]bool{}}
			firstDispatcher := NewDispatcher(db.Pool, cachemocks.NewLenientClient(), firstSender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
				BatchSize:           10,
				LockTimeout:         time.Minute,
				PollInterval:        time.Second,
				MaxRetries:          3,
				RetryBackoff:        time.Minute,
				DeliveryParallelism: 1,
			})

			firstDispatcher.ProcessOnceForTest(ctx)

			firstSender.mu.Lock()
			firstMessages := append([]string(nil), firstSender.messages...)
			firstSender.mu.Unlock()
			require.Len(t, firstMessages, 1)
			assert.Contains(t, firstMessages[0], tc.roomID+":")
			assert.Contains(t, firstMessages[0], tc.expectedMessageMarker)

			var sentDelivery deliveryTestDeliveryModel
			require.NoError(t, db.First(&sentDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentDelivery.Status)
			require.NotNil(t, sentDelivery.SentAt)
			assert.Equal(t, fixedSentAt, sentDelivery.SentAt.UTC())

			var sentOutbox deliveryTestOutboxModel
			require.NoError(t, db.First(&sentOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentOutbox.Status)
			require.NotNil(t, sentOutbox.SentAt)
			assert.Equal(t, fixedSentAt, sentOutbox.SentAt.UTC())

			var sentTracking deliveryTestTrackingModel
			require.NoError(t, db.Where("kind = ? AND content_id = ?", string(item.Kind), item.ContentID).First(&sentTracking).Error)
			require.NotNil(t, sentTracking.AlarmSentAt)
			assert.Equal(t, fixedSentAt, sentTracking.AlarmSentAt.UTC())
			assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), sentTracking.DeliveryStatus)

			var sentState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.First(&sentState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
			assert.Nil(t, sentState.AuthorizedAt)
			require.NotNil(t, sentState.AlarmSentAt)
			assert.Equal(t, fixedSentAt, sentState.AlarmSentAt.UTC())
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, sentState.DeliveryStatus)

			secondSender := &testSender{failRoom: map[string]bool{}}
			secondDispatcher := NewDispatcher(db.Pool, cachemocks.NewLenientClient(), secondSender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
				BatchSize:           10,
				LockTimeout:         time.Minute,
				PollInterval:        time.Second,
				MaxRetries:          3,
				RetryBackoff:        time.Minute,
				DeliveryParallelism: 1,
			})

			secondDispatcher.ProcessOnceForTest(ctx)

			secondSender.mu.Lock()
			secondMessageCount := len(secondSender.messages)
			secondSender.mu.Unlock()
			assert.Equal(t, 0, secondMessageCount)

			var persistedDelivery deliveryTestDeliveryModel
			require.NoError(t, db.First(&persistedDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), persistedDelivery.Status)
			require.NotNil(t, persistedDelivery.SentAt)
			assert.Equal(t, fixedSentAt, persistedDelivery.SentAt.UTC())

			var persistedOutbox deliveryTestOutboxModel
			require.NoError(t, db.First(&persistedOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), persistedOutbox.Status)
			require.NotNil(t, persistedOutbox.SentAt)
			assert.Equal(t, fixedSentAt, persistedOutbox.SentAt.UTC())

			var persistedTracking deliveryTestTrackingModel
			require.NoError(t, db.Where("kind = ? AND content_id = ?", string(item.Kind), item.ContentID).First(&persistedTracking).Error)
			require.NotNil(t, persistedTracking.AlarmSentAt)
			assert.Equal(t, fixedSentAt, persistedTracking.AlarmSentAt.UTC())
			assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), persistedTracking.DeliveryStatus)

			var persistedState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.First(&persistedState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
			assert.Nil(t, persistedState.AuthorizedAt)
			require.NotNil(t, persistedState.AlarmSentAt)
			assert.Equal(t, fixedSentAt, persistedState.AlarmSentAt.UTC())
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, persistedState.DeliveryStatus)

			var deliveryRows []deliveryTestDeliveryModel
			require.NoError(t, db.Where("outbox_id = ?", item.ID).Order("id ASC").Find(&deliveryRows).Error)
			require.Len(t, deliveryRows, 1)
		})
	}
}

func TestProcessOnce_RestartRecoveryResendsOnlyPendingCommunityShortsPostExactlyOnce(t *testing.T) {
	fixedSentAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	withFixedSentAtNow(t, fixedSentAt)

	testCases := []struct {
		name          string
		spec          recoveryInputFixtureSpec
		sentMarker    string
		pendingMarker string
	}{
		{
			name: "community",
			spec: recoveryInputFixtureSpec{
				kind:               domain.OutboxKindCommunityPost,
				channelID:          "UC_restart_recovery_community",
				roomID:             "room-community",
				sentContentID:      "post-restart-recovery-sent",
				sentPayload:        `{"canonical_post_id":"community:post-restart-recovery-sent","post_id":"post-restart-recovery-sent","content_text":"community restart recovery sent body","published_at":"2026-04-10T01:09:00Z"}`,
				pendingContentID:   "post-restart-recovery-pending",
				pendingPayload:     `{"canonical_post_id":"community:post-restart-recovery-pending","post_id":"post-restart-recovery-pending","content_text":"community restart recovery pending body","published_at":"2026-04-10T01:12:00Z"}`,
				sentDetectedAt:     time.Date(2026, 4, 10, 1, 9, 30, 0, time.UTC),
				pendingDetectedAt:  time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
				sentPublishedAt:    time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC),
				pendingPublishedAt: time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
				retryReadyAt:       fixedSentAt.Add(-30 * time.Second),
				alreadySentAt:      fixedSentAt.Add(-2 * time.Minute),
			},
			sentMarker:    "community restart recovery sent body",
			pendingMarker: "community restart recovery pending body",
		},
		{
			name: "shorts",
			spec: recoveryInputFixtureSpec{
				kind:               domain.OutboxKindNewShort,
				channelID:          "UC_restart_recovery_shorts",
				roomID:             "room-shorts",
				sentContentID:      "short-restart-recovery-sent",
				sentPayload:        `{"canonical_post_id":"short:short-restart-recovery-sent","video_id":"short-restart-recovery-sent","title":"short restart recovery sent title","published_at":"2026-04-10T01:09:00Z"}`,
				pendingContentID:   "short-restart-recovery-pending",
				pendingPayload:     `{"canonical_post_id":"short:short-restart-recovery-pending","video_id":"short-restart-recovery-pending","title":"short restart recovery pending title","published_at":"2026-04-10T01:12:00Z"}`,
				sentDetectedAt:     time.Date(2026, 4, 10, 1, 9, 30, 0, time.UTC),
				pendingDetectedAt:  time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
				sentPublishedAt:    time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC),
				pendingPublishedAt: time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
				retryReadyAt:       fixedSentAt.Add(-30 * time.Second),
				alreadySentAt:      fixedSentAt.Add(-2 * time.Minute),
			},
			sentMarker:    "short restart recovery sent title",
			pendingMarker: "short restart recovery pending title",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newRecoveryInputFixtureDB(t, "restart_recovery_selective_"+tc.name)
			fixture := seedCommunityShortsRecoveryInputFixture(t, db, tc.spec)

			firstSender := &testSender{failRoom: map[string]bool{}}
			firstDispatcher := NewDispatcher(db.Pool, cachemocks.NewLenientClient(), firstSender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
				BatchSize:           10,
				LockTimeout:         time.Minute,
				PollInterval:        time.Second,
				MaxRetries:          3,
				RetryBackoff:        time.Minute,
				DeliveryParallelism: 1,
			})

			firstDispatcher.ProcessOnceForTest(ctx)

			firstSender.mu.Lock()
			firstMessages := append([]string(nil), firstSender.messages...)
			firstSender.mu.Unlock()
			require.Len(t, firstMessages, 1)
			assert.Contains(t, firstMessages[0], tc.spec.roomID+":")
			assert.Contains(t, firstMessages[0], tc.pendingMarker)
			assert.NotContains(t, firstMessages[0], tc.sentMarker)

			secondSender := &testSender{failRoom: map[string]bool{}}
			secondDispatcher := NewDispatcher(db.Pool, cachemocks.NewLenientClient(), secondSender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
				BatchSize:           10,
				LockTimeout:         time.Minute,
				PollInterval:        time.Second,
				MaxRetries:          3,
				RetryBackoff:        time.Minute,
				DeliveryParallelism: 1,
			})

			secondDispatcher.ProcessOnceForTest(ctx)

			secondSender.mu.Lock()
			secondMessageCount := len(secondSender.messages)
			secondSender.mu.Unlock()
			assert.Equal(t, 0, secondMessageCount)

			var updatedSentDelivery deliveryTestDeliveryModel
			require.NoError(t, db.First(&updatedSentDelivery, fixture.sentDelivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), updatedSentDelivery.Status)
			assert.Equal(t, 1, updatedSentDelivery.AttemptCount)
			require.NotNil(t, updatedSentDelivery.SentAt)
			assert.Equal(t, fixedSentAt, updatedSentDelivery.SentAt.UTC())

			var updatedPendingDelivery deliveryTestDeliveryModel
			require.NoError(t, db.First(&updatedPendingDelivery, fixture.pendingDelivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), updatedPendingDelivery.Status)
			assert.Equal(t, 1, updatedPendingDelivery.AttemptCount)
			require.NotNil(t, updatedPendingDelivery.SentAt)
			assert.Equal(t, fixedSentAt, updatedPendingDelivery.SentAt.UTC())

			var updatedSentOutbox deliveryTestOutboxModel
			require.NoError(t, db.First(&updatedSentOutbox, fixture.sentOutbox.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), updatedSentOutbox.Status)
			require.NotNil(t, updatedSentOutbox.SentAt)
			assert.Equal(t, fixedSentAt, updatedSentOutbox.SentAt.UTC())

			var updatedPendingOutbox deliveryTestOutboxModel
			require.NoError(t, db.First(&updatedPendingOutbox, fixture.pendingOutbox.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), updatedPendingOutbox.Status)
			require.NotNil(t, updatedPendingOutbox.SentAt)
			assert.Equal(t, fixedSentAt, updatedPendingOutbox.SentAt.UTC())

			var updatedSentTracking deliveryTestTrackingModel
			require.NoError(t, db.Where("kind = ? AND content_id = ?", string(fixture.sentOutbox.Kind), fixture.sentOutbox.ContentID).First(&updatedSentTracking).Error)
			require.NotNil(t, updatedSentTracking.AlarmSentAt)
			assert.Equal(t, tc.spec.alreadySentAt, updatedSentTracking.AlarmSentAt.UTC())
			assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), updatedSentTracking.DeliveryStatus)

			var updatedPendingTracking deliveryTestTrackingModel
			require.NoError(t, db.Where("kind = ? AND content_id = ?", string(fixture.pendingOutbox.Kind), fixture.pendingOutbox.ContentID).First(&updatedPendingTracking).Error)
			require.NotNil(t, updatedPendingTracking.AlarmSentAt)
			assert.Equal(t, fixedSentAt, updatedPendingTracking.AlarmSentAt.UTC())
			assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), updatedPendingTracking.DeliveryStatus)

			var updatedSentState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.First(&updatedSentState, "kind = ? AND post_id = ?", fixture.sentOutbox.Kind, fixture.sentPostID).Error)
			require.NotNil(t, updatedSentState.AlarmSentAt)
			assert.Equal(t, tc.spec.alreadySentAt, updatedSentState.AlarmSentAt.UTC())
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, updatedSentState.DeliveryStatus)

			var updatedPendingState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.First(&updatedPendingState, "kind = ? AND post_id = ?", fixture.pendingOutbox.Kind, fixture.pendingPostID).Error)
			assert.Nil(t, updatedPendingState.AuthorizedAt)
			require.NotNil(t, updatedPendingState.AlarmSentAt)
			assert.Equal(t, fixedSentAt, updatedPendingState.AlarmSentAt.UTC())
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, updatedPendingState.DeliveryStatus)

			var deliveryRows []deliveryTestDeliveryModel
			require.NoError(t, db.Order("id ASC").Find(&deliveryRows).Error)
			require.Len(t, deliveryRows, 2)
		})
	}
}
