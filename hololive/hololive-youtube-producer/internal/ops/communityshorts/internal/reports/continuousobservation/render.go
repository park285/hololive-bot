package continuousobservation

import (
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"

	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/alarmhistory"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/channelsummary"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/deliverylogs"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/latencycause"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendcounts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func RenderMarkdown(report Report) string {
	var builder strings.Builder
	closeout, missingAlarmCloseout, stateConsistencyCloseout := resolveCloseouts(report)

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Continuous Observation Report")
	writeMetadata(&builder, report)
	writeCloseoutSection(&builder, closeout, missingAlarmCloseout, stateConsistencyCloseout)
	writeEmbeddedSections(&builder, report)

	return builder.String()
}

func resolveCloseouts(
	report Report,
) (Closeout24H, MissingAlarmCloseout, StateConsistencyCloseout) {
	closeout := report.Closeout24H
	if closeout.Status == "" {
		closeout = buildCloseout24H(report.Observation, report.TargetBaseline, report.SendCounts, report.LatencyCause)
	}

	missingAlarmCloseout := report.MissingAlarmCloseout24H
	if missingAlarmCloseout.Status == "" {
		missingAlarmCloseout = buildMissingAlarmCloseout(report.Observation, report.TargetBaseline, report.AlarmSentHistoryDataset, nil)
	}

	stateConsistencyCloseout := report.StateConsistencyCloseout24H
	if stateConsistencyCloseout.Status == "" {
		stateConsistencyCloseout = buildStateConsistencyCloseout(report.Observation, report.TargetBaseline, report.AlarmSentHistoryDataset, nil)
	}

	return closeout, missingAlarmCloseout, stateConsistencyCloseout
}

func writeMetadata(
	builder *strings.Builder,
	report Report,
) {
	md.WriteKV(builder, "generated at", md.Code(shared.FormatSendCountTime(report.GeneratedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(strings.TrimSpace(report.Observation.RuntimeName))+
			", cutover: "+
			md.Code(shared.FormatSendCountTime(report.Observation.BigBangCutoverAt)),
	)
	md.WriteKV(builder, "observation status", md.Code(string(report.Observation.Status)))
	md.WriteKV(
		builder,
		"observation window",
		md.Code(shared.FormatSendCountTime(report.Observation.ObservationStartedAt))+
			" -> "+
			md.Code(shared.FormatSendCountTime(report.Observation.ObservationEndsAt)),
	)
	md.WriteKV(
		builder,
		"deployment completed at",
		md.Code(shared.FormatSendCountTime(report.Observation.DeploymentCompletedAt))+
			", observed until: "+
			md.Code(shared.FormatSendCountTime(report.Observation.ObservedUntil)),
	)
	md.WriteKV(
		builder,
		"target channels",
		md.Code(strconv.Itoa(report.Observation.TargetChannelCount))+
			", app version: "+
			md.Code(strings.TrimSpace(report.Observation.AppVersion)),
	)
}

func writeCloseoutSection(
	builder *strings.Builder,
	closeout Closeout24H,
	missingAlarmCloseout MissingAlarmCloseout,
	stateConsistencyCloseout StateConsistencyCloseout,
) {
	md.WriteHeading(builder, 2, "24h Closeout")
	md.WriteKV(builder, "scope", buildCloseoutScopeMarkdown(closeout))
	md.WriteKV(builder, "internal over-2m closeout", buildCloseout24HMarkdown(closeout))
	md.WriteKV(builder, "closeout statement", closeout.Statement)
	md.WriteKV(builder, "missing alarm closeout", buildMissingAlarmCloseoutMarkdown(missingAlarmCloseout))
	md.WriteKV(builder, "missing alarm statement", missingAlarmCloseout.Statement)
	md.WriteKV(builder, "state consistency closeout", buildStateConsistencyCloseoutMarkdown(stateConsistencyCloseout))
	md.WriteKV(builder, "state consistency statement", stateConsistencyCloseout.Statement)
}

func writeEmbeddedSections(
	builder *strings.Builder,
	report Report,
) {
	md.WriteHeading(builder, 2, "Target Baseline")
	builder.WriteString(renderTargetBaseline(report.TargetBaseline))
	appendSection(builder, channelsummary.RenderMarkdown(report.ChannelSummary))
	appendSection(builder, sendcounts.RenderMarkdown(report.SendCounts))
	if report.AlarmSentHistoryDataset != nil {
		appendSection(builder, alarmhistory.RenderDatasetMarkdown(*report.AlarmSentHistoryDataset))
	}
	appendSection(builder, deliverylogs.RenderMarkdown(report.DeliveryLogs))
	appendSection(builder, latencycause.RenderPeriodMarkdown(report.LatencyPeriods))
	appendSection(builder, latencycause.RenderMarkdown(report.LatencyCause))
}

func appendSection(builder *strings.Builder, markdown string) {
	if builder == nil || strings.TrimSpace(markdown) == "" {
		return
	}
	builder.WriteString("\n")
	builder.WriteString(md.PromoteHeadings(markdown, 1))
	builder.WriteString("\n")
}

func buildCloseoutScopeMarkdown(
	closeout Closeout24H,
) string {
	return md.Code(shared.FallbackSendCountValue(closeout.AggregationScope)) +
		", target_channels=" + md.Code(strconv.Itoa(closeout.TargetChannelCount)) +
		", observed_posts=" + md.Code(strconv.Itoa(closeout.ObservedPostCount)) +
		", period_label=" + md.Code(shared.FallbackSendCountValue(closeout.ObservationPeriodLabel))
}

func buildCloseout24HMarkdown(
	closeout Closeout24H,
) string {
	return "status=" + md.Code(string(closeout.Status)) +
		", internal_system_cause_posts=" + md.Code(strconv.FormatInt(closeout.InternalExceededPostCount, 10)) +
		", over_2m_posts=" + md.Code(strconv.FormatInt(closeout.TotalExceededPostCount, 10)) +
		", non_internal_system_cause_posts=" + md.Code(strconv.FormatInt(closeout.NonInternalExceededPostCount, 10)) +
		", excluded_external_collection_posts=" + md.Code(strconv.FormatInt(closeout.ExcludedExternalExceededPostCount, 10)) +
		", rule=" + md.Code(closeout.Rule)
}

func buildMissingAlarmCloseoutMarkdown(
	closeout MissingAlarmCloseout,
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

func buildStateConsistencyCloseoutMarkdown(
	closeout StateConsistencyCloseout,
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

func renderTargetBaseline(
	baseline communityshorts.TargetBaseline,
) string {
	var builder strings.Builder

	md.WriteKV(&builder, "generated at", md.Code(shared.FormatSendCountTime(baseline.GeneratedAt)))
	md.WriteKV(
		&builder,
		"final delivery owner",
		md.Code(strings.TrimSpace(baseline.Runtime.FinalDeliveryOwner))+
			", big-bang enabled: "+
			md.Code(formatBool(baseline.Runtime.CommunityShortsBigBangEnabled)),
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
		targetBaselineColumns,
		buildTargetBaselineRows(baseline.Channels),
	)

	return builder.String()
}

var targetBaselineColumns = []md.Column{
	{Header: "channel_id"},
	{Header: "owner"},
	{Header: "community_enabled"},
	{Header: "community_rooms", AlignRight: true},
	{Header: "community_mode"},
	{Header: "shorts_enabled"},
	{Header: "shorts_rooms", AlignRight: true},
	{Header: "shorts_mode"},
}

func buildTargetBaselineRows(
	channels []communityshorts.TargetBaselineChannel,
) [][]string {
	rows := make([][]string, 0, len(channels))
	for i := range channels {
		communityRoute, _ := communityshorts.RouteForType(channels[i].Routes, domain.AlarmTypeCommunity)
		shortsRoute, _ := communityshorts.RouteForType(channels[i].Routes, domain.AlarmTypeShorts)
		rows = append(rows, []string{
			md.Code(shared.FallbackSendCountValue(channels[i].ChannelID)),
			md.Code(shared.FallbackSendCountValue(channels[i].OwnerLabel)),
			md.Code(formatBool(communityRoute.AlarmEnabled)),
			strconv.Itoa(communityRoute.SubscriberRoomCount),
			md.Code(shared.FallbackSendCountValue(communityRoute.EffectiveDeliveryMode)),
			md.Code(formatBool(shortsRoute.AlarmEnabled)),
			strconv.Itoa(shortsRoute.SubscriberRoomCount),
			md.Code(shared.FallbackSendCountValue(shortsRoute.EffectiveDeliveryMode)),
		})
	}
	return rows
}

func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
