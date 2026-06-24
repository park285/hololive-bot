package latencycause

import (
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func RenderMarkdown(report *Report) string {
	if report == nil {
		return renderMarkdown(&Report{})
	}
	return renderMarkdown(report)
}

func renderMarkdown(report *Report) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Latency Cause Report")
	writeReportMetadata(&builder, report)

	if len(report.Periods) == 0 {
		builder.WriteString("\n조회된 community/shorts 지연 원인 리포트가 없습니다.\n")
		return builder.String()
	}

	for i := range report.Periods {
		writePeriod(&builder, &report.Periods[i], &report.Verification)
	}

	return builder.String()
}

func writeReportMetadata(builder *strings.Builder, report *Report) {
	md.WriteKV(builder, "generated at", md.Code(shared.FormatSendCountTime(report.GeneratedAt)))
	md.WriteKV(builder, "mode", md.Code(string(report.Query.Mode)))
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		md.WriteKV(
			builder,
			"window",
			md.Code(shared.FormatSendCountTimePtr(report.Query.WindowStart))+
				" -> "+
				md.Code(shared.FormatSendCountTimePtr(report.Query.WindowEnd)),
		)
	}
	md.WriteKV(builder, "observed at basis", md.Code(shared.FallbackSendCountValue(report.ObservedAtBasis)))
	md.WriteKV(builder, "threshold millis", md.Code(strconv.FormatInt(report.ThresholdMillis, 10)))
	md.WriteKV(builder, "internal cause rule", md.Code(report.Verification.InternalCauseRule))
	md.WriteKV(builder, "non internal rule", md.Code(report.Verification.NonInternalCauseRule))
	md.WriteKV(builder, "excluded external rule", md.Code(report.Verification.ExcludedExternalRule))
	md.WriteKV(builder, "insufficient evidence rule", md.Code(report.Verification.InsufficientEvidenceRule))
	md.WriteKV(builder, "cause evidence fields", md.Code(strings.Join(report.Verification.EvidenceFieldCatalog, ", ")))
	md.WriteKV(builder, "periods", md.Code(strconv.Itoa(len(report.Periods))))
}

func writePeriod(
	builder *strings.Builder,
	period *PeriodView,
	verification *Verification,
) {
	if period == nil || verification == nil {
		return
	}
	builder.WriteString("\n")
	md.WriteHeading(builder, 2, md.Code(strings.TrimSpace(period.Summary.Label)))
	md.WriteKV(
		builder,
		"window",
		md.Code(shared.FormatSendCountTime(period.Summary.StartAt))+
			" -> "+
			md.Code(shared.FormatSendCountTime(period.Summary.EndAt)),
	)
	md.WriteKV(builder, "latency summary", buildPeriodSummaryMarkdown(&period.Summary))
	md.WriteKV(builder, "cause summary", buildCauseSummaryMarkdown(&period.CauseSummary))
	md.WriteKV(
		builder,
		"excluded external reference",
		"excluded_external_delay_posts="+md.Code(strconv.FormatInt(period.CauseSummary.ExcludedExternalDelayPostCount, 10))+
			", rule="+md.Code(verification.ExcludedExternalRule),
	)

	if len(period.Rows) == 0 {
		builder.WriteString("\n2분 초과 community/shorts 게시물이 없습니다.\n")
		return
	}

	md.WriteTable(builder, markdownColumns, buildMarkdownRows(period.Rows))
}

