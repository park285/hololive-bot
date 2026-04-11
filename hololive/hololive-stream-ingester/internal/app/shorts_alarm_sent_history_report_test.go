package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func TestBuildShortsAlarmSentHistoryReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	firstPublishedAt := windowStart.Add(10 * time.Minute)
	firstDetectedAt := firstPublishedAt.Add(12 * time.Second)
	firstAlarmSentAt := firstPublishedAt.Add(55 * time.Second)
	secondPublishedAt := windowStart.Add(40 * time.Minute)
	secondDetectedAt := secondPublishedAt.Add(8 * time.Second)
	secondAlarmSentAt := secondPublishedAt.Add(58 * time.Second)

	report := BuildShortsAlarmSentHistoryReport(
		[]trackingrepo.ShortsAlarmSentHistoryRow{
			{
				PostID:            "short:post-2",
				ContentID:         "post-2",
				ChannelID:         "UC_SHORT_2",
				ActualPublishedAt: &secondPublishedAt,
				DetectedAt:        secondDetectedAt,
				AlarmSentAt:       secondAlarmSentAt,
			},
			{
				PostID:            "short:post-1",
				ContentID:         "post-1",
				ChannelID:         "UC_SHORT_1",
				ActualPublishedAt: &firstPublishedAt,
				DetectedAt:        firstDetectedAt,
				AlarmSentAt:       firstAlarmSentAt,
			},
		},
		trackingrepo.ObservationPostComparisonResult{},
		ShortsAlarmSentHistoryQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		generatedAt,
	)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Equal(t, "youtube-scraper", report.Query.ObservationRuntimeName)
	require.NotNil(t, report.Query.ObservationBigBangCutoverAt)
	require.Equal(t, cutoverAt, report.Query.ObservationBigBangCutoverAt.UTC())
	require.NotNil(t, report.Query.WindowStart)
	require.Equal(t, windowStart, report.Query.WindowStart.UTC())
	require.NotNil(t, report.Query.WindowEnd)
	require.Equal(t, windowEnd, report.Query.WindowEnd.UTC())

	require.Equal(t, 2, report.Summary.CollectedRowCount)
	require.Equal(t, 0, report.Summary.DuplicateRowCount)
	require.Equal(t, 2, report.Summary.SentPostCount)
	require.NotNil(t, report.Summary.EarliestAlarmSentAt)
	require.Equal(t, firstAlarmSentAt, report.Summary.EarliestAlarmSentAt.UTC())
	require.NotNil(t, report.Summary.LatestAlarmSentAt)
	require.Equal(t, secondAlarmSentAt, report.Summary.LatestAlarmSentAt.UTC())

	require.Len(t, report.Rows, 2)
	require.Equal(t, "short:post-1", report.Rows[0].PostID)
	require.Equal(t, firstAlarmSentAt, report.Rows[0].AlarmSentAt.UTC())
	require.Equal(t, "short:post-2", report.Rows[1].PostID)
	require.Equal(t, secondAlarmSentAt, report.Rows[1].AlarmSentAt.UTC())

	markdown := RenderShortsAlarmSentHistoryMarkdown(report)
	require.Contains(t, markdown, "# YouTube Shorts Alarm Sent History")
	require.Contains(t, markdown, "collected_rows=`2`")
	require.Contains(t, markdown, "duplicates_removed=`0`")
	require.Contains(t, markdown, "sent_posts=`2`")
	require.Contains(t, markdown, "`short:post-1`")
	require.Contains(t, markdown, "`short:post-2`")
	require.Contains(t, markdown, "alarm_sent_at")
	require.Contains(t, markdown, "identifier_mismatch_candidates=`0`")
}

