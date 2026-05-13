package communityshortsops

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func TestBuildCommunityShortsAlarmSentHistoryDatasetReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	communityPublishedAt := windowStart.Add(10 * time.Minute)
	communityDetectedAt := communityPublishedAt.Add(15 * time.Second)
	communityAlarmSentAt := communityPublishedAt.Add(70 * time.Second)
	shortsPublishedAt := windowStart.Add(20 * time.Minute)
	shortsDetectedAt := shortsPublishedAt.Add(10 * time.Second)
	shortsAlarmSentAt := shortsPublishedAt.Add(65 * time.Second)
	missingPublishedAt := windowStart.Add(30 * time.Minute)
	missingDetectedAt := missingPublishedAt.Add(12 * time.Second)

	report := BuildCommunityShortsAlarmSentHistoryDatasetReport(
		[]trackingrepo.CommunityAlarmSentHistoryRow{{
			PostID:            "community:post-1",
			ContentID:         "post-1",
			ChannelID:         "UC_COMMUNITY",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
			AlarmSentAt:       communityAlarmSentAt,
		}},
		[]trackingrepo.ShortsAlarmSentHistoryRow{{
			PostID:            "short:post-1",
			ContentID:         "video-1",
			ChannelID:         "UC_SHORTS",
			ActualPublishedAt: &shortsPublishedAt,
			DetectedAt:        shortsDetectedAt,
			AlarmSentAt:       shortsAlarmSentAt,
		}},
		trackingrepo.ObservationPostComparisonResult{
			Summary: trackingrepo.ObservationPostComparisonSummary{
				BaselineUniquePostCount: 3,
				MatchedPostCount:        2,
				UnsentPostCount:         1,
			},
			VerdictRows: []trackingrepo.ObservationPostComparisonVerdictRow{
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
					AlarmType:         domain.AlarmTypeCommunity,
					ChannelID:         "UC_COMMUNITY",
					CanonicalPostID:   "community:post-1",
					ContentID:         "post-1",
					ActualPublishedAt: &communityPublishedAt,
					DetectedAt:        &communityDetectedAt,
					AlarmSentAt:       &communityAlarmSentAt,
					BaselineCount:     1,
					SentCount:         1,
				},
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
					AlarmType:         domain.AlarmTypeShorts,
					ChannelID:         "UC_SHORTS",
					CanonicalPostID:   "short:post-1",
					ContentID:         "video-1",
					ActualPublishedAt: &shortsPublishedAt,
					DetectedAt:        &shortsDetectedAt,
					AlarmSentAt:       &shortsAlarmSentAt,
					BaselineCount:     1,
					SentCount:         1,
				},
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictUnsent,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonBaselineWithoutSentHistory,
					AlarmType:         domain.AlarmTypeCommunity,
					ChannelID:         "UC_MISSING",
					CanonicalPostID:   "community:post-missing",
					ActualPublishedAt: &missingPublishedAt,
					DetectedAt:        &missingDetectedAt,
					BaselineCount:     1,
					SentCount:         0,
				},
			},
		},
		CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		generatedAt,
	)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Equal(t, "youtube-scraper", report.Query.ObservationRuntimeName)
	require.Equal(t, 2, report.Summary.CollectedRowCount)
	require.Equal(t, 0, report.Summary.DuplicateRowCount)
	require.Equal(t, 2, report.Summary.SentPostCount)
	require.Equal(t, 1, report.Summary.CommunitySentPostCount)
	require.Equal(t, 1, report.Summary.ShortsSentPostCount)
	require.Equal(t, 3, report.Summary.BaselinePostCount)
	require.Equal(t, 2, report.Summary.MatchedPostCount)
	require.Equal(t, 1, report.Summary.UnsentPostCount)
	require.Equal(t, 3, report.Summary.VerificationRowCount)
	require.Equal(t, 3, report.Summary.ReferenceRowCount)
	require.NotNil(t, report.Summary.EarliestAlarmSentAt)
	require.Equal(t, communityAlarmSentAt, report.Summary.EarliestAlarmSentAt.UTC())
	require.NotNil(t, report.Summary.LatestAlarmSentAt)
	require.Equal(t, shortsAlarmSentAt, report.Summary.LatestAlarmSentAt.UTC())

	require.Len(t, report.Rows, 2)
	require.Equal(t, domain.AlarmTypeCommunity, report.Rows[0].AlarmType)
	require.Equal(t, "COMMUNITY|UC_COMMUNITY|community:post-1", report.Rows[0].PostKey)
	require.Equal(t, domain.AlarmTypeShorts, report.Rows[1].AlarmType)
	require.Equal(t, "SHORTS|UC_SHORTS|short:post-1", report.Rows[1].PostKey)

	require.Len(t, report.VerificationRows, 3)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictMatched, report.VerificationRows[0].Verdict)
	require.Equal(t, "COMMUNITY|UC_COMMUNITY|community:post-1", report.VerificationRows[0].PostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictMatched, report.VerificationRows[1].Verdict)
	require.Equal(t, "SHORTS|UC_SHORTS|short:post-1", report.VerificationRows[1].PostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictUnsent, report.VerificationRows[2].Verdict)
	require.Equal(t, "COMMUNITY|UC_MISSING|community:post-missing", report.VerificationRows[2].PostKey)

	require.Len(t, report.ReferenceRows, 3)
	require.Equal(t, "UC_COMMUNITY|community:post-1", report.ReferenceRows[0].ChannelPostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictMatched, report.ReferenceRows[0].VerificationVerdict)
	require.Equal(t, "UC_SHORTS|short:post-1", report.ReferenceRows[1].ChannelPostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictMatched, report.ReferenceRows[1].VerificationVerdict)
	require.Equal(t, "UC_MISSING|community:post-missing", report.ReferenceRows[2].ChannelPostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictUnsent, report.ReferenceRows[2].VerificationVerdict)

	require.False(t, report.Results.MissingAlarmEvaluated)
	require.Len(t, report.Results.AlarmTypeComparisons, 2)
	require.Equal(t, domain.AlarmTypeCommunity, report.Results.AlarmTypeComparisons[0].AlarmType)
	require.Equal(t, 2, report.Results.AlarmTypeComparisons[0].BaselinePostCount)
	require.Equal(t, 1, report.Results.AlarmTypeComparisons[0].SentPostCount)
	require.Equal(t, 1, report.Results.AlarmTypeComparisons[0].MatchedPostCount)
	require.Equal(t, 1, report.Results.AlarmTypeComparisons[0].UnsentPostCount)
	require.Len(t, report.Results.ChannelComparisons, 3)
	require.Equal(t, "UC_COMMUNITY", report.Results.ChannelComparisons[0].ChannelID)
	require.Equal(t, 1, report.Results.ChannelComparisons[0].MatchedPostCount)

	markdown := RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Alarm Sent History Dataset")
	require.Contains(t, markdown, "baseline_posts=`3`")
	require.Contains(t, markdown, "unsent_posts=`1`")
	require.Contains(t, markdown, "reference_rows=`3`")
	require.Contains(t, markdown, "## Results")
	require.Contains(t, markdown, "finalized send-state comparison pending")
	require.Contains(t, markdown, "### By Alarm Type")
	require.Contains(t, markdown, "### By Channel")
	require.Contains(t, markdown, "Verification Rows")
	require.Contains(t, markdown, "Normalized Verification Reference Rows")
	require.Contains(t, markdown, "Normalized Sent History Rows")
	require.Contains(t, markdown, "COMMUNITY/UC_COMMUNITY/community:post-1")
	require.Contains(t, markdown, "UC_COMMUNITY/community:post-1")
}

func TestAttachCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	sentPublishedAt := windowStart.Add(10 * time.Minute)
	sentDetectedAt := sentPublishedAt.Add(10 * time.Second)
	sentAlarmSentAt := sentPublishedAt.Add(40 * time.Second)
	attemptedPublishedAt := windowStart.Add(20 * time.Minute)
	attemptedDetectedAt := attemptedPublishedAt.Add(12 * time.Second)
	notSentPublishedAt := windowStart.Add(30 * time.Minute)
	notSentDetectedAt := notSentPublishedAt.Add(11 * time.Second)
	missingPublishedAt := windowStart.Add(40 * time.Minute)
	missingDetectedAt := missingPublishedAt.Add(9 * time.Second)

	report := BuildCommunityShortsAlarmSentHistoryDatasetReport(
		nil,
		nil,
		trackingrepo.ObservationPostComparisonResult{
			Summary: trackingrepo.ObservationPostComparisonSummary{
				BaselineUniquePostCount: 4,
				MatchedPostCount:        1,
				UnsentPostCount:         3,
			},
			VerdictRows: []trackingrepo.ObservationPostComparisonVerdictRow{
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
					AlarmType:         domain.AlarmTypeCommunity,
					ChannelID:         "UC_SENT",
					CanonicalPostID:   "community:post-sent",
					ActualPublishedAt: &sentPublishedAt,
					DetectedAt:        &sentDetectedAt,
					AlarmSentAt:       &sentAlarmSentAt,
					BaselineCount:     1,
					SentCount:         1,
				},
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictUnsent,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonBaselineWithoutSentHistory,
					AlarmType:         domain.AlarmTypeShorts,
					ChannelID:         "UC_ATTEMPTED",
					CanonicalPostID:   "short:post-attempted",
					ActualPublishedAt: &attemptedPublishedAt,
					DetectedAt:        &attemptedDetectedAt,
					BaselineCount:     1,
				},
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictUnsent,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonBaselineWithoutSentHistory,
					AlarmType:         domain.AlarmTypeCommunity,
					ChannelID:         "UC_NOT_SENT",
					CanonicalPostID:   "community:post-not-sent",
					ActualPublishedAt: &notSentPublishedAt,
					DetectedAt:        &notSentDetectedAt,
					BaselineCount:     1,
				},
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictUnsent,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonBaselineWithoutSentHistory,
					AlarmType:         domain.AlarmTypeCommunity,
					ChannelID:         "UC_MISSING_STATE",
					CanonicalPostID:   "community:post-missing-state",
					ActualPublishedAt: &missingPublishedAt,
					DetectedAt:        &missingDetectedAt,
					BaselineCount:     1,
				},
			},
		},
		CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		windowEnd,
	)

	report = attachCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(report, []outbox.PostSendCount{
		{
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_SENT",
			PostID:            "community:post-sent",
			ContentID:         "post-sent",
			ActualPublishedAt: &sentPublishedAt,
			DetectedAt:        &sentDetectedAt,
			AlarmSentAt:       &sentAlarmSentAt,
			FirstSuccessAt:    &sentAlarmSentAt,
			LastSuccessAt:     &sentAlarmSentAt,
			SuccessSendCount:  1,
			SuccessRoomCount:  1,
		},
		{
			AlarmType:          domain.AlarmTypeShorts,
			ChannelID:          "UC_ATTEMPTED",
			PostID:             "short:post-attempted",
			ContentID:          "short:post-attempted",
			ActualPublishedAt:  &attemptedPublishedAt,
			DetectedAt:         &attemptedDetectedAt,
			OutboxCount:        1,
			FailedAttemptCount: 2,
		},
		{
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_NOT_SENT",
			PostID:            "community:post-not-sent",
			ContentID:         "community:post-not-sent",
			ActualPublishedAt: &notSentPublishedAt,
			DetectedAt:        &notSentDetectedAt,
		},
	})

	require.Equal(t, 3, report.Summary.SendStatePostCount)
	require.Equal(t, 3, report.Summary.MissingAlarmPostCount)
	require.Equal(t, 1, report.Summary.MissingSendStatePostCount)
	require.Equal(t, 1, report.Summary.AttemptedMissingPostCount)
	require.Equal(t, 1, report.Summary.NotSentMissingPostCount)
	require.True(t, report.Results.MissingAlarmEvaluated)
	require.False(t, report.Results.MissingAlarmZero)
	require.Equal(t, 3, report.Results.MissingAlarmPostCount)
	require.Len(t, report.MissingAlarmRows, 3)
	require.Equal(t, CommunityShortsMissingAlarmReasonAttempted, report.MissingAlarmRows[0].MissingReason)
	require.Equal(t, CommunityShortsPerPostSendStateAttemptedWithoutSuccess, report.MissingAlarmRows[0].SendState)
	require.Equal(t, "SHORTS|UC_ATTEMPTED|short:post-attempted", report.MissingAlarmRows[0].PostKey)
	require.Equal(t, CommunityShortsMissingAlarmReasonNotSent, report.MissingAlarmRows[1].MissingReason)
	require.Equal(t, CommunityShortsPerPostSendStateNotSent, report.MissingAlarmRows[1].SendState)
	require.Equal(t, CommunityShortsMissingAlarmReasonSendStateMissing, report.MissingAlarmRows[2].MissingReason)
	require.Empty(t, report.MissingAlarmRows[2].SendState)
	require.Equal(t, "COMMUNITY|UC_MISSING_STATE|community:post-missing-state", report.MissingAlarmRows[2].PostKey)

	markdown := RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report)
	require.Contains(t, markdown, "missing_alarm_posts=`3`")
	require.Contains(t, markdown, "Missing Alarm Rows")
	require.Contains(t, markdown, "send_state_missing")
	require.Contains(t, markdown, "attempted_without_success")
}

