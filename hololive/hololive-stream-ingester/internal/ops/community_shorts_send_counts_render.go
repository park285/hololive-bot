package ops

import (
	"fmt"
	"strings"
)

func RenderCommunityShortsSendCountMarkdown(report CommunityShortsSendCountReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Post Send Counts Report\n\n")
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
	if report.Query.Mode == communityShortsSendCountQueryModeObservation {
		builder.WriteString("- observation runtime: `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
		builder.WriteString("`, cutover: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
		builder.WriteString("`\n")
	}
	builder.WriteString("- summary: posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.PostCount))
	builder.WriteString("`, successful_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SuccessfulPostCount))
	builder.WriteString("`, zero_success_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ZeroSuccessPostCount))
	builder.WriteString("`, duplicate_success_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateSuccessPostCount))
	builder.WriteString("`, failed_attempt_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.FailedAttemptPostCount))
	builder.WriteString("`, outbox_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.OutboxMissingPostCount))
	builder.WriteString("`, external_collection_source_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ExternalCollectionSourcePostCount))
	builder.WriteString("`, internal_delivery_source_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.InternalDeliverySourcePostCount))
	builder.WriteString("`, mixed_delay_source_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MixedDelaySourcePostCount))
	builder.WriteString("`, queue_wait_cause_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.QueueWaitCausePostCount))
	builder.WriteString("`, retry_accumulation_cause_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.RetryAccumulationCausePostCount))
	builder.WriteString("`, job_failure_cause_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.JobFailureCausePostCount))
	builder.WriteString("`\n")
	builder.WriteString("- duplicate alarm verdict: status=`")
	builder.WriteString(string(report.Verification.DuplicateAlarmStatus))
	builder.WriteString("`, duplicate_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Verification.DuplicateAlarmPostCount))
	builder.WriteString("`, rule=`")
	builder.WriteString(report.Verification.DuplicateAlarmRule)
	builder.WriteString("`\n")

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts 게시물이 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| status | alarm_type | channel_id | post_id | actual_published_at | detected_at | alarm_sent_at | delay_seconds | delay_source | publish_to_detect_ms | internal_delay_cause | queue_wait_ms | retry_accumulation_ms | job_failure_detected | latency_classification_status | latency_classification_evidence | outbox_count | success_send_count | success_room_count | duplicate_success_count | failed_attempt_count |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | ---: | --- | ---: | --- | ---: | ---: | --- | --- | --- | ---: | ---: | ---: | ---: | ---: |\n")
	for i := range report.Rows {
		row := report.Rows[i]
		builder.WriteString("| `")
		builder.WriteString(resolveCommunityShortsSendCountStatus(row))
		builder.WriteString("` | `")
		builder.WriteString(string(row.AlarmType))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(resolveCommunityShortsSendCountPostID(row)))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportAlarmSentAt))
		builder.WriteString("` | ")
		builder.WriteString(formatCommunityShortsSendCountFloat64Ptr(row.ReportDelaySeconds))
		builder.WriteString(" | `")
		builder.WriteString(string(row.DelaySource))
		builder.WriteString("` | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.PublishToDetectMillis))
		builder.WriteString(" | `")
		builder.WriteString(string(row.InternalDelayCause))
		builder.WriteString("` | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.QueueWaitMillis))
		builder.WriteString(" | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.RetryAccumulationMillis))
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountBool(row.JobFailureDetected))
		builder.WriteString("` | `")
		builder.WriteString(string(row.LatencyClassification.Status))
		builder.WriteString("` | `")
		builder.WriteString(renderCommunityShortsLatencyClassificationEvidence(row.LatencyClassification))
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", row.OutboxCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.SuccessSendCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.SuccessRoomCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.DuplicateSuccessCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.FailedAttemptCount))
		builder.WriteString(" |\n")
	}

	return builder.String()
}

func buildCommunityShortsSendCountVerification(summary CommunityShortsSendCountSummary) CommunityShortsSendCountVerification {
	status := communityShortsSendCountDuplicateAlarmPass
	if summary.DuplicateSuccessPostCount > 0 {
		status = communityShortsSendCountDuplicateAlarmFail
	}

	return CommunityShortsSendCountVerification{
		DuplicateAlarmStatus:    status,
		DuplicateAlarmPostCount: summary.DuplicateSuccessPostCount,
		DuplicateAlarmRule:      communityShortsSendCountDuplicateAlarmRule,
	}
}

func resolveCommunityShortsSendCountStatus(row CommunityShortsSendCountRow) string {
	switch {
	case row.OutboxCount == 0:
		return "outbox_missing"
	case row.DuplicateSuccessCount > 0:
		return "duplicate_success"
	case row.SuccessSendCount == 0:
		return "no_success"
	case row.FailedAttemptCount > 0:
		return "failed_attempts"
	default:
		return "ok"
	}
}