func TestBuildShortsAlarmSentHistoryReport_DeduplicatesCanonicalPostID(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	duplicatePublishedAt := windowStart.Add(15 * time.Minute)
	duplicateDetectedAt := duplicatePublishedAt.Add(6 * time.Second)
	duplicateAlarmSentAt := duplicatePublishedAt.Add(45 * time.Second)
	duplicateAlarmSentLaterAt := duplicatePublishedAt.Add(70 * time.Second)
	secondPublishedAt := windowStart.Add(30 * time.Minute)
	secondDetectedAt := secondPublishedAt.Add(9 * time.Second)
	secondAlarmSentAt := secondPublishedAt.Add(50 * time.Second)

	report := BuildShortsAlarmSentHistoryReport(
		[]trackingrepo.ShortsAlarmSentHistoryRow{
			{
				PostID:            "post-duplicate",
				ContentID:         "post-duplicate",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &duplicatePublishedAt,
				DetectedAt:        duplicateDetectedAt.Add(4 * time.Second),
				AlarmSentAt:       duplicateAlarmSentLaterAt,
			},
			{
				PostID:            "short:post-duplicate",
				ContentID:         "post-duplicate",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &duplicatePublishedAt,
				DetectedAt:        duplicateDetectedAt,
				AlarmSentAt:       duplicateAlarmSentAt,
			},
			{
				PostID:            "short:post-ok",
				ContentID:         "post-ok",
				ChannelID:         "UC_OK",
				ActualPublishedAt: &secondPublishedAt,
				DetectedAt:        secondDetectedAt,
				AlarmSentAt:       secondAlarmSentAt,
			},
		},
		trackingrepo.ObservationPostComparisonResult{},
		ShortsAlarmSentHistoryQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		generatedAt,
	)

	require.Equal(t, 3, report.Summary.CollectedRowCount)
	require.Equal(t, 1, report.Summary.DuplicateRowCount)
	require.Equal(t, 2, report.Summary.SentPostCount)
	require.Len(t, report.Rows, 2)
	require.Equal(t, "short:post-duplicate", report.Rows[0].PostID)
	require.Equal(t, duplicateDetectedAt, report.Rows[0].DetectedAt.UTC())
	require.Equal(t, duplicateAlarmSentAt, report.Rows[0].AlarmSentAt.UTC())
	require.Equal(t, "short:post-ok", report.Rows[1].PostID)

	markdown := RenderShortsAlarmSentHistoryMarkdown(report)
	require.Contains(t, markdown, "duplicates_removed=`1`")
	require.Contains(t, markdown, "`short:post-duplicate`")
	require.NotContains(t, markdown, "`post-duplicate` | `UC_DUP` | `post-duplicate` | `2026-")
}

func TestRenderShortsAlarmSentHistoryMarkdown_ComparisonVerdicts(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	publishedAt := windowStart.Add(20 * time.Minute)
	detectedAt := publishedAt.Add(6 * time.Second)
	alarmSentAt := publishedAt.Add(44 * time.Second)

	report := BuildShortsAlarmSentHistoryReport(
		[]trackingrepo.ShortsAlarmSentHistoryRow{{
			PostID:            "short:post-sent",
			ContentID:         "post-sent",
			ChannelID:         "UC_SHORT_REVIEW",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
			AlarmSentAt:       alarmSentAt,
		}},
		trackingrepo.ObservationPostComparisonResult{
			Summary: trackingrepo.ObservationPostComparisonSummary{
				BaselineUniquePostCount: 1,
				MatchedPostCount:        1,
			},
			VerdictRows: []trackingrepo.ObservationPostComparisonVerdictRow{{
				Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
				Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
				AlarmType:         "SHORTS",
				ChannelID:         "UC_SHORT_REVIEW",
				CanonicalPostID:   "short:post-sent",
				ActualPublishedAt: &publishedAt,
				AlarmSentAt:       &alarmSentAt,
				BaselineCount:     1,
				SentCount:         1,
			}},
		},
		ShortsAlarmSentHistoryQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		generatedAt,
	)

	markdown := RenderShortsAlarmSentHistoryMarkdown(report)
	require.Contains(t, markdown, "Comparison Verdicts")
	require.Contains(t, markdown, "canonical_identifier_matched")
	require.Contains(t, markdown, "short:post-sent")
}
