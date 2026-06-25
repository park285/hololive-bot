package dispatch

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
)

type retryFinalizeOnceTestCase struct {
	name                  string
	kind                  domain.OutboxKind
	channelID             string
	contentID             string
	roomID                string
	payload               string
	expectedMessageMarker string
}

func TestProcessOnce_RetryAfterCommunityShortsSendFailureSendsExactlyOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                  string
		kind                  domain.OutboxKind
		channelID             string
		contentID             string
		roomID                string
		payload               string
		expectedMessageMarker string
	}{
		{
			name:                  "community",
			kind:                  domain.OutboxKindCommunityPost,
			channelID:             "UC_retry_exact_once_community",
			contentID:             "post-retry-exact-once",
			roomID:                "room-community",
			payload:               `{"post_id":"post-retry-exact-once","content_text":"community retry body","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "community retry body",
		},
		{
			name:                  "shorts",
			kind:                  domain.OutboxKindNewShort,
			channelID:             "UC_retry_exact_once_shorts",
			contentID:             "short-retry-exact-once",
			roomID:                "room-shorts",
			payload:               `{"video_id":"short-retry-exact-once","title":"short retry title","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "short retry title",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			db := newDeliveryPool(t)

			now := time.Now().UTC()
			item := domain.YouTubeNotificationOutbox{
				Kind:          tc.kind,
				ChannelID:     tc.channelID,
				ContentID:     tc.contentID,
				Payload:       tc.payload,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: now,
			}
			require.NoError(t, insertDeliveryTestRows(db, &item).Error)
			postID := store.CanonicalDeliveryPostID(item.Kind, item.ContentID)
			require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
				Kind:               string(item.Kind),
				ContentID:          item.ContentID,
				CanonicalContentID: postID,
				ChannelID:          item.ChannelID,
				DetectedAt:         now,
			}).Error)

			delivery := domain.YouTubeNotificationDelivery{
				OutboxID:      item.ID,
				RoomID:        tc.roomID,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: now,
			}
			require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

			sender := &testSender{failRoom: map[string]bool{tc.roomID: true}}
			dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &Config{
				BatchSize:           10,
				LockTimeout:         time.Minute,
				PollInterval:        time.Second,
				MaxRetries:          3,
				RetryBackoff:        time.Minute,
				DeliveryParallelism: 1,
			})

			dispatcher.ProcessOnceForTest(ctx)

			var failedDelivery deliveryTestDeliveryModel
			require.NoError(t, firstDeliveryTestRow(db, &failedDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusPending), failedDelivery.Status)
			assert.Equal(t, 1, failedDelivery.AttemptCount)
			assert.Nil(t, failedDelivery.LockedAt)
			assert.Nil(t, failedDelivery.SentAt)

			var failedOutbox deliveryTestOutboxModel
			require.NoError(t, firstDeliveryTestRow(db, &failedOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusPending), failedOutbox.Status)
			assert.Nil(t, failedOutbox.SentAt)

			var releasedState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, firstDeliveryTestRow(db, &releasedState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
			assert.Nil(t, releasedState.AuthorizedAt)
			assert.Nil(t, releasedState.AlarmSentAt)
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, releasedState.DeliveryStatus)

			sender.mu.Lock()
			delete(sender.failRoom, tc.roomID)
			firstAttemptMessages := append([]string(nil), sender.messages...)
			sender.mu.Unlock()
			require.Len(t, firstAttemptMessages, 0)

			retryAt := time.Now().UTC().Add(-time.Second)
			require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationDelivery{}, map[string]any{"next_attempt_at": retryAt, "locked_at": nil}, "id = ?", delivery.ID).Error)

			dispatcher.ProcessOnceForTest(ctx)
			dispatcher.ProcessOnceForTest(ctx)

			var sentDelivery deliveryTestDeliveryModel
			require.NoError(t, firstDeliveryTestRow(db, &sentDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentDelivery.Status)
			assert.Equal(t, 1, sentDelivery.AttemptCount)
			require.NotNil(t, sentDelivery.SentAt)

			var sentOutbox deliveryTestOutboxModel
			require.NoError(t, firstDeliveryTestRow(db, &sentOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentOutbox.Status)
			require.NotNil(t, sentOutbox.SentAt)

			var sentTracking deliveryTestTrackingModel
			require.NoError(t, firstDeliveryTestRowWhere(db, &sentTracking, "kind = ? AND content_id = ?", string(item.Kind), item.ContentID).Error)
			require.NotNil(t, sentTracking.AlarmSentAt)
			assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), sentTracking.DeliveryStatus)

			var sentState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, firstDeliveryTestRow(db, &sentState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
			assert.Nil(t, sentState.AuthorizedAt)
			require.NotNil(t, sentState.AlarmSentAt)
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, sentState.DeliveryStatus)

			var deliveryRows []deliveryTestDeliveryModel
			require.NoError(t, findDeliveryTestRowsOrderedWhere(db, &deliveryRows, "id ASC", "outbox_id = ?", item.ID).Error)
			require.Len(t, deliveryRows, 1)

			sender.mu.Lock()
			messages := append([]string(nil), sender.messages...)
			sender.mu.Unlock()
			require.Len(t, messages, 1)
			assert.Contains(t, messages[0], tc.roomID+":")
			assert.Contains(t, messages[0], tc.expectedMessageMarker)
		})
	}
}

