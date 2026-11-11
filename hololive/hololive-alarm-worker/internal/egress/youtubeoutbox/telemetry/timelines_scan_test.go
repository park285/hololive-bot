package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/timeline"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestListPostDeliveryTimelinesReturnsUTCTimesAndNullableColumns(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := mustNewTestRepository(t, pool)
	detectedAt := time.Date(2026, time.June, 4, 21, 0, 0, 0, time.FixedZone("KST", 9*60*60))
	sentAt := detectedAt.Add(30 * time.Second)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded)
		VALUES ($1, 'utc-row', 'utc-row', 'UC_classification', $2, $3, 30000, true)
	`, string(domain.OutboxKindCommunityPost), detectedAt, sentAt)
	require.NoError(t, err)

	timelines, err := repository.ListPostDeliveryTimelinesByTrackingIdentities(ctx, []timeline.PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "utc-row"},
	})
	require.NoError(t, err)
	require.Len(t, timelines, 1)

	row := timelines[0]
	require.NotNil(t, row.DetectedAt)
	require.Equal(t, time.UTC, row.DetectedAt.Location())
	require.True(t, row.DetectedAt.Equal(detectedAt))
	require.NotNil(t, row.AlarmSentAt)
	require.Equal(t, time.UTC, row.AlarmSentAt.Location())
	require.True(t, row.AlarmSentAt.Equal(sentAt))
	require.Nil(t, row.ActualPublishedAt)
	require.Nil(t, row.FirstAttemptStartedAt)
	require.Equal(t, new(true), row.AlarmLatencyExceeded)
}
