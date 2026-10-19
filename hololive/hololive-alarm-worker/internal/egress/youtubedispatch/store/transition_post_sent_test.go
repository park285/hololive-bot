package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// community·shorts 첫 성공은 사전 표시 없이 CompleteSent 한 트랜잭션에서 tracking과 post 단위 알람 상태를 SENT로 만든다.
// 같은 post의 다른 room이 뒤에 성공해도 post 단위 발송 시각은 가장 이른 값을 유지한다.
func TestCompleteSentMarksPostSentWithoutClaim(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	const (
		videoID   = "short-post-sent"
		postID    = "short:" + videoID
		firstRoom = "room-post-sent-a"
		laterRoom = "room-post-sent-b"
	)

	seedTransitionPostSentRows(t, pool, videoID, postID, firstRoom, laterRoom)

	transition := newTestTransitionStore(t, pool)
	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)
	require.Empty(t, prepared.Blocked)
	require.Len(t, prepared.ActiveRows, 2)

	rowsByRoom := make(map[string]domain.YouTubeNotificationDelivery, len(prepared.ActiveRows))

	for _, row := range prepared.ActiveRows {
		rowsByRoom[row.RoomID] = row
	}

	firstSentAt := completeTransitionPostSentRoom(t, transition, pool, rowsByRoom[firstRoom], outboxes)
	requireTransitionPostSent(t, pool, postID, firstSentAt)

	laterSentAt := completeTransitionPostSentRoom(t, transition, pool, rowsByRoom[laterRoom], outboxes)
	require.True(t, laterSentAt.After(firstSentAt))
	requireTransitionPostSent(t, pool, postID, firstSentAt)
}

// seedTransitionPostSentRows는 API가 outbox와 함께 만드는 미발송 tracking 행과 두 room의 delivery를 만든다.
// 알람 상태 행(post 단위)은 만들지 않는다.
func seedTransitionPostSentRows(t *testing.T, pool *pgxpool.Pool, videoID, postID string, roomIDs ...string) {
	t.Helper()

	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)

	var outboxID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_outbox (
			kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at
		) VALUES ($1, $2, $3, $4::jsonb, $5, 0, $6, $6)
		RETURNING id
	`, domain.OutboxKindNewShort, "channel-post-sent", videoID, transitionShortTelemetryPayload(videoID),
		domain.OutboxStatusPending, createdAt).Scan(&outboxID))

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_alarm_tracking (
			kind, content_id, canonical_content_id, channel_id, detected_at, delivery_status
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, domain.OutboxKindNewShort, videoID, postID, "channel-post-sent", createdAt,
		domain.YouTubeContentAlarmDeliveryStatusPending)
	require.NoError(t, err)

	for _, roomID := range roomIDs {
		_, err = pool.Exec(t.Context(), `
			INSERT INTO youtube_notification_delivery (
				outbox_id, room_id, status, attempt_count, next_attempt_at, created_at
			) VALUES ($1, $2, $3, 0, $4, $4)
		`, outboxID, roomID, domain.OutboxStatusPending, createdAt)
		require.NoError(t, err)
	}
}

func completeTransitionPostSentRoom(
	t *testing.T,
	transition *TransitionStore,
	pool *pgxpool.Pool,
	row domain.YouTubeNotificationDelivery,
	outboxes map[int64]domain.YouTubeNotificationOutbox,
) time.Time {
	t.Helper()

	operation, begin, err := transition.BeginSending(t.Context(), []domain.YouTubeNotificationDelivery{row}, outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, begin.Outcome)

	completed, err := transition.CompleteSent(t.Context(), operation, DeliveryModePerRoom)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, completed.Outcome)

	var sentAt time.Time

	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT sent_at FROM youtube_notification_delivery WHERE id = $1`, row.ID).Scan(&sentAt))

	return sentAt.UTC()
}

func requireTransitionPostSent(t *testing.T, pool *pgxpool.Pool, postID string, wantSentAt time.Time) {
	t.Helper()

	var (
		stateSentAt    time.Time
		stateStatus    string
		trackingSentAt time.Time
		trackingStatus string
	)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT alarm_sent_at, delivery_status
		FROM youtube_community_shorts_alarm_states
		WHERE kind = $1 AND post_id = $2
	`, domain.OutboxKindNewShort, postID).Scan(&stateSentAt, &stateStatus))
	require.Equal(t, wantSentAt, stateSentAt.UTC())
	require.Equal(t, string(domain.YouTubeCommunityShortsAlarmStateStatusSent), stateStatus)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT alarm_sent_at, delivery_status
		FROM youtube_content_alarm_tracking
		WHERE kind = $1 AND canonical_content_id = $2
	`, domain.OutboxKindNewShort, postID).Scan(&trackingSentAt, &trackingStatus))
	require.Equal(t, wantSentAt, trackingSentAt.UTC())
	require.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), trackingStatus)
}
