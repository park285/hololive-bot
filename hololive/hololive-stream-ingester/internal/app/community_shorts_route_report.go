package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

const (
	communityShortsRouteUsageNoRecentPosts          = "no_recent_posts"
	communityShortsRouteUsageNoPathObserved         = "no_path_observed"
	communityShortsRouteUsageNewOnlyVerified        = "new_only_verified"
	communityShortsRouteUsageUnexpectedPathDetected = "unexpected_path_detected"
	communityShortsRouteUsageMixedPathsDetected     = "mixed_paths_detected"
)

type CommunityShortsRouteVerificationReport struct {
	GeneratedAt time.Time                                 `json:"generated_at"`
	WindowStart time.Time                                 `json:"window_start"`
	WindowEnd   time.Time                                 `json:"window_end"`
	Runtime     CommunityShortsRouteVerificationRuntime   `json:"runtime"`
	Summary     CommunityShortsRouteVerificationSummary   `json:"summary"`
	Channels    []CommunityShortsRouteVerificationChannel `json:"channels"`
}

type CommunityShortsRouteVerificationRuntime struct {
	FinalDeliveryOwner              string     `json:"final_delivery_owner"`
	CommunityShortsBigBangEnabled   bool       `json:"community_shorts_bigbang_enabled"`
	CommunityShortsBigBangCutoverAt *time.Time `json:"community_shorts_bigbang_cutover_at,omitempty"`
	ExpectedTelemetryPath           string     `json:"expected_telemetry_path"`
}

type CommunityShortsRouteVerificationSummary struct {
	TargetChannelCount       int `json:"target_channel_count"`
	RouteCount               int `json:"route_count"`
	ActiveRouteCount         int `json:"active_route_count"`
	DisabledRouteCount       int `json:"disabled_route_count"`
	PendingCutoverRouteCount int `json:"pending_cutover_route_count"`
	NewOnlyUsageRouteCount   int `json:"new_only_usage_route_count"`
	NoRecentPostRouteCount   int `json:"no_recent_post_route_count"`
	NoPathObservedRouteCount int `json:"no_path_observed_route_count"`
	UnexpectedPathRouteCount int `json:"unexpected_path_route_count"`
	MixedPathRouteCount      int `json:"mixed_path_route_count"`
}

type CommunityShortsRouteVerificationChannel struct {
	OwnerLabel string                                  `json:"owner_label"`
	ChannelID  string                                  `json:"channel_id"`
	Routes     []CommunityShortsRouteVerificationRoute `json:"routes"`
}

type CommunityShortsRouteVerificationRoute struct {
	AlarmType               domain.AlarmType `json:"alarm_type"`
	ActivationState         string           `json:"activation_state"`
	AlarmEnabled            bool             `json:"alarm_enabled"`
	SubscriberRoomCount     int              `json:"subscriber_room_count"`
	DeploymentTargetOwner   string           `json:"deployment_target_owner"`
	DeploymentTargetPath    string           `json:"deployment_target_path"`
	ExpectedTelemetryPath   string           `json:"expected_telemetry_path"`
	ActualUsageState        string           `json:"actual_usage_state"`
	ObservedPostCount       int              `json:"observed_post_count"`
	SuccessfulPostCount     int              `json:"successful_post_count"`
	NewPathOnlyPostCount    int              `json:"new_path_only_post_count"`
	NoPathPostCount         int              `json:"no_path_post_count"`
	UnexpectedPathPostCount int              `json:"unexpected_path_post_count"`
	MixedPathPostCount      int              `json:"mixed_path_post_count"`
	ObservedPaths           []string         `json:"observed_paths"`
	LatestPublishedAt       *time.Time       `json:"latest_published_at,omitempty"`
	LatestSuccessAt         *time.Time       `json:"latest_success_at,omitempty"`
}

type communityShortsRouteReportKey struct {
	channelID string
	alarmType domain.AlarmType
}

type communityShortsRouteReportContentKey struct {
	channelID string
	alarmType domain.AlarmType
	contentID string
}

func CollectCommunityShortsRouteVerificationReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (CommunityShortsRouteVerificationReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if since.IsZero() {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: since is empty")
	}
	since = since.UTC()
	if since.After(now) {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: since is after now")
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	memberRepository := sharedproviders.ProvideMemberRepository(databaseResources.Service, logger)
	members, err := memberRepository.GetAllMembers(ctx)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: load members: %w", err)
	}

	alarmRepository := sharedalarm.NewRepository(databaseResources.Service, logger)
	alarms, err := alarmRepository.LoadAll(ctx)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: load alarms: %w", err)
	}

	channels := buildCommunityShortsOperationalChannelsFromMembers(members)
	baseline, err := buildCommunityShortsTargetBaseline(channels, alarms, cfg.Ingestion, now)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: build baseline: %w", err)
	}

	telemetryRepo := outbox.NewDeliveryTelemetryRepository(databaseResources.Service.GetGormDB())
	pathUsageRows, err := telemetryRepo.ListPostDeliveryPathUsageSince(ctx, since)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: list delivery path usage: %w", err)
	}

	sendCountRows, err := telemetryRepo.ListPostSendCountsSince(ctx, since)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: list send counts: %w", err)
	}

	return BuildCommunityShortsRouteVerificationReport(baseline, pathUsageRows, sendCountRows, now, since), nil
}

