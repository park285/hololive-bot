package communityshortsops

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

func BuildCommunityShortsRouteVerificationReport(
	baseline communityshorts.TargetBaseline,
	pathUsageRows []outbox.PostDeliveryPathUsage,
	sendCountRows []outbox.PostSendCount,
	generatedAt time.Time,
	since time.Time,
) CommunityShortsRouteVerificationReport {
	generatedAt = resolveCommunityShortsRouteReportGeneratedAt(generatedAt, baseline.GeneratedAt)
	since = normalizeRouteReportTime(since)

	pathUsageIndex := indexCommunityShortsRouteReportPathUsage(pathUsageRows)
	sendCountIndex := indexCommunityShortsRouteReportSendCounts(sendCountRows)
	report := newCommunityShortsRouteVerificationReport(baseline, generatedAt, since)

	for i := range baseline.Channels {
		channelReport := buildCommunityShortsRouteVerificationChannel(baseline.Channels[i], sendCountIndex, pathUsageIndex, &report.Summary)
		report.Channels = append(report.Channels, channelReport)
	}

	return report
}

func resolveCommunityShortsRouteReportGeneratedAt(generatedAt time.Time, baselineGeneratedAt time.Time) time.Time {
	generatedAt = normalizeRouteReportTime(generatedAt)
	if generatedAt.IsZero() {
		return normalizeRouteReportTime(baselineGeneratedAt)
	}
	return generatedAt
}

func indexCommunityShortsRouteReportPathUsage(
	rows []outbox.PostDeliveryPathUsage,
) map[communityShortsRouteReportContentKey][]outbox.PostDeliveryPathUsage {
	index := make(map[communityShortsRouteReportContentKey][]outbox.PostDeliveryPathUsage)
	for i := range rows {
		key, ok := communityShortsRouteReportContentKeyFor(rows[i].ChannelID, rows[i].AlarmType, rows[i].ContentID)
		if !ok {
			continue
		}
		index[key] = append(index[key], rows[i])
	}
	return index
}

func indexCommunityShortsRouteReportSendCounts(
	rows []outbox.PostSendCount,
) map[communityShortsRouteReportKey][]outbox.PostSendCount {
	index := make(map[communityShortsRouteReportKey][]outbox.PostSendCount)
	for i := range rows {
		key, ok := communityShortsRouteReportKeyFor(rows[i].ChannelID, rows[i].AlarmType, rows[i].ContentID)
		if !ok {
			continue
		}
		index[key] = append(index[key], rows[i])
	}
	return index
}

func communityShortsRouteReportKeyFor(channelID string, alarmType domain.AlarmType, contentID string) (communityShortsRouteReportKey, bool) {
	channelID = strings.TrimSpace(channelID)
	contentID = strings.TrimSpace(contentID)
	if channelID == "" || contentID == "" {
		return communityShortsRouteReportKey{}, false
	}
	if !isCommunityShortsRouteReportAlarmType(alarmType) {
		return communityShortsRouteReportKey{}, false
	}
	return communityShortsRouteReportKey{channelID: channelID, alarmType: alarmType}, true
}

func communityShortsRouteReportContentKeyFor(
	channelID string,
	alarmType domain.AlarmType,
	contentID string,
) (communityShortsRouteReportContentKey, bool) {
	key, ok := communityShortsRouteReportKeyFor(channelID, alarmType, contentID)
	if !ok {
		return communityShortsRouteReportContentKey{}, false
	}
	return communityShortsRouteReportContentKey{
		channelID: key.channelID,
		alarmType: key.alarmType,
		contentID: strings.TrimSpace(contentID),
	}, true
}

func isCommunityShortsRouteReportAlarmType(alarmType domain.AlarmType) bool {
	return alarmType == domain.AlarmTypeCommunity || alarmType == domain.AlarmTypeShorts
}

