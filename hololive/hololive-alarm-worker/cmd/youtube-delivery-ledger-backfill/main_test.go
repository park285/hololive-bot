package main

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/backfill"
)

func TestBackfillOptionsRequirePairedCoverageEvidence(t *testing.T) {
	tests := map[string][]string{
		"timestamp without confirmation": {"--legacy-coverage-start-at=2026-08-01T00:00:00Z"},
		"confirmation without timestamp": {"--confirm-historical-coverage"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseBackfillOptions(args, io.Discard)
			require.ErrorIs(t, err, backfill.ErrCoverageConfirmationRequired)
		})
	}
}

func TestBackfillOptionsParseExplicitCoverageEvidence(t *testing.T) {
	options, err := parseBackfillOptions([]string{
		"--batch-size=25",
		"--legacy-coverage-start-at=2026-08-01T09:00:00+09:00",
		"--confirm-historical-coverage",
	}, io.Discard)
	require.NoError(t, err)
	require.Equal(t, 25, options.BatchSize)
	require.True(t, options.HistoricalCoverageChecked)
	require.NotNil(t, options.LegacyCoverageStartAt)
	require.True(t, options.LegacyCoverageStartAt.Equal(time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)))
}

func TestBackfillOptionsRejectInvalidCoverageTimestamp(t *testing.T) {
	_, err := parseBackfillOptions([]string{
		"--legacy-coverage-start-at=not-a-timestamp",
		"--confirm-historical-coverage",
	}, io.Discard)
	require.Error(t, err)
	require.NotErrorIs(t, err, backfill.ErrCoverageConfirmationRequired)
}

func TestBackfillHelpReturnsSuccess(t *testing.T) {
	require.Zero(t, run(t.Context(), []string{"--help"}, io.Discard))
}
