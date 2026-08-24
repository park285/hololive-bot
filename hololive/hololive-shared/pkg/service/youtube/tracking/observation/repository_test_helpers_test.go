package observation

import (
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testChannelID               = "UC_TEST"
	testShortChannelID          = "UC_SHORT"
	testCommunityPostID         = "post-1"
	testShortContentID          = "short-1"
	testShortCanonicalPostID    = "short:short-1"
	testDuplicateDeliveryPostID = "post-duplicate-delivery"

	testDuplicateDeliveryCanonicalPostID = "community:post-duplicate-delivery"
)

func newTrackingTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

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

	require.NoError(t, pgxscan.Select(t.Context(), db, &rows, `
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