type postSendFinalizeFailureSender struct {
	mu           sync.Mutex
	messages     []string
	afterSend    func(roomID, message string) error
	afterSendErr error
}

func (s *postSendFinalizeFailureSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	s.messages = append(s.messages, roomID+":"+message)
	hook := s.afterSend
	s.mu.Unlock()

	if hook != nil {
		if err := hook(roomID, message); err != nil {
			s.mu.Lock()
			if s.afterSendErr == nil {
				s.afterSendErr = err
			}
			s.mu.Unlock()
		}
	}

	return nil
}

func (s *postSendFinalizeFailureSender) sentMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := make([]string, len(s.messages))
	copy(cloned, s.messages)
	return cloned
}

func (s *postSendFinalizeFailureSender) hookError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.afterSendErr
}

func TestProcessOnce_RetryAfterCommunityShortsPostSendFinalizeFailureKeepsSingleDeliveredAlarm(t *testing.T) {
	t.Parallel()

	testCases := []retryFinalizeOnceTestCase{
		{
			name:                  "community",
			kind:                  domain.OutboxKindCommunityPost,
			channelID:             "UC_retry_finalize_community",
			contentID:             "post-retry-finalize-once",
			roomID:                "room-community",
			payload:               `{"canonical_post_id":"community:post-retry-finalize-once","post_id":"post-retry-finalize-once","content_text":"community finalize body","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "community finalize body",
		},
		{
			name:                  "shorts",
			kind:                  domain.OutboxKindNewShort,
			channelID:             "UC_retry_finalize_shorts",
			contentID:             "short-retry-finalize-once",
			roomID:                "room-shorts",
			payload:               `{"canonical_post_id":"short:short-retry-finalize-once","video_id":"short-retry-finalize-once","title":"short finalize title","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "short finalize title",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runRetryAfterPostSendFinalizeFailureKeepsSingleDeliveredAlarm(t, &tc)
		})
	}
}

func runRetryAfterPostSendFinalizeFailureKeepsSingleDeliveredAlarm(
	t *testing.T,
	tc *retryFinalizeOnceTestCase,
) {
	t.Helper()

	ctx := context.Background()
	db := newDeliveryPool(t)

	now := time.Now().UTC()
	item, delivery := insertRetryFinalizeOnceRows(t, db, tc, now)

	postID := store.CanonicalDeliveryPostID(item.Kind, item.ContentID)
	sender := newPostSendFinalizeFailureSender(db, item.Kind, postID, now.Add(-10*time.Minute))
	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.ProcessOnceForTest(ctx)
	require.NoError(t, sender.hookError())
	require.Len(t, sender.sentMessages(), 1)

	assertRetryFinalizeOncePendingState(t, db, item.ID, delivery.ID)

	retryAt := time.Now().UTC().Add(-time.Second)
	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationDelivery{}, map[string]any{"next_attempt_at": retryAt, "locked_at": nil}, "id = ?", delivery.ID).Error)

	dispatcher.ProcessOnceForTest(ctx)

	assertRetryFinalizeOnceSentState(t, db, &item, delivery.ID, postID)
	assertRetryFinalizeOnceMessages(t, sender, tc)
}

