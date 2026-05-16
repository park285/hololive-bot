package reports

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuildCommunityShortsSendCountReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	since := generatedAt.Add(-24 * time.Hour)

	duplicatePublishedAt := generatedAt.Add(-2 * time.Hour)
	duplicateDetectedAt := generatedAt.Add(-119 * time.Minute)
	duplicateSuccessAt := generatedAt.Add(-110 * time.Minute)
	failedDetectedAt := generatedAt.Add(-90 * time.Minute)
	okPublishedAt := generatedAt.Add(-30 * time.Minute)
	okSuccessAt := generatedAt.Add(-20 * time.Minute)
	publishToDetectInternalMillis := int64(20 * time.Second / time.Millisecond)
	publishToDetectExternalMillis := int64(130 * time.Second / time.Millisecond)
	publishToDetectMixedMillis := int64(70 * time.Second / time.Millisecond)
	queueWaitMillis := int64(45 * time.Second / time.Millisecond)
	retryAccumulationMillis := int64(80 * time.Second / time.Millisecond)
	jobFailureRetryMillis := int64(120 * time.Second / time.Millisecond)

	report := BuildCommunityShortsSendCountReport([]outbox.PostSendCount{
		{
			AlarmType:             domain.AlarmTypeCommunity,
			ChannelID:             "UC_DUP",
			PostID:                "post-duplicate",
			ContentID:             "post-duplicate",
			ActualPublishedAt:     &duplicatePublishedAt,
			DetectedAt:            &duplicateDetectedAt,
			LastSuccessAt:         &duplicateSuccessAt,
			OutboxCount:           1,
			SuccessSendCount:      2,
			SuccessRoomCount:      1,
			DuplicateSuccessCount: 1,
		},
		{
			AlarmType:          domain.AlarmTypeShorts,
			ChannelID:          "UC_FAIL",
			PostID:             "short-no-success",
			ContentID:          "short-no-success",
			DetectedAt:         &failedDetectedAt,
			FailedAttemptCount: 2,
			OutboxCount:        0,
		},
		{
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         "UC_OK",
			PostID:            "short-ok",
			ContentID:         "short-ok",
			ActualPublishedAt: &okPublishedAt,
			LastSuccessAt:     &okSuccessAt,
			OutboxCount:       1,
			SuccessSendCount:  1,
			SuccessRoomCount:  1,
		},
	}, []outbox.PostDeliveryTimeline{
		{
			AlarmType:               domain.AlarmTypeCommunity,
			ChannelID:               "UC_DUP",
			ContentID:               "post-duplicate",
			PublishToDetectMillis:   &publishToDetectInternalMillis,
			DelaySource:             outbox.PostDelaySourceInternalDelivery,
			QueueWaitMillis:         &queueWaitMillis,
			RetryAccumulationMillis: &retryAccumulationMillis,
			InternalDelayCause:      outbox.PostInternalDelayCauseRetryAccumulation,
		},
		{
			AlarmType:               domain.AlarmTypeShorts,
			ChannelID:               "UC_FAIL",
			ContentID:               "short-no-success",
			PublishToDetectMillis:   &publishToDetectExternalMillis,
			DelaySource:             outbox.PostDelaySourceExternalCollection,
			RetryAccumulationMillis: &jobFailureRetryMillis,
			JobFailureDetected:      true,
			InternalDelayCause:      outbox.PostInternalDelayCauseJobFailure,
		},
		{
			AlarmType:             domain.AlarmTypeShorts,
			ChannelID:             "UC_OK",
			ContentID:             "short-ok",
			PublishToDetectMillis: &publishToDetectMixedMillis,
			DelaySource:           outbox.PostDelaySourceMixed,
			QueueWaitMillis:       &queueWaitMillis,
			InternalDelayCause:    outbox.PostInternalDelayCauseQueueWait,
		},
	}, generatedAt, since)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Equal(t, communityShortsSendCountQueryModeRecent, report.Query.Mode)
	require.NotNil(t, report.Query.WindowStart)
	require.Equal(t, since, report.Query.WindowStart.UTC())
	require.NotNil(t, report.Query.WindowEnd)
	require.Equal(t, generatedAt, report.Query.WindowEnd.UTC())
	require.Equal(t, since, report.WindowStart)
	require.Equal(t, generatedAt, report.WindowEnd)
	require.Equal(t, 3, report.Summary.PostCount)
	require.Equal(t, 2, report.Summary.SuccessfulPostCount)
	require.Equal(t, 1, report.Summary.ZeroSuccessPostCount)
	require.Equal(t, 1, report.Summary.DuplicateSuccessPostCount)
	require.Equal(t, 1, report.Summary.FailedAttemptPostCount)
	require.Equal(t, 1, report.Summary.OutboxMissingPostCount)
	require.Equal(t, 1, report.Summary.ExternalCollectionSourcePostCount)
	require.Equal(t, 1, report.Summary.InternalDeliverySourcePostCount)
	require.Equal(t, 1, report.Summary.MixedDelaySourcePostCount)
	require.Equal(t, 1, report.Summary.QueueWaitCausePostCount)
	require.Equal(t, 1, report.Summary.RetryAccumulationCausePostCount)
	require.Equal(t, 1, report.Summary.JobFailureCausePostCount)
	require.Equal(t, communityShortsSendCountDuplicateAlarmFail, report.Verification.DuplicateAlarmStatus)
	require.Equal(t, 1, report.Verification.DuplicateAlarmPostCount)
	require.Equal(t, communityShortsSendCountDuplicateAlarmRule, report.Verification.DuplicateAlarmRule)

	byPostID := make(map[string]CommunityShortsSendCountRow, len(report.Rows))
	for i := range report.Rows {
		byPostID[report.Rows[i].PostID] = report.Rows[i]
	}
	require.Len(t, byPostID, 3)
	require.Equal(t, int64(1), byPostID["post-duplicate"].DuplicateSuccessCount)
	require.NotNil(t, byPostID["post-duplicate"].PublishToDetectMillis)
	require.Equal(t, publishToDetectInternalMillis, *byPostID["post-duplicate"].PublishToDetectMillis)
	require.Equal(t, outbox.PostDelaySourceInternalDelivery, byPostID["post-duplicate"].DelaySource)
	require.Equal(t, outbox.PostInternalDelayCauseRetryAccumulation, byPostID["post-duplicate"].InternalDelayCause)
	require.NotNil(t, byPostID["post-duplicate"].RetryAccumulationMillis)
	require.Equal(t, domain.AlarmTypeCommunity, byPostID["post-duplicate"].ReportAlarmType)
	require.Equal(t, "UC_DUP", byPostID["post-duplicate"].ReportChannelID)
	require.Equal(t, "post-duplicate", byPostID["post-duplicate"].ReportPostID)
	require.NotNil(t, byPostID["post-duplicate"].ReportActualPublishedAt)
	require.Equal(t, duplicatePublishedAt, byPostID["post-duplicate"].ReportActualPublishedAt.UTC())
	require.NotNil(t, byPostID["post-duplicate"].ReportAlarmSentAt)
	require.Equal(t, duplicateSuccessAt, byPostID["post-duplicate"].ReportAlarmSentAt.UTC())
	require.NotNil(t, byPostID["post-duplicate"].ReportDelaySeconds)
	require.InDelta(t, 600.0, *byPostID["post-duplicate"].ReportDelaySeconds, 0.0001)
	require.Equal(t, int64(0), byPostID["short-no-success"].SuccessSendCount)
	require.NotNil(t, byPostID["short-no-success"].PublishToDetectMillis)
	require.Equal(t, publishToDetectExternalMillis, *byPostID["short-no-success"].PublishToDetectMillis)
	require.True(t, byPostID["short-no-success"].JobFailureDetected)
	require.Equal(t, outbox.PostDelaySourceExternalCollection, byPostID["short-no-success"].DelaySource)
	require.Equal(t, outbox.PostInternalDelayCauseJobFailure, byPostID["short-no-success"].InternalDelayCause)
	require.Nil(t, byPostID["short-no-success"].ReportAlarmSentAt)
	require.Nil(t, byPostID["short-no-success"].ReportDelaySeconds)
	require.Equal(t, int64(1), byPostID["short-ok"].SuccessRoomCount)
	require.NotNil(t, byPostID["short-ok"].PublishToDetectMillis)
	require.Equal(t, publishToDetectMixedMillis, *byPostID["short-ok"].PublishToDetectMillis)
	require.Equal(t, outbox.PostDelaySourceMixed, byPostID["short-ok"].DelaySource)
	require.Equal(t, outbox.PostInternalDelayCauseQueueWait, byPostID["short-ok"].InternalDelayCause)
	require.NotNil(t, byPostID["short-ok"].ReportActualPublishedAt)
	require.Equal(t, okPublishedAt, byPostID["short-ok"].ReportActualPublishedAt.UTC())
	require.NotNil(t, byPostID["short-ok"].ReportAlarmSentAt)
	require.Equal(t, okSuccessAt, byPostID["short-ok"].ReportAlarmSentAt.UTC())
	require.NotNil(t, byPostID["short-ok"].ReportDelaySeconds)
	require.InDelta(t, 600.0, *byPostID["short-ok"].ReportDelaySeconds, 0.0001)

	rowJSON, err := json.Marshal(byPostID["post-duplicate"])
	require.NoError(t, err)
	require.Contains(t, string(rowJSON), "\"alarm_type\":\"COMMUNITY\"")
	require.Contains(t, string(rowJSON), "\"channel_id\":\"UC_DUP\"")
	require.Contains(t, string(rowJSON), "\"post_id\":\"post-duplicate\"")
	require.Contains(t, string(rowJSON), "\"actual_published_at\":\"2026-04-10T10:00:00Z\"")
	require.Contains(t, string(rowJSON), "\"alarm_sent_at\":\"2026-04-10T10:10:00Z\"")
	require.Contains(t, string(rowJSON), "\"delay_seconds\":600")

	markdown := RenderCommunityShortsSendCountMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Post Send Counts Report")
	require.Contains(t, markdown, "mode: `recent_window`")
	require.Contains(t, markdown, "duplicate_success_posts=`1`")
	require.Contains(t, markdown, "external_collection_source_posts=`1`")
	require.Contains(t, markdown, "internal_delivery_source_posts=`1`")
	require.Contains(t, markdown, "mixed_delay_source_posts=`1`")
	require.Contains(t, markdown, "queue_wait_cause_posts=`1`")
	require.Contains(t, markdown, "retry_accumulation_cause_posts=`1`")
	require.Contains(t, markdown, "job_failure_cause_posts=`1`")
	require.Contains(t, markdown, "duplicate alarm verdict: status=`fail`, duplicate_posts=`1`, rule=`duplicate_success_posts == 0`")
	require.Contains(t, markdown, "| status | alarm_type | channel_id | post_id | actual_published_at | detected_at | alarm_sent_at | delay_seconds |")
	require.Contains(t, markdown, "600.000")
	require.Contains(t, markdown, "`external_collection`")
	require.Contains(t, markdown, "`internal_delivery`")
	require.Contains(t, markdown, "`mixed`")
	require.Contains(t, markdown, "`short-no-success`")
}

