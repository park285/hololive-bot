package communityshortsops

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

const communityShortsLatencyCauseNone = "(none)"

func RenderCommunityShortsLatencyCauseMarkdown(report CommunityShortsLatencyCauseReport) string {
	var builder strings.Builder

	writeCommunityShortsMarkdownHeading(&builder, 1, "YouTube Community/Shorts Latency Cause Report")
	writeCommunityShortsLatencyCauseReportMetadata(&builder, report)

	if len(report.Periods) == 0 {
		builder.WriteString("\n조회된 community/shorts 지연 원인 리포트가 없습니다.\n")
		return builder.String()
	}

	for i := range report.Periods {
		writeCommunityShortsLatencyCausePeriod(&builder, report.Periods[i], report.Verification)
	}

	return builder.String()
}

func writeCommunityShortsLatencyCauseReportMetadata(builder *strings.Builder, report CommunityShortsLatencyCauseReport) {
	writeCommunityShortsMarkdownKV(builder, "generated at", formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	writeCommunityShortsMarkdownKV(builder, "mode", formatCommunityShortsMarkdownCode(string(report.Query.Mode)))
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		writeCommunityShortsMarkdownKV(
			builder,
			"window",
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))+
				" -> "+
				formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd)),
		)
	}
	if report.Query.Mode == communityShortsLatencyCauseQueryModeObservation {
		writeCommunityShortsMarkdownKV(
			builder,
			"observation runtime",
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))+
				", cutover: "+
				formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
		)
	}
	writeCommunityShortsMarkdownKV(builder, "observed at basis", formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(report.ObservedAtBasis)))
	writeCommunityShortsMarkdownKV(builder, "threshold millis", formatCommunityShortsMarkdownCode(strconv.FormatInt(report.ThresholdMillis, 10)))
	writeCommunityShortsMarkdownKV(builder, "internal cause rule", formatCommunityShortsMarkdownCode(report.Verification.InternalCauseRule))
	writeCommunityShortsMarkdownKV(builder, "non internal rule", formatCommunityShortsMarkdownCode(report.Verification.NonInternalCauseRule))
	writeCommunityShortsMarkdownKV(builder, "excluded external rule", formatCommunityShortsMarkdownCode(report.Verification.ExcludedExternalRule))
	writeCommunityShortsMarkdownKV(builder, "insufficient evidence rule", formatCommunityShortsMarkdownCode(report.Verification.InsufficientEvidenceRule))
	writeCommunityShortsMarkdownKV(builder, "cause evidence fields", formatCommunityShortsMarkdownCode(strings.Join(report.Verification.EvidenceFieldCatalog, ", ")))
	writeCommunityShortsMarkdownKV(builder, "periods", formatCommunityShortsMarkdownCode(strconv.Itoa(len(report.Periods))))
}

func writeCommunityShortsLatencyCausePeriod(
	builder *strings.Builder,
	period CommunityShortsLatencyCausePeriodView,
	verification CommunityShortsLatencyCauseVerification,
) {
	builder.WriteString("\n")
	writeCommunityShortsMarkdownHeading(builder, 2, formatCommunityShortsMarkdownCode(strings.TrimSpace(period.Summary.Label)))
	writeCommunityShortsMarkdownKV(
		builder,
		"window",
		formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(period.Summary.StartAt))+
			" -> "+
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(period.Summary.EndAt)),
	)
	writeCommunityShortsMarkdownKV(builder, "latency summary", buildCommunityShortsLatencyPeriodSummaryMarkdown(period.Summary))
	writeCommunityShortsMarkdownKV(builder, "cause summary", buildCommunityShortsLatencyCauseSummaryMarkdown(period.CauseSummary))
	writeCommunityShortsMarkdownKV(
		builder,
		"excluded external reference",
		"excluded_external_delay_posts="+formatCommunityShortsMarkdownCode(strconv.FormatInt(period.CauseSummary.ExcludedExternalDelayPostCount, 10))+
			", rule="+formatCommunityShortsMarkdownCode(verification.ExcludedExternalRule),
	)

	if len(period.Rows) == 0 {
		builder.WriteString("\n2분 초과 community/shorts 게시물이 없습니다.\n")
		return
	}

	writeCommunityShortsMarkdownTable(builder, communityShortsLatencyCauseMarkdownColumns, buildCommunityShortsLatencyCauseMarkdownRows(period.Rows))
}

