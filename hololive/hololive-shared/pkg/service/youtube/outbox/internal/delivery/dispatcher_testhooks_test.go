package delivery

import "context"

func (d *Dispatcher) setOnProcessOnce(fn func()) {
	d.testHooks.onProcessOnce = fn
}

func (d *Dispatcher) setOnAggregateSync(fn func()) {
	d.testHooks.onAggregateSync = fn
}

func (d *Dispatcher) setOnCleanup(fn func()) {
	d.testHooks.onCleanup = fn
}

func (d *Dispatcher) CleanupForTest(ctx context.Context) {
	d.cleanup(ctx)
}

func (d *Dispatcher) AggregateSyncForTest(ctx context.Context) {
	d.aggregateSyncOnce(ctx)
}
