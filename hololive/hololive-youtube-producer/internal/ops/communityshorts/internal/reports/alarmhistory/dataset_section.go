package alarmhistory

import (
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func writeDatasetMetadata(builder *strings.Builder, report DatasetReport) {
	md.WriteKV(builder, "generated at", md.Code(shared.FormatSendCountTime(report.GeneratedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(shared.FallbackSendCountValue(report.Query.ObservationRuntimeName))+
			", cutover: "+
			md.Code(shared.FormatSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
	)
	md.WriteKV(
		builder,
		"window",
		md.Code(shared.FormatSendCountTimePtr(report.Query.WindowStart))+
			" -> "+
			md.Code(shared.FormatSendCountTimePtr(report.Query.WindowEnd)),
	)
	md.WriteKV(builder, "summary", buildDatasetSummaryMarkdown(report.Summary))
}

func writeDatasetResults(builder *strings.Builder, results DatasetResults) {
	md.WriteHeading(builder, 2, "Results")
	md.WriteKV(builder, "missing alarm aggregation", buildMissingAlarmAggregation(results))
	if results.MissingAlarmEvaluated {
		md.WriteKV(builder, "omission closeout", buildOmissionCloseout(results))
	}
	writeComparisonSection(
		builder,
		"By Alarm Type",
		alarmTypeColumns,
		buildAlarmTypeMarkdownRows(results.AlarmTypeComparisons),
		"게시물 유형별 대조 결과가 없습니다.",
	)
	writeComparisonSection(
		builder,
		"By Channel",
		channelColumns,
		buildChannelMarkdownRows(results.ChannelComparisons),
		"채널별 대조 결과가 없습니다.",
	)
}

func writeComparisonSection(
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
