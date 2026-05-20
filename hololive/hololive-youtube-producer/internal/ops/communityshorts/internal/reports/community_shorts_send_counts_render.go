package reports

import (
	"strconv"
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

func RenderCommunityShortsSendCountMarkdown(report CommunityShortsSendCountReport) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Post Send Counts Report")
	md.WriteKV(&builder, "generated at", md.Code(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	md.WriteKV(&builder, "mode", md.Code(string(report.Query.Mode)))
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		md.WriteKV(
			&builder,
			"window",
			md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))+
				" -> "+
				md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd)),
		)
	}
	if report.Query.Mode == communityShortsSendCountQueryModeObservation {
		md.WriteKV(
			&builder,
			"observation runtime",
			md.Code(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))+
				", cutover: "+
				md.Code(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
		)
	}
	md.WriteKV(&builder, "summary", buildCommunityShortsSendCountSummaryMarkdown(report.Summary))
	md.WriteKV(&builder, "duplicate alarm verdict", buildCommunityShortsSendCountVerificationMarkdown(report.Verification))

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts 게시물이 없습니다.\n")
		return builder.String()
	}

	md.WriteTable(&builder, communityShortsSendCountMarkdownColumns, buildCommunityShortsSendCountMarkdownRows(report.Rows))

	return builder.String()
}

var communityShortsSendCountMarkdownColumns = []md.Column{
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
		"posts=" + md.Code(strconv.Itoa(summary.PostCount)),
		"successful_posts=" + md.Code(strconv.Itoa(summary.SuccessfulPostCount)),
		"zero_success_posts=" + md.Code(strconv.Itoa(summary.ZeroSuccessPostCount)),
		"duplicate_success_posts=" + md.Code(strconv.Itoa(summary.DuplicateSuccessPostCount)),
		"failed_attempt_posts=" + md.Code(strconv.Itoa(summary.FailedAttemptPostCount)),
		"outbox_missing_posts=" + md.Code(strconv.Itoa(summary.OutboxMissingPostCount)),
		"external_collection_source_posts=" + md.Code(strconv.Itoa(summary.ExternalCollectionSourcePostCount)),
		"internal_delivery_source_posts=" + md.Code(strconv.Itoa(summary.InternalDeliverySourcePostCount)),
		"mixed_delay_source_posts=" + md.Code(strconv.Itoa(summary.MixedDelaySourcePostCount)),
		"queue_wait_cause_posts=" + md.Code(strconv.Itoa(summary.QueueWaitCausePostCount)),
		"retry_accumulation_cause_posts=" + md.Code(strconv.Itoa(summary.RetryAccumulationCausePostCount)),
		"job_failure_cause_posts=" + md.Code(strconv.Itoa(summary.JobFailureCausePostCount)),
	}
	return strings.Join(parts, ", ")
}

func buildCommunityShortsSendCountVerificationMarkdown(verification CommunityShortsSendCountVerification) string {
	return strings.Join([]string{
		"status=" + md.Code(string(verification.DuplicateAlarmStatus)),
		"duplicate_posts=" + md.Code(strconv.Itoa(verification.DuplicateAlarmPostCount)),
		"rule=" + md.Code(verification.DuplicateAlarmRule),
	}, ", ")
}

func buildCommunityShortsSendCountMarkdownRows(rows []CommunityShortsSendCountRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(resolveCommunityShortsSendCountStatus(row)),
			md.Code(string(row.AlarmType)),
			md.Code(fallbackCommunityShortsSendCountValue(row.ChannelID)),
			md.Code(fallbackCommunityShortsSendCountValue(resolveCommunityShortsSendCountPostID(row))),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ReportActualPublishedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ReportAlarmSentAt)),
			formatCommunityShortsSendCountFloat64Ptr(row.ReportDelaySeconds),
			md.Code(string(row.DelaySource)),
			formatCommunityShortsSendCountInt64Ptr(row.PublishToDetectMillis),
			md.Code(string(row.InternalDelayCause)),
			formatCommunityShortsSendCountInt64Ptr(row.QueueWaitMillis),
			formatCommunityShortsSendCountInt64Ptr(row.RetryAccumulationMillis),
			md.Code(formatCommunityShortsSendCountBool(row.JobFailureDetected)),
			md.Code(string(row.LatencyClassification.Status)),
			md.Code(renderCommunityShortsLatencyClassificationEvidence(row.LatencyClassification)),
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
	for _, status := range communityShortsSendCountStatuses(row) {
		if status.match {
			return status.value
		}
	}
	return "ok"
}

type communityShortsSendCountStatus struct {
	match bool
	value string
}

func communityShortsSendCountStatuses(row CommunityShortsSendCountRow) []communityShortsSendCountStatus {
	return []communityShortsSendCountStatus{
		{match: row.OutboxCount == 0, value: "outbox_missing"},
		{match: row.DuplicateSuccessCount > 0, value: "duplicate_success"},
		{match: row.SuccessSendCount == 0, value: "no_success"},
		{match: row.FailedAttemptCount > 0, value: "failed_attempts"},
	}
}
