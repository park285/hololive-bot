package youtubedispatch

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v4/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

	testCases := []retryFinalizeOnceTestCase{
		{
			name:                  testCaseNameCommunity,
			kind:                  domain.OutboxKindCommunityPost,
			channelID:             "UC_retry_exact_once_community",
			contentID:             "post-retry-exact-once",
			roomID:                testRoomCommunity,
			payload:               `{"canonical_post_id":"community:post-retry-exact-once","post_id":"post-retry-exact-once","content_text":"community retry body","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "community retry body",
		},
		{
			name:                  testCaseNameShorts,
			kind:                  domain.OutboxKindNewShort,
			channelID:             "UC_retry_exact_once_shorts",
			contentID:             "short-retry-exact-once",
			roomID:                testRoomShorts,
			payload:               `{"canonical_post_id":"short:short-retry-exact-once","video_id":"short-retry-exact-once","title":"short retry title","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "short retry title",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runRetryExactOnceCase(t, tc)
		})
	}
}

func runRetryExactOnceCase(t *testing.T, tc retryFinalizeOnceTestCase) {
	t.Helper()

	ctx := t.Context()
	db := newDeliveryPool(t)
	item, delivery, postID := seedRetryExactOnceFixture(t, db, tc)

	sender := &testSender{failRoom: map[string]bool{tc.roomID: true}, failErr: iris.ErrRateLimited}
	dispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.ProcessOnceForTest(ctx)

	assertRetryExactOnceFirstAttemptDeferred(t, db, item, delivery.ID, postID)

	sender.mu.Lock()
	delete(sender.failRoom, tc.roomID)

	firstAttemptMessages := append([]string(nil), sender.messages...)
	sender.mu.Unlock()
	require.Empty(t, firstAttemptMessages)

	retryAt := time.Now().UTC().Add(-time.Second)
	require.NoError(t, updateDeliveryTestRowByID(db, &domain.YouTubeNotificationDelivery{}, map[string]any{"next_attempt_at": retryAt, "locked_at": nil}, delivery.ID).Error)

	dispatcher.ProcessOnceForTest(ctx)
	dispatcher.ProcessOnceForTest(ctx)

	snapshot := assertCommunityShortsPostSent(t, db, item, delivery.ID, postID)
	assert.Equal(t, 1, snapshot.delivery.AttemptCount)

	var deliveryRows []deliveryTestDeliveryModel

	require.NoError(t, findDeliveryTestRowsOrderedWhere(db, &deliveryRows, "id ASC", "outbox_id = $1", item.ID).Error)
	require.Len(t, deliveryRows, 1)

	sender.mu.Lock()

	messages := append([]string(nil), sender.messages...)
	sender.mu.Unlock()
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], tc.roomID+":")
	assert.Contains(t, messages[0], tc.expectedMessageMarker)

	assertRetryExactOnceAttemptTelemetry(t, db, delivery.ID, postID)
}

// 두 시도(rate limit 실패, 재시도 성공)는 각 lifecycle 전이 트랜잭션이 한 행씩 기록한다.
// 실패 사유는 lifecycle Reason 어휘를 쓴다.
func assertRetryExactOnceAttemptTelemetry(t *testing.T, db *pgxpool.Pool, deliveryID int64, postID string) {
	t.Helper()

	queryRows, err := db.Query(t.Context(), `
		SELECT attempt_ordinal, post_id, delivery_mode, send_result, COALESCE(failure_reason, '')
		FROM youtube_notification_delivery_telemetry
		WHERE delivery_id = $1
		ORDER BY attempt_ordinal
	`, deliveryID)
	require.NoError(t, err)

	defer queryRows.Close()

	var got []string

	for queryRows.Next() {
		var (
			ordinal                            int
			rowPostID, mode, result, reasonTag string
		)

		require.NoError(t, queryRows.Scan(&ordinal, &rowPostID, &mode, &result, &reasonTag))
		assert.Equal(t, postID, rowPostID)

		got = append(got, fmt.Sprintf("%d:%s:%s:%s", ordinal, mode, result, reasonTag))
	}

	require.NoError(t, queryRows.Err())
	assert.Equal(t, []string{
		"1:" + deliveryModePerRoom + ":" + sendResultFailure + ":" + string(lifecycleReasonRateLimited),
		"2:" + deliveryModePerRoom + ":" + sendResultSuccess + ":",
	}, got)
}

func seedRetryExactOnceFixture(
	t *testing.T,
	db *pgxpool.Pool,
	tc retryFinalizeOnceTestCase,
) (domain.YouTubeNotificationOutbox, domain.YouTubeNotificationDelivery, string) {
	t.Helper()

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

	postID := mustCanonicalDeliveryPostID(item.Kind, item.ContentID)
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

	return item, delivery, postID
}

func assertRetryExactOnceFirstAttemptDeferred(
	t *testing.T,
	db *pgxpool.Pool,
	item domain.YouTubeNotificationOutbox,
	deliveryID int64,
	postID string,
) {
	t.Helper()

	var failedDelivery deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &failedDelivery, deliveryID).Error)
	assert.Equal(t, string(domain.OutboxStatusPending), failedDelivery.Status)
	assert.Equal(t, 1, failedDelivery.AttemptCount)
	assert.Nil(t, failedDelivery.LockedAt)
	assert.Nil(t, failedDelivery.SentAt)

	var failedOutbox deliveryTestOutboxModel

	require.NoError(t, firstDeliveryTestRow(db, &failedOutbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusPending), failedOutbox.Status)
	assert.Nil(t, failedOutbox.SentAt)

	var sentStateCount int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeCommunityShortsAlarmState{}, &sentStateCount,
		"kind = $1 AND post_id = $2 AND alarm_sent_at IS NOT NULL", item.Kind, postID).Error)
	assert.Zero(t, sentStateCount, "failed attempt must not mark the post sent")
}
