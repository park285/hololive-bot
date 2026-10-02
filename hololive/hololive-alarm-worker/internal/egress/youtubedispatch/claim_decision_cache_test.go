package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
)

func TestClaimDecisionCacheSharesDecisionAndClaimProof(t *testing.T) {
	t.Parallel()

	for _, token := range []*dispatchstate.ClaimToken{nil, {AuthorizedAt: time.Now()}} {
		cache := newClaimDecisionCache()
		want := claimResult{decision: deliveryClaimDecisionProceed, claimToken: token}
		calls := 0
		compute := func(context.Context) (claimResult, error) { calls++; return want, nil }
		first, err := cache.ResolveClaim(t.Context(), "post", compute)
		require.NoError(t, err)
		require.False(t, first.Hit)
		require.Equal(t, want, first.Decision)

		second, err := cache.ResolveClaim(t.Context(), "post", compute)
		require.NoError(t, err)
		require.True(t, second.Hit)
		require.Equal(t, want, second.Decision)
		require.Equal(t, 1, calls)
	}
}

func TestClaimDecisionCacheDoesNotCacheErrors(t *testing.T) {
	t.Parallel()

	cache := newClaimDecisionCache()
	cause := errors.New("claim failed")
	_, err := cache.ResolveClaim(t.Context(), "post", func(context.Context) (claimResult, error) { return claimResult{}, cause })
	require.ErrorIs(t, err, cause)

	result, err := cache.ResolveClaim(t.Context(), "post", func(context.Context) (claimResult, error) {
		return claimResult{decision: deliveryClaimDecisionAlreadySent}, nil
	})
	require.NoError(t, err)
	require.False(t, result.Hit)
	require.Equal(t, deliveryClaimDecisionAlreadySent, result.Decision.decision)
}

func TestClaimDecisionCacheConcurrentMissComputesOnce(t *testing.T) {
	t.Parallel()

	cache := newClaimDecisionCache()

	var (
		computes, hits, misses atomic.Int32
		workers                sync.WaitGroup
	)

	for range 16 {
		workers.Go(func() {
			result, err := cache.ResolveClaim(t.Context(), "post", func(context.Context) (claimResult, error) {
				computes.Add(1)
				time.Sleep(10 * time.Millisecond)

				return claimResult{decision: deliveryClaimDecisionProceed}, nil
			})
			if err != nil {
				t.Error(err)

				return
			}

			if result.Hit {
				hits.Add(1)
			} else {
				misses.Add(1)
			}
		})
	}

	workers.Wait()
	require.EqualValues(t, 1, computes.Load())
	require.EqualValues(t, 1, misses.Load())
	require.EqualValues(t, 15, hits.Load())
}

func TestClaimDecisionCacheDistinctKeysComputeConcurrently(t *testing.T) {
	t.Parallel()

	cache := newClaimDecisionCache()
	entered := make(chan struct{}, 8)
	release := make(chan struct{})

	var workers sync.WaitGroup

	for i := range 8 {
		workers.Go(func() {
			_, err := cache.ResolveClaim(t.Context(), fmt.Sprint(i), func(context.Context) (claimResult, error) {
				entered <- struct{}{}

				<-release

				return claimResult{}, nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}

	for range 8 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			workers.Wait()
			t.Fatal("distinct claim keys were serialized")
		}
	}

	close(release)
	workers.Wait()
}

func TestClaimDecisionCacheWaitingCallerCanCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := newClaimDecisionCache()
		release := make(chan struct{})

		var owner sync.WaitGroup

		owner.Go(func() {
			_, err := cache.ResolveClaim(t.Context(), "post", func(context.Context) (claimResult, error) {
				<-release

				return claimResult{}, nil
			})
			if err != nil {
				t.Error(err)
			}
		})
		synctest.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		waiterResult := make(chan error, 1)

		go func() {
			_, err := cache.ResolveClaim(ctx, "post", func(context.Context) (claimResult, error) {
				t.Error("waiter computed owner's claim")

				return claimResult{}, nil
			})
			waiterResult <- err
		}()

		synctest.Wait()
		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-waiterResult, context.Canceled)
		close(release)
		owner.Wait()
	})
}

func TestClaimDecisionCachePanicReleasesKey(t *testing.T) {
	t.Parallel()

	cache := newClaimDecisionCache()

	require.Panics(t, func() {
		_, err := cache.ResolveClaim(t.Context(), "post", func(context.Context) (claimResult, error) { panic("claim panic") })
		require.NoError(t, err)
	})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

	defer cancel()

	result, err := cache.ResolveClaim(ctx, "post", func(context.Context) (claimResult, error) {
		return claimResult{decision: deliveryClaimDecisionProceed}, nil
	})
	require.NoError(t, err)
	require.False(t, result.Hit)
}