func newCommunityShortsRouteVerificationReport(
	baseline communityshorts.TargetBaseline,
	generatedAt time.Time,
	since time.Time,
) CommunityShortsRouteVerificationReport {
	return CommunityShortsRouteVerificationReport{
		GeneratedAt: generatedAt,
		WindowStart: since,
		WindowEnd:   generatedAt,
		Runtime: CommunityShortsRouteVerificationRuntime{
			FinalDeliveryOwner:              baseline.Runtime.FinalDeliveryOwner,
			CommunityShortsBigBangEnabled:   baseline.Runtime.CommunityShortsBigBangEnabled,
			CommunityShortsBigBangCutoverAt: cloneRouteReportTime(baseline.Runtime.CommunityShortsBigBangCutoverAt),
			ExpectedTelemetryPath:           communityshorts.NewDeliveryPath,
		},
		Summary: CommunityShortsRouteVerificationSummary{
			TargetChannelCount: len(baseline.Channels),
		},
		Channels: make([]CommunityShortsRouteVerificationChannel, 0, len(baseline.Channels)),
	}
}

func buildCommunityShortsRouteVerificationChannel(
	channel communityshorts.TargetBaselineChannel,
	sendCountIndex map[communityShortsRouteReportKey][]outbox.PostSendCount,
	pathUsageIndex map[communityShortsRouteReportContentKey][]outbox.PostDeliveryPathUsage,
	summary *CommunityShortsRouteVerificationSummary,
) CommunityShortsRouteVerificationChannel {
	channelReport := CommunityShortsRouteVerificationChannel{
		OwnerLabel: strings.TrimSpace(channel.OwnerLabel),
		ChannelID:  strings.TrimSpace(channel.ChannelID),
		Routes:     make([]CommunityShortsRouteVerificationRoute, 0, len(channel.Routes)),
	}
	for i := range channel.Routes {
		baseRoute := channel.Routes[i]
		routeKey := communityShortsRouteReportKey{channelID: channelReport.ChannelID, alarmType: baseRoute.AlarmType}
		routeReport := buildCommunityShortsRouteVerificationRoute(baseRoute, sendCountIndex[routeKey], pathUsageIndex)
		channelReport.Routes = append(channelReport.Routes, routeReport)
		applyCommunityShortsRouteVerificationSummary(summary, routeReport)
	}
	return channelReport
}

func buildCommunityShortsRouteVerificationRoute(
	baseRoute communityshorts.TargetBaselineChannelRoute,
	sendCounts []outbox.PostSendCount,
	pathUsageIndex map[communityShortsRouteReportContentKey][]outbox.PostDeliveryPathUsage,
) CommunityShortsRouteVerificationRoute {
	route := CommunityShortsRouteVerificationRoute{
		AlarmType:             baseRoute.AlarmType,
		ActivationState:       strings.TrimSpace(baseRoute.EffectiveDeliveryMode),
		AlarmEnabled:          baseRoute.AlarmEnabled,
		SubscriberRoomCount:   baseRoute.SubscriberRoomCount,
		DeploymentTargetOwner: strings.TrimSpace(baseRoute.FinalDeliveryOwner),
		DeploymentTargetPath:  strings.TrimSpace(baseRoute.FinalDeliveryPath),
		ExpectedTelemetryPath: communityshorts.NewDeliveryPath,
		ObservedPaths:         make([]string, 0),
	}

	observedPathSet := make(map[string]struct{})
	for i := range sendCounts {
		applyCommunityShortsRouteVerificationPost(&route, sendCounts[i], pathUsageIndex, observedPathSet)
	}

	route.ObservedPaths = sortedRouteReportPaths(observedPathSet)
	route.ActualUsageState = resolveCommunityShortsActualUsageState(route)
	return route
}

func applyCommunityShortsRouteVerificationPost(
	route *CommunityShortsRouteVerificationRoute,
	sendCount outbox.PostSendCount,
	pathUsageIndex map[communityShortsRouteReportContentKey][]outbox.PostDeliveryPathUsage,
	observedPathSet map[string]struct{},
) {
	route.ObservedPostCount++
	if sendCount.SuccessSendCount > 0 {
		route.SuccessfulPostCount++
	}

	contentKey, _ := communityShortsRouteReportContentKeyFor(sendCount.ChannelID, sendCount.AlarmType, sendCount.ContentID)
	postUsage := classifyCommunityShortsPostPaths(pathUsageIndex[contentKey])
	for _, path := range postUsage.ObservedPaths {
		observedPathSet[path] = struct{}{}
	}
	applyCommunityShortsRouteVerificationPostUsage(route, postUsage.State)
	route.LatestPublishedAt = laterRouteReportTime(route.LatestPublishedAt, resolveRouteReportPublishedAt(sendCount.ActualPublishedAt, sendCount.DetectedAt))
	route.LatestSuccessAt = laterRouteReportTime(route.LatestSuccessAt, resolveRouteReportSuccessAt(sendCount.LastSuccessAt, sendCount.AlarmSentAt, sendCount.FirstSuccessAt))
}

