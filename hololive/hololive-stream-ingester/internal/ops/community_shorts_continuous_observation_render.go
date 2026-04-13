package ops

import (
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	runtimeapp "github.com/kapu/hololive-stream-ingester/internal/runtime"
)

func RenderCommunityShortsContinuousObservationMarkdown(report CommunityShortsContinuousObservationReport) string {
	var builder strings.Builder
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

	builder.WriteString("# YouTube Community/Shorts Continuous Observation Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation runtime: `")
	builder.WriteString(strings.TrimSpace(report.Observation.RuntimeName))
	builder.WriteString("`, cutover: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.BigBangCutoverAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation status: `")
	builder.WriteString(string(report.Observation.Status))
	builder.WriteString("`\n")
	builder.WriteString("- observation window: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.ObservationStartedAt))
	builder.WriteString("` -> `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.ObservationEndsAt))
	builder.WriteString("`\n")
	builder.WriteString("- deployment completed at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.DeploymentCompletedAt))
	builder.WriteString("`, observed until: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.ObservedUntil))
	builder.WriteString("`\n")
	builder.WriteString("- target channels: `")
	builder.WriteString(fmt.Sprintf("%d", report.Observation.TargetChannelCount))
	builder.WriteString("`, app version: `")
	builder.WriteString(strings.TrimSpace(report.Observation.AppVersion))
	builder.WriteString("`\n")
	builder.WriteString("\n## 24h Closeout\n\n")
	builder.WriteString("- scope: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(closeout.AggregationScope))
	builder.WriteString("`, target_channels=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.TargetChannelCount))
	builder.WriteString("`, observed_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.ObservedPostCount))
	builder.WriteString("`, period_label=`")
	builder.WriteString(fallbackCommunityShortsSendCountValue(closeout.ObservationPeriodLabel))
	builder.WriteString("`\n")
	builder.WriteString("- internal over-2m closeout: status=`")
	builder.WriteString(string(closeout.Status))
	builder.WriteString("`, internal_system_cause_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.InternalExceededPostCount))
	builder.WriteString("`, over_2m_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.TotalExceededPostCount))
	builder.WriteString("`, non_internal_system_cause_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.NonInternalExceededPostCount))
	builder.WriteString("`, excluded_external_collection_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.ExcludedExternalExceededPostCount))
	builder.WriteString("`, rule=`")
	builder.WriteString(closeout.Rule)
	builder.WriteString("`\n")
	builder.WriteString("- closeout statement: ")
	builder.WriteString(closeout.Statement)
	builder.WriteString("\n")
	builder.WriteString("- missing alarm closeout: status=`")
	builder.WriteString(string(missingAlarmCloseout.Status))
	builder.WriteString("`, reference_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.ReferencePostCount))
	builder.WriteString("`, send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.SendStatePostCount))
	builder.WriteString("`, missing_alarm_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.MissingAlarmPostCount))
	builder.WriteString("`, missing_send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.MissingSendStatePostCount))
	builder.WriteString("`, attempted_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.AttemptedMissingPostCount))
	builder.WriteString("`, not_sent_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.NotSentMissingPostCount))
	builder.WriteString("`, rule=`")
	builder.WriteString(missingAlarmCloseout.Rule)
	builder.WriteString("`\n")
	builder.WriteString("- missing alarm statement: ")
	builder.WriteString(missingAlarmCloseout.Statement)
	builder.WriteString("\n")
	builder.WriteString("- state consistency closeout: status=`")
	builder.WriteString(string(stateConsistencyCloseout.Status))
	builder.WriteString("`, reference_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.ReferencePostCount))
	builder.WriteString("`, send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.SendStatePostCount))
	builder.WriteString("`, duplicate_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.DuplicateSentPostCount))
	builder.WriteString("`, missing_alarm_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.MissingAlarmPostCount))
	builder.WriteString("`, missing_send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.MissingSendStatePostCount))
	builder.WriteString("`, attempted_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.AttemptedMissingPostCount))
	builder.WriteString("`, not_sent_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.NotSentMissingPostCount))
	builder.WriteString("`, rule=`")
	builder.WriteString(stateConsistencyCloseout.Rule)
	builder.WriteString("`\n")
	builder.WriteString("- state consistency statement: ")
	builder.WriteString(stateConsistencyCloseout.Statement)
	builder.WriteString("\n")

	builder.WriteString("\n## Target Baseline\n\n")
	builder.WriteString(renderCommunityShortsContinuousObservationTargetBaseline(report.TargetBaseline))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsChannelSummaryMarkdown(report.ChannelSummary), 1))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsSendCountMarkdown(report.SendCounts), 1))
	builder.WriteString("\n")
	if report.AlarmSentHistoryDataset != nil {
		builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(*report.AlarmSentHistoryDataset), 1))
		builder.WriteString("\n")
	}
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsDeliveryLogMarkdown(report.DeliveryLogs), 1))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsLatencyPeriodMarkdown(report.LatencyPeriods), 1))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsLatencyCauseMarkdown(report.LatencyCause), 1))
	builder.WriteString("\n")

	return builder.String()
}

func renderCommunityShortsContinuousObservationTargetBaseline(
	baseline runtimeapp.CommunityShortsTargetBaseline,
) string {
	var builder strings.Builder

	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(baseline.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- final delivery owner: `")
	builder.WriteString(strings.TrimSpace(baseline.Runtime.FinalDeliveryOwner))
	builder.WriteString("`, big-bang enabled: `")
	builder.WriteString(formatCommunityShortsContinuousObservationBool(baseline.Runtime.CommunityShortsBigBangEnabled))
	builder.WriteString("`\n")
	builder.WriteString("- runtime target channels: `")
	builder.WriteString(fmt.Sprintf("%d", baseline.Runtime.TargetChannelCount))
	builder.WriteString("`, channel rows: `")
	builder.WriteString(fmt.Sprintf("%d", len(baseline.Channels)))
	builder.WriteString("`\n")

	if len(baseline.Channels) == 0 {
		builder.WriteString("\n활성 운영 채널 baseline이 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| channel_id | owner | community_enabled | community_rooms | community_mode | shorts_enabled | shorts_rooms | shorts_mode |\n")
	builder.WriteString("| --- | --- | --- | ---: | --- | --- | ---: | --- |\n")
	for i := range baseline.Channels {
		communityRoute, _ := runtimeapp.CommunityShortsRouteForType(baseline.Channels[i].Routes, domain.AlarmTypeCommunity)
		shortsRoute, _ := runtimeapp.CommunityShortsRouteForType(baseline.Channels[i].Routes, domain.AlarmTypeShorts)
		builder.WriteString("| `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(baseline.Channels[i].ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(baseline.Channels[i].OwnerLabel))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsContinuousObservationBool(communityRoute.AlarmEnabled))
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", communityRoute.SubscriberRoomCount))
		builder.WriteString(" | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(communityRoute.EffectiveDeliveryMode))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsContinuousObservationBool(shortsRoute.AlarmEnabled))
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", shortsRoute.SubscriberRoomCount))
		builder.WriteString(" | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(shortsRoute.EffectiveDeliveryMode))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func promoteCommunityShortsMarkdownHeadings(markdown string, depth int) string {
	if depth <= 0 || strings.TrimSpace(markdown) == "" {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	prefix := strings.Repeat("#", depth)
	for i := range lines {
		trimmed := strings.TrimLeft(lines[i], " ")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		heading := trimmed
		count := 0
		for count < len(heading) && heading[count] == '#' {
			count++
		}
		if count == 0 || count >= len(heading) || heading[count] != ' ' {
			continue
		}
		indent := lines[i][:len(lines[i])-len(trimmed)]
		lines[i] = indent + prefix + heading
	}
	return strings.Join(lines, "\n")
}

func formatCommunityShortsContinuousObservationBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
