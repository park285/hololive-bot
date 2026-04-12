package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type communityShortsObservationWindowRepository interface {
	FindCommunityShortsObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time) (*domain.YouTubeCommunityShortsObservationWindow, error)
	FindClosedCommunityShortsObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time, now time.Time) (*domain.YouTubeCommunityShortsObservationWindow, error)
}

type communityShortsObservationQueryState struct {
	Window             *domain.YouTubeCommunityShortsObservationWindow
	EffectiveWindowEnd time.Time
	Finalized          bool
}

func resolveCommunityShortsObservationQueryState(
	ctx context.Context,
	repository communityShortsObservationWindowRepository,
	runtimeName string,
	bigBangCutoverAt time.Time,
	now time.Time,
) (communityShortsObservationQueryState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if repository == nil {
		return communityShortsObservationQueryState{}, fmt.Errorf("observation window repository is nil")
	}
	if strings.TrimSpace(runtimeName) == "" {
		return communityShortsObservationQueryState{}, fmt.Errorf("runtime name is empty")
	}
	if bigBangCutoverAt.IsZero() {
		return communityShortsObservationQueryState{}, fmt.Errorf("big-bang cutover at is empty")
	}
	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	window, err := repository.FindCommunityShortsObservationWindow(ctx, strings.TrimSpace(runtimeName), bigBangCutoverAt.UTC())
	if err != nil {
		return communityShortsObservationQueryState{}, fmt.Errorf("load observation window: %w", err)
	}
	if window == nil {
		return communityShortsObservationQueryState{}, nil
	}

	if communityShortsObservationWindowNeedsFinalization(window, now) {
		window, err = repository.FindClosedCommunityShortsObservationWindow(ctx, strings.TrimSpace(runtimeName), bigBangCutoverAt.UTC(), now)
		if err != nil {
			return communityShortsObservationQueryState{}, fmt.Errorf("finalize observation window: %w", err)
		}
		if window == nil {
			return communityShortsObservationQueryState{}, nil
		}
		return communityShortsObservationQueryState{
			Window:             window,
			EffectiveWindowEnd: normalizeCommunityShortsSendCountTime(window.ObservationEndedAt),
			Finalized:          true,
		}, nil
	}

	effectiveWindowEnd := now
	observationEndedAt := normalizeCommunityShortsSendCountTime(window.ObservationEndedAt)
	if observationEndedAt.IsZero() {
		return communityShortsObservationQueryState{}, fmt.Errorf("observation window end is empty")
	}
	if effectiveWindowEnd.After(observationEndedAt) {
		effectiveWindowEnd = observationEndedAt
	}

	return communityShortsObservationQueryState{
		Window:             window,
		EffectiveWindowEnd: effectiveWindowEnd,
		Finalized:          false,
	}, nil
}

func communityShortsObservationWindowNeedsFinalization(
	window *domain.YouTubeCommunityShortsObservationWindow,
	now time.Time,
) bool {
	if window == nil {
		return false
	}
	if window.ClosedAt != nil || window.FinalizedPostBaselineAt != nil {
		return true
	}
	observationEndedAt := normalizeCommunityShortsSendCountTime(window.ObservationEndedAt)
	if observationEndedAt.IsZero() {
		return false
	}
	return !normalizeCommunityShortsSendCountTime(now).Before(observationEndedAt)
}
