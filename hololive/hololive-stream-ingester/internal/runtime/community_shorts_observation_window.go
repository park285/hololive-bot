package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const communityShortsObservationWindowDuration = 24 * time.Hour

type communityShortsObservationWindowWriter interface {
	EnsureCommunityShortsObservationWindow(ctx context.Context, window *domain.YouTubeCommunityShortsObservationWindow) error
}

func (r *StreamIngesterRuntime) ensureCommunityShortsObservationWindow(ctx context.Context) error {
	if r == nil || r.communityShortsObservationWindowWriter == nil || !r.CommunityShortsBigBangPolicy.Enabled() {
		return nil
	}

	deploymentCompletedAt := communityShortsObservationNow(r.timeNow)
	window, err := buildCommunityShortsObservationWindow(
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

func buildCommunityShortsObservationWindow(
	runtimeName string,
	appVersion string,
	policy communityShortsBigBangPolicy,
	deploymentCompletedAt time.Time,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	if !policy.Enabled() {
		return nil, fmt.Errorf("policy is not enabled")
	}

	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return nil, fmt.Errorf("runtime name is empty")
	}

	normalizedAppVersion := strings.TrimSpace(appVersion)
	if normalizedAppVersion == "" {
		return nil, fmt.Errorf("app version is empty")
	}

	deploymentCompletedAt = deploymentCompletedAt.UTC()
	if deploymentCompletedAt.IsZero() {
		return nil, fmt.Errorf("deployment completed at is empty")
	}

	if policy.TargetChannelCount() <= 0 {
		return nil, fmt.Errorf("target channel count must be greater than zero")
	}

	return &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           normalizedRuntimeName,
		BigBangCutoverAt:      policy.CutoverAt(),
		AppVersion:            normalizedAppVersion,
		TargetChannelCount:    policy.TargetChannelCount(),
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    deploymentCompletedAt.Add(communityShortsObservationWindowDuration),
	}, nil
}

func communityShortsObservationNow(nowFn func() time.Time) time.Time {
	if nowFn == nil {
		return time.Now().UTC()
	}
	now := nowFn()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func (r *StreamIngesterRuntime) configVersion() string {
	if r == nil || r.Config == nil {
		return ""
	}
	return strings.TrimSpace(r.Config.Version)
}