func insertRetryFinalizeOnceRows(
	t *testing.T,
	db *pgxpool.Pool,
	tc *retryFinalizeOnceTestCase,
	now time.Time,
) (domain.YouTubeNotificationOutbox, domain.YouTubeNotificationDelivery) {
	t.Helper()

	item := domain.YouTubeNotificationOutbox{
		Kind:          tc.kind,
		ChannelID:     tc.channelID,
		ContentID:     tc.contentID,
		Payload:       tc.payload,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
		Kind:               string(item.Kind),
		ContentID:          item.ContentID,
		CanonicalContentID: store.CanonicalDeliveryPostID(item.Kind, item.ContentID),
		ChannelID:          item.ChannelID,
		DetectedAt:         now,
	}).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        tc.roomID,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)
	return item, delivery
}

func newPostSendFinalizeFailureSender(
	db *pgxpool.Pool,
	kind domain.OutboxKind,
	postID string,
	staleAuthorizedAt time.Time,
) *postSendFinalizeFailureSender {
	var mutateOnce sync.Once
	return &postSendFinalizeFailureSender{
		afterSend: func(_ string, _ string) error {
			var hookErr error
			mutateOnce.Do(func() {
				hookErr = staleRetryFinalizeOnceClaim(db, kind, postID, staleAuthorizedAt)
			})
			return hookErr
		},
	}
}

func staleRetryFinalizeOnceClaim(
	db *pgxpool.Pool,
	kind domain.OutboxKind,
	postID string,
	staleAuthorizedAt time.Time,
) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		result := updateDeliveryTestRowsWhere(db, &domain.YouTubeCommunityShortsAlarmState{}, map[string]any{
			"authorized_at": staleAuthorizedAt,
			"updated_at":    time.Now().UTC(),
		}, "kind = ? AND post_id = ?", kind, postID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("post-send finalize hook: claim row not found for %s", postID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertRetryFinalizeOncePendingState(t *testing.T, db *pgxpool.Pool, outboxID, deliveryID int64) {
	t.Helper()

	var pendingDelivery deliveryTestDeliveryModel
	require.NoError(t, firstDeliveryTestRow(db, &pendingDelivery, deliveryID).Error)
	assert.Equal(t, string(domain.OutboxStatusPending), pendingDelivery.Status)
	assert.Nil(t, pendingDelivery.SentAt)

	var pendingOutbox deliveryTestOutboxModel
	require.NoError(t, firstDeliveryTestRow(db, &pendingOutbox, outboxID).Error)
	assert.Equal(t, string(domain.OutboxStatusPending), pendingOutbox.Status)
	assert.Nil(t, pendingOutbox.SentAt)
}

func assertRetryFinalizeOnceSentState(
	t *testing.T,
	db *pgxpool.Pool,
	item *domain.YouTubeNotificationOutbox,
	deliveryID int64,
	postID string,
) {
	t.Helper()

	var sentDelivery deliveryTestDeliveryModel
	require.NoError(t, firstDeliveryTestRow(db, &sentDelivery, deliveryID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), sentDelivery.Status)
	require.NotNil(t, sentDelivery.SentAt)

	var sentOutbox deliveryTestOutboxModel
	require.NoError(t, firstDeliveryTestRow(db, &sentOutbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), sentOutbox.Status)
	require.NotNil(t, sentOutbox.SentAt)

	var sentTracking deliveryTestTrackingModel
	require.NoError(t, firstDeliveryTestRowWhere(db, &sentTracking, "kind = ? AND content_id = ?", string(item.Kind), item.ContentID).Error)
	require.NotNil(t, sentTracking.AlarmSentAt)
	assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), sentTracking.DeliveryStatus)

	var sentState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &sentState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
	assert.Nil(t, sentState.AuthorizedAt)
	require.NotNil(t, sentState.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, sentState.DeliveryStatus)

	var deliveryRows []deliveryTestDeliveryModel
	require.NoError(t, findDeliveryTestRowsOrderedWhere(db, &deliveryRows, "id ASC", "outbox_id = ?", item.ID).Error)
	require.Len(t, deliveryRows, 1)
}

func assertRetryFinalizeOnceMessages(
	t *testing.T,
	sender *postSendFinalizeFailureSender,
	tc *retryFinalizeOnceTestCase,
) {
	t.Helper()

	messages := sender.sentMessages()
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], tc.roomID+":")
	assert.Contains(t, messages[0], tc.expectedMessageMarker)
}
