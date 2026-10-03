package youtubedispatch

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

// ProcessOnceForTest는 테스트에서 한 폴링 사이클을 실행합니다. DB 전이와 테스트 sender 호출을 포함합니다.
func (d *Dispatcher) ProcessOnceForTest(ctx context.Context) {
	d.processOnce(ctx)
}
