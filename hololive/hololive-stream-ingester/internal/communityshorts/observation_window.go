package communityshorts

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const ObservationWindowDuration = 24 * time.Hour

func BuildObservationWindow(
	runtimeName string,
	appVersion string,
	policy Policy,
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
		ObservationEndedAt:    deploymentCompletedAt.Add(ObservationWindowDuration),
	}, nil
}

func ObservationNow(nowFn func() time.Time) time.Time {
	if nowFn == nil {
		return time.Now().UTC()
	}
	now := nowFn()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}