func applyCommunityShortsRouteVerificationPostUsage(route *CommunityShortsRouteVerificationRoute, usageState string) {
	switch usageState {
	case communityShortsRouteUsageNewOnlyVerified:
		route.NewPathOnlyPostCount++
	case communityShortsRouteUsageMixedPathsDetected:
		route.MixedPathPostCount++
	case communityShortsRouteUsageUnexpectedPathDetected:
		route.UnexpectedPathPostCount++
	default:
		route.NoPathPostCount++
	}
}

func applyCommunityShortsRouteVerificationSummary(
	summary *CommunityShortsRouteVerificationSummary,
	route CommunityShortsRouteVerificationRoute,
) {
	if summary == nil {
		return
	}
	summary.RouteCount++
	applyCommunityShortsRouteVerificationRouteStateSummary(summary, route)
	applyCommunityShortsRouteVerificationUsageSummary(summary, route)
}

func applyCommunityShortsRouteVerificationRouteStateSummary(
	summary *CommunityShortsRouteVerificationSummary,
	route CommunityShortsRouteVerificationRoute,
) {
	if route.AlarmEnabled {
		summary.ActiveRouteCount++
	} else {
		summary.DisabledRouteCount++
	}
	if route.ActivationState == communityshorts.DeliveryModePending {
		summary.PendingCutoverRouteCount++
	}
}

func applyCommunityShortsRouteVerificationUsageSummary(
	summary *CommunityShortsRouteVerificationSummary,
	route CommunityShortsRouteVerificationRoute,
) {
	if !route.AlarmEnabled {
		return
	}
	applyCommunityShortsRouteVerificationPrimaryUsageSummary(summary, route.ActualUsageState)
	applyCommunityShortsRouteVerificationPathIssueSummary(summary, route.ActualUsageState)
}

func applyCommunityShortsRouteVerificationPrimaryUsageSummary(
	summary *CommunityShortsRouteVerificationSummary,
	actualUsageState string,
) {
	switch actualUsageState {
	case communityShortsRouteUsageNewOnlyVerified:
		summary.NewOnlyUsageRouteCount++
	case communityShortsRouteUsageNoRecentPosts:
		summary.NoRecentPostRouteCount++
	case communityShortsRouteUsageNoPathObserved:
		summary.NoPathObservedRouteCount++
	}
}

func applyCommunityShortsRouteVerificationPathIssueSummary(
	summary *CommunityShortsRouteVerificationSummary,
	actualUsageState string,
) {
	switch actualUsageState {
	case communityShortsRouteUsageUnexpectedPathDetected:
		summary.UnexpectedPathRouteCount++
	case communityShortsRouteUsageMixedPathsDetected:
		summary.MixedPathRouteCount++
	}
}

func RenderCommunityShortsRouteVerificationMarkdown(report CommunityShortsRouteVerificationReport) string {
	var builder strings.Builder

	builder.WriteString(buildCommunityShortsRouteVerificationMetadataMarkdown(report))

	for i := range report.Channels {
		builder.WriteString(buildCommunityShortsRouteVerificationChannelMarkdown(report.Channels[i]))
	}

	return builder.String()
}

