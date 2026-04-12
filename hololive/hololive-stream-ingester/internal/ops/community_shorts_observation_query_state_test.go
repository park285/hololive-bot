package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type communityShortsObservationWindowRepositoryStub struct {
	findWindow       *domain.YouTubeCommunityShortsObservationWindow
	findErr          error
	findClosedWindow *domain.YouTubeCommunityShortsObservationWindow
	findClosedErr    error
	findClosedCalls  int
}

func (s *communityShortsObservationWindowRepositoryStub) FindCommunityShortsObservationWindow(
	context.Context,
	string,
	time.Time,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.findWindow, nil
}

func (s *communityShortsObservationWindowRepositoryStub) FindClosedCommunityShortsObservationWindow(
	context.Context,
	string,
	time.Time,
	time.Time,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	s.findClosedCalls++
	if s.findClosedErr != nil {
		return nil, s.findClosedErr
	}
	return s.findClosedWindow, nil
}

func TestResolveCommunityShortsObservationQueryStateUsesActiveWindowBeforeClose(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)
	now := windowStart.Add(37 * time.Minute)

	repository := &communityShortsObservationWindowRepositoryStub{
		findWindow: &domain.YouTubeCommunityShortsObservationWindow{
			RuntimeName:          "youtube-scraper",
			BigBangCutoverAt:     windowStart,
			ObservationStartedAt: windowStart,
			ObservationEndedAt:   windowEnd,
		},
	}

	state, err := resolveCommunityShortsObservationQueryState(context.Background(), repository, "youtube-scraper", windowStart, now)
	require.NoError(t, err)
	require.NotNil(t, state.Window)
	require.False(t, state.Finalized)
	require.Equal(t, now, state.EffectiveWindowEnd)
	require.Equal(t, 0, repository.findClosedCalls)
}

func TestResolveCommunityShortsObservationQueryStateFinalizesWindowAfterEnd(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)
	now := windowEnd.Add(2 * time.Minute)
	closedAt := windowEnd
	baselineAt := windowEnd

	repository := &communityShortsObservationWindowRepositoryStub{
		findWindow: &domain.YouTubeCommunityShortsObservationWindow{
			RuntimeName:          "youtube-scraper",
			BigBangCutoverAt:     windowStart,
			ObservationStartedAt: windowStart,
			ObservationEndedAt:   windowEnd,
		},
		findClosedWindow: &domain.YouTubeCommunityShortsObservationWindow{
			RuntimeName:             "youtube-scraper",
			BigBangCutoverAt:        windowStart,
			ObservationStartedAt:    windowStart,
			ObservationEndedAt:      windowEnd,
			ClosedAt:                &closedAt,
			FinalizedPostBaselineAt: &baselineAt,
		},
	}

	state, err := resolveCommunityShortsObservationQueryState(context.Background(), repository, "youtube-scraper", windowStart, now)
	require.NoError(t, err)
	require.NotNil(t, state.Window)
	require.True(t, state.Finalized)
	require.Equal(t, windowEnd, state.EffectiveWindowEnd)
	require.Equal(t, 1, repository.findClosedCalls)
}

func TestResolveCommunityShortsObservationQueryStateReturnsFindClosedError(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)
	repository := &communityShortsObservationWindowRepositoryStub{
		findWindow: &domain.YouTubeCommunityShortsObservationWindow{
			RuntimeName:          "youtube-scraper",
			BigBangCutoverAt:     windowStart,
			ObservationStartedAt: windowStart,
			ObservationEndedAt:   windowEnd,
		},
		findClosedErr: errors.New("boom"),
	}

	_, err := resolveCommunityShortsObservationQueryState(context.Background(), repository, "youtube-scraper", windowStart, windowEnd.Add(time.Minute))
	require.EqualError(t, err, "finalize observation window: boom")
}
