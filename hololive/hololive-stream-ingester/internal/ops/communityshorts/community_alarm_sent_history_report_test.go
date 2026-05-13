package communityshortsops

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func TestBuildCommunityAlarmSentHistoryReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	firstPublishedAt := windowStart.Add(10 * time.Minute)
	firstDetectedAt := firstPublishedAt.Add(20 * time.Second)
	firstAlarmSentAt := firstPublishedAt.Add(70 * time.Second)
	secondPublishedAt := windowStart.Add(40 * time.Minute)
	secondDetectedAt := secondPublishedAt.Add(15 * time.Second)
	secondAlarmSentAt := secondPublishedAt.Add(65 * time.Second)

	report := BuildCommunityAlarmSentHistoryReport(
		[]trackingrepo.CommunityAlarmSentHistoryRow{
			{
				PostID:            "community:post-2",
				ContentID:         "post-2",
				ChannelID:         "UC_COMMUNITY_2",
				ActualPublishedAt: &secondPublishedAt,
				DetectedAt:        secondDetectedAt,
				AlarmSentAt:       secondAlarmSentAt,
			},
			{
				PostID:            "community:post-1",
				ContentID:         "post-1",
				ChannelID:         "UC_COMMUNITY_1",
				ActualPublishedAt: &firstPublishedAt,
				DetectedAt:        firstDetectedAt,
				AlarmSentAt:       firstAlarmSentAt,
			},
		},
		trackingrepo.ObservationPostComparisonResult{},
		CommunityAlarmSentHistoryQuery{
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
	require.Equal(t, "community:post-1", report.Rows[0].PostID)
	require.Equal(t, firstAlarmSentAt, report.Rows[0].AlarmSentAt.UTC())
	require.Equal(t, "community:post-2", report.Rows[1].PostID)
	require.Equal(t, secondAlarmSentAt, report.Rows[1].AlarmSentAt.UTC())

	markdown := RenderCommunityAlarmSentHistoryMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community Alarm Sent History")
	require.Contains(t, markdown, "collected_rows=`2`")
	require.Contains(t, markdown, "duplicates_removed=`0`")
	require.Contains(t, markdown, "sent_posts=`2`")
	require.Contains(t, markdown, "`community:post-1`")
	require.Contains(t, markdown, "`community:post-2`")
	require.Contains(t, markdown, "alarm_sent_at")
	require.Contains(t, markdown, "identifier_mismatch_candidates=`0`")
}

func TestBuildCommunityAlarmSentHistoryReport_DeduplicatesCanonicalPostID(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	duplicatePublishedAt := windowStart.Add(15 * time.Minute)
	duplicateDetectedAt := duplicatePublishedAt.Add(10 * time.Second)
	duplicateAlarmSentAt := duplicatePublishedAt.Add(50 * time.Second)
	duplicateAlarmSentLaterAt := duplicatePublishedAt.Add(70 * time.Second)
	secondPublishedAt := windowStart.Add(30 * time.Minute)
	secondDetectedAt := secondPublishedAt.Add(12 * time.Second)
	secondAlarmSentAt := secondPublishedAt.Add(55 * time.Second)

	report := BuildCommunityAlarmSentHistoryReport(
		[]trackingrepo.CommunityAlarmSentHistoryRow{
			{
				PostID:            "post-duplicate",
				ContentID:         "post-duplicate",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &duplicatePublishedAt,
				DetectedAt:        duplicateDetectedAt.Add(5 * time.Second),
				AlarmSentAt:       duplicateAlarmSentLaterAt,
			},
			{
				PostID:            "community:post-duplicate",
				ContentID:         "post-duplicate",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &duplicatePublishedAt,
				DetectedAt:        duplicateDetectedAt,
				AlarmSentAt:       duplicateAlarmSentAt,
			},
			{
				PostID:            "community:post-ok",
				ContentID:         "post-ok",
				ChannelID:         "UC_OK",
				ActualPublishedAt: &secondPublishedAt,
				DetectedAt:        secondDetectedAt,
				AlarmSentAt:       secondAlarmSentAt,
			},
		},
		trackingrepo.ObservationPostComparisonResult{},
		CommunityAlarmSentHistoryQuery{
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
	require.Equal(t, "community:post-duplicate", report.Rows[0].PostID)
	require.Equal(t, duplicateDetectedAt, report.Rows[0].DetectedAt.UTC())
	require.Equal(t, duplicateAlarmSentAt, report.Rows[0].AlarmSentAt.UTC())
	require.Equal(t, "community:post-ok", report.Rows[1].PostID)

	markdown := RenderCommunityAlarmSentHistoryMarkdown(report)
	require.Contains(t, markdown, "duplicates_removed=`1`")
	require.Contains(t, markdown, "`community:post-duplicate`")
	require.NotContains(t, markdown, "`post-duplicate` | `UC_DUP` | `post-duplicate` | `2026-")
}

func TestRenderCommunityAlarmSentHistoryMarkdown_IdentifierMismatchCandidates(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	publishedAt := windowStart.Add(25 * time.Minute)
	detectedAt := publishedAt.Add(10 * time.Second)
	alarmSentAt := publishedAt.Add(55 * time.Second)

	report := BuildCommunityAlarmSentHistoryReport(
		[]trackingrepo.CommunityAlarmSentHistoryRow{{
			PostID:            "community:post-sent",
			ContentID:         "post-sent",
			ChannelID:         "UC_REVIEW",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
			AlarmSentAt:       alarmSentAt,
		}},
		trackingrepo.ObservationPostComparisonResult{
			Summary: trackingrepo.ObservationPostComparisonSummary{
				BaselineUniquePostCount:          1,
				IdentifierMismatchCandidateCount: 1,
			},
			IdentifierMismatchCandidates: []trackingrepo.ObservationIdentifierMismatchCandidate{{
				Kind:             "COMMUNITY_POST",
				AlarmType:        "COMMUNITY",
				ChannelID:        "UC_REVIEW",
				MatchPublishedAt: &publishedAt,
				MatchTitleHint:   "same title hint",
				MatchBasis:       []string{"actual_published_at", "title_hint"},
				ReviewStatus:     trackingrepo.ObservationIdentifierMismatchCandidateReviewStatusPendingReview,
				BaselineRows: []trackingrepo.ObservationPostComparisonRow{{
					CanonicalPostID: "community:post-baseline",
				}},
				SentRows: []trackingrepo.ObservationPostComparisonRow{{
					CanonicalPostID: "community:post-sent",
				}},
			}},
			VerdictRows: []trackingrepo.ObservationPostComparisonVerdictRow{{
				Verdict:                trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate,
				Reason:                 trackingrepo.ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview,
				AlarmType:              "COMMUNITY",
				ChannelID:              "UC_REVIEW",
				BaselineCount:          1,
				SentCount:              1,
				MatchPublishedAt:       &publishedAt,
				MatchBasis:             []string{"actual_published_at", "title_hint"},
				ReviewStatus:           trackingrepo.ObservationIdentifierMismatchCandidateReviewStatusPendingReview,
				RelatedBaselinePostIDs: []string{"community:post-baseline"},
				RelatedSentPostIDs:     []string{"community:post-sent"},
			}},
		},
		CommunityAlarmSentHistoryQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		generatedAt,
	)

	markdown := RenderCommunityAlarmSentHistoryMarkdown(report)
	require.Contains(t, markdown, "Comparison Verdicts")
	require.Contains(t, markdown, "auxiliary_metadata_match_pending_review")
	require.Contains(t, markdown, "Identifier Mismatch Candidates")
	require.Contains(t, markdown, "pending_review")
	require.Contains(t, markdown, "community:post-baseline")
	require.Contains(t, markdown, "community:post-sent")
	require.Contains(t, markdown, "same title hint")
}
