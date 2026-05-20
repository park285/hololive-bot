package reports

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuildCommunityShortsLatencyCauseReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	periods := []outbox.PostLatencyPeriod{
		{Label: "last_1h", StartAt: generatedAt.Add(-time.Hour), EndAt: generatedAt},
		{Label: "last_24h", StartAt: generatedAt.Add(-24 * time.Hour), EndAt: generatedAt},
	}

	externalPublishedAt := generatedAt.Add(-50 * time.Minute)
	externalDetectedAt := externalPublishedAt.Add(130 * time.Second)
	externalAlarmSentAt := externalPublishedAt.Add(3 * time.Minute)
	externalLatencyMillis := int64(3 * time.Minute / time.Millisecond)
	externalExceeded := true

	internalPublishedAt := generatedAt.Add(-40 * time.Minute)
	internalDetectedAt := internalPublishedAt.Add(20 * time.Second)
	internalAlarmSentAt := internalPublishedAt.Add(4 * time.Minute)
	internalLatencyMillis := int64(4 * time.Minute / time.Millisecond)
	internalExceeded := true

	withinTargetPublishedAt := generatedAt.Add(-2 * time.Hour)
	withinTargetDetectedAt := withinTargetPublishedAt.Add(30 * time.Second)
	withinTargetAlarmSentAt := withinTargetPublishedAt.Add(90 * time.Second)
	withinTargetLatencyMillis := int64(90 * time.Second / time.Millisecond)
	withinTargetExceeded := false

	publishToDetectExternalMillis := int64(130 * time.Second / time.Millisecond)
	publishToDetectInternalMillis := int64(20 * time.Second / time.Millisecond)
	internalLatencyInternalMillis := int64(220 * time.Second / time.Millisecond)
	queueWaitMillis := int64(90 * time.Second / time.Millisecond)
	retryAccumulationMillis := int64(75 * time.Second / time.Millisecond)

	report, err := BuildCommunityShortsLatencyCauseReport(
		[]outbox.PostSendCount{
			{
				AlarmType:            domain.AlarmTypeCommunity,
				ChannelID:            "UC_EXTERNAL",
				PostID:               "community-external",
				ContentID:            "community-external",
				ActualPublishedAt:    &externalPublishedAt,
				DetectedAt:           &externalDetectedAt,
				AlarmSentAt:          &externalAlarmSentAt,
				AlarmLatencyMillis:   &externalLatencyMillis,
				AlarmLatencyExceeded: &externalExceeded,
			},
			{
				AlarmType:            domain.AlarmTypeShorts,
				ChannelID:            "UC_INTERNAL",
				PostID:               "short-internal",
				ContentID:            "short-internal",
				ActualPublishedAt:    &internalPublishedAt,
				DetectedAt:           &internalDetectedAt,
				AlarmSentAt:          &internalAlarmSentAt,
				AlarmLatencyMillis:   &internalLatencyMillis,
				AlarmLatencyExceeded: &internalExceeded,
			},
			{
				AlarmType:            domain.AlarmTypeCommunity,
				ChannelID:            "UC_OK",
				PostID:               "community-ok",
				ContentID:            "community-ok",
				ActualPublishedAt:    &withinTargetPublishedAt,
				DetectedAt:           &withinTargetDetectedAt,
				AlarmSentAt:          &withinTargetAlarmSentAt,
				AlarmLatencyMillis:   &withinTargetLatencyMillis,
				AlarmLatencyExceeded: &withinTargetExceeded,
			},
		},
		[]outbox.PostDeliveryTimeline{
			{
				AlarmType:             domain.AlarmTypeCommunity,
				ChannelID:             "UC_EXTERNAL",
				PostID:                "community-external",
				ContentID:             "community-external",
				ActualPublishedAt:     &externalPublishedAt,
				DetectedAt:            &externalDetectedAt,
				AlarmSentAt:           &externalAlarmSentAt,
				AlarmLatencyMillis:    &externalLatencyMillis,
				AlarmLatencyExceeded:  &externalExceeded,
				PublishToDetectMillis: &publishToDetectExternalMillis,
				DelaySource:           outbox.PostDelaySourceExternalCollection,
				LatencyClassification: outbox.PostLatencyClassificationResult{
					Status:             outbox.PostLatencyClassificationStatusExceeded,
					ThresholdMillis:    int64(2 * time.Minute / time.Millisecond),
					DelaySource:        outbox.PostDelaySourceExternalCollection,
					InternalDelayCause: outbox.PostInternalDelayCauseNone,
					Evidence: []outbox.PostLatencyClassificationEvidence{{
						Key:      outbox.PostLatencyClassificationEvidenceKeyPublishToDetect,
						Millis:   &publishToDetectExternalMillis,
						Selected: true,
					}},
				},
			},
			{
				AlarmType:               domain.AlarmTypeShorts,
				ChannelID:               "UC_INTERNAL",
				PostID:                  "short-internal",
				ContentID:               "short-internal",
				ActualPublishedAt:       &internalPublishedAt,
				DetectedAt:              &internalDetectedAt,
				AlarmSentAt:             &internalAlarmSentAt,
				AlarmLatencyMillis:      &internalLatencyMillis,
				AlarmLatencyExceeded:    &internalExceeded,
				PublishToDetectMillis:   &publishToDetectInternalMillis,
				InternalLatencyMillis:   &internalLatencyInternalMillis,
				QueueWaitMillis:         &queueWaitMillis,
				RetryAccumulationMillis: &retryAccumulationMillis,
				DelaySource:             outbox.PostDelaySourceInternalDelivery,
				InternalDelayCause:      outbox.PostInternalDelayCauseQueueWait,
				LatencyClassification: outbox.PostLatencyClassificationResult{
					Status:             outbox.PostLatencyClassificationStatusExceeded,
					ThresholdMillis:    int64(2 * time.Minute / time.Millisecond),
					DelaySource:        outbox.PostDelaySourceInternalDelivery,
					InternalDelayCause: outbox.PostInternalDelayCauseQueueWait,
					Evidence: []outbox.PostLatencyClassificationEvidence{{
						Key:      outbox.PostLatencyClassificationEvidenceKeyQueueWait,
						Millis:   &queueWaitMillis,
						Selected: true,
					}},
				},
			},
		},
		generatedAt,
		periods,
	)
	require.NoError(t, err)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Equal(t, communityShortsLatencyCauseQueryModeRecent, report.Query.Mode)
	require.NotNil(t, report.Query.WindowStart)
	require.Equal(t, generatedAt.Add(-24*time.Hour), report.Query.WindowStart.UTC())
	require.NotNil(t, report.Query.WindowEnd)
	require.Equal(t, generatedAt, report.Query.WindowEnd.UTC())
	require.Equal(t, communityShortsLatencyCauseObservedAtBasis, report.ObservedAtBasis)
	require.Equal(t, int64(2*time.Minute/time.Millisecond), report.ThresholdMillis)
	require.Equal(t, communityShortsLatencyCauseInternalCauseRule, report.Verification.InternalCauseRule)
	require.Equal(t, communityShortsLatencyCauseNonInternalCauseRule, report.Verification.NonInternalCauseRule)
	require.Equal(t, communityShortsLatencyCauseExcludedExternalRule, report.Verification.ExcludedExternalRule)
	require.Equal(t, communityShortsLatencyCauseInsufficientEvidence, report.Verification.InsufficientEvidenceRule)
	require.Equal(t, communityShortsLatencyCauseEvidenceFieldCatalog, report.Verification.EvidenceFieldCatalog)
	require.Len(t, report.RequestedPeriods, 2)
	require.Len(t, report.Periods, 2)

	lastHour := report.Periods[0]
	require.Equal(t, "last_1h", lastHour.Summary.Label)
	require.Equal(t, int64(2), lastHour.Summary.ExceededPostCount)
	require.Equal(t, int64(2), lastHour.CauseSummary.ExceededPostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.InternalSystemCausePostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.NonInternalSystemCausePostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.ExcludedExternalDelayPostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.CommunityExceededPostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.ShortsExceededPostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.ExternalCollectionSourcePostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.InternalDeliverySourcePostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.InternalCauseCandidatePostCount)
	require.Equal(t, int64(1), lastHour.CauseSummary.QueueWaitCausePostCount)
	require.Equal(t, int64(0), lastHour.CauseSummary.InsufficientEvidencePostCount)
	require.Len(t, lastHour.Rows, 2)
	require.Equal(t, "short-internal", lastHour.Rows[0].PostID)
	require.Equal(t, outbox.PostDelaySourceInternalDelivery, lastHour.Rows[0].DelaySource)
	require.Equal(t, outbox.PostInternalDelayCauseQueueWait, lastHour.Rows[0].InternalDelayCause)
	require.Equal(t, CommunityShortsInternalCauseJudgmentInternalSystem, lastHour.Rows[0].InternalCauseJudgment)
	require.Contains(t, lastHour.Rows[0].InternalCauseBasis, "delay_source=internal_delivery")
	require.Contains(t, lastHour.Rows[0].InternalCauseBasis, "internal_delay_cause=queue_wait")
	require.Equal(t, CommunityShortsLatencyCauseEvidence{
		Fields: []string{
			"delay_source",
			"internal_delay_cause",
			"internal_latency_millis",
			"queue_wait_millis",
			"latency_classification.evidence",
		},
		SelectedClassificationKeys: []outbox.PostLatencyClassificationEvidenceKey{
			outbox.PostLatencyClassificationEvidenceKeyQueueWait,
		},
	}, lastHour.Rows[0].CauseEvidence)
	require.Equal(t, outbox.PostLatencyClassificationStatusExceeded, lastHour.Rows[0].LatencyClassification.Status)
	require.NotNil(t, lastHour.Rows[0].ObservedAt)
	require.Equal(t, internalPublishedAt, lastHour.Rows[0].ObservedAt.UTC())
	require.Equal(t, "community-external", lastHour.Rows[1].PostID)
	require.Equal(t, outbox.PostDelaySourceExternalCollection, lastHour.Rows[1].DelaySource)
	require.Equal(t, CommunityShortsInternalCauseJudgmentNonInternal, lastHour.Rows[1].InternalCauseJudgment)
	require.Equal(t, "delay_source=external_collection", lastHour.Rows[1].InternalCauseBasis)
	require.Equal(t, CommunityShortsLatencyCauseEvidence{
		Fields: []string{
			"delay_source",
			"internal_delay_cause",
			"publish_to_detect_millis",
			"latency_classification.evidence",
		},
		SelectedClassificationKeys: []outbox.PostLatencyClassificationEvidenceKey{
			outbox.PostLatencyClassificationEvidenceKeyPublishToDetect,
		},
	}, lastHour.Rows[1].CauseEvidence)

	lastDay := report.Periods[1]
	require.Equal(t, "last_24h", lastDay.Summary.Label)
	require.Equal(t, int64(3), lastDay.Summary.TotalPostCount)
	require.Equal(t, int64(2), lastDay.Summary.ExceededPostCount)
	require.Equal(t, int64(2), lastDay.CauseSummary.ExceededPostCount)
	require.Len(t, lastDay.Rows, 2)

	markdown := RenderCommunityShortsLatencyCauseMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Latency Cause Report")
	require.Contains(t, markdown, "- mode: `recent_window`")
	require.Contains(t, markdown, "observed at basis: `COALESCE(actual_published_at, detected_at)`")
	require.Contains(t, markdown, "internal cause rule: `internal_system if delay_source in {internal_delivery,mixed} OR (internal_delay_cause != none AND delay_source != external_collection)`")
	require.Contains(t, markdown, "excluded external rule: `delay_source = external_collection rows stay logged as reference-only excluded_external_delay_posts and do not contribute to failure-driving counts`")
	require.Contains(t, markdown, "cause evidence fields: `delay_source, internal_delay_cause, alarm_latency_millis, publish_to_detect_millis, internal_latency_millis, queue_wait_millis, retry_accumulation_millis, job_failure_detected, latency_classification.status, latency_classification.evidence`")
	require.Contains(t, markdown, "## `last_1h`")
	require.Contains(t, markdown, "internal_system_cause_posts=`1`")
	require.Contains(t, markdown, "non_internal_system_cause_posts=`1`")
	require.Contains(t, markdown, "excluded_external_delay_posts=`1`")
	require.Contains(t, markdown, "external_collection_source_posts=`1`")
	require.Contains(t, markdown, "internal_delivery_source_posts=`1`")
	require.Contains(t, markdown, "queue_wait_cause_posts=`1`")
	require.Contains(t, markdown, "cause_evidence_fields")
	require.Contains(t, markdown, "delay_source, internal_delay_cause, publish_to_detect_millis, latency_classification.evidence")
	require.Contains(t, markdown, "`community-external`")
	require.Contains(t, markdown, "`short-internal`")
}