func buildCommunityShortsRouteVerificationMetadataMarkdown(report CommunityShortsRouteVerificationReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Channel Route Verification Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatRouteReportTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- window: `")
	builder.WriteString(formatRouteReportTime(report.WindowStart))
	builder.WriteString("` -> `")
	builder.WriteString(formatRouteReportTime(report.WindowEnd))
	builder.WriteString("`\n")
	builder.WriteString("- runtime final owner: `")
	builder.WriteString(fallbackRouteReportValue(report.Runtime.FinalDeliveryOwner, "(empty)"))
	builder.WriteString("`\n")
	builder.WriteString("- big-bang enabled: `")
	fmt.Fprintf(&builder, "%t", report.Runtime.CommunityShortsBigBangEnabled)
	builder.WriteString("`\n")
	builder.WriteString("- telemetry path expectation: `")
	builder.WriteString(report.Runtime.ExpectedTelemetryPath)
	builder.WriteString("`\n")
	builder.WriteString("- summary: target_channels=`")
	fmt.Fprintf(&builder, "%d", report.Summary.TargetChannelCount)
	builder.WriteString("`, routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.RouteCount)
	builder.WriteString("`, active_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.ActiveRouteCount)
	builder.WriteString("`, disabled_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.DisabledRouteCount)
	builder.WriteString("`, pending_cutover_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.PendingCutoverRouteCount)
	builder.WriteString("`, new_only_usage_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.NewOnlyUsageRouteCount)
	builder.WriteString("`, no_recent_post_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.NoRecentPostRouteCount)
	builder.WriteString("`, no_path_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.NoPathObservedRouteCount)
	builder.WriteString("`, unexpected_path_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.UnexpectedPathRouteCount)
	builder.WriteString("`, mixed_path_routes=`")
	fmt.Fprintf(&builder, "%d", report.Summary.MixedPathRouteCount)
	builder.WriteString("`\n")

	return builder.String()
}

func buildCommunityShortsRouteVerificationChannelMarkdown(channel CommunityShortsRouteVerificationChannel) string {
	var builder strings.Builder

	builder.WriteString("\n## ")
	if channel.OwnerLabel != "" {
		builder.WriteString(channel.OwnerLabel)
		builder.WriteString(" (`")
		builder.WriteString(channel.ChannelID)
		builder.WriteString("`)")
	} else {
		builder.WriteString("`")
		builder.WriteString(channel.ChannelID)
		builder.WriteString("`")
	}
	builder.WriteString("\n\n")
	for i := range channel.Routes {
		builder.WriteString(buildCommunityShortsRouteVerificationRouteMarkdown(channel.Routes[i]))
	}

	return builder.String()
}

func buildCommunityShortsRouteVerificationRouteMarkdown(route CommunityShortsRouteVerificationRoute) string {
	var builder strings.Builder

	builder.WriteString("- ")
	builder.WriteString(string(route.AlarmType))
	builder.WriteString(": activation=`")
	builder.WriteString(fallbackRouteReportValue(route.ActivationState, "(empty)"))
	builder.WriteString("`, deployment=`")
	builder.WriteString(fallbackRouteReportValue(route.DeploymentTargetPath, "(empty)"))
	builder.WriteString("`, telemetry_target=`")
	builder.WriteString(fallbackRouteReportValue(route.ExpectedTelemetryPath, "(empty)"))
	builder.WriteString("`, actual=`")
	builder.WriteString(fallbackRouteReportValue(route.ActualUsageState, "(empty)"))
	builder.WriteString("`, rooms=`")
	fmt.Fprintf(&builder, "%d", route.SubscriberRoomCount)
	builder.WriteString("`, posts=`")
	fmt.Fprintf(&builder, "%d", route.ObservedPostCount)
	builder.WriteString("`, success_posts=`")
	fmt.Fprintf(&builder, "%d", route.SuccessfulPostCount)
	builder.WriteString("`, new_path_posts=`")
	fmt.Fprintf(&builder, "%d", route.NewPathOnlyPostCount)
	builder.WriteString("`, mixed_path_posts=`")
	fmt.Fprintf(&builder, "%d", route.MixedPathPostCount)
	builder.WriteString("`, unexpected_path_posts=`")
	fmt.Fprintf(&builder, "%d", route.UnexpectedPathPostCount)
	builder.WriteString("`, no_path_posts=`")
	fmt.Fprintf(&builder, "%d", route.NoPathPostCount)
	builder.WriteString("`, observed_paths=")
	builder.WriteString(formatRouteReportPaths(route.ObservedPaths))
	builder.WriteString(", latest_published_at=`")
	builder.WriteString(formatRouteReportTimePtr(route.LatestPublishedAt))
	builder.WriteString("`, latest_success_at=`")
	builder.WriteString(formatRouteReportTimePtr(route.LatestSuccessAt))
	builder.WriteString("`\n")

	return builder.String()
}
