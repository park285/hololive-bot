package reports

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

const communityShortsLatencyCauseNone = "(none)"

func RenderCommunityShortsLatencyCauseMarkdown(report CommunityShortsLatencyCauseReport) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Latency Cause Report")
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
	md.WriteKV(builder, "generated at", md.Code(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	md.WriteKV(builder, "mode", md.Code(string(report.Query.Mode)))
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		md.WriteKV(
			builder,
			"window",
			md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))+
				" -> "+
				md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd)),
		)
	}
	if report.Query.Mode == communityShortsLatencyCauseQueryModeObservation {
		md.WriteKV(
			builder,
			"observation runtime",
			md.Code(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))+
				", cutover: "+
				md.Code(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
		)
	}
	md.WriteKV(builder, "observed at basis", md.Code(fallbackCommunityShortsSendCountValue(report.ObservedAtBasis)))
	md.WriteKV(builder, "threshold millis", md.Code(strconv.FormatInt(report.ThresholdMillis, 10)))
	md.WriteKV(builder, "internal cause rule", md.Code(report.Verification.InternalCauseRule))
	md.WriteKV(builder, "non internal rule", md.Code(report.Verification.NonInternalCauseRule))
	md.WriteKV(builder, "excluded external rule", md.Code(report.Verification.ExcludedExternalRule))
	md.WriteKV(builder, "insufficient evidence rule", md.Code(report.Verification.InsufficientEvidenceRule))
	md.WriteKV(builder, "cause evidence fields", md.Code(strings.Join(report.Verification.EvidenceFieldCatalog, ", ")))
	md.WriteKV(builder, "periods", md.Code(strconv.Itoa(len(report.Periods))))
}

func writeCommunityShortsLatencyCausePeriod(
	builder *strings.Builder,
	period CommunityShortsLatencyCausePeriodView,
	verification CommunityShortsLatencyCauseVerification,
) {
	builder.WriteString("\n")
	md.WriteHeading(builder, 2, md.Code(strings.TrimSpace(period.Summary.Label)))
	md.WriteKV(
		builder,
		"window",
		md.Code(formatCommunityShortsSendCountTime(period.Summary.StartAt))+
			" -> "+
			md.Code(formatCommunityShortsSendCountTime(period.Summary.EndAt)),
	)
	md.WriteKV(builder, "latency summary", buildCommunityShortsLatencyPeriodSummaryMarkdown(period.Summary))
	md.WriteKV(builder, "cause summary", buildCommunityShortsLatencyCauseSummaryMarkdown(period.CauseSummary))
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

	md.WriteTable(builder, communityShortsLatencyCauseMarkdownColumns, buildCommunityShortsLatencyCauseMarkdownRows(period.Rows))
}

var communityShortsLatencyCauseMarkdownColumns = []md.Column{
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
		"total_posts=" + md.Code(strconv.FormatInt(summary.TotalPostCount, 10)),
		"alarm_sent_posts=" + md.Code(strconv.FormatInt(summary.AlarmSentPostCount, 10)),
		"pending_posts=" + md.Code(strconv.FormatInt(summary.PendingPostCount, 10)),
		"measured_posts=" + md.Code(strconv.FormatInt(summary.LatencyMeasuredPostCount, 10)),
		"avg_latency_ms=" + md.Code(formatCommunityShortsSendCountInt64Ptr(summary.AverageLatencyMillis)),
		"p95_latency_ms=" + md.Code(formatCommunityShortsSendCountInt64Ptr(summary.P95LatencyMillis)),
		"max_latency_ms=" + md.Code(formatCommunityShortsSendCountInt64Ptr(summary.MaxLatencyMillis)),
		"over_2m_posts=" + md.Code(strconv.FormatInt(summary.ExceededPostCount, 10)),
	}, ", ")
}

func buildCommunityShortsLatencyCauseSummaryMarkdown(summary CommunityShortsLatencyCauseSummary) string {
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

func buildCommunityShortsLatencyCauseMarkdownRows(rows []CommunityShortsLatencyCauseRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			md.Code(fallbackCommunityShortsSendCountValue(row.ChannelID)),
			md.Code(fallbackCommunityShortsSendCountValue(row.PostID)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ObservedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt)),
			formatCommunityShortsSendCountInt64Ptr(row.AlarmLatencyMillis),
			md.Code(string(row.InternalCauseJudgment)),
			md.Code(fallbackCommunityShortsSendCountValue(row.InternalCauseBasis)),
			md.Code(renderCommunityShortsLatencyCauseEvidenceFields(row.CauseEvidence)),
			md.Code(string(row.DelaySource)),
			md.Code(string(row.InternalDelayCause)),
			formatCommunityShortsSendCountInt64Ptr(row.PublishToDetectMillis),
			formatCommunityShortsSendCountInt64Ptr(row.InternalLatencyMillis),
			formatCommunityShortsSendCountInt64Ptr(row.QueueWaitMillis),
			formatCommunityShortsSendCountInt64Ptr(row.RetryAccumulationMillis),
			md.Code(formatCommunityShortsSendCountBool(row.JobFailureDetected)),
			md.Code(string(row.LatencyClassification.Status)),
			md.Code(renderCommunityShortsLatencyClassificationEvidence(row.LatencyClassification)),
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
