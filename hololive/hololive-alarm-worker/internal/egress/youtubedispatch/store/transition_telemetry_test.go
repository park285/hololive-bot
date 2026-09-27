package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

// delivery telemetry는 lifecycle 전이 트랜잭션 안에서 owner 시도 하나당 한 행만 기록한다
// (DEC-20260926-hololive-delivery-telemetry-single-path). 팔로워(follower)는 시도가 아니라 기록하지 않는다.
type transitionTelemetryRow struct {
	DeliveryID     int64
	AttemptOrdinal int
	PostID         string
	DeliveryMode   string
	SendResult     string
	FailureReason  string
}

func TestTransitionApplyPreparedFailureRecordsOwnerAttemptTelemetry(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	ownerID, followerID := seedTransitionShortLogicalGroup(t, pool, "short-telemetry-prepared", "room-telemetry-prepared")
	transition := newTestTransitionStore(t, pool)
	prepared, outboxes := claimTransitionTestActiveRows(t, transition, pool)

	result, err := transition.ApplyPreparedFailure(
		ctx, prepared, outboxes, lifecycle.FailureRetryable, "message_missing", 0, DeliveryModePerRoom,
	)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	// 전이에 도달한 행은 payload canonical_post_id가 있고 content_id와 일치하므로, logical key는 claim 경로가 아직 쓰는
	// ResolveTelemetryPostID의 첫 후보(canonical_post_id)와 같은 값이다.
	postID := telemetry.ResolveTelemetryPostID(domain.OutboxKindNewShort, "short-telemetry-prepared",
		transitionShortTelemetryPayload("short-telemetry-prepared"))
	require.Equal(t, "short:short-telemetry-prepared", postID)

	// owner와 follower는 서로 다른 delivery이므로, owner 한 행뿐인 정확한 목록은 follower가 기록하지 않았음을 뜻한다.
	require.NotEqual(t, followerID, ownerID)
	require.Equal(t, []transitionTelemetryRow{{
		DeliveryID: ownerID, AttemptOrdinal: 1, PostID: postID,
		DeliveryMode: string(DeliveryModePerRoom), SendResult: attemptResultFailure, FailureReason: "message_missing",
	}}, loadTransitionTelemetryRows(t, pool))
}

// revive는 attempt_count를 0으로 되돌린다. 순번을 attempt_count에서 만들면 이전 시도와 겹쳐 새 시도가 사라지므로,
// 이미 기록된 최대 순번 다음 값을 써야 한다.
func TestTransitionAttemptTelemetryOrdinalContinuesAfterRevive(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	ownerID, _ := seedTransitionShortLogicalGroup(t, pool, "short-telemetry-revive", "room-telemetry-revive")
	seedTransitionTelemetryAttempt(t, pool, ownerID, 1)
	seedTransitionTelemetryAttempt(t, pool, ownerID, 2)

	transition := newTestTransitionStore(t, pool)
	prepared, outboxes := claimTransitionTestActiveRows(t, transition, pool)

	result, err := transition.ApplyPreparedFailure(
		ctx, prepared, outboxes, lifecycle.FailureRetryable, "pre_send_claim", 0, DeliveryModeGrouped,
	)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	rows := loadTransitionTelemetryRows(t, pool)
	require.Len(t, rows, 3)
	require.Equal(t, 3, rows[2].AttemptOrdinal)
	require.Equal(t, string(DeliveryModeGrouped), rows[2].DeliveryMode)
	require.Equal(t, "pre_send_claim", rows[2].FailureReason)
}

// telemetry processor는 방출한 행을 retention(기본 24h)이 지나면 지운다. 그 뒤 재시도나 재개된 발송이 순번을
// 1부터 다시 쓰면 감사 로그에 같은 (delivery_id, attempt_ordinal)이 두 번 나오므로, revive 전까지 누적되는
// attempt_count를 하한으로 쓴다.
func TestTransitionAttemptTelemetryOrdinalKeepsAttemptCountAfterRetention(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	ownerID, _ := seedTransitionShortLogicalGroup(t, pool, "short-telemetry-retained", "room-telemetry-retained")
	// 앞선 세 번의 실패 시도 telemetry는 retention cleanup으로 이미 지워졌고 attempt_count만 남은 상태다.
	_, err := pool.Exec(ctx, `UPDATE youtube_notification_delivery SET attempt_count = 3 WHERE id = $1`, ownerID)
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)
	prepared, outboxes := claimTransitionTestActiveRows(t, transition, pool)

	result, err := transition.ApplyPreparedFailure(
		ctx, prepared, outboxes, lifecycle.FailureRetryable, "message_missing", 0, DeliveryModePerRoom,
	)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	rows := loadTransitionTelemetryRows(t, pool)
	require.Len(t, rows, 1)
	require.Equal(t, ownerID, rows[0].DeliveryID)
	require.Equal(t, 4, rows[0].AttemptOrdinal)
}

