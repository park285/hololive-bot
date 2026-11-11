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

	cacheClient := &cachemocks.Client{
		WaitUntilReadyFunc: func(context.Context, time.Duration) error { return nil },
	}
	botLifecycle := lifecycle.NewBotLifecycle(newBotTestLogger(), cacheClient, &testIrisClient{}, "http://iris")

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
