package observation

import (
	"context"
	"testing"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

func newTrackingTestDB(t *testing.T) *pgxpool.Pool {
	return newTrackingTestDBWithMaxOpenConns(t, 1)
}

func newTrackingTestDBWithMaxOpenConns(t *testing.T, maxOpenConns int) *pgxpool.Pool {
	t.Helper()
	if maxOpenConns < 1 {
		maxOpenConns = 1
	}
	pool := dbtest.NewPool(t)
	pool.Config().MaxConns = int32(maxOpenConns)
	return pool
}

func selectTrackingRowsForTest(t *testing.T, db trackingDB) []domain.YouTubeContentAlarmTracking {
	t.Helper()
	var rows []domain.YouTubeContentAlarmTracking
	require.NoError(t, pgxscan.Select(context.Background(), db, &rows, `
		SELECT kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at,
		       alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status,
		       COALESCE(latency_classification_status, '') AS latency_classification_status,
		       COALESCE(delay_source, '') AS delay_source,
		       COALESCE(internal_delay_cause, '') AS internal_delay_cause,
		       created_at, updated_at
		FROM youtube_content_alarm_tracking
		ORDER BY content_id ASC
	`))
	return rows
}

func insertAlarmStatesForTest(t *testing.T, db trackingDB, rows []domain.YouTubeCommunityShortsAlarmState) {
	t.Helper()
	now := time.Now().UTC()
	for i := range rows {
		row := rows[i]
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = now
		}
		_, err := dbx.ExecSQL(context.Background(), db, "insert alarm state for test", `
			INSERT INTO youtube_community_shorts_alarm_states
				(kind, post_id, content_id, channel_id, actual_published_at, detected_at,
				 published_at_retry_after, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, row.Kind, row.PostID, row.ContentID, row.ChannelID, row.ActualPublishedAt,
			row.DetectedAt, row.PublishedAtRetryAfter, row.AuthorizedAt, row.AlarmSentAt,
			row.DeliveryStatus, row.CreatedAt, row.UpdatedAt)
		require.NoError(t, err)
	}
}

func ensureObservationWindowForBaselineTest(t *testing.T, db trackingDB, row *domain.YouTubeCommunityShortsObservationPostBaseline) {
	t.Helper()
	observationEndedAt := row.FinalizedAt
	if observationEndedAt.IsZero() {
		observationEndedAt = time.Now().UTC()
	}
	observationStartedAt := observationEndedAt.Add(-24 * time.Hour)
	deploymentCompletedAt := observationStartedAt.Add(-time.Minute)
	_, err := dbx.ExecSQL(context.Background(), db, "insert observation window for baseline test", `
		INSERT INTO youtube_community_shorts_observation_windows
			(runtime_name, bigbang_cutover_at, app_version, target_channel_count,
			 deployment_completed_at, observation_started_at, observation_ended_at,
			 closed_at, finalized_post_baseline_at, finalized_post_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (runtime_name, bigbang_cutover_at) DO UPDATE
		SET observation_started_at = EXCLUDED.observation_started_at,
		    observation_ended_at = EXCLUDED.observation_ended_at,
		    closed_at = EXCLUDED.closed_at,
		    finalized_post_baseline_at = EXCLUDED.finalized_post_baseline_at,
		    finalized_post_count = EXCLUDED.finalized_post_count,
		    updated_at = EXCLUDED.updated_at
	`, row.RuntimeName, row.BigBangCutoverAt, "test", 1,
		deploymentCompletedAt, observationStartedAt, observationEndedAt,
		observationEndedAt, observationEndedAt, 0, deploymentCompletedAt, observationEndedAt)
	require.NoError(t, err)

	var exists bool
	err = db.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM youtube_community_shorts_observation_windows
			WHERE runtime_name = $1 AND bigbang_cutover_at = $2
		)
	`, row.RuntimeName, row.BigBangCutoverAt).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists)
}

func insertObservationBaselinesForTest(t *testing.T, db trackingDB, rows []domain.YouTubeCommunityShortsObservationPostBaseline) {
	t.Helper()
	now := time.Now().UTC()
	for i := range rows {
		row := rows[i]
		ensureObservationWindowForBaselineTest(t, db, &row)
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = now
		}
		_, err := dbx.ExecSQL(context.Background(), db, "insert observation baseline for test", `
			INSERT INTO youtube_community_shorts_observation_post_baselines
				(runtime_name, bigbang_cutover_at, kind, post_id, channel_id,
				 actual_published_at, detected_at, finalized_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, row.RuntimeName, row.BigBangCutoverAt, row.Kind, row.PostID, row.ChannelID,
			row.ActualPublishedAt, row.DetectedAt, row.FinalizedAt, row.CreatedAt, row.UpdatedAt)
		require.NoError(t, err)
	}
}

func newLegacyAlarmStateTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := newTrackingTestDB(t)
	_, err := db.Exec(context.Background(), `DROP TABLE youtube_community_shorts_alarm_states`)
	require.NoError(t, err)
	_, err = db.Exec(context.Background(), `
		CREATE TABLE youtube_community_shorts_alarm_states (
			kind TEXT NOT NULL,
			post_id TEXT NOT NULL,
			content_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			actual_published_at TIMESTAMPTZ,
			detected_at TIMESTAMPTZ NOT NULL,
			authorized_at TIMESTAMPTZ,
			alarm_sent_at TIMESTAMPTZ,
			delivery_status TEXT NOT NULL DEFAULT 'DETECTED',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (kind, post_id)
		)
	`)
	require.NoError(t, err)
	return db
}
