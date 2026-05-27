package shared

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type ObservationWindowRepository interface {
	FindCommunityShortsObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time) (*domain.YouTubeCommunityShortsObservationWindow, error)
	FindClosedCommunityShortsObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time, now time.Time) (*domain.YouTubeCommunityShortsObservationWindow, error)
}

type ObservationQueryState struct {
	Window             *domain.YouTubeCommunityShortsObservationWindow
	EffectiveWindowEnd time.Time
	Finalized          bool
}

type ObservationQueryRequest struct {
	Context          context.Context
	RuntimeName      string
	BigBangCutoverAt time.Time
	Now              time.Time
}

func ResolveObservationQueryState(
	ctx context.Context,
	repository ObservationWindowRepository,
	runtimeName string,
	bigBangCutoverAt time.Time,
	now time.Time,
) (ObservationQueryState, error) {
	request, err := newObservationQueryRequest(ctx, repository, runtimeName, bigBangCutoverAt, now)
	if err != nil {
		return ObservationQueryState{}, err
	}

	window, err := repository.FindCommunityShortsObservationWindow(request.Context, request.RuntimeName, request.BigBangCutoverAt)
	if err != nil {
		return ObservationQueryState{}, fmt.Errorf("load observation window: %w", err)
	}
	if window == nil {
		return ObservationQueryState{}, nil
	}

	if observationWindowNeedsFinalization(window, request.Now) {
		return resolveFinalizedObservationQueryState(repository, request)
	}
	return activeObservationQueryState(window, request.Now)
}

func newObservationQueryRequest(
	ctx context.Context,
	repository ObservationWindowRepository,
	runtimeName string,
	bigBangCutoverAt time.Time,
	now time.Time,
) (ObservationQueryRequest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runtimeName = strings.TrimSpace(runtimeName)
	if err := validateObservationQueryInputs(repository, runtimeName, bigBangCutoverAt); err != nil {
		return ObservationQueryRequest{}, err
	}
	now = NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return ObservationQueryRequest{
		Context:          ctx,
		RuntimeName:      runtimeName,
		BigBangCutoverAt: bigBangCutoverAt.UTC(),
		Now:              now,
	}, nil
}

func validateObservationQueryInputs(
	repository ObservationWindowRepository,
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

func resolveFinalizedObservationQueryState(
	repository ObservationWindowRepository,
	request ObservationQueryRequest,
) (ObservationQueryState, error) {
	window, err := repository.FindClosedCommunityShortsObservationWindow(
		request.Context,
		request.RuntimeName,
		request.BigBangCutoverAt,
		request.Now,
	)
	if err != nil {
		return ObservationQueryState{}, fmt.Errorf("finalize observation window: %w", err)
	}
	if window == nil {
		return ObservationQueryState{}, nil
	}
	return ObservationQueryState{
		Window:             window,
		EffectiveWindowEnd: NormalizeSendCountTime(window.ObservationEndedAt),
		Finalized:          true,
	}, nil
}

func activeObservationQueryState(
	window *domain.YouTubeCommunityShortsObservationWindow,
	now time.Time,
) (ObservationQueryState, error) {
	effectiveWindowEnd := now
	observationEndedAt := NormalizeSendCountTime(window.ObservationEndedAt)
	if observationEndedAt.IsZero() {
		return ObservationQueryState{}, fmt.Errorf("observation window end is empty")
	}
	if effectiveWindowEnd.After(observationEndedAt) {
		effectiveWindowEnd = observationEndedAt
	}

	return ObservationQueryState{
		Window:             window,
		EffectiveWindowEnd: effectiveWindowEnd,
		Finalized:          false,
	}, nil
}

func observationWindowNeedsFinalization(
	window *domain.YouTubeCommunityShortsObservationWindow,
	now time.Time,
) bool {
	if window == nil {
		return false
	}
	if window.ClosedAt != nil || window.FinalizedPostBaselineAt != nil {
		return true
	}
	observationEndedAt := NormalizeSendCountTime(window.ObservationEndedAt)
	if observationEndedAt.IsZero() {
		return false
	}
	return !NormalizeSendCountTime(now).Before(observationEndedAt)
}