// 결과 불명 시도는 공백으로 두지 않는다(DEC-20260731-reply-outcome-unknown-fail-closed). 격리 sweep의
// 트랜잭션이 owner 시도를 outcome_unknown으로 기록한다.
func TestTransitionQuarantineRecordsOutcomeUnknownAttempt(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	ownerID, _ := seedTransitionShortLogicalGroup(t, pool, "short-telemetry-quarantine", "room-telemetry-quarantine")
	staleLockedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET status = $1, locked_at = $2, row_version = 2
		WHERE id = $3
	`, DeliveryStatusSending, staleLockedAt, ownerID)
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)

	result, err := transition.QuarantineStaleLogicalGroups(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, 2, result.QuarantinedDeliveries)

	require.Equal(t, []transitionTelemetryRow{{
		DeliveryID: ownerID, AttemptOrdinal: 1, PostID: "short:short-telemetry-quarantine",
		DeliveryMode: string(deliveryModeStaleSweep), SendResult: attemptResultOutcomeUnknown,
		FailureReason: string(staleSendingOutcomeUnknownReason),
	}}, loadTransitionTelemetryRows(t, pool))
}

// 호출자가 넘기는 발송 방식은 per_room·grouped뿐이다. 잘못된 값은 DB를 건드리기 전에 거절한다.
func TestTransitionRejectsInvalidCallerDeliveryMode(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	rows := []domain.YouTubeNotificationDelivery{{ID: 1, OutboxID: 1, RoomID: "room"}}

	for _, mode := range []DeliveryMode{"", deliveryModeStaleSweep, "recovered"} {
		result, err := transition.ApplyPreparedFailure(ctx, rows, nil, lifecycle.FailureRetryable, "message_missing", 0, mode)
		require.Error(t, err, "mode %q", mode)
		require.Equal(t, ApplyConflict, result.Outcome)
	}
}

func seedTransitionShortLogicalGroup(t *testing.T, pool *pgxpool.Pool, videoID, roomID string) (int64, int64) {
	t.Helper()

	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	payload := transitionShortTelemetryPayload(videoID)

	var ids [2]int64

	// 두 outbox의 content_id는 같은 short logical key로 정규화되어 한 logical group(owner 하나, follower 하나)을 이룬다.
	for index, contentID := range []string{videoID, " " + videoID + " "} {
		var outboxID int64

		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO youtube_notification_outbox (
				kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at
			) VALUES ($1, $2, $3, $4::jsonb, $5, 0, $6, $7)
			RETURNING id
		`, domain.OutboxKindNewShort, "channel-telemetry", contentID, payload, domain.OutboxStatusPending,
			createdAt, createdAt.Add(time.Duration(index)*time.Second)).Scan(&outboxID))

		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO youtube_notification_delivery (
				outbox_id, room_id, status, attempt_count, next_attempt_at, created_at
			) VALUES ($1, $2, $3, 0, $4, $5)
			RETURNING id
		`, outboxID, roomID, domain.OutboxStatusPending, createdAt, createdAt.Add(time.Duration(index)*time.Second)).Scan(&ids[index]))
	}

	return ids[0], ids[1]
}

// poller가 만드는 payload처럼 canonical_post_id는 prefix가 붙은 logical ID다.
func transitionShortTelemetryPayload(videoID string) string {
	return `{"canonical_post_id":"short:` + videoID + `","video_id":"` + videoID + `","title":"telemetry"}`
}

func claimTransitionTestActiveRows(
	t *testing.T,
	transition *TransitionStore,
	pool *pgxpool.Pool,
) ([]domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) {
	t.Helper()

	claimed, err := transition.ClaimPending(t.Context(), 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(t.Context(), claimed, outboxes)
	require.NoError(t, err)
	require.Empty(t, prepared.Blocked)
	require.Len(t, prepared.ActiveRows, 1)

	return prepared.ActiveRows, outboxes
}

func seedTransitionTelemetryAttempt(t *testing.T, pool *pgxpool.Pool, deliveryID int64, ordinal int) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_notification_delivery_telemetry (
			delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type,
			dedupe_key, delivery_path, delivery_mode, send_result, event_at, next_attempt_at
		)
		SELECT d.id, $2, d.outbox_id, o.channel_id, o.content_id, 'short:seed', d.room_id, 'SHORTS',
			'seed', 'youtube_outbox_dispatcher', 'per_room', 'failure', $3, $3
		FROM youtube_notification_delivery d
		JOIN youtube_notification_outbox o ON o.id = d.outbox_id
		WHERE d.id = $1
	`, deliveryID, ordinal, now)
	require.NoError(t, err)
}

func loadTransitionTelemetryRows(t *testing.T, pool *pgxpool.Pool) []transitionTelemetryRow {
	t.Helper()

	queryRows, err := pool.Query(t.Context(), `
		SELECT delivery_id, attempt_ordinal, post_id, delivery_mode, send_result, COALESCE(failure_reason, '')
		FROM youtube_notification_delivery_telemetry
		ORDER BY delivery_id, attempt_ordinal
	`)
	require.NoError(t, err)

	defer queryRows.Close()

	var rows []transitionTelemetryRow

	for queryRows.Next() {
		var row transitionTelemetryRow

		require.NoError(t, queryRows.Scan(&row.DeliveryID, &row.AttemptOrdinal, &row.PostID, &row.DeliveryMode, &row.SendResult, &row.FailureReason))

		rows = append(rows, row)
	}

	require.NoError(t, queryRows.Err())

	return rows
}
