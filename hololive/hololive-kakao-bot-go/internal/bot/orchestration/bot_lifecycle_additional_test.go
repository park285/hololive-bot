// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot/orchestration/lifecycle"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/park285/shared-go/pkg/workerpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), nil, &testIrisClient{}, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)

		err := botLifecycle.Start(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is not configured")
	})

	t.Run("cache readiness failure", func(t *testing.T) {
		cacheClient := &cachemocks.Client{
			WaitUntilReadyFunc: func(context.Context, time.Duration) error { return errors.New("down") },
		}
		botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)

		err := botLifecycle.Start(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valkey connection timeout")
	})

	t.Run("degraded mode then stop", func(t *testing.T) {
		cacheClient := &cachemocks.Client{
			WaitUntilReadyFunc: func(context.Context, time.Duration) error { return nil },
		}
		stopCh := make(chan struct{})
		close(stopCh)

		botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, nil, "http://iris", stopCh, make(chan struct{}), nil, nil, nil)

		err := botLifecycle.Start(t.Context())
		require.NoError(t, err)
	})
}

func TestBotLifecycleStart_ContextCanceled(t *testing.T) {
	t.Parallel()

	cacheClient := &cachemocks.Client{
		WaitUntilReadyFunc: func(context.Context, time.Duration) error { return nil },
	}
	botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "http://iris", make(chan struct{}), make(chan struct{}), nil, nil, nil)

	ctx, cancel := context.WithCancel(t.Context())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := botLifecycle.Start(ctx)
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

	pool := workerpool.NewQueued(workerpool.QueuedConfig{Workers: 10, QueueSize: 100})

	holodex := &lifecycleTestHolodex{}
	postgres := &lifecycleTestPostgres{}
	doneCh := make(chan struct{})

	botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "http://iris", make(chan struct{}), doneCh, pool, holodex, postgres)

	require.NoError(t, botLifecycle.Shutdown(t.Context()))
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

	botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), nil, nil, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)
	b := &Bot{lifecycle: botLifecycle}

	err := b.Start(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache is not configured")

	require.NoError(t, b.Shutdown(t.Context()))
}