func TestBuildCommunityShortsLatencyCauseReportWithQuery_ObservationWindow(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	generatedAt := windowEnd.Add(15 * time.Minute)
	publishedAt := windowStart.Add(35 * time.Minute)
	detectedAt := publishedAt.Add(15 * time.Second)
	alarmSentAt := publishedAt.Add(3 * time.Minute)
	latencyMillis := int64(3 * time.Minute / time.Millisecond)
	exceeded := true
	queueWaitMillis := int64(100 * time.Second / time.Millisecond)
	internalLatencyMillis := int64(165 * time.Second / time.Millisecond)

	report, err := BuildCommunityShortsLatencyCauseReportWithQuery(
		[]outbox.PostSendCount{{
			AlarmType:            domain.AlarmTypeCommunity,
			ChannelID:            "UC_OBSERVATION",
			PostID:               "community-observation",
			ContentID:            "community-observation",
			ActualPublishedAt:    &publishedAt,
			DetectedAt:           &detectedAt,
			AlarmSentAt:          &alarmSentAt,
			AlarmLatencyMillis:   &latencyMillis,
			AlarmLatencyExceeded: &exceeded,
		}},
		[]outbox.PostDeliveryTimeline{{
			AlarmType:             domain.AlarmTypeCommunity,
			ChannelID:             "UC_OBSERVATION",
			PostID:                "community-observation",
			ContentID:             "community-observation",
			ActualPublishedAt:     &publishedAt,
			DetectedAt:            &detectedAt,
			AlarmSentAt:           &alarmSentAt,
			AlarmLatencyMillis:    &latencyMillis,
			AlarmLatencyExceeded:  &exceeded,
			InternalLatencyMillis: &internalLatencyMillis,
			QueueWaitMillis:       &queueWaitMillis,
			DelaySource:           outbox.PostDelaySourceInternalDelivery,
			InternalDelayCause:    outbox.PostInternalDelayCauseQueueWait,
			LatencyClassification: outbox.PostLatencyClassificationResult{
				Status:             outbox.PostLatencyClassificationStatusExceeded,
				ThresholdMillis:    int64(2 * time.Minute / time.Millisecond),
				DelaySource:        outbox.PostDelaySourceInternalDelivery,
				InternalDelayCause: outbox.PostInternalDelayCauseQueueWait,
			},
		}},
		CommunityShortsLatencyCauseQuery{
			Mode:                        communityShortsLatencyCauseQueryModeObservation,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
			ObservationRuntimeName:      "youtube-producer",
			ObservationBigBangCutoverAt: &cutoverAt,
		},
		generatedAt,
		[]outbox.PostLatencyPeriod{{
			Label:   communityShortsLatencyCauseObservationPeriodLabel,
			StartAt: windowStart,
			EndAt:   windowEnd,
		}},
	)
	require.NoError(t, err)
	require.Equal(t, communityShortsLatencyCauseQueryModeObservation, report.Query.Mode)
	require.NotNil(t, report.Query.WindowStart)
	require.Equal(t, windowStart, report.Query.WindowStart.UTC())
	require.NotNil(t, report.Query.WindowEnd)
	require.Equal(t, windowEnd, report.Query.WindowEnd.UTC())
	require.Equal(t, "youtube-producer", report.Query.ObservationRuntimeName)
	require.NotNil(t, report.Query.ObservationBigBangCutoverAt)
	require.Equal(t, cutoverAt, report.Query.ObservationBigBangCutoverAt.UTC())
	require.Len(t, report.RequestedPeriods, 1)
	require.Equal(t, communityShortsLatencyCauseObservationPeriodLabel, report.RequestedPeriods[0].Label)
	require.Equal(t, 24*time.Hour, report.RequestedPeriods[0].Window)
	require.Len(t, report.Periods, 1)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.InternalSystemCausePostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.QueueWaitCausePostCount)
	require.Equal(t, communityShortsLatencyCauseInternalCauseRule, report.Verification.InternalCauseRule)

	markdown := RenderCommunityShortsLatencyCauseMarkdown(report)
	require.Contains(t, markdown, "- mode: `observation_window`")
	require.Contains(t, markdown, "- observation runtime: `youtube-producer`, cutover: `2026-04-10T00:00:00Z`")
	require.Contains(t, markdown, "## `observation_window`")
	require.Contains(t, markdown, "`community-observation`")
}

