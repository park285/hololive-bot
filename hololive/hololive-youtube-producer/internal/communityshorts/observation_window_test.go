package communityshorts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildObservationWindow(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	policy, err := BuildPolicy(config.IngestionConfig{
		CommunityShortsBigBangCutoverAt: cutoverAt,
	}, []OperationalChannel{{OwnerLabel: "A", ChannelID: "UC_1", Enabled: true}, {OwnerLabel: "B", ChannelID: "UC_2", Enabled: true}})
	require.NoError(t, err)

	record, err := BuildObservationWindow(RuntimeOwnerYouTubeProducer, "2.0.0", policy, deploymentCompletedAt)
	require.NoError(t, err)
	require.Equal(t, RuntimeOwnerYouTubeProducer, record.RuntimeName)
	require.Equal(t, cutoverAt, record.BigBangCutoverAt.UTC())
	require.Equal(t, "2.0.0", record.AppVersion)
	require.Equal(t, 2, record.TargetChannelCount)
	require.Equal(t, deploymentCompletedAt.Add(ObservationWindowDuration), record.ObservationEndedAt.UTC())
}

func TestObservationNow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 10, 1, 15, 0, 0, time.FixedZone("KST", 9*60*60))
	require.Equal(t, now.UTC(), ObservationNow(func() time.Time { return now }))
}
