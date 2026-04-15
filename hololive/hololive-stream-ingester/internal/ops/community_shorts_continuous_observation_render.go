package ops

import (
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"

	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

func RenderCommunityShortsContinuousObservationMarkdown(report CommunityShortsContinuousObservationReport) string {
	var builder strings.Builder
	closeout, missingAlarmCloseout, stateConsistencyCloseout := resolveCommunityShortsContinuousObservationCloseouts(report)

	writeCommunityShortsMarkdownHeading(&builder, 1, "YouTube Community/Shorts Continuous Observation Report")
	writeCommunityShortsContinuousObservationMetadata(&builder, report)
	writeCommunityShortsContinuousObservationCloseoutSection(&builder, closeout, missingAlarmCloseout, stateConsistencyCloseout)
	writeCommunityShortsContinuousObservationEmbeddedSections(&builder, report)

	return builder.String()
}

func resolveCommunityShortsContinuousObservationCloseouts(
	report CommunityShortsContinuousObservationReport,
) (
	CommunityShortsContinuousObservation24HCloseout,
	CommunityShortsContinuousObservationMissingAlarmCloseout,
	CommunityShortsContinuousObservationStateConsistencyCloseout,
) {
	closeout := report.Closeout24H
	if closeout.Status == "" {
		closeout = buildCommunityShortsContinuousObservation24HCloseout(
			report.Observation,
			report.TargetBaseline,
			report.SendCounts,
			report.LatencyCause,
		)
	}

	missingAlarmCloseout := report.MissingAlarmCloseout24H
	if missingAlarmCloseout.Status == "" {
		missingAlarmCloseout = buildCommunityShortsContinuousObservationMissingAlarmCloseout(
			report.Observation,
			report.TargetBaseline,
			report.AlarmSentHistoryDataset,
			nil,
		)
	}

	stateConsistencyCloseout := report.StateConsistencyCloseout24H
	if stateConsistencyCloseout.Status == "" {
		stateConsistencyCloseout = buildCommunityShortsContinuousObservationStateConsistencyCloseout(
			report.Observation,
			report.TargetBaseline,
			report.AlarmSentHistoryDataset,
			nil,
		)
	}

	return closeout, missingAlarmCloseout, stateConsistencyCloseout
}

func writeCommunityShortsContinuousObservationMetadata(
	builder *strings.Builder,
	report CommunityShortsContinuousObservationReport,
) {
	writeCommunityShortsMarkdownKV(builder, "generated at", formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	writeCommunityShortsMarkdownKV(
		builder,
		"observation runtime",
		formatCommunityShortsMarkdownCode(strings.TrimSpace(report.Observation.RuntimeName))+
			", cutover: "+
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.Observation.BigBangCutoverAt)),
	)
	writeCommunityShortsMarkdownKV(builder, "observation status", formatCommunityShortsMarkdownCode(string(report.Observation.Status)))
	writeCommunityShortsMarkdownKV(
		builder,
		"observation window",
		formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.Observation.ObservationStartedAt))+
			" -> "+
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.Observation.ObservationEndsAt)),
	)
	writeCommunityShortsMarkdownKV(
		builder,
		"deployment completed at",
		formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.Observation.DeploymentCompletedAt))+
			", observed until: "+
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.Observation.ObservedUntil)),
	)
	writeCommunityShortsMarkdownKV(
		builder,
		"target channels",
		formatCommunityShortsMarkdownCode(strconv.Itoa(report.Observation.TargetChannelCount))+
			", app version: "+
			formatCommunityShortsMarkdownCode(strings.TrimSpace(report.Observation.AppVersion)),
	)
}

