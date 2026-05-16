package ingesterruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

type communityShortsObservationWindowWriter interface {
	EnsureCommunityShortsObservationWindow(ctx context.Context, window *domain.YouTubeCommunityShortsObservationWindow) error
}

func (r *StreamIngesterRuntime) ensureCommunityShortsObservationWindow(ctx context.Context) error {
	if r == nil || r.communityShortsObservationWindowWriter == nil || !r.CommunityShortsBigBangPolicy.Enabled() {
		return nil
	}

	deploymentCompletedAt := communityshorts.ObservationNow(r.timeNow)
	window, err := communityshorts.BuildObservationWindow(
		r.runtimeName(),
		r.configVersion(),
		r.CommunityShortsBigBangPolicy,
		deploymentCompletedAt,
	)
	if err != nil {
		return fmt.Errorf("ensure community shorts observation window: build record: %w", err)
	}
	if err := r.communityShortsObservationWindowWriter.EnsureCommunityShortsObservationWindow(ctx, window); err != nil {
		return fmt.Errorf("ensure community shorts observation window: persist record: %w", err)
	}

	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("Community/shorts observation window ensured",
		slog.String("runtime", window.RuntimeName),
		slog.String("app_version", window.AppVersion),
		slog.Time("community_shorts_bigbang_cutover_at", window.BigBangCutoverAt),
		slog.Time("deployment_completed_at", window.DeploymentCompletedAt),
		slog.Time("observation_started_at", window.ObservationStartedAt),
		slog.Time("observation_ended_at", window.ObservationEndedAt),
		slog.Int("community_shorts_bigbang_target_channels", window.TargetChannelCount),
	)

	return nil
}

func (r *StreamIngesterRuntime) configVersion() string {
	if r == nil || r.Config == nil {
		return ""
	}
	return strings.TrimSpace(r.Config.Version)
}
