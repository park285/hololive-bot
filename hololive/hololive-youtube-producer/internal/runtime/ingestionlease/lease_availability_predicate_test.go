package ingestionlease

import (
	"testing"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/stretchr/testify/require"
)

func photoSyncGuardTreatsAsAvailable(result polling.JobClaimResult) bool {
	return result == polling.JobClaimAcquired
}

func TestIngestionLeaseAvailabilityTruthTablePinsCurrentSites(t *testing.T) {
	t.Parallel()

	cases := []struct {
		result         polling.JobClaimResult
		photoSyncAvail bool
	}{
		{polling.JobClaimAcquired, true},
		{polling.JobClaimPeerOwned, false},
		{polling.JobClaimAlreadyCompleted, false},
		{polling.JobClaimUnavailable, false},
		{polling.JobClaimResult(""), false},
		{polling.JobClaimResult("totally_unknown"), false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.photoSyncAvail, photoSyncGuardTreatsAsAvailable(tc.result), "photo_sync_guard result=%q", tc.result)
	}
}
