package runtime

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
)

func TestLLMSchedulerRuntimeCloseNilAndOnce(t *testing.T) {
	var absent *LLMSchedulerRuntime

	absent.Close()

	var cleanups atomic.Int32

	runtime := &LLMSchedulerRuntime{Managed: lifecycle.NewManaged(func() { cleanups.Add(1) })}

	var callers sync.WaitGroup

	for range 8 {
		callers.Go(runtime.Close)
	}

	callers.Wait()

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup count = %d, want 1", got)
	}
}
