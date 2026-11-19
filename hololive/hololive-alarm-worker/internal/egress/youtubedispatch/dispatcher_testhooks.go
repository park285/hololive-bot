package youtubedispatch

type dispatcherTestHooks struct {
	onProcessOnce   func()
	onAggregateSync func()
	onCleanup       func()
	onRevive        func()
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

func (h *dispatcherTestHooks) fireRevive() {
	if h.onRevive != nil {
		h.onRevive()
	}
}