func TestBuildCommunityShortsLatencyCauseReport_UsesInsufficientEvidenceWhenTimelineMissing(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	periods := []outbox.PostLatencyPeriod{{Label: "last_24h", StartAt: generatedAt.Add(-24 * time.Hour), EndAt: generatedAt}}
	publishedAt := generatedAt.Add(-30 * time.Minute)
	alarmSentAt := publishedAt.Add(5 * time.Minute)
	latencyMillis := int64(5 * time.Minute / time.Millisecond)
	exceeded := true

	report, err := BuildCommunityShortsLatencyCauseReport(
		[]outbox.PostSendCount{{
			AlarmType:            domain.AlarmTypeCommunity,
			ChannelID:            "UC_MISSING",
			ContentID:            "community-missing-timeline",
			ActualPublishedAt:    &publishedAt,
			AlarmSentAt:          &alarmSentAt,
			AlarmLatencyMillis:   &latencyMillis,
			AlarmLatencyExceeded: &exceeded,
		}},
		nil,
		generatedAt,
		periods,
	)
	require.NoError(t, err)
	require.Len(t, report.Periods, 1)
	require.Len(t, report.Periods[0].Rows, 1)
	require.Equal(t, outbox.PostLatencyClassificationStatusInsufficientEvidence, report.Periods[0].Rows[0].LatencyClassification.Status)
	require.Equal(t, CommunityShortsInternalCauseJudgmentNonInternal, report.Periods[0].Rows[0].InternalCauseJudgment)
	require.Equal(t, "latency_classification=insufficient_evidence", report.Periods[0].Rows[0].InternalCauseBasis)
	require.Equal(t, CommunityShortsLatencyCauseEvidence{
		Fields: []string{"delay_source", "internal_delay_cause", "latency_classification.status"},
	}, report.Periods[0].Rows[0].CauseEvidence)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.NoDominantSourcePostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.NonInternalSystemCausePostCount)
	require.Equal(t, int64(0), report.Periods[0].CauseSummary.ExcludedExternalDelayPostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.InsufficientEvidencePostCount)
}

