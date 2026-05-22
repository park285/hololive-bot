package reports

import (
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

func writeCommunityShortsAlarmSentHistoryDatasetMetadata(
	builder *strings.Builder,
	report CommunityShortsAlarmSentHistoryDatasetReport,
) {
	md.WriteKV(builder, "generated at", md.Code(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))+
			", cutover: "+
			md.Code(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
	)
	md.WriteKV(
		builder,
		"window",
		md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))+
			" -> "+
			md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd)),
	)
	md.WriteKV(builder, "summary", buildCommunityShortsAlarmSentHistoryDatasetSummaryMarkdown(report.Summary))
}

func writeCommunityShortsAlarmSentHistoryDatasetResults(
	builder *strings.Builder,
	results CommunityShortsAlarmSentHistoryDatasetResults,
) {
	md.WriteHeading(builder, 2, "Results")
	md.WriteKV(builder, "missing alarm aggregation", buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmAggregation(results))
	if results.MissingAlarmEvaluated {
		md.WriteKV(builder, "omission closeout", buildCommunityShortsAlarmSentHistoryDatasetOmissionCloseout(results))
	}
	writeCommunityShortsAlarmSentHistoryDatasetComparisonSection(
		builder,
		"By Alarm Type",
		communityShortsAlarmSentHistoryDatasetAlarmTypeColumns,
		buildCommunityShortsAlarmSentHistoryDatasetAlarmTypeMarkdownRows(results.AlarmTypeComparisons),
		"게시물 유형별 대조 결과가 없습니다.",
	)
	writeCommunityShortsAlarmSentHistoryDatasetComparisonSection(
		builder,
		"By Channel",
		communityShortsAlarmSentHistoryDatasetChannelColumns,
		buildCommunityShortsAlarmSentHistoryDatasetChannelMarkdownRows(results.ChannelComparisons),
		"채널별 대조 결과가 없습니다.",
	)
}

func writeCommunityShortsAlarmSentHistoryDatasetComparisonSection(
	builder *strings.Builder,
	title string,
	columns []md.Column,
	rows [][]string,
	emptyMessage string,
) {
	if len(rows) == 0 {
		builder.WriteString("\n")
		md.WriteMessage(builder, emptyMessage)
		return
	}
	md.WriteHeading(builder, 3, title)
	md.WriteTable(builder, columns, rows)
}
