package reports

import (
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"

	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

func RenderCommunityShortsContinuousObservationMarkdown(report CommunityShortsContinuousObservationReport) string {
	var builder strings.Builder
	closeout, missingAlarmCloseout, stateConsistencyCloseout := resolveCommunityShortsContinuousObservationCloseouts(report)

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Continuous Observation Report")
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
	md.WriteKV(builder, "generated at", md.Code(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(strings.TrimSpace(report.Observation.RuntimeName))+
			", cutover: "+
			md.Code(formatCommunityShortsSendCountTime(report.Observation.BigBangCutoverAt)),
	)
	md.WriteKV(builder, "observation status", md.Code(string(report.Observation.Status)))
	md.WriteKV(
		builder,
		"observation window",
		md.Code(formatCommunityShortsSendCountTime(report.Observation.ObservationStartedAt))+
			" -> "+
			md.Code(formatCommunityShortsSendCountTime(report.Observation.ObservationEndsAt)),
	)
	md.WriteKV(
		builder,
		"deployment completed at",
		md.Code(formatCommunityShortsSendCountTime(report.Observation.DeploymentCompletedAt))+
			", observed until: "+
			md.Code(formatCommunityShortsSendCountTime(report.Observation.ObservedUntil)),
	)
	md.WriteKV(
		builder,
		"target channels",
		md.Code(strconv.Itoa(report.Observation.TargetChannelCount))+
			", app version: "+
			md.Code(strings.TrimSpace(report.Observation.AppVersion)),
	)
}

func writeCommunityShortsContinuousObservationCloseoutSection(
	builder *strings.Builder,
	closeout CommunityShortsContinuousObservation24HCloseout,
	missingAlarmCloseout CommunityShortsContinuousObservationMissingAlarmCloseout,
	stateConsistencyCloseout CommunityShortsContinuousObservationStateConsistencyCloseout,
) {
	md.WriteHeading(builder, 2, "24h Closeout")
	md.WriteKV(builder, "scope", buildCommunityShortsContinuousObservationCloseoutScopeMarkdown(closeout))
	md.WriteKV(builder, "internal over-2m closeout", buildCommunityShortsContinuousObservation24HCloseoutMarkdown(closeout))
	md.WriteKV(builder, "closeout statement", closeout.Statement)
	md.WriteKV(builder, "missing alarm closeout", buildCommunityShortsContinuousObservationMissingAlarmCloseoutMarkdown(missingAlarmCloseout))
	md.WriteKV(builder, "missing alarm statement", missingAlarmCloseout.Statement)
	md.WriteKV(builder, "state consistency closeout", buildCommunityShortsContinuousObservationStateConsistencyCloseoutMarkdown(stateConsistencyCloseout))
	md.WriteKV(builder, "state consistency statement", stateConsistencyCloseout.Statement)
}

func writeCommunityShortsContinuousObservationEmbeddedSections(
	builder *strings.Builder,
	report CommunityShortsContinuousObservationReport,
) {
	md.WriteHeading(builder, 2, "Target Baseline")
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
	builder.WriteString(md.PromoteHeadings(markdown, 1))
	builder.WriteString("\n")
}

func buildCommunityShortsContinuousObservationCloseoutScopeMarkdown(
	closeout CommunityShortsContinuousObservation24HCloseout,
) string {
	return md.Code(fallbackCommunityShortsSendCountValue(closeout.AggregationScope)) +
		", target_channels=" + md.Code(strconv.Itoa(closeout.TargetChannelCount)) +
		", observed_posts=" + md.Code(strconv.Itoa(closeout.ObservedPostCount)) +
		", period_label=" + md.Code(fallbackCommunityShortsSendCountValue(closeout.ObservationPeriodLabel))
}

func buildCommunityShortsContinuousObservation24HCloseoutMarkdown(
	closeout CommunityShortsContinuousObservation24HCloseout,
) string {
	return "status=" + md.Code(string(closeout.Status)) +
		", internal_system_cause_posts=" + md.Code(strconv.FormatInt(closeout.InternalExceededPostCount, 10)) +
		", over_2m_posts=" + md.Code(strconv.FormatInt(closeout.TotalExceededPostCount, 10)) +
		", non_internal_system_cause_posts=" + md.Code(strconv.FormatInt(closeout.NonInternalExceededPostCount, 10)) +
		", excluded_external_collection_posts=" + md.Code(strconv.FormatInt(closeout.ExcludedExternalExceededPostCount, 10)) +
		", rule=" + md.Code(closeout.Rule)
}

func buildCommunityShortsContinuousObservationMissingAlarmCloseoutMarkdown(
	closeout CommunityShortsContinuousObservationMissingAlarmCloseout,
) string {
	return "status=" + md.Code(string(closeout.Status)) +
		", reference_posts=" + md.Code(strconv.Itoa(closeout.ReferencePostCount)) +
		", send_state_posts=" + md.Code(strconv.Itoa(closeout.SendStatePostCount)) +
		", missing_alarm_posts=" + md.Code(strconv.Itoa(closeout.MissingAlarmPostCount)) +
		", missing_send_state_posts=" + md.Code(strconv.Itoa(closeout.MissingSendStatePostCount)) +
		", attempted_missing_posts=" + md.Code(strconv.Itoa(closeout.AttemptedMissingPostCount)) +
		", not_sent_missing_posts=" + md.Code(strconv.Itoa(closeout.NotSentMissingPostCount)) +
		", rule=" + md.Code(closeout.Rule)
}

func buildCommunityShortsContinuousObservationStateConsistencyCloseoutMarkdown(
	closeout CommunityShortsContinuousObservationStateConsistencyCloseout,
) string {
	return "status=" + md.Code(string(closeout.Status)) +
		", reference_posts=" + md.Code(strconv.Itoa(closeout.ReferencePostCount)) +
		", send_state_posts=" + md.Code(strconv.Itoa(closeout.SendStatePostCount)) +
		", duplicate_sent_posts=" + md.Code(strconv.Itoa(closeout.DuplicateSentPostCount)) +
		", missing_alarm_posts=" + md.Code(strconv.Itoa(closeout.MissingAlarmPostCount)) +
		", missing_send_state_posts=" + md.Code(strconv.Itoa(closeout.MissingSendStatePostCount)) +
		", attempted_missing_posts=" + md.Code(strconv.Itoa(closeout.AttemptedMissingPostCount)) +
		", not_sent_missing_posts=" + md.Code(strconv.Itoa(closeout.NotSentMissingPostCount)) +
		", rule=" + md.Code(closeout.Rule)
}

func renderCommunityShortsContinuousObservationTargetBaseline(
	baseline communityshorts.TargetBaseline,
) string {
	var builder strings.Builder

	md.WriteKV(&builder, "generated at", md.Code(formatCommunityShortsSendCountTime(baseline.GeneratedAt)))
	md.WriteKV(
		&builder,
		"final delivery owner",
		md.Code(strings.TrimSpace(baseline.Runtime.FinalDeliveryOwner))+
			", big-bang enabled: "+
			md.Code(formatCommunityShortsContinuousObservationBool(baseline.Runtime.CommunityShortsBigBangEnabled)),
	)
	md.WriteKV(
		&builder,
		"runtime target channels",
		md.Code(strconv.Itoa(baseline.Runtime.TargetChannelCount))+
			", channel rows: "+
			md.Code(strconv.Itoa(len(baseline.Channels))),
	)

	if len(baseline.Channels) == 0 {
		builder.WriteString("\n활성 운영 채널 baseline이 없습니다.\n")
		return builder.String()
	}

	md.WriteTable(
		&builder,
		communityShortsContinuousObservationTargetBaselineColumns,
		buildCommunityShortsContinuousObservationTargetBaselineRows(baseline.Channels),
	)

	return builder.String()
}

var communityShortsContinuousObservationTargetBaselineColumns = []md.Column{
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
			md.Code(fallbackCommunityShortsSendCountValue(channels[i].ChannelID)),
			md.Code(fallbackCommunityShortsSendCountValue(channels[i].OwnerLabel)),
			md.Code(formatCommunityShortsContinuousObservationBool(communityRoute.AlarmEnabled)),
			strconv.Itoa(communityRoute.SubscriberRoomCount),
			md.Code(fallbackCommunityShortsSendCountValue(communityRoute.EffectiveDeliveryMode)),
			md.Code(formatCommunityShortsContinuousObservationBool(shortsRoute.AlarmEnabled)),
			strconv.Itoa(shortsRoute.SubscriberRoomCount),
			md.Code(fallbackCommunityShortsSendCountValue(shortsRoute.EffectiveDeliveryMode)),
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