func TestBuildCommunityShortsAlarmSentHistoryDatasetReportDeduplicatesPerAlarmType(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	publishedAt := windowStart.Add(10 * time.Minute)
	communityDetectedAt := publishedAt.Add(10 * time.Second)
	communityAlarmSentAt := publishedAt.Add(45 * time.Second)
	shortsDetectedAt := publishedAt.Add(12 * time.Second)
	shortsAlarmSentAt := publishedAt.Add(50 * time.Second)

	report := BuildCommunityShortsAlarmSentHistoryDatasetReport(
		[]trackingrepo.CommunityAlarmSentHistoryRow{
			{
				PostID:            "post-dup",
				ContentID:         "post-dup",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &publishedAt,
				DetectedAt:        communityDetectedAt.Add(5 * time.Second),
				AlarmSentAt:       communityAlarmSentAt.Add(10 * time.Second),
			},
			{
				PostID:            "community:post-dup",
				ContentID:         "post-dup",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &publishedAt,
				DetectedAt:        communityDetectedAt,
				AlarmSentAt:       communityAlarmSentAt,
			},
		},
		[]trackingrepo.ShortsAlarmSentHistoryRow{
			{
				PostID:            "video-dup",
				ContentID:         "video-dup",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &publishedAt,
				DetectedAt:        shortsDetectedAt.Add(4 * time.Second),
				AlarmSentAt:       shortsAlarmSentAt.Add(10 * time.Second),
			},
			{
				PostID:            "short:video-dup",
				ContentID:         "video-dup",
				ChannelID:         "UC_DUP",
				ActualPublishedAt: &publishedAt,
				DetectedAt:        shortsDetectedAt,
				AlarmSentAt:       shortsAlarmSentAt,
			},
		},
		trackingrepo.ObservationPostComparisonResult{},
		CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		windowEnd,
	)

	require.Equal(t, 4, report.Summary.CollectedRowCount)
	require.Equal(t, 2, report.Summary.DuplicateRowCount)
	require.Equal(t, 2, report.Summary.SentPostCount)
	require.Zero(t, report.Summary.ReferenceRowCount)
	require.Len(t, report.Rows, 2)
	require.Equal(t, "community:post-dup", report.Rows[0].PostID)
	require.Equal(t, communityDetectedAt, report.Rows[0].DetectedAt.UTC())
	require.Equal(t, shortsDetectedAt, report.Rows[1].DetectedAt.UTC())
	require.Empty(t, report.VerificationRows)
}

