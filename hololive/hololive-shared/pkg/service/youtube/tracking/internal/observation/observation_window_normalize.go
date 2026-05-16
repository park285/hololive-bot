package observation

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type normalizedObservationWindowText struct {
	runtimeName string
	appVersion  string
}

func normalizeCommunityShortsObservationWindowText(
	window *domain.YouTubeCommunityShortsObservationWindow,
) (normalizedObservationWindowText, error) {
	runtimeName := strings.TrimSpace(window.RuntimeName)
	if runtimeName == "" {
		return normalizedObservationWindowText{}, fmt.Errorf("runtime name is empty")
	}

	appVersion := strings.TrimSpace(window.AppVersion)
	if appVersion == "" {
		return normalizedObservationWindowText{}, fmt.Errorf("app version is empty")
	}

	return normalizedObservationWindowText{
		runtimeName: runtimeName,
		appVersion:  appVersion,
	}, nil
}

type normalizedObservationWindowTimes struct {
	bigBangCutoverAt      time.Time
	deploymentCompletedAt time.Time
	observationStartedAt  time.Time
	observationEndedAt    time.Time
}

func normalizeCommunityShortsObservationWindowTimes(
	window *domain.YouTubeCommunityShortsObservationWindow,
) (normalizedObservationWindowTimes, error) {
	bigBangCutoverAt, err := normalizeRequiredObservationWindowTime(window.BigBangCutoverAt, "big-bang cutover at")
	if err != nil {
		return normalizedObservationWindowTimes{}, err
	}
	deploymentCompletedAt, err := normalizeRequiredObservationWindowTime(window.DeploymentCompletedAt, "deployment completed at")
	if err != nil {
		return normalizedObservationWindowTimes{}, err
	}
	observationStartedAt, err := normalizeRequiredObservationWindowTime(window.ObservationStartedAt, "observation started at")
	if err != nil {
		return normalizedObservationWindowTimes{}, err
	}
	observationEndedAt, err := normalizeRequiredObservationWindowTime(window.ObservationEndedAt, "observation ended at")
	if err != nil {
		return normalizedObservationWindowTimes{}, err
	}

	return normalizedObservationWindowTimes{
		bigBangCutoverAt:      bigBangCutoverAt,
		deploymentCompletedAt: deploymentCompletedAt,
		observationStartedAt:  observationStartedAt,
		observationEndedAt:    observationEndedAt,
	}, nil
}

func normalizeRequiredObservationWindowTime(value time.Time, fieldName string) (time.Time, error) {
	normalizedValue := yttimestamp.Normalize(value)
	if normalizedValue.IsZero() {
		return time.Time{}, fmt.Errorf("%s is empty", fieldName)
	}
	return normalizedValue, nil
}

func validateCommunityShortsObservationWindowTiming(
	deploymentCompletedAt time.Time,
	observationStartedAt time.Time,
	observationEndedAt time.Time,
) error {
	if !observationEndedAt.After(observationStartedAt) {
		return fmt.Errorf("observation ended at must be after observation started at")
	}
	if observationEndedAt.Sub(observationStartedAt) != communityShortsObservationWindowDuration {
		return fmt.Errorf("observation window duration must be exactly 24h")
	}
	if !deploymentCompletedAt.Equal(observationStartedAt) {
		return fmt.Errorf("deployment completed at must match observation started at")
	}
	return nil
}

func validateCommunityShortsObservationWindowFinalization(
	window *domain.YouTubeCommunityShortsObservationWindow,
	closedAt *time.Time,
	finalizedPostBaselineAt *time.Time,
) error {
	if window.FinalizedPostCount < 0 {
		return fmt.Errorf("finalized post count must not be negative")
	}
	if finalizedPostBaselineAt != nil && closedAt == nil {
		return fmt.Errorf("finalized post baseline at requires closed at")
	}
	if finalizedPostBaselineAt == nil && window.FinalizedPostCount != 0 {
		return fmt.Errorf("finalized post count requires finalized post baseline at")
	}
	return nil
}

func normalizeCommunityShortsObservationWindowClosedAt(
	closedAt *time.Time,
	observationEndedAt time.Time,
) (*time.Time, error) {
	if closedAt == nil {
		return nil, nil
	}

	normalizedClosedAt := yttimestamp.Normalize(*closedAt)
	if normalizedClosedAt.IsZero() {
		return nil, nil
	}
	if !normalizedClosedAt.Equal(observationEndedAt) {
		return nil, fmt.Errorf("closed at must match observation ended at")
	}
	return &normalizedClosedAt, nil
}

func normalizeCommunityShortsObservationFinalizedAt(
	finalizedAt *time.Time,
	observationEndedAt time.Time,
) (*time.Time, error) {
	if finalizedAt == nil {
		return nil, nil
	}

	normalizedFinalizedAt := yttimestamp.Normalize(*finalizedAt)
	if normalizedFinalizedAt.IsZero() {
		return nil, nil
	}
	if !normalizedFinalizedAt.Equal(observationEndedAt) {
		return nil, fmt.Errorf("finalized post baseline at must match observation ended at")
	}
	return &normalizedFinalizedAt, nil
}

func communityShortsObservationWindowClosed(window *domain.YouTubeCommunityShortsObservationWindow) bool {
	if window == nil || window.ClosedAt == nil || window.ClosedAt.IsZero() {
		return false
	}
	return window.ClosedAt.UTC().Equal(window.ObservationEndedAt.UTC())
}