var markdownColumns = []md.Column{
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

func buildPeriodSummaryMarkdown(summary *outbox.PostLatencyPeriodSummary) string {
	if summary == nil {
		return ""
	}
	return strings.Join([]string{
		"total_posts=" + md.Code(strconv.FormatInt(summary.TotalPostCount, 10)),
		"alarm_sent_posts=" + md.Code(strconv.FormatInt(summary.AlarmSentPostCount, 10)),
		"pending_posts=" + md.Code(strconv.FormatInt(summary.PendingPostCount, 10)),
		"measured_posts=" + md.Code(strconv.FormatInt(summary.LatencyMeasuredPostCount, 10)),
		"avg_latency_ms=" + md.Code(shared.FormatSendCountInt64Ptr(summary.AverageLatencyMillis)),
		"p95_latency_ms=" + md.Code(shared.FormatSendCountInt64Ptr(summary.P95LatencyMillis)),
		"max_latency_ms=" + md.Code(shared.FormatSendCountInt64Ptr(summary.MaxLatencyMillis)),
		"over_2m_posts=" + md.Code(strconv.FormatInt(summary.ExceededPostCount, 10)),
	}, ", ")
}

func buildCauseSummaryMarkdown(summary *Summary) string {
	if summary == nil {
		return ""
	}
	return strings.Join([]string{
		"exceeded_posts=" + md.Code(strconv.FormatInt(summary.ExceededPostCount, 10)),
		"internal_system_cause_posts=" + md.Code(strconv.FormatInt(summary.InternalSystemCausePostCount, 10)),
		"non_internal_system_cause_posts=" + md.Code(strconv.FormatInt(summary.NonInternalSystemCausePostCount, 10)),
		"excluded_external_delay_posts=" + md.Code(strconv.FormatInt(summary.ExcludedExternalDelayPostCount, 10)),
		"community_exceeded_posts=" + md.Code(strconv.FormatInt(summary.CommunityExceededPostCount, 10)),
		"shorts_exceeded_posts=" + md.Code(strconv.FormatInt(summary.ShortsExceededPostCount, 10)),
		"external_collection_source_posts=" + md.Code(strconv.FormatInt(summary.ExternalCollectionSourcePostCount, 10)),
		"internal_delivery_source_posts=" + md.Code(strconv.FormatInt(summary.InternalDeliverySourcePostCount, 10)),
		"mixed_delay_source_posts=" + md.Code(strconv.FormatInt(summary.MixedDelaySourcePostCount, 10)),
		"no_dominant_source_posts=" + md.Code(strconv.FormatInt(summary.NoDominantSourcePostCount, 10)),
		"internal_cause_candidate_posts=" + md.Code(strconv.FormatInt(summary.InternalCauseCandidatePostCount, 10)),
		"queue_wait_cause_posts=" + md.Code(strconv.FormatInt(summary.QueueWaitCausePostCount, 10)),
		"retry_accumulation_cause_posts=" + md.Code(strconv.FormatInt(summary.RetryAccumulationCausePostCount, 10)),
		"job_failure_cause_posts=" + md.Code(strconv.FormatInt(summary.JobFailureCausePostCount, 10)),
		"unclassified_internal_cause_posts=" + md.Code(strconv.FormatInt(summary.UnclassifiedInternalCausePostCount, 10)),
		"insufficient_evidence_posts=" + md.Code(strconv.FormatInt(summary.InsufficientEvidencePostCount, 10)),
	}, ", ")
}

func buildMarkdownRows(rows []Row) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			md.Code(shared.FallbackSendCountValue(row.ChannelID)),
			md.Code(shared.FallbackSendCountValue(row.PostID)),
			md.Code(shared.FormatSendCountTimePtr(row.ObservedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.DetectedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.AlarmSentAt)),
			shared.FormatSendCountInt64Ptr(row.AlarmLatencyMillis),
			md.Code(string(row.InternalCauseJudgment)),
			md.Code(shared.FallbackSendCountValue(row.InternalCauseBasis)),
			md.Code(renderEvidenceFields(row.CauseEvidence)),
			md.Code(string(row.DelaySource)),
			md.Code(string(row.InternalDelayCause)),
			shared.FormatSendCountInt64Ptr(row.PublishToDetectMillis),
			shared.FormatSendCountInt64Ptr(row.InternalLatencyMillis),
			shared.FormatSendCountInt64Ptr(row.QueueWaitMillis),
			shared.FormatSendCountInt64Ptr(row.RetryAccumulationMillis),
			md.Code(shared.FormatSendCountBool(row.JobFailureDetected)),
			md.Code(string(row.LatencyClassification.Status)),
			md.Code(shared.RenderLatencyClassificationEvidence(&row.LatencyClassification)),
		})
	}
	return markdownRows
}

func buildCauseSummary(rows []Row) Summary {
	summary := Summary{}
	for i := range rows {
		accumulateCauseSummaryRow(&summary, &rows[i])
	}
	return summary
}

func accumulateCauseSummaryRow(summary *Summary, row *Row) {
	if summary == nil || row == nil {
		return
	}
	summary.ExceededPostCount++
	accumulateJudgment(summary, row)
	accumulateAlarmType(summary, row.AlarmType)
	accumulateDelaySource(summary, row.DelaySource)
	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		summary.InsufficientEvidencePostCount++
	}
}

func accumulateJudgment(summary *Summary, row *Row) {
	if row == nil {
		return
	}
	if row.InternalCauseJudgment != InternalCauseJudgmentInternalSystem {
		summary.NonInternalSystemCausePostCount++
		return
	}
	summary.InternalSystemCausePostCount++
	summary.InternalCauseCandidatePostCount++
	incrementInternalCause(summary, row.InternalDelayCause)
}

func accumulateAlarmType(summary *Summary, alarmType domain.AlarmType) {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		summary.CommunityExceededPostCount++
	case domain.AlarmTypeShorts:
		summary.ShortsExceededPostCount++
	case domain.AlarmTypeLive, domain.AlarmTypeBirthday, domain.AlarmTypeAnniversary:
		return
	}
}

func accumulateDelaySource(summary *Summary, delaySource outbox.PostDelaySource) {
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

func buildVerification() Verification {
	return Verification{
		InternalCauseRule:        internalCauseRule,
		NonInternalCauseRule:     nonInternalCauseRule,
		ExcludedExternalRule:     excludedExternalRule,
		InsufficientEvidenceRule: insufficientEvidence,
		EvidenceFieldCatalog:     append([]string(nil), evidenceFieldCatalog...),
	}
}

func renderEvidenceFields(evidence Evidence) string {
	if len(evidence.Fields) == 0 {
		return shared.NoneValue
	}
	return strings.Join(evidence.Fields, ", ")
}

func incrementInternalCause(summary *Summary, cause outbox.PostInternalDelayCause) {
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
