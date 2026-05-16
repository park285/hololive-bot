package reports

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

type communityShortsObservationQueryRequest struct {
	Context          context.Context
	RuntimeName      string
	BigBangCutoverAt time.Time
	Now              time.Time
}

func resolveCommunityShortsObservationQueryState(
	ctx context.Context,
	repository communityShortsObservationWindowRepository,
	runtimeName string,
	bigBangCutoverAt time.Time,
	now time.Time,
) (communityShortsObservationQueryState, error) {
	request, err := newCommunityShortsObservationQueryRequest(ctx, repository, runtimeName, bigBangCutoverAt, now)
	if err != nil {
		return communityShortsObservationQueryState{}, err
	}

	window, err := repository.FindCommunityShortsObservationWindow(request.Context, request.RuntimeName, request.BigBangCutoverAt)
	if err != nil {
		return communityShortsObservationQueryState{}, fmt.Errorf("load observation window: %w", err)
	}
	if window == nil {
		return communityShortsObservationQueryState{}, nil
	}

	if communityShortsObservationWindowNeedsFinalization(window, request.Now) {
		return resolveFinalizedCommunityShortsObservationQueryState(repository, request)
	}
	return activeCommunityShortsObservationQueryState(window, request.Now)
}

func newCommunityShortsObservationQueryRequest(
	ctx context.Context,
	repository communityShortsObservationWindowRepository,
	runtimeName string,
	bigBangCutoverAt time.Time,
	now time.Time,
) (communityShortsObservationQueryRequest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runtimeName = strings.TrimSpace(runtimeName)
	if err := validateCommunityShortsObservationQueryInputs(repository, runtimeName, bigBangCutoverAt); err != nil {
		return communityShortsObservationQueryRequest{}, err
	}
	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return communityShortsObservationQueryRequest{
		Context:          ctx,
		RuntimeName:      runtimeName,
		BigBangCutoverAt: bigBangCutoverAt.UTC(),
		Now:              now,
	}, nil
}

func validateCommunityShortsObservationQueryInputs(
	repository communityShortsObservationWindowRepository,
	runtimeName string,
	bigBangCutoverAt time.Time,
) error {
	if repository == nil {
		return fmt.Errorf("observation window repository is nil")
	}
	if runtimeName == "" {
		return fmt.Errorf("runtime name is empty")
	}
	if bigBangCutoverAt.IsZero() {
		return fmt.Errorf("big-bang cutover at is empty")
	}
	return nil
}

func resolveFinalizedCommunityShortsObservationQueryState(
	repository communityShortsObservationWindowRepository,
	request communityShortsObservationQueryRequest,
) (communityShortsObservationQueryState, error) {
	window, err := repository.FindClosedCommunityShortsObservationWindow(
		request.Context,
		request.RuntimeName,
		request.BigBangCutoverAt,
		request.Now,
	)
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

func activeCommunityShortsObservationQueryState(
	window *domain.YouTubeCommunityShortsObservationWindow,
	now time.Time,
) (communityShortsObservationQueryState, error) {
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