func TestRenderCommunityShortsLatencyCauseMarkdown_OutputStability(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 15, 12, 34, 56, 0, time.UTC)
	windowStart := generatedAt.Add(-2 * time.Hour)
	windowEnd := generatedAt
	periodStart := generatedAt.Add(-time.Hour)
	periodEnd := generatedAt
	observedAt := generatedAt.Add(-50 * time.Minute)
	actualPublishedAt := observedAt
	detectedAt := observedAt.Add(20 * time.Second)
	alarmSentAt := observedAt.Add(3 * time.Minute)
	alarmLatencyMillis := int64(3 * time.Minute / time.Millisecond)
	publishToDetectMillis := int64(20 * time.Second / time.Millisecond)
	internalLatencyMillis := int64(160 * time.Second / time.Millisecond)
	queueWaitMillis := int64(80 * time.Second / time.Millisecond)

	report := CommunityShortsLatencyCauseReport{
		GeneratedAt:     generatedAt,
		Query:           CommunityShortsLatencyCauseQuery{Mode: communityShortsLatencyCauseQueryModeRecent, WindowStart: &windowStart, WindowEnd: &windowEnd},
		ObservedAtBasis: communityShortsLatencyCauseObservedAtBasis,
		ThresholdMillis: int64(2 * time.Minute / time.Millisecond),
		Verification: CommunityShortsLatencyCauseVerification{
			InternalCauseRule:        communityShortsLatencyCauseInternalCauseRule,
			NonInternalCauseRule:     communityShortsLatencyCauseNonInternalCauseRule,
			ExcludedExternalRule:     communityShortsLatencyCauseExcludedExternalRule,
			InsufficientEvidenceRule: communityShortsLatencyCauseInsufficientEvidence,
			EvidenceFieldCatalog:     append([]string(nil), communityShortsLatencyCauseEvidenceFieldCatalog...),
		},
		Periods: []CommunityShortsLatencyCausePeriodView{{
			Summary: outbox.PostLatencyPeriodSummary{
				Label:                    "last_1h",
				StartAt:                  periodStart,
				EndAt:                    periodEnd,
				TotalPostCount:           1,
				AlarmSentPostCount:       1,
				PendingPostCount:         0,
				LatencyMeasuredPostCount: 1,
				AverageLatencyMillis:     &alarmLatencyMillis,
				P95LatencyMillis:         &alarmLatencyMillis,
				MaxLatencyMillis:         &alarmLatencyMillis,
				ExceededPostCount:        1,
			},
			CauseSummary: CommunityShortsLatencyCauseSummary{
				ExceededPostCount:               1,
				InternalSystemCausePostCount:    1,
				NonInternalSystemCausePostCount: 0,
				CommunityExceededPostCount:      1,
				InternalDeliverySourcePostCount: 1,
				InternalCauseCandidatePostCount: 1,
				QueueWaitCausePostCount:         1,
			},
			Rows: []CommunityShortsLatencyCauseRow{{
				AlarmType:             domain.AlarmTypeCommunity,
				ChannelID:             "UC_TEST",
				PostID:                "community-post",
				ObservedAt:            &observedAt,
				ActualPublishedAt:     &actualPublishedAt,
				DetectedAt:            &detectedAt,
				AlarmSentAt:           &alarmSentAt,
				AlarmLatencyMillis:    &alarmLatencyMillis,
				PublishToDetectMillis: &publishToDetectMillis,
				InternalLatencyMillis: &internalLatencyMillis,
				QueueWaitMillis:       &queueWaitMillis,
				DelaySource:           outbox.PostDelaySourceInternalDelivery,
				InternalDelayCause:    outbox.PostInternalDelayCauseQueueWait,
				InternalCauseJudgment: CommunityShortsInternalCauseJudgmentInternalSystem,
				InternalCauseBasis:    "delay_source=internal_delivery",
				CauseEvidence: CommunityShortsLatencyCauseEvidence{
					Fields: []string{
						"delay_source",
						"internal_delay_cause",
						"internal_latency_millis",
						"queue_wait_millis",
					},
				},
				LatencyClassification: outbox.PostLatencyClassificationResult{
					Status: outbox.PostLatencyClassificationStatusExceeded,
					Evidence: []outbox.PostLatencyClassificationEvidence{{
						Key:      outbox.PostLatencyClassificationEvidenceKeyQueueWait,
						Millis:   &queueWaitMillis,
						Selected: true,
					}},
				},
			}},
		}},
	}

	require.Equal(t, strings.Join([]string{
		"# YouTube Community/Shorts Latency Cause Report",
		"",
		"- generated at: `2026-04-15T12:34:56Z`",
		"- mode: `recent_window`",
		"- window: `2026-04-15T10:34:56Z` -> `2026-04-15T12:34:56Z`",
		"- observed at basis: `COALESCE(actual_published_at, detected_at)`",
		"- threshold millis: `120000`",
		"- internal cause rule: `internal_system if delay_source in {internal_delivery,mixed} OR (internal_delay_cause != none AND delay_source != external_collection)`",
		"- non internal rule: `non_internal if delay_source = external_collection OR (delay_source = none AND internal_delay_cause = none)`",
		"- excluded external rule: `delay_source = external_collection rows stay logged as reference-only excluded_external_delay_posts and do not contribute to failure-driving counts`",
		"- insufficient evidence rule: `latency_classification.status = insufficient_evidence keeps the row in non_internal and increments insufficient_evidence_posts`",
		"- cause evidence fields: `delay_source, internal_delay_cause, alarm_latency_millis, publish_to_detect_millis, internal_latency_millis, queue_wait_millis, retry_accumulation_millis, job_failure_detected, latency_classification.status, latency_classification.evidence`",
		"- periods: `1`",
		"",
		"## `last_1h`",
		"",
		"- window: `2026-04-15T11:34:56Z` -> `2026-04-15T12:34:56Z`",
		"- latency summary: total_posts=`1`, alarm_sent_posts=`1`, pending_posts=`0`, measured_posts=`1`, avg_latency_ms=`180000`, p95_latency_ms=`180000`, max_latency_ms=`180000`, over_2m_posts=`1`",
		"- cause summary: exceeded_posts=`1`, internal_system_cause_posts=`1`, non_internal_system_cause_posts=`0`, excluded_external_delay_posts=`0`, community_exceeded_posts=`1`, shorts_exceeded_posts=`0`, external_collection_source_posts=`0`, internal_delivery_source_posts=`1`, mixed_delay_source_posts=`0`, no_dominant_source_posts=`0`, internal_cause_candidate_posts=`1`, queue_wait_cause_posts=`1`, retry_accumulation_cause_posts=`0`, job_failure_cause_posts=`0`, unclassified_internal_cause_posts=`0`, insufficient_evidence_posts=`0`",
		"- excluded external reference: excluded_external_delay_posts=`0`, rule=`delay_source = external_collection rows stay logged as reference-only excluded_external_delay_posts and do not contribute to failure-driving counts`",
		"",
		"| alarm_type | channel_id | post_id | observed_at | actual_published_at | detected_at | alarm_sent_at | alarm_latency_ms | internal_cause_judgment | internal_cause_basis | cause_evidence_fields | delay_source | internal_delay_cause | publish_to_detect_ms | internal_latency_ms | queue_wait_ms | retry_accumulation_ms | job_failure_detected | cause_classification_status | cause_classification_evidence |",
		"| --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | --- | --- | --- |",
		"| `COMMUNITY` | `UC_TEST` | `community-post` | `2026-04-15T11:44:56Z` | `2026-04-15T11:44:56Z` | `2026-04-15T11:45:16Z` | `2026-04-15T11:47:56Z` | 180000 | `internal_system` | `delay_source=internal_delivery` | `delay_source, internal_delay_cause, internal_latency_millis, queue_wait_millis` | `internal_delivery` | `queue_wait` | 20000 | 160000 | 80000 |  | `false` | `exceeded` | `queue_wait=80000[selected]` |",
	}, "\n")+"\n", RenderCommunityShortsLatencyCauseMarkdown(report))
}

