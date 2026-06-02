package delivery

// dispatcherTestHooks는 디스패처 루프의 관측 지점을 테스트에서만 가로채기 위한
// unexported 슬롯 묶음이다. production 바이너리에는 nil 슬롯만 남고 fire 호출은
// nil 체크 후 즉시 반환하므로 런타임 동작에 영향을 주지 않는다. 슬롯 설정과
// ForTest 진입점은 dispatcher_testhooks_test.go에서만 제공된다.
type dispatcherTestHooks struct {
	onProcessOnce   func()
	onAggregateSync func()
	onCleanup       func()
}

func (h *dispatcherTestHooks) fireProcessOnce() {
	if h.onProcessOnce != nil {
		h.onProcessOnce()
	}
}

func (h *dispatcherTestHooks) fireAggregateSync() {
	if h.onAggregateSync != nil {
		h.onAggregateSync()
	}
}

func (h *dispatcherTestHooks) fireCleanup() {
	if h.onCleanup != nil {
		h.onCleanup()
	}
}
