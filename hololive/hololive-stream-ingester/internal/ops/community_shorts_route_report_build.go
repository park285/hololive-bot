package ops

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
	generatedAt = normalizeRouteReportTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = normalizeRouteReportTime(baseline.GeneratedAt)
	}
	since = normalizeRouteReportTime(since)

	pathUsageIndex := make(map[communityShortsRouteReportContentKey][]outbox.PostDeliveryPathUsage)
	for i := range pathUsageRows {
		row := pathUsageRows[i]
		channelID := strings.TrimSpace(row.ChannelID)
		contentID := strings.TrimSpace(row.ContentID)
		if channelID == "" || contentID == "" {
			continue
		}
		if row.AlarmType != domain.AlarmTypeCommunity && row.AlarmType != domain.AlarmTypeShorts {
			continue
		}
		key := communityShortsRouteReportContentKey{
			channelID: channelID,
			alarmType: row.AlarmType,
			contentID: contentID,
		}
		pathUsageIndex[key] = append(pathUsageIndex[key], row)
	}

	sendCountIndex := make(map[communityShortsRouteReportKey][]outbox.PostSendCount)
	for i := range sendCountRows {
		row := sendCountRows[i]
		channelID := strings.TrimSpace(row.ChannelID)
		contentID := strings.TrimSpace(row.ContentID)
		if channelID == "" || contentID == "" {
			continue
		}
		if row.AlarmType != domain.AlarmTypeCommunity && row.AlarmType != domain.AlarmTypeShorts {
			continue
		}
		key := communityShortsRouteReportKey{channelID: channelID, alarmType: row.AlarmType}
		sendCountIndex[key] = append(sendCountIndex[key], row)
	}

	report := CommunityShortsRouteVerificationReport{
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

	for i := range baseline.Channels {
		channel := baseline.Channels[i]
		channelReport := CommunityShortsRouteVerificationChannel{
			OwnerLabel: strings.TrimSpace(channel.OwnerLabel),
			ChannelID:  strings.TrimSpace(channel.ChannelID),
			Routes:     make([]CommunityShortsRouteVerificationRoute, 0, len(channel.Routes)),
		}

		for j := range channel.Routes {
			baseRoute := channel.Routes[j]
			routeKey := communityShortsRouteReportKey{channelID: channelReport.ChannelID, alarmType: baseRoute.AlarmType}
			routeReport := buildCommunityShortsRouteVerificationRoute(baseRoute, sendCountIndex[routeKey], pathUsageIndex)
			channelReport.Routes = append(channelReport.Routes, routeReport)
			applyCommunityShortsRouteVerificationSummary(&report.Summary, routeReport)
		}

		report.Channels = append(report.Channels, channelReport)
	}

	return report
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
		sendCount := sendCounts[i]
		route.ObservedPostCount++
		if sendCount.SuccessSendCount > 0 {
			route.SuccessfulPostCount++
		}

		for _, path := range classifyCommunityShortsPostPaths(pathUsageIndex[communityShortsRouteReportContentKey{
			channelID: strings.TrimSpace(sendCount.ChannelID),
			alarmType: sendCount.AlarmType,
			contentID: strings.TrimSpace(sendCount.ContentID),
		}]).ObservedPaths {
			observedPathSet[path] = struct{}{}
		}

		postUsage := classifyCommunityShortsPostPaths(pathUsageIndex[communityShortsRouteReportContentKey{
			channelID: strings.TrimSpace(sendCount.ChannelID),
			alarmType: sendCount.AlarmType,
			contentID: strings.TrimSpace(sendCount.ContentID),
		}])
		switch postUsage.State {
		case communityShortsRouteUsageNewOnlyVerified:
			route.NewPathOnlyPostCount++
		case communityShortsRouteUsageMixedPathsDetected:
			route.MixedPathPostCount++
		case communityShortsRouteUsageUnexpectedPathDetected:
			route.UnexpectedPathPostCount++
		default:
			route.NoPathPostCount++
		}

		route.LatestPublishedAt = laterRouteReportTime(route.LatestPublishedAt, resolveRouteReportPublishedAt(sendCount.ActualPublishedAt, sendCount.DetectedAt))
		route.LatestSuccessAt = laterRouteReportTime(route.LatestSuccessAt, resolveRouteReportSuccessAt(sendCount.LastSuccessAt, sendCount.AlarmSentAt, sendCount.FirstSuccessAt))
	}

	route.ObservedPaths = sortedRouteReportPaths(observedPathSet)
	route.ActualUsageState = resolveCommunityShortsActualUsageState(route)
	return route
}

func applyCommunityShortsRouteVerificationSummary(
	summary *CommunityShortsRouteVerificationSummary,
	route CommunityShortsRouteVerificationRoute,
) {
	if summary == nil {
		return
	}
	summary.RouteCount++
	if route.AlarmEnabled {
		summary.ActiveRouteCount++
	} else {
		summary.DisabledRouteCount++
	}
	if route.ActivationState == communityshorts.DeliveryModePending {
		summary.PendingCutoverRouteCount++
	}
	if !route.AlarmEnabled {
		return
	}
	switch route.ActualUsageState {
	case communityShortsRouteUsageNewOnlyVerified:
		summary.NewOnlyUsageRouteCount++
	case communityShortsRouteUsageNoRecentPosts:
		summary.NoRecentPostRouteCount++
	case communityShortsRouteUsageNoPathObserved:
		summary.NoPathObservedRouteCount++
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
