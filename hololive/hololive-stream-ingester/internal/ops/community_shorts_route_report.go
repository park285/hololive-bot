package ops

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

	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
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
	ctx, logger, now, since, err := normalizeCommunityShortsRouteReportInputs(ctx, cfg, logger, now, since)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, err
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, cfg, logger)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	if session == nil || session.postgres == nil {
		return CommunityShortsRouteVerificationReport{}, fmt.Errorf("collect community shorts route verification report: session is nil")
	}

	baseline, err := buildCommunityShortsRouteReportBaseline(ctx, cfg, logger, session, now)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, err
	}

	pathUsageRows, sendCountRows, err := loadCommunityShortsRouteReportRows(ctx, session, since)
	if err != nil {
		return CommunityShortsRouteVerificationReport{}, err
	}

	return BuildCommunityShortsRouteVerificationReport(baseline, pathUsageRows, sendCountRows, now, since), nil
}

func normalizeCommunityShortsRouteReportInputs(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (context.Context, *slog.Logger, time.Time, time.Time, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return ctx, logger, now, since, fmt.Errorf("collect community shorts route verification report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if since.IsZero() {
		return ctx, logger, now, since, fmt.Errorf("collect community shorts route verification report: since is empty")
	}
	since = since.UTC()
	if since.After(now) {
		return ctx, logger, now, since, fmt.Errorf("collect community shorts route verification report: since is after now")
	}

	return ctx, logger, now, since, nil
}

func buildCommunityShortsRouteReportBaseline(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	session *communityShortsOpsSession,
	now time.Time,
) (communityshorts.TargetBaseline, error) {
	memberRepository := sharedproviders.ProvideMemberRepository(session.postgres, logger)
	members, err := memberRepository.GetAllMembers(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("collect community shorts route verification report: load members: %w", err)
	}

	alarmRepository := sharedalarm.NewRepository(session.postgres, logger)
	alarms, err := alarmRepository.LoadAll(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("collect community shorts route verification report: load alarms: %w", err)
	}

	channels := communityshorts.BuildOperationalChannelsFromMembers(members)
	baseline, err := communityshorts.BuildTargetBaseline(channels, alarms, cfg.Ingestion, now)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("collect community shorts route verification report: build baseline: %w", err)
	}

	return baseline, nil
}

func loadCommunityShortsRouteReportRows(
	ctx context.Context,
	session *communityShortsOpsSession,
	since time.Time,
) ([]outbox.PostDeliveryPathUsage, []outbox.PostSendCount, error) {
	pathUsageRows, err := session.telemetryRepo.ListPostDeliveryPathUsageSince(ctx, since)
	if err != nil {
		return nil, nil, fmt.Errorf("collect community shorts route verification report: list delivery path usage: %w", err)
	}

	sendCountRows, err := session.telemetryRepo.ListPostSendCountsSince(ctx, since)
	if err != nil {
		return nil, nil, fmt.Errorf("collect community shorts route verification report: list send counts: %w", err)
	}

	return pathUsageRows, sendCountRows, nil
}

type communityShortsPostPathClassification struct {
	State         string
	ObservedPaths []string
}

func classifyCommunityShortsPostPaths(rows []outbox.PostDeliveryPathUsage) communityShortsPostPathClassification {
	paths := sortedRouteReportPaths(collectCommunityShortsPostPathSet(rows))
	return communityShortsPostPathClassification{
		State:         resolveCommunityShortsPostPathState(paths),
		ObservedPaths: paths,
	}
}

func collectCommunityShortsPostPathSet(rows []outbox.PostDeliveryPathUsage) map[string]struct{} {
	pathSet := make(map[string]struct{})
	for i := range rows {
		path := strings.TrimSpace(rows[i].DeliveryPath)
		if path == "" {
			continue
		}
		pathSet[path] = struct{}{}
	}
	return pathSet
}

func resolveCommunityShortsPostPathState(paths []string) string {
	switch len(paths) {
	case 0:
		return communityShortsRouteUsageNoPathObserved
	case 1:
		if paths[0] == communityshorts.NewDeliveryPath {
			return communityShortsRouteUsageNewOnlyVerified
		}
		return communityShortsRouteUsageUnexpectedPathDetected
	default:
		return communityShortsRouteUsageMixedPathsDetected
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
