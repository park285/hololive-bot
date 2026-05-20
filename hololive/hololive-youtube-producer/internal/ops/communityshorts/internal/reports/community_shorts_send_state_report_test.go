package reports

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuildCommunityShortsSendStateReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)

	sentPublishedAt := windowStart.Add(10 * time.Minute)
	sentDetectedAt := sentPublishedAt.Add(20 * time.Second)
	sentAlarmSentAt := sentPublishedAt.Add(70 * time.Second)
	sentFirstEventAt := sentDetectedAt.Add(15 * time.Second)
	sentLastEventAt := sentAlarmSentAt

	attemptedPublishedAt := windowStart.Add(20 * time.Minute)
	attemptedDetectedAt := attemptedPublishedAt.Add(15 * time.Second)
	attemptedFirstEventAt := attemptedDetectedAt.Add(30 * time.Second)
	attemptedLastEventAt := attemptedDetectedAt.Add(2 * time.Minute)

	notSentPublishedAt := windowStart.Add(30 * time.Minute)
	notSentDetectedAt := notSentPublishedAt.Add(10 * time.Second)

	report := BuildCommunityShortsSendStateReport(
		[]outbox.PostSendCount{
			{
				AlarmType:             domain.AlarmTypeCommunity,
				ChannelID:             "UC_COMMUNITY",
				PostID:                "community:post-sent",
				ContentID:             "community:post-sent",
				ActualPublishedAt:     &sentPublishedAt,
				DetectedAt:            &sentDetectedAt,
				AlarmSentAt:           &sentAlarmSentAt,
				FirstEventAt:          &sentFirstEventAt,
				LastEventAt:           &sentLastEventAt,
				FirstSuccessAt:        &sentAlarmSentAt,
				LastSuccessAt:         &sentAlarmSentAt,
				OutboxCount:           1,
				SuccessSendCount:      3,
				SuccessRoomCount:      2,
				DuplicateSuccessCount: 1,
			},
			{
				AlarmType:          domain.AlarmTypeShorts,
				ChannelID:          "UC_SHORTS",
				PostID:             "short:post-attempted",
				ContentID:          "short:post-attempted",
				ActualPublishedAt:  &attemptedPublishedAt,
				DetectedAt:         &attemptedDetectedAt,
				FirstEventAt:       &attemptedFirstEventAt,
				LastEventAt:        &attemptedLastEventAt,
				OutboxCount:        1,
				FailedAttemptCount: 2,
			},
			{
				AlarmType:         domain.AlarmTypeCommunity,
				ChannelID:         "UC_PENDING",
				PostID:            "community:post-not-sent",
				ContentID:         "community:post-not-sent",
				ActualPublishedAt: &notSentPublishedAt,
				DetectedAt:        &notSentDetectedAt,
			},
		},
		CommunityShortsSendStateQuery{
			ObservationRuntimeName:      "youtube-producer",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
			Finalized:                   true,
		},
		generatedAt,
	)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Equal(t, 3, report.Summary.PostStateCount)
	require.Equal(t, 1, report.Summary.SentPostCount)
	require.Equal(t, 1, report.Summary.AttemptedWithoutSuccessPostCount)
	require.Equal(t, 1, report.Summary.NotSentPostCount)
	require.Equal(t, 1, report.Summary.DuplicateSuccessPostCount)
	require.Equal(t, 1, report.Summary.FailedAttemptPostCount)
	require.Equal(t, 2, report.Summary.CommunityPostCount)
	require.Equal(t, 1, report.Summary.ShortsPostCount)
	require.NotNil(t, report.Summary.EarliestObservedAt)
	require.Equal(t, sentPublishedAt, report.Summary.EarliestObservedAt.UTC())
	require.NotNil(t, report.Summary.LatestObservedAt)
	require.Equal(t, notSentPublishedAt, report.Summary.LatestObservedAt.UTC())
	require.NotNil(t, report.Summary.EarliestAlarmSentAt)
	require.Equal(t, sentAlarmSentAt, report.Summary.EarliestAlarmSentAt.UTC())
	require.NotNil(t, report.Summary.LatestAlarmSentAt)
	require.Equal(t, sentAlarmSentAt, report.Summary.LatestAlarmSentAt.UTC())

	require.Len(t, report.Rows, 3)
	require.Equal(t, "community:post-not-sent", report.Rows[0].ReportPostID)
	require.Equal(t, CommunityShortsPerPostSendStateNotSent, report.Rows[0].SendState)
	require.Equal(t, "short:post-attempted", report.Rows[1].ReportPostID)
	require.Equal(t, CommunityShortsPerPostSendStateAttemptedWithoutSuccess, report.Rows[1].SendState)
	require.Equal(t, "community:post-sent", report.Rows[2].ReportPostID)
	require.Equal(t, CommunityShortsPerPostSendStateSent, report.Rows[2].SendState)
	require.Equal(t, "COMMUNITY|UC_COMMUNITY|community:post-sent", report.Rows[2].PostKey)
	require.NotNil(t, report.Rows[2].ReportAlarmSentAt)
	require.Equal(t, sentAlarmSentAt, report.Rows[2].ReportAlarmSentAt.UTC())

	markdown := RenderCommunityShortsSendStateMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Send State Report")
	require.Contains(t, markdown, "post_states=`3`")
	require.Contains(t, markdown, "attempted_without_success_posts=`1`")
	require.Contains(t, markdown, "duplicate_success_posts=`1`")
	require.Contains(t, markdown, "`attempted_without_success`")
	require.Contains(t, markdown, "COMMUNITY|UC_COMMUNITY|community:post-sent")
}
