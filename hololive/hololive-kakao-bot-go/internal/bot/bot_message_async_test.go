package bot

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
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

func TestExecuteCommandAsync_RejectsWorkerPoolBackpressureWithoutLaunchingTask(t *testing.T) {
	t.Parallel()

	pool, err := workerpool.New(workerpool.Config{
		Size:           1,
		ExpiryDuration: time.Second,
		Nonblocking:    true,
	})
	require.NoError(t, err)

	blocker := make(chan struct{})

	require.NoError(t, pool.Submit(func() {
		<-blocker
	}))
	require.Eventually(t, func() bool {
		return pool.Running() == 1
	}, time.Second, 10*time.Millisecond)

	t.Cleanup(func() {
		close(blocker)
		pool.Wait()
		pool.Shutdown()
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

	select {
	case msg := <-msgCh:
		assert.Equal(t, "room-1", msg.room)
		assert.Contains(t, msg.message, "잠시 후 다시 시도")
	case <-time.After(time.Second):
		t.Fatal("expected backpressure message to be sent")
	}

	time.Sleep(50 * time.Millisecond)
	assert.Zero(t, executed.Load(), "expected rejected task to be dropped")
}
