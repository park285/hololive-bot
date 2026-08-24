package workerruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

type deliveryOutboxDispatcherFunc func(context.Context) error

func (fn deliveryOutboxDispatcherFunc) Run(ctx context.Context) error {
	if err := fn(ctx); err != nil {
		return fmt.Errorf("fn: %w", err)
	}

	return nil
}

func TestDeliveryOutboxDispatcherRunnerJoinsDispatcherShutdown(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	stopped := make(chan struct{})
	runner := NewDeliveryOutboxDispatcherRunner(
		deliveryOutboxDispatcherFunc(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(stopped)

			return nil
		}),
		slog.New(slog.DiscardHandler),
	)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- runner.Start(ctx) }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not start")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner.Start() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner returned before dispatcher shutdown was joined")
	}

	select {
	case <-stopped:
	default:
		t.Fatal("runner returned before dispatcher Run exited")
	}
}

func TestDeliveryOutboxDispatcherRunnerReturnsDispatcherError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("dispatcher failed")
	runner := NewDeliveryOutboxDispatcherRunner(
		deliveryOutboxDispatcherFunc(func(context.Context) error { return wantErr }),
		nil,
	)

	if err := runner.Start(t.Context()); !errors.Is(err, wantErr) {
		t.Fatalf("runner.Start() error = %v, want %v", err, wantErr)
	}
}

func TestDeliveryOutboxDispatcherRunnerAllowsDisabledDispatcher(t *testing.T) {
	t.Parallel()

	runner := NewDeliveryOutboxDispatcherRunner(nil, nil)
	if err := runner.Start(t.Context()); err != nil {
		t.Fatalf("runner.Start() error = %v, want nil", err)
	}
}