func TestBuildCommunityShortsLatencyCauseReport_ExternalCollectionOverridesInternalCauseForFailureAggregation(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	publishedAt := generatedAt.Add(-10 * time.Minute)
	detectedAt := publishedAt.Add(3 * time.Minute)
	alarmSentAt := publishedAt.Add(4 * time.Minute)
	latencyMillis := int64(alarmSentAt.Sub(publishedAt) / time.Millisecond)
	exceeded := true
	publishToDetectMillis := int64(detectedAt.Sub(publishedAt) / time.Millisecond)
	retryAccumulationMillis := int64(30 * time.Second / time.Millisecond)
	periods := []outbox.PostLatencyPeriod{{Label: "last_1h", StartAt: generatedAt.Add(-time.Hour), EndAt: generatedAt}}

	report, err := BuildCommunityShortsLatencyCauseReport(
		[]outbox.PostSendCount{{
			AlarmType:            domain.AlarmTypeCommunity,
			ChannelID:            "UC_EXTERNAL_OVERRIDE",
			ContentID:            "community-external-override",
			ActualPublishedAt:    &publishedAt,
			DetectedAt:           &detectedAt,
			AlarmSentAt:          &alarmSentAt,
			AlarmLatencyMillis:   &latencyMillis,
			AlarmLatencyExceeded: &exceeded,
		}},
		[]outbox.PostDeliveryTimeline{{
			AlarmType:               domain.AlarmTypeCommunity,
			ChannelID:               "UC_EXTERNAL_OVERRIDE",
			ContentID:               "community-external-override",
			PublishToDetectMillis:   &publishToDetectMillis,
			RetryAccumulationMillis: &retryAccumulationMillis,
			DelaySource:             outbox.PostDelaySourceExternalCollection,
			InternalDelayCause:      outbox.PostInternalDelayCauseRetryAccumulation,
			LatencyClassification: outbox.PostLatencyClassificationResult{
				Status:             outbox.PostLatencyClassificationStatusExceeded,
				ThresholdMillis:    int64(2 * time.Minute / time.Millisecond),
				DelaySource:        outbox.PostDelaySourceExternalCollection,
				InternalDelayCause: outbox.PostInternalDelayCauseRetryAccumulation,
			},
		}},
		generatedAt,
		periods,
	)
	require.NoError(t, err)
	require.Len(t, report.Periods, 1)
	require.Len(t, report.Periods[0].Rows, 1)
	require.Equal(t, CommunityShortsInternalCauseJudgmentNonInternal, report.Periods[0].Rows[0].InternalCauseJudgment)
	require.Equal(t, "delay_source=external_collection", report.Periods[0].Rows[0].InternalCauseBasis)
	require.Equal(t, int64(0), report.Periods[0].CauseSummary.InternalSystemCausePostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.NonInternalSystemCausePostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.ExcludedExternalDelayPostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.ExternalCollectionSourcePostCount)
	require.Equal(t, int64(0), report.Periods[0].CauseSummary.RetryAccumulationCausePostCount)
}

