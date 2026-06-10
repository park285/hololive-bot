package producerruntime

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/stretchr/testify/require"
)

func recoveryLoopTreatsAsAvailable(result poller.JobClaimResult) bool {
	switch result {
	case poller.JobClaimAcquired:
		return true
	case poller.JobClaimPeerOwned, poller.JobClaimAlreadyCompleted:
		return true
	default:
		return false
	}
}

func jobClaimerTreatsAsAvailable(result poller.JobClaimResult) bool {
	return !(result == poller.JobClaimUnavailable)
}

func TestPollerLeaseAvailabilityTruthTablePinsCurrentSites(t *testing.T) {
	t.Parallel()

	cases := []struct {
		result        poller.JobClaimResult
		recoveryAvail bool
		claimerAvail  bool
	}{
		{poller.JobClaimAcquired, true, true},
		{poller.JobClaimPeerOwned, true, true},
		{poller.JobClaimAlreadyCompleted, true, true},
		{poller.JobClaimUnavailable, false, false},
		{poller.JobClaimResult(""), false, true},
		{poller.JobClaimResult("totally_unknown"), false, true},
	}

	for _, tc := range cases {
		require.Equal(t, tc.recoveryAvail, recoveryLoopTreatsAsAvailable(tc.result), "handleRecoveryLoopClaim result=%q", tc.result)
		require.Equal(t, tc.claimerAvail, jobClaimerTreatsAsAvailable(tc.result), "readinessReportingJobClaimer result=%q", tc.result)
	}
}
