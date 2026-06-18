package routereport

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

	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

const (
	routeUsageNoRecentPosts          = "no_recent_posts"
	routeUsageNoPathObserved         = "no_path_observed"
	routeUsageNewOnlyVerified        = "new_only_verified"
	routeUsageUnexpectedPathDetected = "unexpected_path_detected"
	routeUsageMixedPathsDetected     = "mixed_paths_detected"
)

type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Runtime     Runtime   `json:"runtime"`
	Summary     Summary   `json:"summary"`
	Channels    []Channel `json:"channels"`
}

type Runtime struct {
	FinalDeliveryOwner              string     `json:"final_delivery_owner"`
	CommunityShortsBigBangEnabled   bool       `json:"community_shorts_bigbang_enabled"`
	CommunityShortsBigBangCutoverAt *time.Time `json:"community_shorts_bigbang_cutover_at,omitempty"`
	ExpectedTelemetryPath           string     `json:"expected_telemetry_path"`
}

type Summary struct {
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

type Channel struct {
	OwnerLabel string  `json:"owner_label"`
	ChannelID  string  `json:"channel_id"`
	Routes     []Route `json:"routes"`
}

type Route struct {
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

type routeKey struct {
	channelID string
	alarmType domain.AlarmType
}

type contentKey struct {
	channelID string
	alarmType domain.AlarmType
	contentID string
}

func Collect(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (Report, error) {
	ctx, logger, now, since, err := normalizeCollectInputs(ctx, appConfig, logger, now, since)
	if err != nil {
		return Report{}, err
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts route verification report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}
	if session == nil || session.Postgres == nil {
		return Report{}, fmt.Errorf("collect community shorts route verification report: session is nil")
	}

	baseline, err := buildBaseline(ctx, appConfig, logger, session, now)
	if err != nil {
		return Report{}, err
	}
	pathUsageRows, sendCountRows, err := loadRows(ctx, session, since)
	if err != nil {
		return Report{}, err
	}

	return Build(&baseline, pathUsageRows, sendCountRows, now, since), nil
}

func normalizeCollectInputs(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (normalizedCtx context.Context, normalizedLogger *slog.Logger, normalizedNow, normalizedSince time.Time, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if appConfig == nil {
		return ctx, logger, now, since, fmt.Errorf("collect community shorts route verification report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	since = shared.NormalizeSendCountTime(since)
	if since.IsZero() {
		return ctx, logger, now, since, fmt.Errorf("collect community shorts route verification report: since is empty")
	}
	if since.After(now) {
		return ctx, logger, now, since, fmt.Errorf("collect community shorts route verification report: since is after now")
	}

	return ctx, logger, now, since, nil
}

func buildBaseline(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	session *shared.OpsSession,
	now time.Time,
) (communityshorts.TargetBaseline, error) {
	memberRepository := sharedproviders.ProvideMemberRepository(session.Postgres, logger)
	members, err := memberRepository.GetAllMembers(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("collect community shorts route verification report: load members: %w", err)
	}

	alarmRepository := sharedalarm.NewRepository(session.Postgres, logger)
	alarms, err := alarmRepository.LoadAll(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("collect community shorts route verification report: load alarms: %w", err)
	}

	channels := communityshorts.BuildOperationalChannelsFromMembers(members)
	baseline, err := communityshorts.BuildTargetBaseline(channels, alarms, appConfig.Ingestion, now)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("collect community shorts route verification report: build baseline: %w", err)
	}
	return baseline, nil
}

func loadRows(
	ctx context.Context,
	session *shared.OpsSession,
	since time.Time,
) (pathUsageRows []outbox.PostDeliveryPathUsage, sendCountRows []outbox.PostSendCount, err error) {
	pathUsageRows, err = session.TelemetryRepository.ListPostDeliveryPathUsageSince(ctx, since)
	if err != nil {
		return nil, nil, fmt.Errorf("collect community shorts route verification report: list delivery path usage: %w", err)
	}

	sendCountRows, err = session.TelemetryRepository.ListPostSendCountsSince(ctx, since)
	if err != nil {
		return nil, nil, fmt.Errorf("collect community shorts route verification report: list send counts: %w", err)
	}
	return pathUsageRows, sendCountRows, nil
}

type postPathClassification struct {
	State         string
	ObservedPaths []string
}

func classifyPostPaths(rows []outbox.PostDeliveryPathUsage) postPathClassification {
	paths := sortedPaths(collectPathSet(rows))
	return postPathClassification{
		State:         resolvePostPathState(paths),
		ObservedPaths: paths,
	}
}

func collectPathSet(rows []outbox.PostDeliveryPathUsage) map[string]struct{} {
	pathSet := make(map[string]struct{})
	for i := range rows {
		path := strings.TrimSpace(rows[i].DeliveryPath)
		if path != "" {
			pathSet[path] = struct{}{}
		}
	}
	return pathSet
}

func resolvePostPathState(paths []string) string {
	switch len(paths) {
	case 0:
		return routeUsageNoPathObserved
	case 1:
		if paths[0] == communityshorts.NewDeliveryPath {
			return routeUsageNewOnlyVerified
		}
		return routeUsageUnexpectedPathDetected
	default:
		return routeUsageMixedPathsDetected
	}
}

func resolveActualUsageState(route *Route) string {
	if route == nil {
		return routeUsageNoRecentPosts
	}
	if route.ObservedPostCount == 0 {
		return routeUsageNoRecentPosts
	}
	if route.MixedPathPostCount > 0 {
		return routeUsageMixedPathsDetected
	}
	if route.UnexpectedPathPostCount > 0 {
		return routeUsageUnexpectedPathDetected
	}
	if route.NoPathPostCount > 0 {
		return routeUsageNoPathObserved
	}
	return routeUsageNewOnlyVerified
}

func laterTime(current, candidate *time.Time) *time.Time {
	candidate = shared.CloneSendCountTime(candidate)
	if candidate == nil {
		return shared.CloneSendCountTime(current)
	}
	if current == nil || candidate.After(current.UTC()) {
		return candidate
	}
	return shared.CloneSendCountTime(current)
}

func sortedPaths(pathSet map[string]struct{}) []string {
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}
