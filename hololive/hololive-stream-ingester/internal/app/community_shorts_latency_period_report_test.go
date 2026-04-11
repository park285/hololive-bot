package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuildCommunityShortsLatencyPeriodReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	avgHour := int64(45000)
	p95Hour := int64(89000)
	maxHour := int64(110000)
	avgDay := int64(76000)
	p95Day := int64(125000)
	maxDay := int64(210000)

	report := BuildCommunityShortsLatencyPeriodReport([]outbox.PostLatencyPeriodSummary{
		{
			Label:                      "last_24h",
			StartAt:                    generatedAt.Add(-24 * time.Hour),
			EndAt:                      generatedAt,
			TotalPostCount:             18,
			AlarmSentPostCount:         17,
			PendingPostCount:           1,
			LatencyMeasuredPostCount:   16,
			ExceededPostCount:          2,
			CommunityExceededPostCount: 1,
			ShortsExceededPostCount:    1,
			AverageLatencyMillis:       &avgDay,
			P95LatencyMillis:           &p95Day,
			MaxLatencyMillis:           &maxDay,
		},
		{
			Label:                      "last_1h",
			StartAt:                    generatedAt.Add(-time.Hour),
			EndAt:                      generatedAt,
			TotalPostCount:             3,
			AlarmSentPostCount:         3,
			PendingPostCount:           0,
			LatencyMeasuredPostCount:   3,
			ExceededPostCount:          0,
			CommunityExceededPostCount: 0,
			ShortsExceededPostCount:    0,
			AverageLatencyMillis:       &avgHour,
			P95LatencyMillis:           &p95Hour,
			MaxLatencyMillis:           &maxHour,
		},
	}, generatedAt)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Len(t, report.Periods, 2)
	require.Equal(t, "last_24h", report.Periods[0].Label)
	require.Equal(t, generatedAt.Add(-24*time.Hour), report.Periods[0].StartAt)
	require.NotNil(t, report.Periods[0].P95LatencyMillis)
	require.Equal(t, p95Day, *report.Periods[0].P95LatencyMillis)
	require.Equal(t, "last_1h", report.Periods[1].Label)
	require.NotNil(t, report.Periods[1].P95LatencyMillis)
	require.Equal(t, p95Hour, *report.Periods[1].P95LatencyMillis)

	markdown := RenderCommunityShortsLatencyPeriodMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Latency Period Report")
	require.Contains(t, markdown, "p95_latency_ms")
	require.Contains(t, markdown, "`last_24h`")
	require.Contains(t, markdown, "`last_1h`")
	require.Contains(t, markdown, "| `last_24h` | `2026-04-09T12:00:00Z` | `2026-04-10T12:00:00Z` | 18 | 17 | 1 | 16 | 76000 | 125000 | 210000 | 2 | 1 | 1 |")
}

func TestBuildCommunityShortsLatencyPeriods_DefaultsAndValidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	periods, err := buildCommunityShortsLatencyPeriods(now, nil)
	require.NoError(t, err)
	require.Len(t, periods, 3)
	require.Equal(t, "last_1h", periods[0].Label)
	require.Equal(t, now.Add(-time.Hour), periods[0].StartAt)
	require.Equal(t, now, periods[0].EndAt)
	require.Equal(t, "last_24h", periods[1].Label)
	require.Equal(t, "last_7d", periods[2].Label)

	_, err = buildCommunityShortsLatencyPeriods(now, []CommunityShortsLatencyPeriodSpec{{Label: "dup", Window: time.Hour}, {Label: "dup", Window: 2 * time.Hour}})
	require.ErrorContains(t, err, "duplicate label")

	_, err = buildCommunityShortsLatencyPeriods(now, []CommunityShortsLatencyPeriodSpec{{Label: "", Window: time.Hour}})
	require.ErrorContains(t, err, "label is empty")

	_, err = buildCommunityShortsLatencyPeriods(now, []CommunityShortsLatencyPeriodSpec{{Label: "bad", Window: 0}})
	require.ErrorContains(t, err, "window must be greater than zero")
}
