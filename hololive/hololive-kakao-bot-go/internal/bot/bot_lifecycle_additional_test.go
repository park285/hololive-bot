package bot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
)

type lifecycleTestPostgres struct {
	closeErr    error
	closeCalled bool
}

func (p *lifecycleTestPostgres) GetPool() *pgxpool.Pool { return nil }
func (p *lifecycleTestPostgres) GetGormDB() *gorm.DB    { return nil }
func (p *lifecycleTestPostgres) Ping(context.Context) error {
	return nil
}
func (p *lifecycleTestPostgres) Close() error {
	p.closeCalled = true
	return p.closeErr
}

type lifecycleTestHolodex struct {
	stopCalled bool
}

func (h *lifecycleTestHolodex) Stop() { h.stopCalled = true }
func (h *lifecycleTestHolodex) GetLiveStreams(context.Context) ([]*domain.Stream, error) {
	return nil, nil
}
func (h *lifecycleTestHolodex) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}
func (h *lifecycleTestHolodex) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}
func (h *lifecycleTestHolodex) GetChannel(context.Context, string) (*domain.Channel, error) {
	return nil, nil
}

func TestBotLifecycleStartBranches(t *testing.T) {
	t.Parallel()

	t.Run("cache not configured", func(t *testing.T) {
		lifecycle := NewBotLifecycle(newBotTestLogger(), nil, &testIrisClient{}, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)

		err := lifecycle.Start(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is not configured")
	})

	t.Run("cache readiness failure", func(t *testing.T) {
		cacheClient := &cachemocks.Client{
			WaitUntilReadyFunc: func(context.Context, time.Duration) error { return errors.New("down") },
		}
		lifecycle := NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)

		err := lifecycle.Start(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valkey connection timeout")
	})

	t.Run("degraded mode then stop", func(t *testing.T) {
		cacheClient := &cachemocks.Client{
			WaitUntilReadyFunc: func(context.Context, time.Duration) error { return nil },
		}
		stopCh := make(chan struct{})
		close(stopCh)
		lifecycle := NewBotLifecycle(newBotTestLogger(), cacheClient, nil, "http://iris", stopCh, make(chan struct{}), nil, nil, nil)

		err := lifecycle.Start(context.Background())
		require.NoError(t, err)
	})
}

func TestBotLifecycleStart_ContextCanceled(t *testing.T) {
	t.Parallel()

	cacheClient := &cachemocks.Client{
		WaitUntilReadyFunc: func(context.Context, time.Duration) error { return nil },
	}
	lifecycle := NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "http://iris", make(chan struct{}), make(chan struct{}), nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := lifecycle.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
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

	pool, err := workerpool.New(workerpool.DefaultConfig())
	require.NoError(t, err)

	holodex := &lifecycleTestHolodex{}
	postgres := &lifecycleTestPostgres{}
	doneCh := make(chan struct{})

	lifecycle := NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "http://iris", make(chan struct{}), doneCh, pool, holodex, postgres)

	require.NoError(t, lifecycle.Shutdown(context.Background()))
	assert.True(t, cacheClosed)
	assert.True(t, holodex.stopCalled)
	assert.True(t, postgres.closeCalled)

	select {
	case <-doneCh:
	default:
		t.Fatal("done channel not closed")
	}
}

func TestBotStartAndShutdownDelegateToLifecycle(t *testing.T) {
	t.Parallel()

	lifecycle := NewBotLifecycle(newBotTestLogger(), nil, nil, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)
	b := &Bot{lifecycle: lifecycle}

	err := b.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache is not configured")

	require.NoError(t, b.Shutdown(context.Background()))
}

