package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-stream-ingester/internal/app"
)

func TestParseContinuousObservationCutover(t *testing.T) {
	t.Parallel()

	cutoverAt, err := parseContinuousObservationCutover("2026-04-11T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC), cutoverAt)
}

func TestNextContinuousObservationIntervalUsesFirstHourCadence(t *testing.T) {
	t.Parallel()

	report := app.CommunityShortsContinuousObservationReport{
		GeneratedAt: time.Date(2026, 4, 11, 0, 20, 0, 0, time.UTC),
		Observation: app.CommunityShortsContinuousObservationWindow{
			ObservationStartedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
			ObservationEndsAt:    time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
			ObservedUntil:        time.Date(2026, 4, 11, 0, 20, 0, 0, time.UTC),
		},
	}
	require.Equal(t, 5*time.Minute, nextContinuousObservationInterval(report))
}

func TestNextContinuousObservationIntervalCapsAtWindowEnd(t *testing.T) {
	t.Parallel()

	report := app.CommunityShortsContinuousObservationReport{
		GeneratedAt: time.Date(2026, 4, 11, 23, 58, 0, 0, time.UTC),
		Observation: app.CommunityShortsContinuousObservationWindow{
			ObservationStartedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
			ObservationEndsAt:    time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
			ObservedUntil:        time.Date(2026, 4, 11, 23, 58, 0, 0, time.UTC),
		},
	}
	require.Equal(t, 2*time.Minute, nextContinuousObservationInterval(report))
}
