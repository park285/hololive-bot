package orchestration

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/lifecycle"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type lifecycleTestPostgres struct {
	closeErr    error
	closeCalled bool
}

func (p *lifecycleTestPostgres) GetPool() *pgxpool.Pool { return nil }
func (p *lifecycleTestPostgres) Ping(context.Context) error {
	return nil
}

func (p *lifecycleTestPostgres) Close() error {
	p.closeCalled = true
	return p.closeErr
}

func TestBotLifecycleStartBranches(t *testing.T) {
	t.Parallel()

	t.Run("cache not configured", func(t *testing.T) {
		t.Parallel()

		botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), nil, &testIrisClient{}, "")

		err := botLifecycle.Start(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is not configured")
	})

	t.Run("cache readiness failure", func(t *testing.T) {
		t.Parallel()

		cacheClient := &cachemocks.Client{
			WaitUntilReadyFunc: func(context.Context, time.Duration) error { return errors.New("down") },
		}
		botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "")

		err := botLifecycle.Start(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valkey connection timeout")
	})
}

func TestBotLifecycleStart_ContextCanceled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		cacheClient := &cachemocks.Client{
			WaitUntilReadyFunc: func(context.Context, time.Duration) error { return nil },
		}
		botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "http://iris")

		ctx, cancel := context.WithCancel(t.Context())

		done := make(chan error, 1)

		go func() { done <- botLifecycle.Start(ctx) }()

		synctest.Wait()
		cancel()

		err := <-done
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})
}

func TestBotLifecycleShutdownBranches(t *testing.T) {
	t.Parallel()

	cacheClosed := false
	cacheClient := &cachemocks.Client{
		CloseFunc: func() error {
			cacheClosed = true
			return nil
		},
	}

	postgres := &lifecycleTestPostgres{}

	botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "http://iris")

	b := &Bot{cache: cacheClient, postgres: postgres, lifecycle: botLifecycle}

	require.NoError(t, b.Shutdown(t.Context()))
	assert.False(t, cacheClosed)
	assert.False(t, postgres.closeCalled)
}

func TestBotStartAndShutdownDelegateToLifecycle(t *testing.T) {
	t.Parallel()

	botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), nil, nil, "")
	b := &Bot{lifecycle: botLifecycle}

	err := b.Start(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache is not configured")

	require.NoError(t, b.Shutdown(t.Context()))
}
