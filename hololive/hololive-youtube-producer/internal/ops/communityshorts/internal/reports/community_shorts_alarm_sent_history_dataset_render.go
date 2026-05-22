package reports

import (
	"fmt"
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

func RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report CommunityShortsAlarmSentHistoryDatasetReport) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Alarm Sent History Dataset")
	writeCommunityShortsAlarmSentHistoryDatasetMetadata(&builder, report)
	writeCommunityShortsAlarmSentHistoryDatasetResults(&builder, report.Results)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Missing Alarm Rows",
		communityShortsAlarmSentHistoryDatasetMissingAlarmColumns,
		buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmMarkdownRows(report.MissingAlarmRows),
		"누락 알람 게시물이 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Verification Rows",
		communityShortsAlarmSentHistoryDatasetVerificationColumns,
		buildCommunityShortsAlarmSentHistoryDatasetVerificationMarkdownRows(report.VerificationRows),
		"검증 verdict row가 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Normalized Verification Reference Rows",
		communityShortsAlarmSentHistoryDatasetReferenceColumns,
		buildCommunityShortsAlarmSentHistoryDatasetReferenceMarkdownRows(report.ReferenceRows),
		"정규화된 검증 기준 row가 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Normalized Sent History Rows",
		communityShortsAlarmSentHistoryDatasetRowsColumns,
		buildCommunityShortsAlarmSentHistoryDatasetMarkdownRows(report.Rows),
		"정규화된 community/shorts sent history row가 없습니다.",
	)
	return builder.String()
}

func normalizeCommunityShortsAlarmSentHistoryDatasetCollectOptions(
	options CommunityShortsAlarmSentHistoryDatasetCollectOptions,
) (CommunityShortsAlarmSentHistoryDatasetQuery, error) {
	runtimeName := strings.TrimSpace(options.ObservationRuntimeName)
	cutoverAt := cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt)
	if runtimeName == "" || cutoverAt == nil || cutoverAt.IsZero() {
		return CommunityShortsAlarmSentHistoryDatasetQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}

	return CommunityShortsAlarmSentHistoryDatasetQuery{
		ObservationRuntimeName:      runtimeName,
		ObservationBigBangCutoverAt: cutoverAt,
	}, nil
}

func normalizeCommunityShortsAlarmSentHistoryDatasetQuery(
	query CommunityShortsAlarmSentHistoryDatasetQuery,
) CommunityShortsAlarmSentHistoryDatasetQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	return query
}
