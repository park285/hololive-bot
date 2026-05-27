package routereport

import (
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func Build(
	baseline communityshorts.TargetBaseline,
	pathUsageRows []outbox.PostDeliveryPathUsage,
	sendCountRows []outbox.PostSendCount,
	generatedAt time.Time,
	since time.Time,
) Report {
	generatedAt = resolveGeneratedAt(generatedAt, baseline.GeneratedAt)
	since = shared.NormalizeSendCountTime(since)
	pathUsageIndex := indexPathUsage(pathUsageRows)
	sendCountIndex := indexSendCounts(sendCountRows)
	report := newReport(baseline, generatedAt, since)

	for i := range baseline.Channels {
		channel := buildChannel(baseline.Channels[i], sendCountIndex, pathUsageIndex, &report.Summary)
		report.Channels = append(report.Channels, channel)
	}
	return report
}

func resolveGeneratedAt(generatedAt time.Time, baselineGeneratedAt time.Time) time.Time {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		return shared.NormalizeSendCountTime(baselineGeneratedAt)
	}
	return generatedAt
}

func indexPathUsage(rows []outbox.PostDeliveryPathUsage) map[contentKey][]outbox.PostDeliveryPathUsage {
	index := make(map[contentKey][]outbox.PostDeliveryPathUsage)
	for i := range rows {
		key, ok := contentKeyFor(rows[i].ChannelID, rows[i].AlarmType, rows[i].ContentID)
		if ok {
			index[key] = append(index[key], rows[i])
		}
	}
	return index
}

func indexSendCounts(rows []outbox.PostSendCount) map[routeKey][]outbox.PostSendCount {
	index := make(map[routeKey][]outbox.PostSendCount)
	for i := range rows {
		key, ok := routeKeyFor(rows[i].ChannelID, rows[i].AlarmType, rows[i].ContentID)
		if ok {
			index[key] = append(index[key], rows[i])
		}
	}
	return index
}

func routeKeyFor(channelID string, alarmType domain.AlarmType, contentID string) (routeKey, bool) {
	channelID = strings.TrimSpace(channelID)
	contentID = strings.TrimSpace(contentID)
	if channelID == "" || contentID == "" || !isRouteAlarmType(alarmType) {
		return routeKey{}, false
	}
	return routeKey{channelID: channelID, alarmType: alarmType}, true
}

func contentKeyFor(channelID string, alarmType domain.AlarmType, contentID string) (contentKey, bool) {
	key, ok := routeKeyFor(channelID, alarmType, contentID)
	if !ok {
		return contentKey{}, false
	}
	return contentKey{
		channelID: key.channelID,
		alarmType: key.alarmType,
		contentID: strings.TrimSpace(contentID),
	}, true
}

func isRouteAlarmType(alarmType domain.AlarmType) bool {
	return alarmType == domain.AlarmTypeCommunity || alarmType == domain.AlarmTypeShorts
}

func newReport(
	baseline communityshorts.TargetBaseline,
	generatedAt time.Time,
	since time.Time,
) Report {
	return Report{
		GeneratedAt: generatedAt,
		WindowStart: since,
		WindowEnd:   generatedAt,
		Runtime: Runtime{
			FinalDeliveryOwner:              baseline.Runtime.FinalDeliveryOwner,
			CommunityShortsBigBangEnabled:   baseline.Runtime.CommunityShortsBigBangEnabled,
			CommunityShortsBigBangCutoverAt: shared.CloneSendCountTime(baseline.Runtime.CommunityShortsBigBangCutoverAt),
			ExpectedTelemetryPath:           communityshorts.NewDeliveryPath,
		},
		Summary:  Summary{TargetChannelCount: len(baseline.Channels)},
		Channels: make([]Channel, 0, len(baseline.Channels)),
	}
}

func buildChannel(
	channel communityshorts.TargetBaselineChannel,
	sendCountIndex map[routeKey][]outbox.PostSendCount,
	pathUsageIndex map[contentKey][]outbox.PostDeliveryPathUsage,
	summary *Summary,
) Channel {
	channelReport := Channel{
		OwnerLabel: strings.TrimSpace(channel.OwnerLabel),
		ChannelID:  strings.TrimSpace(channel.ChannelID),
		Routes:     make([]Route, 0, len(channel.Routes)),
	}
	for i := range channel.Routes {
		baseRoute := channel.Routes[i]
		key := routeKey{channelID: channelReport.ChannelID, alarmType: baseRoute.AlarmType}
		route := buildRoute(baseRoute, sendCountIndex[key], pathUsageIndex)
		channelReport.Routes = append(channelReport.Routes, route)
		applySummary(summary, route)
	}
	return channelReport
}

