package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestProcessOnce_RetriesPersistedDeliveriesWithoutNewOutboxClaim(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_restart_retry",
		ContentID:     "short-restart-retry",
		Payload:       `{"canonical_post_id":"short:short-restart-retry","video_id":"short-restart-retry","title":"restart title","published_at":"2026-04-10T01:11:12Z"}`,
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
		RoomID:        testRoomShorts,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.ProcessOnceForTest(ctx)

	var updatedDelivery deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &updatedDelivery, delivery.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedDelivery.Status)
	require.NotNil(t, updatedDelivery.SentAt)

	var updatedOutbox deliveryTestOutboxModel

	require.NoError(t, firstDeliveryTestRow(db, &updatedOutbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedOutbox.Status)
	require.NotNil(t, updatedOutbox.SentAt)

	sender.mu.Lock()

	messages := append([]string(nil), sender.messages...)
	sender.mu.Unlock()
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], "room-shorts:🔔 **VTuber** 새 쇼츠")
	assert.Contains(t, messages[0], "[restart title](https://www.youtube.com/shorts/short-restart-retry)")
}

func TestProcessOnce_ReconcilesOutboxStatusFromPersistedDeliveryRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC()
	sentAt := now.Add(-30 * time.Second)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_restart_reconcile",
		ContentID:     "post-restart-reconcile",
		Payload:       `{"canonical_post_id":"community:post-restart-reconcile","post_id":"post-restart-reconcile","content_text":"community body","published_at":"2026-04-10T01:11:12Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        testRoomCommunity,
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: now,
		SentAt:        &sentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.AggregateSyncForTest(ctx)

	var updatedOutbox deliveryTestOutboxModel

	require.NoError(t, firstDeliveryTestRow(db, &updatedOutbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedOutbox.Status)
	require.NotNil(t, updatedOutbox.SentAt)

	sender.mu.Lock()

	messageCount := len(sender.messages)
	sender.mu.Unlock()
	assert.Equal(t, 0, messageCount)
}

type restartAlreadySentCase struct {
	name                  string
	kind                  domain.OutboxKind
	channelID             string
	contentID             string
	roomID                string
	payload               string
	detectedAt            time.Time
	actualPublishedAt     time.Time
	expectedMessageMarker string
}

func TestProcessOnce_DoesNotResendAlreadySentCommunityShortsPostAfterDispatcherRestart(t *testing.T) {
	fixedSentAt := time.Now().UTC().Truncate(time.Millisecond)
	withFixedSentAtNow(t, fixedSentAt)

	detectedAt := fixedSentAt.Add(-time.Minute)
	publishedAt := time.Date(2026, time.April, 10, 1, 12, 0, 0, time.UTC)
	testCases := []restartAlreadySentCase{
		{
			name:                  testCaseNameCommunity,
			kind:                  domain.OutboxKindCommunityPost,
			channelID:             "UC_restart_sent_community",
			contentID:             "post-restart-sent",
			roomID:                testRoomCommunity,
			payload:               recoveryInputFixturePayload(domain.OutboxKindCommunityPost, "post-restart-sent", "community restart sent body", "2026-04-10T01:12:00Z"),
			detectedAt:            detectedAt,
			actualPublishedAt:     publishedAt,
			expectedMessageMarker: "community restart sent body",
		},
		{
			name:                  testCaseNameShorts,
			kind:                  domain.OutboxKindNewShort,
			channelID:             "UC_restart_sent_shorts",
			contentID:             "short-restart-sent",
			roomID:                testRoomShorts,
			payload:               recoveryInputFixturePayload(domain.OutboxKindNewShort, "short-restart-sent", "short restart sent title", "2026-04-10T01:12:00Z"),
			detectedAt:            detectedAt,
			actualPublishedAt:     publishedAt,
			expectedMessageMarker: "short restart sent title",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runRestartAlreadySentCase(t, tc, fixedSentAt)
		})
	}
}

func runRestartAlreadySentCase(t *testing.T, tc restartAlreadySentCase, fixedSentAt time.Time) {
	t.Helper()

	ctx := t.Context()
	db := newRecoveryInputFixtureDB(t, "restart_does_not_resend_"+tc.name)
	item, delivery, postID := seedRestartAlreadySentFixture(t, db, tc)

	firstSender := &testSender{failRoom: map[string]bool{}}
	firstDispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), firstSender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
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
	assertCommunityShortsSentAt(t, assertCommunityShortsPostSent(t, db, item, delivery.ID, postID), fixedSentAt)

	secondSender := &testSender{failRoom: map[string]bool{}}
	secondDispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), secondSender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
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
	assertCommunityShortsSentAt(t, assertCommunityShortsPostSent(t, db, item, delivery.ID, postID), fixedSentAt)

	var deliveryRows []deliveryTestDeliveryModel

	require.NoError(t, findDeliveryTestRowsOrderedWhere(db, &deliveryRows, "id ASC", "outbox_id = ?", item.ID).Error)
	require.Len(t, deliveryRows, 1)
}

func seedRestartAlreadySentFixture(
	t *testing.T,
	db *deliveryTestDB,
	tc restartAlreadySentCase,
) (domain.YouTubeNotificationOutbox, domain.YouTubeNotificationDelivery, string) {
	t.Helper()

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
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)

	postID := mustCanonicalDeliveryPostID(item.Kind, item.ContentID)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
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
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	return item, delivery, postID
}

func TestProcessOnce_RestartRecoveryResendsOnlyPendingCommunityShortsPostExactlyOnce(t *testing.T) {
	fixedSentAt := time.Now().UTC().Truncate(time.Millisecond)
	withFixedSentAtNow(t, fixedSentAt)

	testCases := newRecoverySelectiveSendCases(fixedSentAt, recoverySelectiveSendNaming{
		communityChannelID: "UC_restart_recovery_community",
		shortsChannelID:    "UC_restart_recovery_shorts",
		sentSlug:           "restart-recovery-sent",
		pendingSlug:        "restart-recovery-pending",
		communitySentBody:  "community restart recovery sent body",
		communityPendBody:  "community restart recovery pending body",
		shortsSentBody:     "short restart recovery sent title",
		shortsPendBody:     "short restart recovery pending title",
	})

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runRestartRecoverySelectiveCase(t, tc, fixedSentAt)
		})
	}
}

func runRestartRecoverySelectiveCase(t *testing.T, tc recoverySelectiveSendCase, fixedSentAt time.Time) {
	t.Helper()

	ctx := t.Context()
	db := newRecoveryInputFixtureDB(t, "restart_recovery_selective_"+tc.name)
	fixture := seedCommunityShortsRecoveryInputFixture(t, db, &tc.spec)

	firstSender := &testSender{failRoom: map[string]bool{}}
	firstDispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), firstSender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
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
	secondDispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), secondSender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
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

	assertRecoverySelectiveSendPersistedState(t, db, fixture, tc.spec, fixedSentAt)

	var deliveryRows []deliveryTestDeliveryModel

	require.NoError(t, findDeliveryTestRowsOrdered(db, &deliveryRows, "id ASC").Error)
	require.Len(t, deliveryRows, 3)
}