var communityShortsLatencyCauseMarkdownColumns = []communityShortsMarkdownColumn{
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "post_id"},
	{Header: "observed_at"},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "alarm_sent_at"},
	{Header: "alarm_latency_ms", AlignRight: true},
	{Header: "internal_cause_judgment"},
	{Header: "internal_cause_basis"},
	{Header: "cause_evidence_fields"},
	{Header: "delay_source"},
	{Header: "internal_delay_cause"},
	{Header: "publish_to_detect_ms", AlignRight: true},
	{Header: "internal_latency_ms", AlignRight: true},
	{Header: "queue_wait_ms", AlignRight: true},
	{Header: "retry_accumulation_ms", AlignRight: true},
	{Header: "job_failure_detected"},
	{Header: "cause_classification_status"},
	{Header: "cause_classification_evidence"},
}

func buildCommunityShortsLatencyPeriodSummaryMarkdown(summary outbox.PostLatencyPeriodSummary) string {
	return strings.Join([]string{
		"total_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.TotalPostCount, 10)),
		"alarm_sent_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.AlarmSentPostCount, 10)),
		"pending_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.PendingPostCount, 10)),
		"measured_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.LatencyMeasuredPostCount, 10)),
		"avg_latency_ms=" + formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountInt64Ptr(summary.AverageLatencyMillis)),
		"p95_latency_ms=" + formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountInt64Ptr(summary.P95LatencyMillis)),
		"max_latency_ms=" + formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountInt64Ptr(summary.MaxLatencyMillis)),
		"over_2m_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.ExceededPostCount, 10)),
	}, ", ")
}

func buildCommunityShortsLatencyCauseSummaryMarkdown(summary CommunityShortsLatencyCauseSummary) string {
	return strings.Join([]string{
		"exceeded_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.ExceededPostCount, 10)),
		"internal_system_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.InternalSystemCausePostCount, 10)),
		"non_internal_system_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.NonInternalSystemCausePostCount, 10)),
		"excluded_external_delay_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.ExcludedExternalDelayPostCount, 10)),
		"community_exceeded_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.CommunityExceededPostCount, 10)),
		"shorts_exceeded_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.ShortsExceededPostCount, 10)),
		"external_collection_source_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.ExternalCollectionSourcePostCount, 10)),
		"internal_delivery_source_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.InternalDeliverySourcePostCount, 10)),
		"mixed_delay_source_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.MixedDelaySourcePostCount, 10)),
		"no_dominant_source_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.NoDominantSourcePostCount, 10)),
		"internal_cause_candidate_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.InternalCauseCandidatePostCount, 10)),
		"queue_wait_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.QueueWaitCausePostCount, 10)),
		"retry_accumulation_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.RetryAccumulationCausePostCount, 10)),
		"job_failure_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.JobFailureCausePostCount, 10)),
		"unclassified_internal_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.UnclassifiedInternalCausePostCount, 10)),
		"insufficient_evidence_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(summary.InsufficientEvidencePostCount, 10)),
	}, ", ")
}