func TestBuildCommunityShortsAlarmSentHistoryDatasetReferenceRowsNormalizeChannelPostIdentity(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 4, 10, 0, 10, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(10 * time.Second)
	reviewPublishedAt := publishedAt.Add(10 * time.Minute)
	reviewDetectedAt := reviewPublishedAt.Add(12 * time.Second)

	rows := buildCommunityShortsAlarmSentHistoryDatasetReferenceRows([]trackingrepo.ObservationPostComparisonVerdictRow{
		{
			Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
			Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         " UC_MAIN ",
			CanonicalPostID:   " community:post-1 ",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        &detectedAt,
			SentCount:         1,
		},
		{
			Verdict:                trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate,
			Reason:                 trackingrepo.ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview,
			AlarmType:              domain.AlarmTypeCommunity,
			ChannelID:              "UC_REVIEW",
			ActualPublishedAt:      &reviewPublishedAt,
			DetectedAt:             &reviewDetectedAt,
			ReviewStatus:           trackingrepo.ObservationIdentifierMismatchCandidateReviewStatusPendingReview,
			SentCount:              1,
			RelatedBaselinePostIDs: []string{" community:post-base ", "community:post-base", "community:post-base-2"},
			RelatedSentPostIDs:     []string{" community:post-sent ", "community:post-sent"},
		},
		{
			Verdict:         trackingrepo.ObservationPostComparisonVerdictUnexpectedSent,
			Reason:          trackingrepo.ObservationPostComparisonVerdictReasonSentHistoryWithoutBaseline,
			AlarmType:       domain.AlarmTypeShorts,
			ChannelID:       "UC_SKIP",
			CanonicalPostID: "short:skip-me",
			SentCount:       1,
		},
	})

	require.Len(t, rows, 3)
	require.Equal(t, "UC_MAIN|community:post-1", rows[0].ChannelPostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictMatched, rows[0].VerificationVerdict)
	require.Equal(t, "UC_REVIEW|community:post-base", rows[1].ChannelPostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate, rows[1].VerificationVerdict)
	require.Equal(t, trackingrepo.ObservationIdentifierMismatchCandidateReviewStatusPendingReview, rows[1].ReviewStatus)
	require.Equal(t, []string{"community:post-sent"}, rows[1].RelatedSentPostIDs)
	require.Equal(t, "UC_REVIEW|community:post-base-2", rows[2].ChannelPostKey)
}

