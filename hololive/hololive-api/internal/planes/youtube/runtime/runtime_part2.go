package runtime

import (
	"context"
	"errors"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

func (r *Runtime) startGuarded(ctx context.Context, errCh chan<- error, name string, run func()) {
	panicguard.Go(r.Logger, name, func() {
		if err := panicguard.RunE(r.Logger, name, func() error {
			run()

			return nil
		}); err != nil {
			r.reportLoopError(ctx, errCh, name, err)
		}
	})
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.ready.Store(false)
	r.publishHealth()

	if !r.started.CompareAndSwap(true, false) {
		return nil
	}

	r.claiming.Store(false)

	if r.runCancel != nil {
		r.runCancel()
	}

	if !r.Config.Enabled {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, r.Config.ShutdownTimeout)

	defer cancel()

	loopErr := waitForCompletions(shutdownCtx, r.loopDone, r.supervisorLoopCount(), "youtube supervisor loops")
	if loopErr == nil {
		r.closeWork.Do(func() {
			close(r.workCh)
		})
	}

	workerErr := waitForCompletions(shutdownCtx, r.workerDone, r.Config.ConsumerWorkers, "youtube workers")
	r.workerTracker.StopWorkers(r.Config.ConsumerWorkers)

	releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), r.Config.TransactionTimeout)

	defer releaseCancel()

	releaseErr := r.releaseInFlight(releaseCtx)

	return errors.Join(loopErr, workerErr, releaseErr)
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}

	if r.closePool != nil {
		r.closePool()

		r.closePool = nil
	}

	r.pool = nil

	health.RemoveComponent(youtubeHealthComponent)
}

func youtubePlaneClaimKinds() []contract.ObservationKind {
	return []contract.ObservationKind{
		contract.KindCommunityPage,
		contract.KindVideoList,
		contract.KindShortsList,
		contract.KindLiveSnapshot,
		contract.KindViewerSample,
		contract.KindSchedule,
		contract.KindChannelStats,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
	}
}

func (r *Runtime) supervisorLoopCount() int {
	if r == nil || r.loopCount == 0 {
		return 2
	}

	return r.loopCount
}

func (r *Runtime) Ready() bool {
	return r != nil && r.ready.Load()
}

func (r *Runtime) Degraded() bool {
	return r != nil && r.degraded.Load()
}

func (r *Runtime) withDB(ctx context.Context, fn func(context.Context) error) error {
	select {
	case r.dbSem <- struct{}{}:
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("acquire DB slot: %w", err)
		}

		return nil
	}

	defer func() { <-r.dbSem }()

	if err := fn(ctx); err != nil {
		return fmt.Errorf("fn: %w", err)
	}

	return nil
}

func (r *Runtime) publishHealth() {
	health.SetComponent(youtubeHealthComponent, health.ComponentStatus{
		Ready:    r.Ready(),
		Degraded: r.Degraded(),
	})
}

func waitForCompletions(ctx context.Context, done <-chan struct{}, count int, owner string) error {
	for range count {
		if err := waitOneCompletion(ctx, done, owner); err != nil {
			return fmt.Errorf("wait one completion: %w", err)
		}
	}

	return nil
}

func waitOneCompletion(ctx context.Context, done <-chan struct{}, owner string) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s did not join: %w", owner, ctx.Err())
	}
}