func buildCommunityShortsLatencyCauseMarkdownRows(rows []CommunityShortsLatencyCauseRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			formatCommunityShortsMarkdownCode(string(row.AlarmType)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(row.ChannelID)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(row.PostID)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ObservedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt)),
			formatCommunityShortsSendCountInt64Ptr(row.AlarmLatencyMillis),
			formatCommunityShortsMarkdownCode(string(row.InternalCauseJudgment)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(row.InternalCauseBasis)),
			formatCommunityShortsMarkdownCode(renderCommunityShortsLatencyCauseEvidenceFields(row.CauseEvidence)),
			formatCommunityShortsMarkdownCode(string(row.DelaySource)),
			formatCommunityShortsMarkdownCode(string(row.InternalDelayCause)),
			formatCommunityShortsSendCountInt64Ptr(row.PublishToDetectMillis),
			formatCommunityShortsSendCountInt64Ptr(row.InternalLatencyMillis),
			formatCommunityShortsSendCountInt64Ptr(row.QueueWaitMillis),
			formatCommunityShortsSendCountInt64Ptr(row.RetryAccumulationMillis),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountBool(row.JobFailureDetected)),
			formatCommunityShortsMarkdownCode(string(row.LatencyClassification.Status)),
			formatCommunityShortsMarkdownCode(renderCommunityShortsLatencyClassificationEvidence(row.LatencyClassification)),
		})
	}
	return markdownRows
}

func buildCommunityShortsLatencyCauseSummary(rows []CommunityShortsLatencyCauseRow) CommunityShortsLatencyCauseSummary {
	summary := CommunityShortsLatencyCauseSummary{}
	for i := range rows {
		accumulateCommunityShortsLatencyCauseSummaryRow(&summary, rows[i])
	}
	return summary
}

func accumulateCommunityShortsLatencyCauseSummaryRow(
	summary *CommunityShortsLatencyCauseSummary,
	row CommunityShortsLatencyCauseRow,
) {
	if summary == nil {
		return
	}
	summary.ExceededPostCount++
	accumulateCommunityShortsLatencyCauseJudgment(summary, row)
	accumulateCommunityShortsLatencyCauseAlarmType(summary, row.AlarmType)
	accumulateCommunityShortsLatencyCauseDelaySource(summary, row.DelaySource)
	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		summary.InsufficientEvidencePostCount++
	}
}

func accumulateCommunityShortsLatencyCauseJudgment(
	summary *CommunityShortsLatencyCauseSummary,
	row CommunityShortsLatencyCauseRow,
) {
	if row.InternalCauseJudgment != CommunityShortsInternalCauseJudgmentInternalSystem {
		summary.NonInternalSystemCausePostCount++
		return
	}
	summary.InternalSystemCausePostCount++
	summary.InternalCauseCandidatePostCount++
	incrementCommunityShortsLatencyCauseInternalCause(summary, row.InternalDelayCause)
}

func accumulateCommunityShortsLatencyCauseAlarmType(
	summary *CommunityShortsLatencyCauseSummary,
	alarmType domain.AlarmType,
) {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		summary.CommunityExceededPostCount++
	case domain.AlarmTypeShorts:
		summary.ShortsExceededPostCount++
	}
}

func accumulateCommunityShortsLatencyCauseDelaySource(
	summary *CommunityShortsLatencyCauseSummary,
	delaySource outbox.PostDelaySource,
) {
	switch delaySource {
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
		return communityShortsLatencyCauseNone
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

func sortedCommunityShortsLatencyCauseRows(rows []CommunityShortsLatencyCauseRow) []CommunityShortsLatencyCauseRow {
	sortedRows := append([]CommunityShortsLatencyCauseRow(nil), rows...)
	sort.SliceStable(sortedRows, func(left, right int) bool {
		return compareCommunityShortsLatencyCauseRows(sortedRows[left], sortedRows[right])
	})
	return sortedRows
}

func compareCommunityShortsLatencyCauseRows(left, right CommunityShortsLatencyCauseRow) bool {
	leftTime := communityShortsLatencyCauseSortTime(left)
	rightTime := communityShortsLatencyCauseSortTime(right)
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	if left.AlarmType != right.AlarmType {
		return left.AlarmType < right.AlarmType
	}
	if left.ChannelID != right.ChannelID {
		return left.ChannelID < right.ChannelID
	}
	if left.PostID != right.PostID {
		return left.PostID < right.PostID
	}
	return left.ContentID < right.ContentID
}
