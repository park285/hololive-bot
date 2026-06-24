package latencycause

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuild(t *testing.T) {
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

	report, err := Build(
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
	require.Equal(t, queryModeRecent, report.Query.Mode)
	require.NotNil(t, report.Query.WindowStart)
	require.Equal(t, generatedAt.Add(-24*time.Hour), report.Query.WindowStart.UTC())
	require.NotNil(t, report.Query.WindowEnd)
	require.Equal(t, generatedAt, report.Query.WindowEnd.UTC())
	require.Equal(t, observedAtBasis, report.ObservedAtBasis)
	require.Equal(t, int64(2*time.Minute/time.Millisecond), report.ThresholdMillis)
	require.Equal(t, internalCauseRule, report.Verification.InternalCauseRule)
	require.Equal(t, nonInternalCauseRule, report.Verification.NonInternalCauseRule)
	require.Equal(t, excludedExternalRule, report.Verification.ExcludedExternalRule)
	require.Equal(t, insufficientEvidence, report.Verification.InsufficientEvidenceRule)
	require.Equal(t, evidenceFieldCatalog, report.Verification.EvidenceFieldCatalog)
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
	require.Equal(t, InternalCauseJudgmentInternalSystem, lastHour.Rows[0].InternalCauseJudgment)
	require.Contains(t, lastHour.Rows[0].InternalCauseBasis, "delay_source=internal_delivery")
	require.Contains(t, lastHour.Rows[0].InternalCauseBasis, "internal_delay_cause=queue_wait")
	require.Equal(t, Evidence{
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
	require.Equal(t, InternalCauseJudgmentNonInternal, lastHour.Rows[1].InternalCauseJudgment)
	require.Equal(t, "delay_source=external_collection", lastHour.Rows[1].InternalCauseBasis)
	require.Equal(t, Evidence{
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

	markdown := RenderMarkdown(&report)
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

func TestBuild_UsesInsufficientEvidenceWhenTimelineMissing(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	periods := []outbox.PostLatencyPeriod{{Label: "last_24h", StartAt: generatedAt.Add(-24 * time.Hour), EndAt: generatedAt}}
	publishedAt := generatedAt.Add(-30 * time.Minute)
	alarmSentAt := publishedAt.Add(5 * time.Minute)
	latencyMillis := int64(5 * time.Minute / time.Millisecond)
	exceeded := true

	report, err := Build(
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
	require.Equal(t, InternalCauseJudgmentNonInternal, report.Periods[0].Rows[0].InternalCauseJudgment)
	require.Equal(t, "latency_classification=insufficient_evidence", report.Periods[0].Rows[0].InternalCauseBasis)
	require.Equal(t, Evidence{
		Fields: []string{"delay_source", "internal_delay_cause", "latency_classification.status"},
	}, report.Periods[0].Rows[0].CauseEvidence)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.NoDominantSourcePostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.NonInternalSystemCausePostCount)
	require.Equal(t, int64(0), report.Periods[0].CauseSummary.ExcludedExternalDelayPostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.InsufficientEvidencePostCount)
}

func TestRenderMarkdown_OutputStability(t *testing.T) {
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

	report := Report{
		GeneratedAt:     generatedAt,
		Query:           Query{Mode: queryModeRecent, WindowStart: &windowStart, WindowEnd: &windowEnd},
		ObservedAtBasis: observedAtBasis,
		ThresholdMillis: int64(2 * time.Minute / time.Millisecond),
		Verification: Verification{
			InternalCauseRule:        internalCauseRule,
			NonInternalCauseRule:     nonInternalCauseRule,
			ExcludedExternalRule:     excludedExternalRule,
			InsufficientEvidenceRule: insufficientEvidence,
			EvidenceFieldCatalog:     append([]string(nil), evidenceFieldCatalog...),
		},
		Periods: []PeriodView{{
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
			CauseSummary: Summary{
				ExceededPostCount:               1,
				InternalSystemCausePostCount:    1,
				NonInternalSystemCausePostCount: 0,
				CommunityExceededPostCount:      1,
				InternalDeliverySourcePostCount: 1,
				InternalCauseCandidatePostCount: 1,
				QueueWaitCausePostCount:         1,
			},
			Rows: []Row{{
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
				InternalCauseJudgment: InternalCauseJudgmentInternalSystem,
				InternalCauseBasis:    "delay_source=internal_delivery",
				CauseEvidence: Evidence{
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
	}, "\n")+"\n", RenderMarkdown(&report))
}

func TestBuild_ExternalCollectionOverridesInternalCauseForFailureAggregation(t *testing.T) {
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

	report, err := Build(
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
	require.Equal(t, InternalCauseJudgmentNonInternal, report.Periods[0].Rows[0].InternalCauseJudgment)
	require.Equal(t, "delay_source=external_collection", report.Periods[0].Rows[0].InternalCauseBasis)
	require.Equal(t, int64(0), report.Periods[0].CauseSummary.InternalSystemCausePostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.NonInternalSystemCausePostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.ExcludedExternalDelayPostCount)
	require.Equal(t, int64(1), report.Periods[0].CauseSummary.ExternalCollectionSourcePostCount)
	require.Equal(t, int64(0), report.Periods[0].CauseSummary.RetryAccumulationCausePostCount)
}

func TestNormalizeCollectOptions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)

	t.Run("recent mode defaults periods and window", func(t *testing.T) {
		query, periods, err := normalizeCollectOptions(CollectOptions{}, now)
		require.NoError(t, err)
		require.Equal(t, queryModeRecent, query.Mode)
		require.NotNil(t, query.WindowStart)
		require.NotNil(t, query.WindowEnd)
		require.Equal(t, now, query.WindowEnd.UTC())
		require.Len(t, periods, 3)
	})
}

func TestBuildEvidence(t *testing.T) {
	t.Parallel()

	row := Row{
		DelaySource:        outbox.PostDelaySourceMixed,
		InternalDelayCause: outbox.PostInternalDelayCauseRetryAccumulation,
		LatencyClassification: outbox.PostLatencyClassificationResult{
			Status: outbox.PostLatencyClassificationStatusExceeded,
			Evidence: []outbox.PostLatencyClassificationEvidence{
				{Key: outbox.PostLatencyClassificationEvidenceKeyAlarmLatency, Selected: true},
				{Key: outbox.PostLatencyClassificationEvidenceKeyRetryAccumulation, Selected: true},
			},
		},
	}
	got := buildEvidence(&row)

	require.Equal(t, Evidence{
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

func TestClassifyInternalJudgment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		row          Row
		wantJudgment InternalCauseJudgment
		wantBasis    string
	}{
		{
			name: "mixed delay source counts as internal",
			row: Row{
				DelaySource:           outbox.PostDelaySourceMixed,
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusExceeded},
			},
			wantJudgment: InternalCauseJudgmentInternalSystem,
			wantBasis:    "delay_source=mixed",
		},
		{
			name: "explicit internal cause counts as internal without dominant source",
			row: Row{
				InternalDelayCause:    outbox.PostInternalDelayCauseJobFailure,
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusExceeded},
			},
			wantJudgment: InternalCauseJudgmentInternalSystem,
			wantBasis:    "internal_delay_cause=job_failure",
		},
		{
			name: "external collection overrides internal delay cause for failure classification",
			row: Row{
				DelaySource:           outbox.PostDelaySourceExternalCollection,
				InternalDelayCause:    outbox.PostInternalDelayCauseJobFailure,
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusExceeded},
			},
			wantJudgment: InternalCauseJudgmentNonInternal,
			wantBasis:    "delay_source=external_collection",
		},
		{
			name: "insufficient evidence falls back to non internal",
			row: Row{
				LatencyClassification: outbox.PostLatencyClassificationResult{Status: outbox.PostLatencyClassificationStatusInsufficientEvidence},
			},
			wantJudgment: InternalCauseJudgmentNonInternal,
			wantBasis:    "latency_classification=insufficient_evidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotJudgment, gotBasis := classifyInternalJudgment(&tt.row)
			require.Equal(t, tt.wantJudgment, gotJudgment)
			require.Equal(t, tt.wantBasis, gotBasis)
		})
	}
}

func TestBuildPeriodReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	avgHour := int64(45000)
	p95Hour := int64(89000)
	maxHour := int64(110000)
	avgDay := int64(76000)
	p95Day := int64(125000)
	maxDay := int64(210000)

	report := BuildPeriodReport([]outbox.PostLatencyPeriodSummary{
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

	markdown := RenderPeriodMarkdown(&report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Latency Period Report")
	require.Contains(t, markdown, "p95_latency_ms")
	require.Contains(t, markdown, "`last_24h`")
	require.Contains(t, markdown, "`last_1h`")
	require.Contains(t, markdown, "| `last_24h` | `2026-04-09T12:00:00Z` | `2026-04-10T12:00:00Z` | 18 | 17 | 1 | 16 | 76000 | 125000 | 210000 | 2 | 1 | 1 |")
}

func TestBuildPeriods_DefaultsAndValidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	periods, err := buildPeriods(now, nil)
	require.NoError(t, err)
	require.Len(t, periods, 3)
	require.Equal(t, "last_1h", periods[0].Label)
	require.Equal(t, now.Add(-time.Hour), periods[0].StartAt)
	require.Equal(t, now, periods[0].EndAt)
	require.Equal(t, "last_24h", periods[1].Label)
	require.Equal(t, "last_7d", periods[2].Label)

	_, err = buildPeriods(now, []PeriodSpec{{Label: "dup", Window: time.Hour}, {Label: "dup", Window: 2 * time.Hour}})
	require.ErrorContains(t, err, "duplicate label")

	_, err = buildPeriods(now, []PeriodSpec{{Label: "", Window: time.Hour}})
	require.ErrorContains(t, err, "label is empty")

	_, err = buildPeriods(now, []PeriodSpec{{Label: "bad", Window: 0}})
	require.ErrorContains(t, err, "window must be greater than zero")
}
