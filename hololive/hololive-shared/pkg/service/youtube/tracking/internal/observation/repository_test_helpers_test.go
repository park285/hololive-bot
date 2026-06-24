package observation

import (
	"context"
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
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