func buildRoute(
	baseRoute communityshorts.TargetBaselineChannelRoute,
	sendCounts []outbox.PostSendCount,
	pathUsageIndex map[contentKey][]outbox.PostDeliveryPathUsage,
) Route {
	route := newRoute(baseRoute)
	observedPathSet := make(map[string]struct{})
	for i := range sendCounts {
		applyPost(&route, sendCounts[i], pathUsageIndex, observedPathSet)
	}
	route.ObservedPaths = sortedPaths(observedPathSet)
	route.ActualUsageState = resolveActualUsageState(route)
	return route
}

func newRoute(baseRoute communityshorts.TargetBaselineChannelRoute) Route {
	return Route{
		AlarmType:             baseRoute.AlarmType,
		ActivationState:       strings.TrimSpace(baseRoute.EffectiveDeliveryMode),
		AlarmEnabled:          baseRoute.AlarmEnabled,
		SubscriberRoomCount:   baseRoute.SubscriberRoomCount,
		DeploymentTargetOwner: strings.TrimSpace(baseRoute.FinalDeliveryOwner),
		DeploymentTargetPath:  strings.TrimSpace(baseRoute.FinalDeliveryPath),
		ExpectedTelemetryPath: communityshorts.NewDeliveryPath,
		ObservedPaths:         make([]string, 0),
	}
}

func applyPost(
	route *Route,
	sendCount outbox.PostSendCount,
	pathUsageIndex map[contentKey][]outbox.PostDeliveryPathUsage,
	observedPathSet map[string]struct{},
) {
	route.ObservedPostCount++
	if sendCount.SuccessSendCount > 0 {
		route.SuccessfulPostCount++
	}

	contentKey, _ := contentKeyFor(sendCount.ChannelID, sendCount.AlarmType, sendCount.ContentID)
	postUsage := classifyPostPaths(pathUsageIndex[contentKey])
	for _, path := range postUsage.ObservedPaths {
		observedPathSet[path] = struct{}{}
	}
	applyPostUsage(route, postUsage.State)
	route.LatestPublishedAt = latestPublishedAt(route.LatestPublishedAt, sendCount)
	route.LatestSuccessAt = latestSuccessAt(route.LatestSuccessAt, sendCount)
}

func applyPostUsage(route *Route, usageState string) {
	switch usageState {
	case routeUsageNewOnlyVerified:
		route.NewPathOnlyPostCount++
	case routeUsageMixedPathsDetected:
		route.MixedPathPostCount++
	case routeUsageUnexpectedPathDetected:
		route.UnexpectedPathPostCount++
	default:
		route.NoPathPostCount++
	}
}

func latestPublishedAt(current *time.Time, sendCount outbox.PostSendCount) *time.Time {
	if sendCount.ActualPublishedAt != nil {
		return laterTime(current, sendCount.ActualPublishedAt)
	}
	return laterTime(current, sendCount.DetectedAt)
}

func latestSuccessAt(current *time.Time, sendCount outbox.PostSendCount) *time.Time {
	candidates := []*time.Time{sendCount.LastSuccessAt, sendCount.AlarmSentAt, sendCount.FirstSuccessAt}
	for i := range candidates {
		current = laterTime(current, candidates[i])
	}
	return current
}

func applySummary(summary *Summary, route Route) {
	if summary == nil {
		return
	}
	summary.RouteCount++
	applyRouteStateSummary(summary, route)
	applyUsageSummary(summary, route)
}

func applyRouteStateSummary(summary *Summary, route Route) {
	if route.AlarmEnabled {
		summary.ActiveRouteCount++
	} else {
		summary.DisabledRouteCount++
	}
	if route.ActivationState == communityshorts.DeliveryModePending {
		summary.PendingCutoverRouteCount++
	}
}

func applyUsageSummary(summary *Summary, route Route) {
	if !route.AlarmEnabled {
		return
	}
	applyPrimaryUsageSummary(summary, route.ActualUsageState)
	applyPathIssueSummary(summary, route.ActualUsageState)
}

func applyPrimaryUsageSummary(summary *Summary, actualUsageState string) {
	switch actualUsageState {
	case routeUsageNewOnlyVerified:
		summary.NewOnlyUsageRouteCount++
	case routeUsageNoRecentPosts:
		summary.NoRecentPostRouteCount++
	case routeUsageNoPathObserved:
		summary.NoPathObservedRouteCount++
	}
}

