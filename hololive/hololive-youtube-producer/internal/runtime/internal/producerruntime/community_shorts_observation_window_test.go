package producerruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
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

func TestYouTubeProducerRuntimeEnsureCommunityShortsObservationWindowWrites24HourWindow(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	policy, err := communityshorts.BuildPolicy(config.IngestionConfig{
		CommunityShortsBigBangEnabled:   true,
		CommunityShortsBigBangCutoverAt: cutoverAt,
	}, []communityshorts.OperationalChannel{
		{OwnerLabel: "A", ChannelID: "UC_1", Enabled: true},
		{OwnerLabel: "B", ChannelID: "UC_2", Enabled: true},
	})
	require.NoError(t, err)
	writer := &observationWindowWriterStub{}
	runtime := &YouTubeProducerRuntime{
		RuntimeName: youtubeProducerRuntimeName,
		Config: &config.Config{
			Version: "2.0.0",
		},
		CommunityShortsBigBangPolicy:           policy,
		communityShortsObservationWindowWriter: writer,
		timeNow: func() time.Time {
			return deploymentCompletedAt
		},
	}

	require.NoError(t, runtime.ensureCommunityShortsObservationWindow(context.Background()))
	require.Len(t, writer.calls, 1)

	record := writer.calls[0]
	require.Equal(t, youtubeProducerRuntimeName, record.RuntimeName)
	require.Equal(t, cutoverAt, record.BigBangCutoverAt.UTC())
	require.Equal(t, "2.0.0", record.AppVersion)
	require.Equal(t, 2, record.TargetChannelCount)
	require.Equal(t, deploymentCompletedAt, record.DeploymentCompletedAt.UTC())
	require.Equal(t, deploymentCompletedAt, record.ObservationStartedAt.UTC())
	require.Equal(t, deploymentCompletedAt.Add(24*time.Hour), record.ObservationEndedAt.UTC())
}

func TestYouTubeProducerRuntimeEnsureCommunityShortsObservationWindowSkipsWhenPolicyInactive(t *testing.T) {
	t.Parallel()

	writer := &observationWindowWriterStub{}
	runtime := &YouTubeProducerRuntime{
		RuntimeName:                            youtubeProducerRuntimeName,
		Config:                                 &config.Config{Version: "2.0.0"},
		communityShortsObservationWindowWriter: writer,
	}

	require.NoError(t, runtime.ensureCommunityShortsObservationWindow(context.Background()))
	require.Empty(t, writer.calls)
}

func TestYouTubeProducerRuntimeEnsureCommunityShortsObservationWindowReturnsWriterError(t *testing.T) {
	t.Parallel()

	writer := &observationWindowWriterStub{err: errors.New("write failed")}
	policy, err := communityshorts.BuildPolicy(config.IngestionConfig{
		CommunityShortsBigBangEnabled:   true,
		CommunityShortsBigBangCutoverAt: time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC),
	}, []communityshorts.OperationalChannel{{OwnerLabel: "A", ChannelID: "UC_1", Enabled: true}})
	require.NoError(t, err)
	runtime := &YouTubeProducerRuntime{
		RuntimeName: youtubeProducerRuntimeName,
		Config: &config.Config{
			Version: "2.0.0",
		},
		CommunityShortsBigBangPolicy:           policy,
		communityShortsObservationWindowWriter: writer,
		timeNow: func() time.Time {
			return time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
		},
	}

	err = runtime.ensureCommunityShortsObservationWindow(context.Background())
	require.ErrorContains(t, err, "persist record")
}