func TestBuildCommunityShortsAlarmSentHistoryDatasetReportKeepsIdentifierMismatchCandidatesReviewable(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	publishedAt := windowStart.Add(25 * time.Minute)
	detectedAt := publishedAt.Add(10 * time.Second)
	alarmSentAt := publishedAt.Add(55 * time.Second)

	report := BuildCommunityShortsAlarmSentHistoryDatasetReport(
		[]trackingrepo.CommunityAlarmSentHistoryRow{{
			PostID:            "community:post-sent",
			ContentID:         "post-sent",
			ChannelID:         "UC_REVIEW",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
			AlarmSentAt:       alarmSentAt,
		}},
		nil,
		trackingrepo.ObservationPostComparisonResult{
			Summary: trackingrepo.ObservationPostComparisonSummary{
				BaselineUniquePostCount:          1,
				IdentifierMismatchCandidateCount: 1,
			},
			VerdictRows: []trackingrepo.ObservationPostComparisonVerdictRow{{
				Verdict:                trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate,
				Reason:                 trackingrepo.ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview,
				AlarmType:              domain.AlarmTypeCommunity,
				ChannelID:              "UC_REVIEW",
				MatchPublishedAt:       &publishedAt,
				MatchTitleHint:         "same title hint",
				MatchBasis:             []string{"actual_published_at", "title_hint"},
				ReviewStatus:           trackingrepo.ObservationIdentifierMismatchCandidateReviewStatusPendingReview,
				BaselineCount:          1,
				SentCount:              1,
				RelatedBaselinePostIDs: []string{"community:post-baseline"},
				RelatedSentPostIDs:     []string{"community:post-sent"},
			}},
		},
		CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		windowEnd,
	)

	require.Len(t, report.VerificationRows, 1)
	require.Len(t, report.ReferenceRows, 1)
	require.Equal(t, "UC_REVIEW|community:post-baseline", report.ReferenceRows[0].ChannelPostKey)
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate, report.ReferenceRows[0].VerificationVerdict)
	row := report.VerificationRows[0]
	require.Equal(t, trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate, row.Verdict)
	require.Empty(t, row.PostKey)
	require.NotNil(t, row.MatchPublishedAt)
	require.Equal(t, publishedAt, row.MatchPublishedAt.UTC())
	require.Equal(t, []string{"actual_published_at", "title_hint"}, row.MatchBasis)
	require.Equal(t, trackingrepo.ObservationIdentifierMismatchCandidateReviewStatusPendingReview, row.ReviewStatus)
	require.Equal(t, []string{"community:post-baseline"}, row.RelatedBaselinePostIDs)
	require.Equal(t, []string{"community:post-sent"}, row.RelatedSentPostIDs)

	markdown := RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report)
	require.Contains(t, markdown, "pending_review")
	require.Contains(t, markdown, "same title hint")
	require.Contains(t, markdown, "community:post-baseline")
	require.Contains(t, markdown, "community:post-sent")
}

