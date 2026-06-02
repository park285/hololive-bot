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
)

func TestProcessOnce_RetrySkipsAlreadySentCommunityShortsPostAndResendsOnlyPendingPost(t *testing.T) {
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
				channelID:          "UC_retry_selective_community",
				roomID:             "room-community",
				sentContentID:      "post-already-sent",
				sentPayload:        `{"canonical_post_id":"community:post-already-sent","post_id":"post-already-sent","content_text":"community already sent body","published_at":"2026-04-10T01:09:00Z"}`,
				pendingContentID:   "post-retry-pending",
				pendingPayload:     `{"canonical_post_id":"community:post-retry-pending","post_id":"post-retry-pending","content_text":"community pending retry body","published_at":"2026-04-10T01:12:00Z"}`,
				sentDetectedAt:     time.Date(2026, 4, 10, 1, 9, 30, 0, time.UTC),
				pendingDetectedAt:  time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
				sentPublishedAt:    time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC),
				pendingPublishedAt: time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
				retryReadyAt:       fixedSentAt.Add(-30 * time.Second),
				alreadySentAt:      fixedSentAt.Add(-2 * time.Minute),
			},
			sentMarker:    "community already sent body",
			pendingMarker: "community pending retry body",
		},
		{
			name: "shorts",
			spec: recoveryInputFixtureSpec{
				kind:               domain.OutboxKindNewShort,
				channelID:          "UC_retry_selective_shorts",
				roomID:             "room-shorts",
				sentContentID:      "short-already-sent",
				sentPayload:        `{"canonical_post_id":"short:short-already-sent","video_id":"short-already-sent","title":"short already sent title","published_at":"2026-04-10T01:09:00Z"}`,
				pendingContentID:   "short-retry-pending",
				pendingPayload:     `{"canonical_post_id":"short:short-retry-pending","video_id":"short-retry-pending","title":"short pending retry title","published_at":"2026-04-10T01:12:00Z"}`,
				sentDetectedAt:     time.Date(2026, 4, 10, 1, 9, 30, 0, time.UTC),
				pendingDetectedAt:  time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
				sentPublishedAt:    time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC),
				pendingPublishedAt: time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
				retryReadyAt:       fixedSentAt.Add(-30 * time.Second),
				alreadySentAt:      fixedSentAt.Add(-2 * time.Minute),
			},
			sentMarker:    "short already sent title",
			pendingMarker: "short pending retry title",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newRecoveryInputFixtureDB(t, "retry_selective_send_"+tc.name)
			fixture := seedCommunityShortsRecoveryInputFixture(t, db, tc.spec)

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

			sender.mu.Lock()
			messages := append([]string(nil), sender.messages...)
			sender.mu.Unlock()
			require.Len(t, messages, 1)
			assert.Contains(t, messages[0], tc.spec.roomID+":")
			assert.Contains(t, messages[0], tc.pendingMarker)
			assert.NotContains(t, messages[0], tc.sentMarker)
		})
	}
}
