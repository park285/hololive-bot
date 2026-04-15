package ops

import (
	"strconv"
	"strings"
)

func RenderCommunityShortsSendCountMarkdown(report CommunityShortsSendCountReport) string {
	var builder strings.Builder

	writeCommunityShortsMarkdownHeading(&builder, 1, "YouTube Community/Shorts Post Send Counts Report")
	writeCommunityShortsMarkdownKV(&builder, "generated at", formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	writeCommunityShortsMarkdownKV(&builder, "mode", formatCommunityShortsMarkdownCode(string(report.Query.Mode)))
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		writeCommunityShortsMarkdownKV(
			&builder,
			"window",
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))+
				" -> "+
				formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd)),
		)
	}
	if report.Query.Mode == communityShortsSendCountQueryModeObservation {
		writeCommunityShortsMarkdownKV(
			&builder,
			"observation runtime",
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))+
				", cutover: "+
				formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
		)
	}
	writeCommunityShortsMarkdownKV(&builder, "summary", buildCommunityShortsSendCountSummaryMarkdown(report.Summary))
	writeCommunityShortsMarkdownKV(&builder, "duplicate alarm verdict", buildCommunityShortsSendCountVerificationMarkdown(report.Verification))

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts 게시물이 없습니다.\n")
		return builder.String()
	}

	writeCommunityShortsMarkdownTable(&builder, communityShortsSendCountMarkdownColumns, buildCommunityShortsSendCountMarkdownRows(report.Rows))

	return builder.String()
}

var communityShortsSendCountMarkdownColumns = []communityShortsMarkdownColumn{
	{Header: "status"},
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "post_id"},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "alarm_sent_at"},
	{Header: "delay_seconds", AlignRight: true},
	{Header: "delay_source"},
	{Header: "publish_to_detect_ms", AlignRight: true},
	{Header: "internal_delay_cause"},
	{Header: "queue_wait_ms", AlignRight: true},
	{Header: "retry_accumulation_ms", AlignRight: true},
	{Header: "job_failure_detected"},
	{Header: "latency_classification_status"},
	{Header: "latency_classification_evidence"},
	{Header: "outbox_count", AlignRight: true},
	{Header: "success_send_count", AlignRight: true},
	{Header: "success_room_count", AlignRight: true},
	{Header: "duplicate_success_count", AlignRight: true},
	{Header: "failed_attempt_count", AlignRight: true},
}

func buildCommunityShortsSendCountSummaryMarkdown(summary CommunityShortsSendCountSummary) string {
	parts := []string{
		"posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.PostCount)),
		"successful_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.SuccessfulPostCount)),
		"zero_success_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.ZeroSuccessPostCount)),
		"duplicate_success_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.DuplicateSuccessPostCount)),
		"failed_attempt_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.FailedAttemptPostCount)),
		"outbox_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.OutboxMissingPostCount)),
		"external_collection_source_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.ExternalCollectionSourcePostCount)),
		"internal_delivery_source_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.InternalDeliverySourcePostCount)),
		"mixed_delay_source_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.MixedDelaySourcePostCount)),
		"queue_wait_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.QueueWaitCausePostCount)),
		"retry_accumulation_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.RetryAccumulationCausePostCount)),
		"job_failure_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.JobFailureCausePostCount)),
	}
	return strings.Join(parts, ", ")
}

func buildCommunityShortsSendCountVerificationMarkdown(verification CommunityShortsSendCountVerification) string {
	return strings.Join([]string{
		"status=" + formatCommunityShortsMarkdownCode(string(verification.DuplicateAlarmStatus)),
		"duplicate_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(verification.DuplicateAlarmPostCount)),
		"rule=" + formatCommunityShortsMarkdownCode(verification.DuplicateAlarmRule),
	}, ", ")
}

func buildCommunityShortsSendCountMarkdownRows(rows []CommunityShortsSendCountRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			formatCommunityShortsMarkdownCode(resolveCommunityShortsSendCountStatus(row)),
			formatCommunityShortsMarkdownCode(string(row.AlarmType)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(row.ChannelID)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(resolveCommunityShortsSendCountPostID(row))),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ReportActualPublishedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ReportAlarmSentAt)),
			formatCommunityShortsSendCountFloat64Ptr(row.ReportDelaySeconds),
			formatCommunityShortsMarkdownCode(string(row.DelaySource)),
			formatCommunityShortsSendCountInt64Ptr(row.PublishToDetectMillis),
			formatCommunityShortsMarkdownCode(string(row.InternalDelayCause)),
			formatCommunityShortsSendCountInt64Ptr(row.QueueWaitMillis),
			formatCommunityShortsSendCountInt64Ptr(row.RetryAccumulationMillis),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountBool(row.JobFailureDetected)),
			formatCommunityShortsMarkdownCode(string(row.LatencyClassification.Status)),
			formatCommunityShortsMarkdownCode(renderCommunityShortsLatencyClassificationEvidence(row.LatencyClassification)),
			strconv.FormatInt(row.OutboxCount, 10),
			strconv.FormatInt(row.SuccessSendCount, 10),
			strconv.FormatInt(row.SuccessRoomCount, 10),
			strconv.FormatInt(row.DuplicateSuccessCount, 10),
			strconv.FormatInt(row.FailedAttemptCount, 10),
		})
	}
	return markdownRows
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
