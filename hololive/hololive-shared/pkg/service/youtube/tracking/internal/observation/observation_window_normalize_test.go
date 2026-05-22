package observation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestValidateCommunityShortsObservationWindowTiming_ValidWindow(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	require.NoError(t, validateCommunityShortsObservationWindowTiming(start, start, end))
}

func TestValidateCommunityShortsObservationWindowTiming_EndNotAfterStart(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	require.ErrorContains(t,
		validateCommunityShortsObservationWindowTiming(start, start, start),
		"observation ended at must be after observation started at",
	)
}

func TestValidateCommunityShortsObservationWindowTiming_WrongDuration(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	end := start.Add(12 * time.Hour)
	require.ErrorContains(t,
		validateCommunityShortsObservationWindowTiming(start, start, end),
		"observation window duration must be exactly 24h",
	)
}

func TestValidateCommunityShortsObservationWindowTiming_DeploymentMismatch(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	deploy := start.Add(-time.Minute)
	require.ErrorContains(t,
		validateCommunityShortsObservationWindowTiming(deploy, start, end),
		"deployment completed at must match observation started at",
	)
}

func TestNormalizeCommunityShortsObservationWindowClosedAt_NilReturnsNil(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	result, err := normalizeCommunityShortsObservationWindowClosedAt(nil, end)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestNormalizeCommunityShortsObservationWindowClosedAt_MatchingReturnsValue(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	closedAt := end
	result, err := normalizeCommunityShortsObservationWindowClosedAt(&closedAt, end)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, end, result.UTC())
}

func TestNormalizeCommunityShortsObservationWindowClosedAt_MismatchReturnsError(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	closedAt := end.Add(time.Minute)
	_, err := normalizeCommunityShortsObservationWindowClosedAt(&closedAt, end)
	require.ErrorContains(t, err, "closed at must match observation ended at")
}

func TestCommunityShortsObservationWindowClosed_VariousCases(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	closedAt := end

	require.False(t, communityShortsObservationWindowClosed(nil))
	require.False(t, communityShortsObservationWindowClosed(&domain.YouTubeCommunityShortsObservationWindow{
		ObservationEndedAt: end,
		ClosedAt:           nil,
	}))

	zeroTime := time.Time{}
	require.False(t, communityShortsObservationWindowClosed(&domain.YouTubeCommunityShortsObservationWindow{
		ObservationEndedAt: end,
		ClosedAt:           &zeroTime,
	}))

	mismatchTime := end.Add(time.Minute)
	require.False(t, communityShortsObservationWindowClosed(&domain.YouTubeCommunityShortsObservationWindow{
		ObservationEndedAt: end,
		ClosedAt:           &mismatchTime,
	}))

	require.True(t, communityShortsObservationWindowClosed(&domain.YouTubeCommunityShortsObservationWindow{
		ObservationEndedAt: end,
		ClosedAt:           &closedAt,
	}))
}

func TestValidateCommunityShortsObservationWindowFinalization_ValidCases(t *testing.T) {
	t.Parallel()

	closedAt := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	finalizedAt := closedAt

	require.NoError(t, validateCommunityShortsObservationWindowFinalization(
		&domain.YouTubeCommunityShortsObservationWindow{FinalizedPostCount: 0},
		&closedAt, nil,
	))

	require.NoError(t, validateCommunityShortsObservationWindowFinalization(
		&domain.YouTubeCommunityShortsObservationWindow{FinalizedPostCount: 5},
		&closedAt, &finalizedAt,
	))
}

func TestValidateCommunityShortsObservationWindowFinalization_NegativeCount(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, validateCommunityShortsObservationWindowFinalization(
		&domain.YouTubeCommunityShortsObservationWindow{FinalizedPostCount: -1},
		nil, nil,
	), "finalized post count must not be negative")
}

func TestValidateCommunityShortsObservationWindowFinalization_FinalizedWithoutClosed(t *testing.T) {
	t.Parallel()

	finalizedAt := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	require.ErrorContains(t, validateCommunityShortsObservationWindowFinalization(
		&domain.YouTubeCommunityShortsObservationWindow{FinalizedPostCount: 0},
		nil, &finalizedAt,
	), "finalized post baseline at requires closed at")
}

func TestValidateCommunityShortsObservationWindowFinalization_CountWithoutFinalized(t *testing.T) {
	t.Parallel()

	closedAt := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	require.ErrorContains(t, validateCommunityShortsObservationWindowFinalization(
		&domain.YouTubeCommunityShortsObservationWindow{FinalizedPostCount: 3},
		&closedAt, nil,
	), "finalized post count requires finalized post baseline at")
}

func TestNormalizeCommunityShortsObservationFinalizedAt_NilReturnsNil(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	result, err := normalizeCommunityShortsObservationFinalizedAt(nil, end)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestNormalizeCommunityShortsObservationFinalizedAt_MismatchReturnsError(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	wrong := end.Add(time.Minute)
	_, err := normalizeCommunityShortsObservationFinalizedAt(&wrong, end)
	require.ErrorContains(t, err, "finalized post baseline at must match observation ended at")
}

func TestNormalizeCommunityShortsObservationWindowText_RejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := normalizeCommunityShortsObservationWindowText(&domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName: "",
		AppVersion:  "2.0.0",
	})
	require.ErrorContains(t, err, "runtime name is empty")

	_, err = normalizeCommunityShortsObservationWindowText(&domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName: "producer",
		AppVersion:  "",
	})
	require.ErrorContains(t, err, "app version is empty")
}

func TestNormalizeCommunityShortsObservationWindowText_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	result, err := normalizeCommunityShortsObservationWindowText(&domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName: "  youtube-producer  ",
		AppVersion:  "  2.0.0  ",
	})
	require.NoError(t, err)
	require.Equal(t, "youtube-producer", result.runtimeName)
	require.Equal(t, "2.0.0", result.appVersion)
}