func BuildCommunityShortsRouteVerificationReport(
	baseline CommunityShortsTargetBaseline,
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
			ExpectedTelemetryPath:           communityShortsNewDeliveryPath,
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
	baseRoute CommunityShortsTargetBaselineChannelRoute,
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
		ExpectedTelemetryPath: communityShortsNewDeliveryPath,
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

type communityShortsPostPathClassification struct {
	State         string
	ObservedPaths []string
}

func classifyCommunityShortsPostPaths(rows []outbox.PostDeliveryPathUsage) communityShortsPostPathClassification {
	pathSet := make(map[string]struct{})
	for i := range rows {
		path := strings.TrimSpace(rows[i].DeliveryPath)
		if path == "" {
			continue
		}
		pathSet[path] = struct{}{}
	}

	paths := sortedRouteReportPaths(pathSet)
	switch len(paths) {
	case 0:
		return communityShortsPostPathClassification{State: communityShortsRouteUsageNoPathObserved, ObservedPaths: paths}
	case 1:
		if paths[0] == communityShortsNewDeliveryPath {
			return communityShortsPostPathClassification{State: communityShortsRouteUsageNewOnlyVerified, ObservedPaths: paths}
		}
		return communityShortsPostPathClassification{State: communityShortsRouteUsageUnexpectedPathDetected, ObservedPaths: paths}
	default:
		return communityShortsPostPathClassification{State: communityShortsRouteUsageMixedPathsDetected, ObservedPaths: paths}
	}
}

func resolveCommunityShortsActualUsageState(route CommunityShortsRouteVerificationRoute) string {
	if route.ObservedPostCount == 0 {
		return communityShortsRouteUsageNoRecentPosts
	}
	if route.MixedPathPostCount > 0 {
		return communityShortsRouteUsageMixedPathsDetected
	}
	if route.UnexpectedPathPostCount > 0 {
		return communityShortsRouteUsageUnexpectedPathDetected
	}
	if route.NoPathPostCount > 0 {
		return communityShortsRouteUsageNoPathObserved
	}
	return communityShortsRouteUsageNewOnlyVerified
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
	if route.ActivationState == communityShortsDeliveryModePending {
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
	builder.WriteString(fmt.Sprintf("%t", report.Runtime.CommunityShortsBigBangEnabled))
	builder.WriteString("`\n")
	builder.WriteString("- telemetry path expectation: `")
	builder.WriteString(report.Runtime.ExpectedTelemetryPath)
	builder.WriteString("`\n")
	builder.WriteString("- summary: target_channels=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.TargetChannelCount))
	builder.WriteString("`, routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.RouteCount))
	builder.WriteString("`, active_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ActiveRouteCount))
	builder.WriteString("`, disabled_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DisabledRouteCount))
	builder.WriteString("`, pending_cutover_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.PendingCutoverRouteCount))
	builder.WriteString("`, new_only_usage_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.NewOnlyUsageRouteCount))
	builder.WriteString("`, no_recent_post_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.NoRecentPostRouteCount))
	builder.WriteString("`, no_path_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.NoPathObservedRouteCount))
	builder.WriteString("`, unexpected_path_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.UnexpectedPathRouteCount))
	builder.WriteString("`, mixed_path_routes=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MixedPathRouteCount))
	builder.WriteString("`\n")

	for i := range report.Channels {
		channel := report.Channels[i]
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
		for j := range channel.Routes {
			route := channel.Routes[j]
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
			builder.WriteString(fmt.Sprintf("%d", route.SubscriberRoomCount))
			builder.WriteString("`, posts=`")
			builder.WriteString(fmt.Sprintf("%d", route.ObservedPostCount))
			builder.WriteString("`, success_posts=`")
			builder.WriteString(fmt.Sprintf("%d", route.SuccessfulPostCount))
			builder.WriteString("`, new_path_posts=`")
			builder.WriteString(fmt.Sprintf("%d", route.NewPathOnlyPostCount))
			builder.WriteString("`, mixed_path_posts=`")
			builder.WriteString(fmt.Sprintf("%d", route.MixedPathPostCount))
			builder.WriteString("`, unexpected_path_posts=`")
			builder.WriteString(fmt.Sprintf("%d", route.UnexpectedPathPostCount))
			builder.WriteString("`, no_path_posts=`")
			builder.WriteString(fmt.Sprintf("%d", route.NoPathPostCount))
			builder.WriteString("`, observed_paths=")
			builder.WriteString(formatRouteReportPaths(route.ObservedPaths))
			builder.WriteString(", latest_published_at=`")
			builder.WriteString(formatRouteReportTimePtr(route.LatestPublishedAt))
			builder.WriteString("`, latest_success_at=`")
			builder.WriteString(formatRouteReportTimePtr(route.LatestSuccessAt))
			builder.WriteString("`\n")
		}
	}

	return builder.String()
}

func resolveRouteReportPublishedAt(actualPublishedAt, detectedAt *time.Time) *time.Time {
	if actualPublishedAt != nil {
		return cloneRouteReportTime(actualPublishedAt)
	}
	return cloneRouteReportTime(detectedAt)
}

func resolveRouteReportSuccessAt(candidates ...*time.Time) *time.Time {
	var latest *time.Time
	for i := range candidates {
		latest = laterRouteReportTime(latest, candidates[i])
	}
	return latest
}

func laterRouteReportTime(current, candidate *time.Time) *time.Time {
	candidate = cloneRouteReportTime(candidate)
	if candidate == nil {
		return cloneRouteReportTime(current)
	}
	if current == nil {
		return candidate
	}
	if candidate.After(current.UTC()) {
		return candidate
	}
	return cloneRouteReportTime(current)
}

func cloneRouteReportTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := normalizeRouteReportTime(*value)
	if normalized.IsZero() {
		return nil
	}
	return &normalized
}

func normalizeRouteReportTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func sortedRouteReportPaths(pathSet map[string]struct{}) []string {
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		if strings.TrimSpace(path) == "" {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func formatRouteReportPaths(paths []string) string {
	if len(paths) == 0 {
		return "`(none)`"
	}
	formatted := make([]string, 0, len(paths))
	for i := range paths {
		formatted = append(formatted, "`"+paths[i]+"`")
	}
	return strings.Join(formatted, ", ")
}

func formatRouteReportTime(value time.Time) string {
	value = normalizeRouteReportTime(value)
	if value.IsZero() {
		return "(none)"
	}
	return value.Format(time.RFC3339)
}

func formatRouteReportTimePtr(value *time.Time) string {
	if value == nil {
		return "(none)"
	}
	return formatRouteReportTime(*value)
}

func fallbackRouteReportValue(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
