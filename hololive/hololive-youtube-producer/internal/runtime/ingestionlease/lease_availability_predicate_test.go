package ingestionlease

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func photoSyncGuardTreatsAsAvailable(result JobClaimResult) bool {
	return !(result != JobClaimAcquired)
}

func TestIngestionLeaseAvailabilityTruthTablePinsCurrentSites(t *testing.T) {
	t.Parallel()

	cases := []struct {
		result         JobClaimResult
		photoSyncAvail bool
	}{
		{JobClaimAcquired, true},
		{JobClaimPeerOwned, false},
		{JobClaimAlreadyCompleted, false},
		{JobClaimUnavailable, false},
		{JobClaimResult(""), false},
		{JobClaimResult("totally_unknown"), false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.photoSyncAvail, photoSyncGuardTreatsAsAvailable(tc.result), "photo_sync_guard result=%q", tc.result)
	}
}