func TestBuildCommunityShortsSendCountReportWithQuery_ObservationWindow(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	observationEndedAt := observationStartedAt.Add(24 * time.Hour)

	report := BuildCommunityShortsSendCountReportWithQuery(
		nil,
		nil,
		CommunityShortsSendCountQuery{
			Mode:                        communityShortsSendCountQueryModeObservation,
			WindowStart:                 &observationStartedAt,
			WindowEnd:                   &observationEndedAt,
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
		},
		generatedAt,
	)

	require.Equal(t, communityShortsSendCountQueryModeObservation, report.Query.Mode)
	require.NotNil(t, report.Query.WindowStart)
	require.Equal(t, observationStartedAt, report.Query.WindowStart.UTC())
	require.NotNil(t, report.Query.WindowEnd)
	require.Equal(t, observationEndedAt, report.Query.WindowEnd.UTC())
	require.Equal(t, observationStartedAt, report.WindowStart)
	require.Equal(t, observationEndedAt, report.WindowEnd)
	require.Equal(t, "youtube-scraper", report.Query.ObservationRuntimeName)
	require.NotNil(t, report.Query.ObservationBigBangCutoverAt)
	require.Equal(t, cutoverAt, report.Query.ObservationBigBangCutoverAt.UTC())
	require.Equal(t, communityShortsSendCountDuplicateAlarmPass, report.Verification.DuplicateAlarmStatus)
	require.Equal(t, 0, report.Verification.DuplicateAlarmPostCount)
	require.Equal(t, communityShortsSendCountDuplicateAlarmRule, report.Verification.DuplicateAlarmRule)

	markdown := RenderCommunityShortsSendCountMarkdown(report)
	require.Contains(t, markdown, "mode: `observation_window`")
	require.Contains(t, markdown, "observation runtime: `youtube-scraper`, cutover: `2026-04-10T00:00:00Z`")
	require.Contains(t, markdown, "duplicate alarm verdict: status=`pass`, duplicate_posts=`0`, rule=`duplicate_success_posts == 0`")
	require.Contains(t, markdown, "조회된 community/shorts 게시물이 없습니다.")
}