func TestNormalizeCommunityShortsLatencyCauseCollectOptions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)

	t.Run("recent mode defaults periods and window", func(t *testing.T) {
		query, periods, err := normalizeCommunityShortsLatencyCauseCollectOptions(CommunityShortsLatencyCauseCollectOptions{}, now)
		require.NoError(t, err)
		require.Equal(t, communityShortsLatencyCauseQueryModeRecent, query.Mode)
		require.NotNil(t, query.WindowStart)
		require.NotNil(t, query.WindowEnd)
		require.Equal(t, now, query.WindowEnd.UTC())
		require.Len(t, periods, 3)
	})

	t.Run("observation mode rejects period specs", func(t *testing.T) {
		_, _, err := normalizeCommunityShortsLatencyCauseCollectOptions(CommunityShortsLatencyCauseCollectOptions{
			PeriodSpecs:                 []CommunityShortsLatencyPeriodSpec{{Label: "last_24h", Window: 24 * time.Hour}},
			ObservationRuntimeName:      "youtube-producer",
			ObservationBigBangCutoverAt: &cutoverAt,
		}, now)
		require.EqualError(t, err, "period specs and observation window are mutually exclusive")
	})

	t.Run("observation mode requires full key", func(t *testing.T) {
		_, _, err := normalizeCommunityShortsLatencyCauseCollectOptions(CommunityShortsLatencyCauseCollectOptions{
			ObservationRuntimeName: "youtube-producer",
		}, now)
		require.EqualError(t, err, "observation runtime name and cutover must both be set")
	})

	t.Run("observation mode returns query without recent periods", func(t *testing.T) {
		query, periods, err := normalizeCommunityShortsLatencyCauseCollectOptions(CommunityShortsLatencyCauseCollectOptions{
			ObservationRuntimeName:      "youtube-producer",
			ObservationBigBangCutoverAt: &cutoverAt,
		}, now)
		require.NoError(t, err)
		require.Equal(t, communityShortsLatencyCauseQueryModeObservation, query.Mode)
		require.Equal(t, "youtube-producer", query.ObservationRuntimeName)
		require.NotNil(t, query.ObservationBigBangCutoverAt)
		require.Equal(t, cutoverAt, query.ObservationBigBangCutoverAt.UTC())
		require.Nil(t, query.WindowStart)
		require.Nil(t, query.WindowEnd)
		require.Nil(t, periods)
	})
}

