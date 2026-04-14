package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func RenderCommunityShortsLatencyCauseMarkdown(report CommunityShortsLatencyCauseReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Latency Cause Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- mode: `")
	builder.WriteString(string(report.Query.Mode))
	builder.WriteString("`\n")
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		builder.WriteString("- window: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))
		builder.WriteString("` -> `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd))
		builder.WriteString("`\n")
	}
	if report.Query.Mode == communityShortsLatencyCauseQueryModeObservation {
		builder.WriteString("- observation runtime: `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
		builder.WriteString("`, cutover: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
		builder.WriteString("`\n")
	}
	builder.WriteString("- observed at basis: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(report.ObservedAtBasis))
	builder.WriteString("`\n")
	builder.WriteString("- threshold millis: `")
	fmt.Fprintf(&builder, "%d", report.ThresholdMillis)
	builder.WriteString("`\n")
	builder.WriteString("- internal cause rule: `")
	builder.WriteString(report.Verification.InternalCauseRule)
	builder.WriteString("`\n")
	builder.WriteString("- non internal rule: `")
	builder.WriteString(report.Verification.NonInternalCauseRule)
	builder.WriteString("`\n")
	builder.WriteString("- excluded external rule: `")
	builder.WriteString(report.Verification.ExcludedExternalRule)
	builder.WriteString("`\n")
	builder.WriteString("- insufficient evidence rule: `")
	builder.WriteString(report.Verification.InsufficientEvidenceRule)
	builder.WriteString("`\n")
	builder.WriteString("- cause evidence fields: `")
	builder.WriteString(strings.Join(report.Verification.EvidenceFieldCatalog, ", "))
	builder.WriteString("`\n")
	builder.WriteString("- periods: `")
	fmt.Fprintf(&builder, "%d", len(report.Periods))
	builder.WriteString("`\n")

	if len(report.Periods) == 0 {
		builder.WriteString("\n조회된 community/shorts 지연 원인 리포트가 없습니다.\n")
		return builder.String()
	}

	for i := range report.Periods {
		period := report.Periods[i]
		builder.WriteString("\n## `")
		builder.WriteString(strings.TrimSpace(period.Summary.Label))
		builder.WriteString("`\n\n")
		builder.WriteString("- window: `")
		builder.WriteString(formatCommunityShortsSendCountTime(period.Summary.StartAt))
		builder.WriteString("` -> `")
		builder.WriteString(formatCommunityShortsSendCountTime(period.Summary.EndAt))
		builder.WriteString("`\n")
		builder.WriteString("- latency summary: total_posts=`")
		fmt.Fprintf(&builder, "%d", period.Summary.TotalPostCount)
		builder.WriteString("`, alarm_sent_posts=`")
		fmt.Fprintf(&builder, "%d", period.Summary.AlarmSentPostCount)
		builder.WriteString("`, pending_posts=`")
		fmt.Fprintf(&builder, "%d", period.Summary.PendingPostCount)
		builder.WriteString("`, measured_posts=`")
		fmt.Fprintf(&builder, "%d", period.Summary.LatencyMeasuredPostCount)
		builder.WriteString("`, avg_latency_ms=`")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(period.Summary.AverageLatencyMillis))
		builder.WriteString("`, p95_latency_ms=`")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(period.Summary.P95LatencyMillis))
		builder.WriteString("`, max_latency_ms=`")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(period.Summary.MaxLatencyMillis))
		builder.WriteString("`, over_2m_posts=`")
		fmt.Fprintf(&builder, "%d", period.Summary.ExceededPostCount)
		builder.WriteString("`\n")
		builder.WriteString("- cause summary: exceeded_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.ExceededPostCount)
		builder.WriteString("`, internal_system_cause_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.InternalSystemCausePostCount)
		builder.WriteString("`, non_internal_system_cause_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.NonInternalSystemCausePostCount)
		builder.WriteString("`, excluded_external_delay_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.ExcludedExternalDelayPostCount)
		builder.WriteString("`, community_exceeded_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.CommunityExceededPostCount)
		builder.WriteString("`, shorts_exceeded_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.ShortsExceededPostCount)
		builder.WriteString("`, external_collection_source_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.ExternalCollectionSourcePostCount)
		builder.WriteString("`, internal_delivery_source_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.InternalDeliverySourcePostCount)
		builder.WriteString("`, mixed_delay_source_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.MixedDelaySourcePostCount)
		builder.WriteString("`, no_dominant_source_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.NoDominantSourcePostCount)
		builder.WriteString("`, internal_cause_candidate_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.InternalCauseCandidatePostCount)
		builder.WriteString("`, queue_wait_cause_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.QueueWaitCausePostCount)
		builder.WriteString("`, retry_accumulation_cause_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.RetryAccumulationCausePostCount)
		builder.WriteString("`, job_failure_cause_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.JobFailureCausePostCount)
		builder.WriteString("`, unclassified_internal_cause_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.UnclassifiedInternalCausePostCount)
		builder.WriteString("`, insufficient_evidence_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.InsufficientEvidencePostCount)
		builder.WriteString("`\n")
		builder.WriteString("- excluded external reference: excluded_external_delay_posts=`")
		fmt.Fprintf(&builder, "%d", period.CauseSummary.ExcludedExternalDelayPostCount)
		builder.WriteString("`, rule=`")
		builder.WriteString(report.Verification.ExcludedExternalRule)
		builder.WriteString("`\n")

		if len(period.Rows) == 0 {
			builder.WriteString("\n2분 초과 community/shorts 게시물이 없습니다.\n")
			continue
		}

		builder.WriteString("\n| alarm_type | channel_id | post_id | observed_at | actual_published_at | detected_at | alarm_sent_at | alarm_latency_ms | internal_cause_judgment | internal_cause_basis | cause_evidence_fields | delay_source | internal_delay_cause | publish_to_detect_ms | internal_latency_ms | queue_wait_ms | retry_accumulation_ms | job_failure_detected | cause_classification_status | cause_classification_evidence |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | --- | --- | --- |\n")
		for rowIndex := range period.Rows {
			row := period.Rows[rowIndex]
			builder.WriteString("| `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ObservedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt))
			builder.WriteString("` | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.AlarmLatencyMillis))
			builder.WriteString(" | `")
			builder.WriteString(string(row.InternalCauseJudgment))
			builder.WriteString("` | `")
			builder.WriteString(fallbackCommunityShortsSendCountValue(row.InternalCauseBasis))
			builder.WriteString("` | `")
			builder.WriteString(renderCommunityShortsLatencyCauseEvidenceFields(row.CauseEvidence))
			builder.WriteString("` | `")
			builder.WriteString(string(row.DelaySource))
			builder.WriteString("` | `")
			builder.WriteString(string(row.InternalDelayCause))
			builder.WriteString("` | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.PublishToDetectMillis))
			builder.WriteString(" | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.InternalLatencyMillis))
			builder.WriteString(" | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.QueueWaitMillis))
			builder.WriteString(" | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.RetryAccumulationMillis))
			builder.WriteString(" | `")
			builder.WriteString(formatCommunityShortsSendCountBool(row.JobFailureDetected))
			builder.WriteString("` | `")
			builder.WriteString(string(row.LatencyClassification.Status))
			builder.WriteString("` | `")
			builder.WriteString(renderCommunityShortsLatencyClassificationEvidence(row.LatencyClassification))
			builder.WriteString("` |\n")
		}
	}

	return builder.String()
}

func buildCommunityShortsLatencyCauseSummary(rows []CommunityShortsLatencyCauseRow) CommunityShortsLatencyCauseSummary {
	summary := CommunityShortsLatencyCauseSummary{}
	for i := range rows {
		row := rows[i]
		summary.ExceededPostCount++
		switch row.InternalCauseJudgment {
		case CommunityShortsInternalCauseJudgmentInternalSystem:
			summary.InternalSystemCausePostCount++
			summary.InternalCauseCandidatePostCount++
			incrementCommunityShortsLatencyCauseInternalCause(&summary, row.InternalDelayCause)
		default:
			summary.NonInternalSystemCausePostCount++
		}
		switch row.AlarmType {
		case domain.AlarmTypeCommunity:
			summary.CommunityExceededPostCount++
		case domain.AlarmTypeShorts:
			summary.ShortsExceededPostCount++
		}
		switch row.DelaySource {
		case outbox.PostDelaySourceExternalCollection:
			summary.ExcludedExternalDelayPostCount++
			summary.ExternalCollectionSourcePostCount++
		case outbox.PostDelaySourceInternalDelivery:
			summary.InternalDeliverySourcePostCount++
		case outbox.PostDelaySourceMixed:
			summary.MixedDelaySourcePostCount++
		default:
			summary.NoDominantSourcePostCount++
		}
		if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
			summary.InsufficientEvidencePostCount++
		}
	}
	return summary
}

func buildCommunityShortsLatencyCauseVerification() CommunityShortsLatencyCauseVerification {
	return CommunityShortsLatencyCauseVerification{
		InternalCauseRule:        communityShortsLatencyCauseInternalCauseRule,
		NonInternalCauseRule:     communityShortsLatencyCauseNonInternalCauseRule,
		ExcludedExternalRule:     communityShortsLatencyCauseExcludedExternalRule,
		InsufficientEvidenceRule: communityShortsLatencyCauseInsufficientEvidence,
		EvidenceFieldCatalog:     append([]string(nil), communityShortsLatencyCauseEvidenceFieldCatalog...),
	}
}

func renderCommunityShortsLatencyCauseEvidenceFields(evidence CommunityShortsLatencyCauseEvidence) string {
	if len(evidence.Fields) == 0 {
		return "(none)"
	}
	return strings.Join(evidence.Fields, ", ")
}

func incrementCommunityShortsLatencyCauseInternalCause(
	summary *CommunityShortsLatencyCauseSummary,
	cause outbox.PostInternalDelayCause,
) {
	if summary == nil {
		return
	}
	switch cause {
	case outbox.PostInternalDelayCauseQueueWait:
		summary.QueueWaitCausePostCount++
	case outbox.PostInternalDelayCauseRetryAccumulation:
		summary.RetryAccumulationCausePostCount++
	case outbox.PostInternalDelayCauseJobFailure:
		summary.JobFailureCausePostCount++
	default:
		summary.UnclassifiedInternalCausePostCount++
	}
}

func communityShortsLatencyCauseSortTime(row CommunityShortsLatencyCauseRow) time.Time {
	for _, candidate := range []*time.Time{row.ObservedAt, row.AlarmSentAt, row.DetectedAt, row.ActualPublishedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}