func applyPathIssueSummary(summary *Summary, actualUsageState string) {
	switch actualUsageState {
	case routeUsageUnexpectedPathDetected:
		summary.UnexpectedPathRouteCount++
	case routeUsageMixedPathsDetected:
		summary.MixedPathRouteCount++
	}
}

func RenderMarkdown(report Report) string {
	var builder strings.Builder

	writeMetadata(&builder, report)
	for i := range report.Channels {
		writeChannel(&builder, report.Channels[i])
	}

	return builder.String()
}

func writeMetadata(builder *strings.Builder, report Report) {
	md.WriteHeading(builder, 1, "YouTube Community/Shorts Channel Route Verification Report")
	md.WriteKV(builder, "generated at", md.Code(shared.FormatSendCountTime(report.GeneratedAt)))
	md.WriteKV(builder, "window", md.Code(shared.FormatSendCountTime(report.WindowStart))+" -> "+md.Code(shared.FormatSendCountTime(report.WindowEnd)))
	md.WriteKV(builder, "runtime final owner", codeValue(report.Runtime.FinalDeliveryOwner))
	md.WriteKV(builder, "big-bang enabled", md.Code(strconv.FormatBool(report.Runtime.CommunityShortsBigBangEnabled)))
	md.WriteKV(builder, "telemetry path expectation", md.Code(report.Runtime.ExpectedTelemetryPath))
	md.WriteKV(builder, "summary", summaryMarkdown(report.Summary))
}

func summaryMarkdown(summary Summary) string {
	return strings.Join([]string{
		"target_channels=" + codeInt(summary.TargetChannelCount),
		"routes=" + codeInt(summary.RouteCount),
		"active_routes=" + codeInt(summary.ActiveRouteCount),
		"disabled_routes=" + codeInt(summary.DisabledRouteCount),
		"pending_cutover_routes=" + codeInt(summary.PendingCutoverRouteCount),
		"new_only_usage_routes=" + codeInt(summary.NewOnlyUsageRouteCount),
		"no_recent_post_routes=" + codeInt(summary.NoRecentPostRouteCount),
		"no_path_routes=" + codeInt(summary.NoPathObservedRouteCount),
		"unexpected_path_routes=" + codeInt(summary.UnexpectedPathRouteCount),
		"mixed_path_routes=" + codeInt(summary.MixedPathRouteCount),
	}, ", ")
}

func writeChannel(builder *strings.Builder, channel Channel) {
	builder.WriteString("\n")
	md.WriteHeading(builder, 2, channelTitle(channel))
	for i := range channel.Routes {
		builder.WriteString(routeMarkdown(channel.Routes[i]))
	}
}

func channelTitle(channel Channel) string {
	if channel.OwnerLabel != "" {
		return channel.OwnerLabel + " (" + md.Code(channel.ChannelID) + ")"
	}
	return md.Code(channel.ChannelID)
}

func routeMarkdown(route Route) string {
	fields := []string{
		"activation=" + codeValue(route.ActivationState),
		"deployment=" + codeValue(route.DeploymentTargetPath),
		"telemetry_target=" + codeValue(route.ExpectedTelemetryPath),
		"actual=" + codeValue(route.ActualUsageState),
		"rooms=" + codeInt(route.SubscriberRoomCount),
		"posts=" + codeInt(route.ObservedPostCount),
		"success_posts=" + codeInt(route.SuccessfulPostCount),
		"new_path_posts=" + codeInt(route.NewPathOnlyPostCount),
		"mixed_path_posts=" + codeInt(route.MixedPathPostCount),
		"unexpected_path_posts=" + codeInt(route.UnexpectedPathPostCount),
		"no_path_posts=" + codeInt(route.NoPathPostCount),
		"observed_paths=" + formatPaths(route.ObservedPaths),
		"latest_published_at=" + md.Code(shared.FormatSendCountTimePtr(route.LatestPublishedAt)),
		"latest_success_at=" + md.Code(shared.FormatSendCountTimePtr(route.LatestSuccessAt)),
	}
	return "- " + string(route.AlarmType) + ": " + strings.Join(fields, ", ") + "\n"
}

func formatPaths(paths []string) string {
	if len(paths) == 0 {
		return md.Code(shared.NoneValue)
	}
	formatted := make([]string, 0, len(paths))
	for i := range paths {
		formatted = append(formatted, md.Code(paths[i]))
	}
	return strings.Join(formatted, ", ")
}

func codeValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return md.Code("(empty)")
	}
	return md.Code(trimmed)
}

func codeInt(value int) string {
	return md.Code(strconv.Itoa(value))
}