func TestBuildCommunityShortsLatencyCauseEvidence(t *testing.T) {
	t.Parallel()

	got := buildCommunityShortsLatencyCauseEvidence(CommunityShortsLatencyCauseRow{
		DelaySource:        outbox.PostDelaySourceMixed,
		InternalDelayCause: outbox.PostInternalDelayCauseRetryAccumulation,
		LatencyClassification: outbox.PostLatencyClassificationResult{
			Status: outbox.PostLatencyClassificationStatusExceeded,
			Evidence: []outbox.PostLatencyClassificationEvidence{
				{Key: outbox.PostLatencyClassificationEvidenceKeyAlarmLatency, Selected: true},
				{Key: outbox.PostLatencyClassificationEvidenceKeyRetryAccumulation, Selected: true},
			},
		},
	})

	require.Equal(t, CommunityShortsLatencyCauseEvidence{
		Fields: []string{
			"delay_source",
			"internal_delay_cause",
			"publish_to_detect_millis",
			"internal_latency_millis",
			"retry_accumulation_millis",
			"alarm_latency_millis",
			"latency_classification.evidence",
		},
		SelectedClassificationKeys: []outbox.PostLatencyClassificationEvidenceKey{
			outbox.PostLatencyClassificationEvidenceKeyAlarmLatency,
			outbox.PostLatencyClassificationEvidenceKeyRetryAccumulation,
		},
	}, got)
}

