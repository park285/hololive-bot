package ops

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	runtimeapp "github.com/kapu/hololive-stream-ingester/internal/runtime"
)

type CommunityShortsTargetBaseline = runtimeapp.CommunityShortsTargetBaseline
type CommunityShortsTargetBaselineRuntime = runtimeapp.CommunityShortsTargetBaselineRuntime
type CommunityShortsTargetBaselineSources = runtimeapp.CommunityShortsTargetBaselineSources
type CommunityShortsTargetBaselinePath = runtimeapp.CommunityShortsTargetBaselinePath
type CommunityShortsTargetBaselineChannel = runtimeapp.CommunityShortsTargetBaselineChannel
type CommunityShortsTargetBaselineChannelRoute = runtimeapp.CommunityShortsTargetBaselineChannelRoute

func CollectCommunityShortsTargetBaseline(ctx context.Context, cfg *config.Config, logger *slog.Logger) (CommunityShortsTargetBaseline, error) {
	return runtimeapp.CollectCommunityShortsTargetBaseline(ctx, cfg, logger)
}

func buildCommunityShortsOperationalChannelsFromMembers(members []*domain.Member) []runtimeapp.CommunityShortsOperationalChannel {
	return runtimeapp.BuildCommunityShortsOperationalChannelsFromMembers(members)
}

func buildCommunityShortsTargetBaseline(
	channels []runtimeapp.CommunityShortsOperationalChannel,
	alarms []*domain.Alarm,
	ingestionCfg config.IngestionConfig,
	generatedAt time.Time,
) (CommunityShortsTargetBaseline, error) {
	return runtimeapp.BuildCommunityShortsTargetBaseline(channels, alarms, ingestionCfg, generatedAt)
}

func communityShortsRouteForType(
	routes []CommunityShortsTargetBaselineChannelRoute,
	alarmType domain.AlarmType,
) (CommunityShortsTargetBaselineChannelRoute, bool) {
	return runtimeapp.CommunityShortsRouteForType(routes, alarmType)
}
