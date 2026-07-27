package producerruntime

import (
	"testing"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/stretchr/testify/require"
)

func recoveryLoopTreatsAsAvailable(result polling.JobClaimResult) bool {
	switch result {
	case polling.JobClaimAcquired:
		return true
	case polling.JobClaimPeerOwned, polling.JobClaimAlreadyCompleted:
		return true
	case polling.JobClaimUnavailable:
		return false
	default:
		return false
	}
}

func jobClaimerTreatsAsAvailable(result polling.JobClaimResult) bool {
	return result != polling.JobClaimUnavailable
}

func TestPollerLeaseAvailabilityTruthTablePinsCurrentSites(t *testing.T) {
	t.Parallel()

	cases := []struct {
		result        polling.JobClaimResult
		recoveryAvail bool
		claimerAvail  bool
	}{
		{polling.JobClaimAcquired, true, true},
		{polling.JobClaimPeerOwned, true, true},
		{polling.JobClaimAlreadyCompleted, true, true},
		{polling.JobClaimUnavailable, false, false},
		{polling.JobClaimResult(""), false, true},
		{polling.JobClaimResult("totally_unknown"), false, true},
	}

	for _, tc := range cases {
		require.Equal(t, tc.recoveryAvail, recoveryLoopTreatsAsAvailable(tc.result), "handleRecoveryLoopClaim result=%q", tc.result)
		require.Equal(t, tc.claimerAvail, jobClaimerTreatsAsAvailable(tc.result), "readinessReportingJobClaimer result=%q", tc.result)
	}
}
