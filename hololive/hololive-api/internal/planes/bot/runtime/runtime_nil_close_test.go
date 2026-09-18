package botruntime

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
)

func TestBotRuntimeCloseNilAndOnce(t *testing.T) {
	var absent *BotRuntime

	absent.Close()

	var cleanups atomic.Int32

	runtime := &BotRuntime{Managed: lifecycle.NewManaged(func() { cleanups.Add(1) })}

	var callers sync.WaitGroup

	for range 8 {
		callers.Go(runtime.Close)
	}

	callers.Wait()

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup count = %d, want 1", got)
	}
}

func TestDBIntegrationRuntimeCloseNilAndOnce(t *testing.T) {
	var absent *DBIntegrationRuntime

	absent.Close()

	var cleanups atomic.Int32

	runtime := &DBIntegrationRuntime{Managed: lifecycle.NewManaged(func() { cleanups.Add(1) })}

	var callers sync.WaitGroup

	for range 8 {
		callers.Go(runtime.Close)
	}

	callers.Wait()

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup count = %d, want 1", got)
	}
}