func writeCommunityShortsContinuousObservationCloseoutSection(
	builder *strings.Builder,
	closeout CommunityShortsContinuousObservation24HCloseout,
	missingAlarmCloseout CommunityShortsContinuousObservationMissingAlarmCloseout,
	stateConsistencyCloseout CommunityShortsContinuousObservationStateConsistencyCloseout,
) {
	writeCommunityShortsMarkdownHeading(builder, 2, "24h Closeout")
	writeCommunityShortsMarkdownKV(builder, "scope", buildCommunityShortsContinuousObservationCloseoutScopeMarkdown(closeout))
	writeCommunityShortsMarkdownKV(builder, "internal over-2m closeout", buildCommunityShortsContinuousObservation24HCloseoutMarkdown(closeout))
	writeCommunityShortsMarkdownKV(builder, "closeout statement", closeout.Statement)
	writeCommunityShortsMarkdownKV(builder, "missing alarm closeout", buildCommunityShortsContinuousObservationMissingAlarmCloseoutMarkdown(missingAlarmCloseout))
	writeCommunityShortsMarkdownKV(builder, "missing alarm statement", missingAlarmCloseout.Statement)
	writeCommunityShortsMarkdownKV(builder, "state consistency closeout", buildCommunityShortsContinuousObservationStateConsistencyCloseoutMarkdown(stateConsistencyCloseout))
	writeCommunityShortsMarkdownKV(builder, "state consistency statement", stateConsistencyCloseout.Statement)
}

func writeCommunityShortsContinuousObservationEmbeddedSections(
	builder *strings.Builder,
	report CommunityShortsContinuousObservationReport,
) {
	writeCommunityShortsMarkdownHeading(builder, 2, "Target Baseline")
	builder.WriteString(renderCommunityShortsContinuousObservationTargetBaseline(report.TargetBaseline))
	appendCommunityShortsContinuousObservationSection(builder, RenderCommunityShortsChannelSummaryMarkdown(report.ChannelSummary))
	appendCommunityShortsContinuousObservationSection(builder, RenderCommunityShortsSendCountMarkdown(report.SendCounts))
	if report.AlarmSentHistoryDataset != nil {
		appendCommunityShortsContinuousObservationSection(builder, RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(*report.AlarmSentHistoryDataset))
	}
	appendCommunityShortsContinuousObservationSection(builder, RenderCommunityShortsDeliveryLogMarkdown(report.DeliveryLogs))
	appendCommunityShortsContinuousObservationSection(builder, RenderCommunityShortsLatencyPeriodMarkdown(report.LatencyPeriods))
	appendCommunityShortsContinuousObservationSection(builder, RenderCommunityShortsLatencyCauseMarkdown(report.LatencyCause))
}

func appendCommunityShortsContinuousObservationSection(builder *strings.Builder, markdown string) {
	if builder == nil || strings.TrimSpace(markdown) == "" {
		return
	}
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(markdown, 1))
	builder.WriteString("\n")
}

func buildCommunityShortsContinuousObservationCloseoutScopeMarkdown(
	closeout CommunityShortsContinuousObservation24HCloseout,
) string {
	return formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(closeout.AggregationScope)) +
		", target_channels=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.TargetChannelCount)) +
		", observed_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.ObservedPostCount)) +
		", period_label=" + formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(closeout.ObservationPeriodLabel))
}

func buildCommunityShortsContinuousObservation24HCloseoutMarkdown(
	closeout CommunityShortsContinuousObservation24HCloseout,
) string {
	return "status=" + formatCommunityShortsMarkdownCode(string(closeout.Status)) +
		", internal_system_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(closeout.InternalExceededPostCount, 10)) +
		", over_2m_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(closeout.TotalExceededPostCount, 10)) +
		", non_internal_system_cause_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(closeout.NonInternalExceededPostCount, 10)) +
		", excluded_external_collection_posts=" + formatCommunityShortsMarkdownCode(strconv.FormatInt(closeout.ExcludedExternalExceededPostCount, 10)) +
		", rule=" + formatCommunityShortsMarkdownCode(closeout.Rule)
}

