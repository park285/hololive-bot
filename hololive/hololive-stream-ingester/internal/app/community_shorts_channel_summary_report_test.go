package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuildCommunityShortsChannelSummaryReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	since := generatedAt.Add(-24 * time.Hour)
	earliestA := generatedAt.Add(-3 * time.Hour)
	latestA := generatedAt.Add(-2 * time.Hour)
	earliestB := generatedAt.Add(-30 * time.Minute)
	latestB := generatedAt.Add(-20 * time.Minute)

	report := BuildCommunityShortsChannelSummaryReport([]outbox.ChannelPostDeliverySummary{
		{
			ChannelID:                  "UC_A",
			EarliestObservedAt:         &earliestA,
			LatestObservedAt:           &latestA,
			DetectedPostCount:          2,
			AlarmSentPostCount:         2,
			SuccessPostCount:           1,
			FailedPostCount:            1,
			DetectedUnsentPostCount:    1,
			CommunityDetectedPostCount: 1,
			ShortsDetectedPostCount:    1,
		},
		{
			ChannelID:                  "UC_B",
			EarliestObservedAt:         &earliestB,
			LatestObservedAt:           &latestB,
			DetectedPostCount:          2,
			AlarmSentPostCount:         1,
			SuccessPostCount:           1,
			FailedPostCount:            0,
			DetectedUnsentPostCount:    1,
			CommunityDetectedPostCount: 0,
			ShortsDetectedPostCount:    2,
		},
	}, generatedAt, since)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Equal(t, since, report.WindowStart)
	require.Equal(t, generatedAt, report.WindowEnd)
	require.Equal(t, int64(2), report.Summary.ChannelCount)
	require.Equal(t, int64(4), report.Summary.DetectedPostCount)
	require.Equal(t, int64(3), report.Summary.AlarmSentPostCount)
	require.Equal(t, int64(2), report.Summary.SuccessPostCount)
	require.Equal(t, int64(1), report.Summary.FailedPostCount)
	require.Equal(t, int64(2), report.Summary.DetectedUnsentPostCount)
	require.Equal(t, int64(1), report.Summary.CommunityDetectedPostCount)
	require.Equal(t, int64(3), report.Summary.ShortsDetectedPostCount)
	require.Len(t, report.Rows, 2)
	require.Equal(t, "UC_B", report.Rows[0].ChannelID)
	require.Equal(t, "UC_A", report.Rows[1].ChannelID)

	markdown := RenderCommunityShortsChannelSummaryMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Channel Delivery Summary")
	require.Contains(t, markdown, "channels=`2`")
	require.Contains(t, markdown, "alarm_sent_posts=`3`")
	require.Contains(t, markdown, "detected_unsent_posts=`2`")
	require.Contains(t, markdown, "`unsent_pending`")
	require.Contains(t, markdown, "`unsent_with_failures`")
	require.Contains(t, markdown, "`UC_B`")
}