func TestClassifyCommunityShortsLatencyCauseInternalJudgment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		row          CommunityShortsLatencyCauseRow
		wantJudgment CommunityShortsInternalCauseJudgment
		wantBasis    string
	}{
		{
			name: "mixed delay source counts as internal",
			row: CommunityShortsLatencyCauseRow{
				DelaySource:           outbox.PostDelaySourceMixed,
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusExceeded},
			},
			wantJudgment: CommunityShortsInternalCauseJudgmentInternalSystem,
			wantBasis:    "delay_source=mixed",
		},
		{
			name: "explicit internal cause counts as internal without dominant source",
			row: CommunityShortsLatencyCauseRow{
				InternalDelayCause:    outbox.PostInternalDelayCauseJobFailure,
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusExceeded},
			},
			wantJudgment: CommunityShortsInternalCauseJudgmentInternalSystem,
			wantBasis:    "internal_delay_cause=job_failure",
		},
		{
			name: "external collection overrides internal delay cause for failure classification",
			row: CommunityShortsLatencyCauseRow{
				DelaySource:           outbox.PostDelaySourceExternalCollection,
				InternalDelayCause:    outbox.PostInternalDelayCauseJobFailure,
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusExceeded},
			},
			wantJudgment: CommunityShortsInternalCauseJudgmentNonInternal,
			wantBasis:    "delay_source=external_collection",
		},
		{
			name: "insufficient evidence falls back to non internal",
			row: CommunityShortsLatencyCauseRow{
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusInsufficientEvidence},
			},
			wantJudgment: CommunityShortsInternalCauseJudgmentNonInternal,
			wantBasis:    "latency_classification=insufficient_evidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotJudgment, gotBasis := classifyCommunityShortsLatencyCauseInternalJudgment(tt.row)
			require.Equal(t, tt.wantJudgment, gotJudgment)
			require.Equal(t, tt.wantBasis, gotBasis)
		})
	}
}
