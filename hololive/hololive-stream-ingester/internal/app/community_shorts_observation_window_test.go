package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type observationWindowWriterStub struct {
	calls []*domain.YouTubeCommunityShortsObservationWindow
	err   error
}

func (s *observationWindowWriterStub) EnsureCommunityShortsObservationWindow(
	_ context.Context,
	window *domain.YouTubeCommunityShortsObservationWindow,
) error {
	if s.err != nil {
		return s.err
	}
	cloned := *window
	s.calls = append(s.calls, &cloned)
	return nil
}

func TestStreamIngesterRuntimeEnsureCommunityShortsObservationWindowWrites24HourWindow(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	writer := &observationWindowWriterStub{}
	runtime := &StreamIngesterRuntime{
		RuntimeName: youtubeScraperRuntimeName,
		Config: &config.Config{
			Version: "2.0.0",
		},
		CommunityShortsBigBangPolicy: communityShortsBigBangPolicy{
			cutoverAt: cutoverAt,
			targetChannelIDs: map[string]struct{}{
				"UC_1": {},
				"UC_2": {},
			},
		},
		communityShortsObservationWindowWriter: writer,
		timeNow: func() time.Time {
			return deploymentCompletedAt
		},
	}

	require.NoError(t, runtime.ensureCommunityShortsObservationWindow(context.Background()))
	require.Len(t, writer.calls, 1)

	record := writer.calls[0]
	require.Equal(t, youtubeScraperRuntimeName, record.RuntimeName)
	require.Equal(t, cutoverAt, record.BigBangCutoverAt.UTC())
	require.Equal(t, "2.0.0", record.AppVersion)
	require.Equal(t, 2, record.TargetChannelCount)
	require.Equal(t, deploymentCompletedAt, record.DeploymentCompletedAt.UTC())
	require.Equal(t, deploymentCompletedAt, record.ObservationStartedAt.UTC())
	require.Equal(t, deploymentCompletedAt.Add(24*time.Hour), record.ObservationEndedAt.UTC())
}

func TestStreamIngesterRuntimeEnsureCommunityShortsObservationWindowSkipsWhenPolicyInactive(t *testing.T) {
	t.Parallel()

	writer := &observationWindowWriterStub{}
	runtime := &StreamIngesterRuntime{
		RuntimeName:                            youtubeScraperRuntimeName,
		Config:                                 &config.Config{Version: "2.0.0"},
		communityShortsObservationWindowWriter: writer,
	}

	require.NoError(t, runtime.ensureCommunityShortsObservationWindow(context.Background()))
	require.Empty(t, writer.calls)
}

func TestStreamIngesterRuntimeEnsureCommunityShortsObservationWindowReturnsWriterError(t *testing.T) {
	t.Parallel()

	writer := &observationWindowWriterStub{err: errors.New("write failed")}
	runtime := &StreamIngesterRuntime{
		RuntimeName: youtubeScraperRuntimeName,
		Config: &config.Config{
			Version: "2.0.0",
		},
		CommunityShortsBigBangPolicy: communityShortsBigBangPolicy{
			cutoverAt: time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC),
			targetChannelIDs: map[string]struct{}{
				"UC_1": {},
			},
		},
		communityShortsObservationWindowWriter: writer,
		timeNow: func() time.Time {
			return time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
		},
	}

	err := runtime.ensureCommunityShortsObservationWindow(context.Background())
	require.ErrorContains(t, err, "persist record")
}
