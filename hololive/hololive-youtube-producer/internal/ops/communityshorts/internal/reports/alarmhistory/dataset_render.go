package alarmhistory

import (
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

func RenderDatasetMarkdown(report DatasetReport) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Alarm Sent History Dataset")
	writeDatasetMetadata(&builder, report)
	writeDatasetResults(&builder, report.Results)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Missing Alarm Rows",
		missingAlarmColumns,
		buildMissingAlarmMarkdownRows(report.MissingAlarmRows),
		"누락 알람 게시물이 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Verification Rows",
		verificationColumns,
		buildVerificationMarkdownRows(report.VerificationRows),
		"검증 verdict row가 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Normalized Verification Reference Rows",
		referenceColumns,
		buildReferenceMarkdownRows(report.ReferenceRows),
		"정규화된 검증 기준 row가 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Normalized Sent History Rows",
		rowsColumns,
		buildDatasetMarkdownRows(report.Rows),
		"정규화된 community/shorts sent history row가 없습니다.",
	)
	return builder.String()
}
