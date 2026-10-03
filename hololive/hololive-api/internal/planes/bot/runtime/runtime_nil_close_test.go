package botruntime

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestBotRuntimeCloseNilAndOnce(t *testing.T) {
	var absent *BotRuntime

	absent.Close()

	var cleanups atomic.Int32

	runtime := &BotRuntime{cleanup: func() error {
		cleanups.Add(1)

		return nil
	}}

	var callers sync.WaitGroup

	for range 8 {
		callers.Go(runtime.Close)
	}

	callers.Wait()

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup count = %d, want 1", got)
	}
}