func TestRenderCommunityShortsSendCountMarkdown_OutputStability(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 15, 12, 34, 56, 0, time.UTC)
	windowStart := generatedAt.Add(-2 * time.Hour)
	windowEnd := generatedAt
	actualPublishedAt := generatedAt.Add(-95 * time.Minute)
	detectedAt := actualPublishedAt.Add(25 * time.Second)
	alarmSentAt := actualPublishedAt.Add(3 * time.Minute)
	delaySeconds := 180.0
	publishToDetectMillis := int64(25 * time.Second / time.Millisecond)
	queueWaitMillis := int64(90 * time.Second / time.Millisecond)

	report := CommunityShortsSendCountReport{
		GeneratedAt: generatedAt,
		Query: CommunityShortsSendCountQuery{
			Mode:        communityShortsSendCountQueryModeRecent,
			WindowStart: &windowStart,
			WindowEnd:   &windowEnd,
		},
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Summary: CommunityShortsSendCountSummary{
			PostCount:                       1,
			SuccessfulPostCount:             1,
			ZeroSuccessPostCount:            0,
			DuplicateSuccessPostCount:       0,
			FailedAttemptPostCount:          0,
			OutboxMissingPostCount:          0,
			InternalDeliverySourcePostCount: 1,
			QueueWaitCausePostCount:         1,
		},
		Verification: CommunityShortsSendCountVerification{
			DuplicateAlarmStatus:    communityShortsSendCountDuplicateAlarmPass,
			DuplicateAlarmPostCount: 0,
			DuplicateAlarmRule:      communityShortsSendCountDuplicateAlarmRule,
		},
		Rows: []CommunityShortsSendCountRow{{
			PostSendCount: outbox.PostSendCount{
				AlarmType:          domain.AlarmTypeCommunity,
				ChannelID:          "UC_TEST",
				PostID:             "community-post",
				ContentID:          "community-post",
				DetectedAt:         &detectedAt,
				OutboxCount:        1,
				SuccessSendCount:   1,
				SuccessRoomCount:   1,
				FailedAttemptCount: 0,
			},
			ReportAlarmType:         domain.AlarmTypeCommunity,
			ReportChannelID:         "UC_TEST",
			ReportPostID:            "community-post",
			ReportActualPublishedAt: &actualPublishedAt,
			ReportAlarmSentAt:       &alarmSentAt,
			ReportDelaySeconds:      &delaySeconds,
			DelaySource:             outbox.PostDelaySourceInternalDelivery,
			PublishToDetectMillis:   &publishToDetectMillis,
			InternalDelayCause:      outbox.PostInternalDelayCauseQueueWait,
			QueueWaitMillis:         &queueWaitMillis,
			LatencyClassification: outbox.PostLatencyClassificationResult{
				Status: outbox.PostLatencyClassificationStatusExceeded,
				Evidence: []outbox.PostLatencyClassificationEvidence{{
					Key:      outbox.PostLatencyClassificationEvidenceKeyQueueWait,
					Millis:   &queueWaitMillis,
					Selected: true,
				}},
			},
		}},
	}

	require.Equal(t, strings.Join([]string{
		"# YouTube Community/Shorts Post Send Counts Report",
		"",
		"- generated at: `2026-04-15T12:34:56Z`",
		"- mode: `recent_window`",
		"- window: `2026-04-15T10:34:56Z` -> `2026-04-15T12:34:56Z`",
		"- summary: posts=`1`, successful_posts=`1`, zero_success_posts=`0`, duplicate_success_posts=`0`, failed_attempt_posts=`0`, outbox_missing_posts=`0`, external_collection_source_posts=`0`, internal_delivery_source_posts=`1`, mixed_delay_source_posts=`0`, queue_wait_cause_posts=`1`, retry_accumulation_cause_posts=`0`, job_failure_cause_posts=`0`",
		"- duplicate alarm verdict: status=`pass`, duplicate_posts=`0`, rule=`duplicate_success_posts == 0`",
		"",
		"| status | alarm_type | channel_id | post_id | actual_published_at | detected_at | alarm_sent_at | delay_seconds | delay_source | publish_to_detect_ms | internal_delay_cause | queue_wait_ms | retry_accumulation_ms | job_failure_detected | latency_classification_status | latency_classification_evidence | outbox_count | success_send_count | success_room_count | duplicate_success_count | failed_attempt_count |",
		"| --- | --- | --- | --- | --- | --- | --- | ---: | --- | ---: | --- | ---: | ---: | --- | --- | --- | ---: | ---: | ---: | ---: | ---: |",
		"| `ok` | `COMMUNITY` | `UC_TEST` | `community-post` | `2026-04-15T10:59:56Z` | `2026-04-15T11:00:21Z` | `2026-04-15T11:02:56Z` | 180.000 | `internal_delivery` | 25000 | `queue_wait` | 90000 |  | `false` | `exceeded` | `queue_wait=90000[selected]` | 1 | 1 | 1 | 0 | 0 |",
	}, "\n")+"\n", RenderCommunityShortsSendCountMarkdown(report))
}