func buildCommunityShortsContinuousObservationMissingAlarmCloseoutMarkdown(
	closeout CommunityShortsContinuousObservationMissingAlarmCloseout,
) string {
	return "status=" + formatCommunityShortsMarkdownCode(string(closeout.Status)) +
		", reference_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.ReferencePostCount)) +
		", send_state_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.SendStatePostCount)) +
		", missing_alarm_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.MissingAlarmPostCount)) +
		", missing_send_state_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.MissingSendStatePostCount)) +
		", attempted_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.AttemptedMissingPostCount)) +
		", not_sent_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.NotSentMissingPostCount)) +
		", rule=" + formatCommunityShortsMarkdownCode(closeout.Rule)
}

func buildCommunityShortsContinuousObservationStateConsistencyCloseoutMarkdown(
	closeout CommunityShortsContinuousObservationStateConsistencyCloseout,
) string {
	return "status=" + formatCommunityShortsMarkdownCode(string(closeout.Status)) +
		", reference_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.ReferencePostCount)) +
		", send_state_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.SendStatePostCount)) +
		", duplicate_sent_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.DuplicateSentPostCount)) +
		", missing_alarm_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.MissingAlarmPostCount)) +
		", missing_send_state_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.MissingSendStatePostCount)) +
		", attempted_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.AttemptedMissingPostCount)) +
		", not_sent_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(closeout.NotSentMissingPostCount)) +
		", rule=" + formatCommunityShortsMarkdownCode(closeout.Rule)
}

func renderCommunityShortsContinuousObservationTargetBaseline(
	baseline communityshorts.TargetBaseline,
) string {
	var builder strings.Builder

	writeCommunityShortsMarkdownKV(&builder, "generated at", formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(baseline.GeneratedAt)))
	writeCommunityShortsMarkdownKV(
		&builder,
		"final delivery owner",
		formatCommunityShortsMarkdownCode(strings.TrimSpace(baseline.Runtime.FinalDeliveryOwner))+
			", big-bang enabled: "+
			formatCommunityShortsMarkdownCode(formatCommunityShortsContinuousObservationBool(baseline.Runtime.CommunityShortsBigBangEnabled)),
	)
	writeCommunityShortsMarkdownKV(
		&builder,
		"runtime target channels",
		formatCommunityShortsMarkdownCode(strconv.Itoa(baseline.Runtime.TargetChannelCount))+
			", channel rows: "+
			formatCommunityShortsMarkdownCode(strconv.Itoa(len(baseline.Channels))),
	)

	if len(baseline.Channels) == 0 {
		builder.WriteString("\n활성 운영 채널 baseline이 없습니다.\n")
		return builder.String()
	}

	writeCommunityShortsMarkdownTable(
		&builder,
		communityShortsContinuousObservationTargetBaselineColumns,
		buildCommunityShortsContinuousObservationTargetBaselineRows(baseline.Channels),
	)

	return builder.String()
}

var communityShortsContinuousObservationTargetBaselineColumns = []communityShortsMarkdownColumn{
	{Header: "channel_id"},
	{Header: "owner"},
	{Header: "community_enabled"},
	{Header: "community_rooms", AlignRight: true},
	{Header: "community_mode"},
	{Header: "shorts_enabled"},
	{Header: "shorts_rooms", AlignRight: true},
	{Header: "shorts_mode"},
}

func buildCommunityShortsContinuousObservationTargetBaselineRows(
	channels []communityshorts.TargetBaselineChannel,
) [][]string {
	rows := make([][]string, 0, len(channels))
	for i := range channels {
		communityRoute, _ := communityshorts.RouteForType(channels[i].Routes, domain.AlarmTypeCommunity)
		shortsRoute, _ := communityshorts.RouteForType(channels[i].Routes, domain.AlarmTypeShorts)
		rows = append(rows, []string{
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(channels[i].ChannelID)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(channels[i].OwnerLabel)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsContinuousObservationBool(communityRoute.AlarmEnabled)),
			strconv.Itoa(communityRoute.SubscriberRoomCount),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(communityRoute.EffectiveDeliveryMode)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsContinuousObservationBool(shortsRoute.AlarmEnabled)),
			strconv.Itoa(shortsRoute.SubscriberRoomCount),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(shortsRoute.EffectiveDeliveryMode)),
		})
	}
	return rows
}

func formatCommunityShortsContinuousObservationBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
