package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type recoverySelectiveSendCase struct {
	name          string
	spec          recoveryInputFixtureSpec
	sentMarker    string
	pendingMarker string
}

func TestProcessOnce_RetrySkipsAlreadySentCommunityShortsPostAndResendsOnlyPendingPost(t *testing.T) {
	fixedSentAt := time.Now().UTC().Truncate(time.Millisecond)
	withFixedSentAtNow(t, fixedSentAt)

	testCases := newRecoverySelectiveSendCases(fixedSentAt, recoverySelectiveSendNaming{
		communityChannelID: "UC_retry_selective_community",
		shortsChannelID:    "UC_retry_selective_shorts",
		sentSlug:           "already-sent",
		pendingSlug:        "retry-pending",
		communitySentBody:  "community already sent body",
		communityPendBody:  "community pending retry body",
		shortsSentBody:     "short already sent title",
		shortsPendBody:     "short pending retry title",
	})

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runRetrySelectiveSendCase(t, tc, fixedSentAt)
		})
	}
}

func runRetrySelectiveSendCase(t *testing.T, tc recoverySelectiveSendCase, fixedSentAt time.Time) {
	t.Helper()

	ctx := t.Context()
	db := newRecoveryInputFixtureDB(t, "retry_selective_send_"+tc.name)
	fixture := seedCommunityShortsRecoveryInputFixture(t, db, &tc.spec)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.ProcessOnceForTest(ctx)

	assertRecoverySelectiveSendPersistedState(t, db, fixture, tc.spec, fixedSentAt)

	sender.mu.Lock()

	messages := append([]string(nil), sender.messages...)
	sender.mu.Unlock()
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], tc.spec.roomID+":")
	assert.Contains(t, messages[0], tc.pendingMarker)
	assert.NotContains(t, messages[0], tc.sentMarker)

	var deliveryRows []deliveryTestDeliveryModel

	require.NoError(t, findDeliveryTestRowsOrdered(db, &deliveryRows, "id ASC").Error)
	require.Len(t, deliveryRows, 3)
}

func assertRecoverySelectiveSendPersistedState(
	t *testing.T,
	db *deliveryTestDB,
	fixture recoveryInputFixture,
	spec recoveryInputFixtureSpec,
	fixedSentAt time.Time,
) {
	t.Helper()

	assertRecoverySelectiveSendDeliveries(t, db, fixture, spec, fixedSentAt)
	assertRecoverySelectiveSendTracking(t, db, fixture, spec, fixedSentAt)
}

func assertRecoverySelectiveSendDeliveries(
	t *testing.T,
	db *deliveryTestDB,
	fixture recoveryInputFixture,
	spec recoveryInputFixtureSpec,
	fixedSentAt time.Time,
) {
	t.Helper()

	var updatedSentDelivery deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &updatedSentDelivery, fixture.sentDelivery.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedSentDelivery.Status)
	assert.Equal(t, 1, updatedSentDelivery.AttemptCount)
	require.NotNil(t, updatedSentDelivery.SentAt)
	assert.Equal(t, spec.alreadySentAt, updatedSentDelivery.SentAt.UTC())

	var updatedPendingDelivery deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &updatedPendingDelivery, fixture.pendingDelivery.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedPendingDelivery.Status)
	assert.Equal(t, 1, updatedPendingDelivery.AttemptCount)
	require.NotNil(t, updatedPendingDelivery.SentAt)
	assert.WithinDuration(t, fixedSentAt, updatedPendingDelivery.SentAt.UTC(), 2*time.Minute)

	var updatedSentOutbox deliveryTestOutboxModel

	require.NoError(t, firstDeliveryTestRow(db, &updatedSentOutbox, fixture.sentOutbox.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedSentOutbox.Status)
	require.NotNil(t, updatedSentOutbox.SentAt)
	assert.WithinDuration(t, fixedSentAt, updatedSentOutbox.SentAt.UTC(), 2*time.Minute)

	var servedDelivery deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &servedDelivery, fixture.servedDelivery.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), servedDelivery.Status)
	assert.Equal(t, 1, servedDelivery.AttemptCount)
	require.NotNil(t, servedDelivery.SentAt)
	assert.Equal(t, spec.alreadySentAt, servedDelivery.SentAt.UTC())

	var servedOutbox deliveryTestOutboxModel

	require.NoError(t, firstDeliveryTestRow(db, &servedOutbox, fixture.servedOutbox.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), servedOutbox.Status)
	require.NotNil(t, servedOutbox.SentAt)
	assert.Equal(t, spec.alreadySentAt, servedOutbox.SentAt.UTC())

	var updatedPendingOutbox deliveryTestOutboxModel

	require.NoError(t, firstDeliveryTestRow(db, &updatedPendingOutbox, fixture.pendingOutbox.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), updatedPendingOutbox.Status)
	require.NotNil(t, updatedPendingOutbox.SentAt)
	assert.WithinDuration(t, fixedSentAt, updatedPendingOutbox.SentAt.UTC(), 2*time.Minute)
}

func assertRecoverySelectiveSendTracking(
	t *testing.T,
	db *deliveryTestDB,
	fixture recoveryInputFixture,
	spec recoveryInputFixtureSpec,
	fixedSentAt time.Time,
) {
	t.Helper()

	var updatedSentTracking deliveryTestTrackingModel

	require.NoError(t, firstDeliveryTestRowWhere(db, &updatedSentTracking, "kind = ? AND content_id = ?", string(fixture.sentOutbox.Kind), fixture.sentOutbox.ContentID).Error)
	require.NotNil(t, updatedSentTracking.AlarmSentAt)
	assert.Equal(t, spec.alreadySentAt, updatedSentTracking.AlarmSentAt.UTC())
	assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), updatedSentTracking.DeliveryStatus)

	var updatedPendingTracking deliveryTestTrackingModel

	require.NoError(t, firstDeliveryTestRowWhere(db, &updatedPendingTracking, "kind = ? AND content_id = ?", string(fixture.pendingOutbox.Kind), fixture.pendingOutbox.ContentID).Error)
	require.NotNil(t, updatedPendingTracking.AlarmSentAt)
	assert.WithinDuration(t, fixedSentAt, updatedPendingTracking.AlarmSentAt.UTC(), 2*time.Minute)
	assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), updatedPendingTracking.DeliveryStatus)

	var updatedSentState domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &updatedSentState, "kind = ? AND post_id = ?", fixture.sentOutbox.Kind, fixture.sentPostID).Error)
	require.NotNil(t, updatedSentState.AlarmSentAt)
	assert.Equal(t, spec.alreadySentAt, updatedSentState.AlarmSentAt.UTC())
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, updatedSentState.DeliveryStatus)

	var updatedPendingState domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &updatedPendingState, "kind = ? AND post_id = ?", fixture.pendingOutbox.Kind, fixture.pendingPostID).Error)
	assert.Nil(t, updatedPendingState.AuthorizedAt)
	require.NotNil(t, updatedPendingState.AlarmSentAt)
	assert.WithinDuration(t, fixedSentAt, updatedPendingState.AlarmSentAt.UTC(), 2*time.Minute)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, updatedPendingState.DeliveryStatus)
}