func TestRenderCommunityShortsAlarmSentHistoryDatasetMarkdownIncludesMissingZeroCloseoutResults(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	communityPublishedAt := windowStart.Add(15 * time.Minute)
	communityDetectedAt := communityPublishedAt.Add(10 * time.Second)
	communityAlarmSentAt := communityPublishedAt.Add(45 * time.Second)
	shortsPublishedAt := windowStart.Add(25 * time.Minute)
	shortsDetectedAt := shortsPublishedAt.Add(8 * time.Second)
	shortsAlarmSentAt := shortsPublishedAt.Add(42 * time.Second)

	report := BuildCommunityShortsAlarmSentHistoryDatasetReport(
		[]trackingrepo.CommunityAlarmSentHistoryRow{{
			PostID:            "community:post-1",
			ContentID:         "community:post-1",
			ChannelID:         "UC_COMMUNITY",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
			AlarmSentAt:       communityAlarmSentAt,
		}},
		[]trackingrepo.ShortsAlarmSentHistoryRow{{
			PostID:            "short:post-1",
			ContentID:         "short:post-1",
			ChannelID:         "UC_SHORTS",
			ActualPublishedAt: &shortsPublishedAt,
			DetectedAt:        shortsDetectedAt,
			AlarmSentAt:       shortsAlarmSentAt,
		}},
		trackingrepo.ObservationPostComparisonResult{
			Summary: trackingrepo.ObservationPostComparisonSummary{
				BaselineUniquePostCount: 2,
				MatchedPostCount:        2,
			},
			VerdictRows: []trackingrepo.ObservationPostComparisonVerdictRow{
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
					AlarmType:         domain.AlarmTypeCommunity,
					ChannelID:         "UC_COMMUNITY",
					CanonicalPostID:   "community:post-1",
					ContentID:         "community:post-1",
					ActualPublishedAt: &communityPublishedAt,
					DetectedAt:        &communityDetectedAt,
					AlarmSentAt:       &communityAlarmSentAt,
					BaselineCount:     1,
					SentCount:         1,
				},
				{
					Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
					Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
					AlarmType:         domain.AlarmTypeShorts,
					ChannelID:         "UC_SHORTS",
					CanonicalPostID:   "short:post-1",
					ContentID:         "short:post-1",
					ActualPublishedAt: &shortsPublishedAt,
					DetectedAt:        &shortsDetectedAt,
					AlarmSentAt:       &shortsAlarmSentAt,
					BaselineCount:     1,
					SentCount:         1,
				},
			},
		},
		CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		windowEnd,
	)

	report = attachCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(report, []outbox.PostSendCount{
		{
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_COMMUNITY",
			PostID:            "community:post-1",
			ContentID:         "community:post-1",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        &communityDetectedAt,
			AlarmSentAt:       &communityAlarmSentAt,
			FirstSuccessAt:    &communityAlarmSentAt,
			LastSuccessAt:     &communityAlarmSentAt,
			SuccessSendCount:  1,
			SuccessRoomCount:  1,
		},
		{
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         "UC_SHORTS",
			PostID:            "short:post-1",
			ContentID:         "short:post-1",
			ActualPublishedAt: &shortsPublishedAt,
			DetectedAt:        &shortsDetectedAt,
			AlarmSentAt:       &shortsAlarmSentAt,
			FirstSuccessAt:    &shortsAlarmSentAt,
			LastSuccessAt:     &shortsAlarmSentAt,
			SuccessSendCount:  1,
			SuccessRoomCount:  1,
		},
	})

	require.True(t, report.Results.MissingAlarmEvaluated)
	require.True(t, report.Results.MissingAlarmZero)
	require.Zero(t, report.Results.MissingAlarmPostCount)
	require.Len(t, report.Results.AlarmTypeComparisons, 2)
	require.Equal(t, domain.AlarmTypeCommunity, report.Results.AlarmTypeComparisons[0].AlarmType)
	require.Equal(t, 1, report.Results.AlarmTypeComparisons[0].BaselinePostCount)
	require.Equal(t, 1, report.Results.AlarmTypeComparisons[0].SentPostCount)
	require.Equal(t, 1, report.Results.AlarmTypeComparisons[0].MatchedPostCount)
	require.Zero(t, report.Results.AlarmTypeComparisons[0].MissingAlarmPostCount)
	require.Len(t, report.Results.ChannelComparisons, 2)
	require.Equal(t, "UC_COMMUNITY", report.Results.ChannelComparisons[0].ChannelID)
	require.Equal(t, 1, report.Results.ChannelComparisons[0].MatchedPostCount)
	require.Zero(t, report.Results.ChannelComparisons[0].MissingAlarmPostCount)

	markdown := RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report)
	require.Contains(t, markdown, "## Results")
	require.Contains(t, markdown, "### By Alarm Type")
	require.Contains(t, markdown, "### By Channel")
	require.Contains(t, markdown, "누락 0건입니다.")
	require.Contains(t, markdown, "| `COMMUNITY` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0 |")
	require.Contains(t, markdown, "| `UC_COMMUNITY` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0 |")
}

func TestRenderCommunityShortsAlarmSentHistoryDatasetMarkdownKeepsEmptySectionScaffold(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 15, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)

	markdown := RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(CommunityShortsAlarmSentHistoryDatasetReport{
		GeneratedAt: generatedAt,
		Query: CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
	})

	sections := []string{
		"## Results",
		"## Missing Alarm Rows",
		"## Verification Rows",
		"## Normalized Verification Reference Rows",
		"## Normalized Sent History Rows",
	}
	lastIndex := -1
	for _, section := range sections {
		index := strings.Index(markdown, section)
		require.NotEqualf(t, -1, index, "missing section %q in markdown:\n%s", section, markdown)
		require.Greater(t, index, lastIndex, "section %q should keep report order", section)
		lastIndex = index
	}

	require.Contains(t, markdown, "finalized send-state comparison pending")
	require.Contains(t, markdown, "누락 알람 게시물이 없습니다.")
	require.Contains(t, markdown, "검증 verdict row가 없습니다.")
	require.Contains(t, markdown, "정규화된 검증 기준 row가 없습니다.")
	require.Contains(t, markdown, "정규화된 community/shorts sent history row가 없습니다.")
}
