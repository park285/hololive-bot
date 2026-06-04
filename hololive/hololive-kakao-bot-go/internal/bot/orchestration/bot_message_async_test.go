package orchestration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/workerpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

func TestExecuteCommandAsync_RunsSynchronouslyWhenWorkerPoolMissing(t *testing.T) {
	t.Parallel()

	registry := command.NewRegistry()
	done := make(chan struct{})

	registry.Register(&testCommand{
		name: "help",
		execute: func(context.Context, *domain.CommandContext, map[string]any) error {
			time.Sleep(50 * time.Millisecond)
			close(done)
			return nil
		},
	})

	b := &Bot{
		logger:          newBotTestLogger(),
		commandRegistry: registry,
	}

	b.executeCommandAsync(t.Context(), &domain.CommandContext{Room: "room-1"}, domain.CommandHelp, nil, "help", "")

	select {
	case <-done:
	default:
		t.Fatal("expected synchronous fallback to complete before returning")
	}
}

func TestExecuteCommandAsync_RejectsImmediatelyOnWorkerPoolBackpressure(t *testing.T) {
	t.Parallel()

	pool := workerpool.NewQueued(workerpool.QueuedConfig{Workers: 1, QueueSize: 1})
	blocker := make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() {
		unblockOnce.Do(func() {
			close(blocker)
		})
	}

	require.True(t, pool.SubmitWait(func() {
		<-blocker
	}))
	require.True(t, pool.SubmitWait(func() {}))

	t.Cleanup(func() {
		unblock()
		pool.StopAndWait()
	})

	var executed atomic.Int32

	registry := command.NewRegistry()
	registry.Register(&testCommand{
		name: "help",
		execute: func(context.Context, *domain.CommandContext, map[string]any) error {
			executed.Add(1)
			return nil
		},
	})

	msgCh := make(chan sentMessage, 1)
	b := &Bot{
		logger:          newBotTestLogger(),
		commandRegistry: registry,
		workerPool:      pool,
		irisClient:      &testIrisClient{messageCh: msgCh},
		formatter:       adapter.NewResponseFormatter("!", nil),
	}

	b.executeCommandAsync(t.Context(), &domain.CommandContext{Room: "room-1"}, domain.CommandHelp, nil, "help", "room-1")

	assert.Zero(t, executed.Load(), "expected rejected task not to run")
	select {
	case msg := <-msgCh:
		assert.Contains(t, msg.message, asyncCommandBackpressureMessage)
	case <-time.After(time.Second):
		t.Fatal("expected backpressure message")
	}

	unblock()
	time.Sleep(20 * time.Millisecond)
	assert.Zero(t, executed.Load(), "rejected task must not run after capacity becomes available")
}
