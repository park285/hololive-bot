package producerruntime

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
	"github.com/stretchr/testify/require"
)

type recoveryLoopClaimResponse struct {
	status poller.JobClaimStatus
	claim  poller.JobClaim
	err    error
}

type recoveryLoopClaimer struct {
	calls     atomic.Int64
	responses []recoveryLoopClaimResponse
}

func (c *recoveryLoopClaimer) TryClaim(
	context.Context,
	string,
	string,
	time.Duration,
	time.Duration,
) (status poller.JobClaimStatus, claim poller.JobClaim, err error) {
	call := int(c.calls.Add(1))
	if len(c.responses) == 0 {
		return poller.JobClaimStatus{Result: poller.JobClaimUnavailable}, nil, fmt.Errorf("missing response")
	}
	index := call - 1
	if index >= len(c.responses) {
		index = len(c.responses) - 1
	}
	response := c.responses[index]
	return response.status, response.claim, response.err
}

type recoveryLoopClaim struct {
	releases atomic.Int64
}

func (c *recoveryLoopClaim) Renew(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (c *recoveryLoopClaim) MarkCompleted(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (c *recoveryLoopClaim) Release(context.Context) (bool, error) {
	c.releases.Add(1)
	return true, nil
}

func TestRecoveryLoopRecoversAfterValkeyReturns(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{ActiveActiveEnabled: true})
	state.MarkRunning()
	claim := &recoveryLoopClaim{}
	claimer := &recoveryLoopClaimer{
		responses: []recoveryLoopClaimResponse{
			{
				status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable},
				err:    fmt.Errorf("valkey unavailable"),
			},
			{
				status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable},
				err:    fmt.Errorf("valkey unavailable"),
			},
			{
				status: poller.JobClaimStatus{Result: poller.JobClaimAcquired},
				claim:  claim,
			},
		},
	}

	stop := startRecoveryLoop(t.Context(), claimer, state, 50*time.Millisecond, 200*time.Millisecond, nil, nil)
	defer stop()

	require.Eventually(t, func() bool {
		_, payload := state.Response()
		return payload["valkey_available"] == true && payload["scraping_paused"] == false
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, int64(1), claim.releases.Load())

	stop()
	callsAfterStop := claimer.calls.Load()
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, callsAfterStop, claimer.calls.Load())
}

func TestRecoveryLoopRespectsCancellation(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{ActiveActiveEnabled: true})
	state.MarkRunning()
	ctx, cancel := context.WithCancel(context.Background())
	stop := startRecoveryLoop(ctx, &recoveryLoopClaimer{}, state, time.Hour, time.Hour, nil, nil)

	cancel()
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("stop did not return promptly after context cancellation")
	}
}

func TestRecoveryLoopIdleWhenLeaseAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := readiness.New("youtube-producer", readiness.Features{ActiveActiveEnabled: true})
		state.MarkRunning()
		state.MarkLeaseAvailable()
		claimer := &recoveryLoopClaimer{
			responses: []recoveryLoopClaimResponse{
				{status: poller.JobClaimStatus{Result: poller.JobClaimAcquired}, claim: &recoveryLoopClaim{}},
			},
		}

		stop := startRecoveryLoop(t.Context(), claimer, state, 50*time.Millisecond, 200*time.Millisecond, nil, nil)
		defer stop()
		for range 10 {
			time.Sleep(50 * time.Millisecond)
		}

		require.LessOrEqual(t, claimer.calls.Load(), int64(1))
		_, payload := state.Response()
		require.Equal(t, true, payload["valkey_available"])
		require.Equal(t, false, payload["scraping_paused"])
	})
}

func TestRecoveryLoopPeerOwnedTreatedAsAvailable(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{ActiveActiveEnabled: true})
	state.MarkRunning()
	claimer := &recoveryLoopClaimer{
		responses: []recoveryLoopClaimResponse{
			{status: poller.JobClaimStatus{Result: poller.JobClaimPeerOwned}},
		},
	}

	stop := startRecoveryLoop(t.Context(), claimer, state, 50*time.Millisecond, 200*time.Millisecond, nil, nil)
	defer stop()

	require.Eventually(t, func() bool {
		_, payload := state.Response()
		return payload["valkey_available"] == true && payload["scraping_paused"] == false
	}, 5*time.Second, 10*time.Millisecond)
}

func TestRecoveryLoopInvokesOnResumeOnTransition(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{ActiveActiveEnabled: true})
	state.MarkRunning()
	claim := &recoveryLoopClaim{}
	claimer := &recoveryLoopClaimer{
		responses: []recoveryLoopClaimResponse{
			{status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable}, err: fmt.Errorf("valkey unavailable")},
			{status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable}, err: fmt.Errorf("valkey unavailable")},
			{status: poller.JobClaimStatus{Result: poller.JobClaimAcquired}, claim: claim},
		},
	}
	var resumeCalls atomic.Int64
	stop := startRecoveryLoop(t.Context(), claimer, state, 50*time.Millisecond, 200*time.Millisecond, nil, func() {
		resumeCalls.Add(1)
	})
	defer stop()
	require.Eventually(t, func() bool { return resumeCalls.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, int64(1), resumeCalls.Load())
}

func TestRecoveryLoopDoesNotInvokeOnResumeWhenAlreadyAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state := readiness.New("youtube-producer", readiness.Features{ActiveActiveEnabled: true})
		state.MarkRunning()
		state.MarkLeaseAvailable()
		claimer := &recoveryLoopClaimer{
			responses: []recoveryLoopClaimResponse{
				{status: poller.JobClaimStatus{Result: poller.JobClaimAcquired}, claim: &recoveryLoopClaim{}},
			},
		}
		var resumeCalls atomic.Int64
		stop := startRecoveryLoop(t.Context(), claimer, state, 50*time.Millisecond, 200*time.Millisecond, nil, func() {
			resumeCalls.Add(1)
		})
		defer stop()
		for range 10 {
			time.Sleep(50 * time.Millisecond)
		}
		require.Equal(t, int64(0), resumeCalls.Load())
	})
}
