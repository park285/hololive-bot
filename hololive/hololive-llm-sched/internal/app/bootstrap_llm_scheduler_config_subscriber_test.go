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

package app

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

type stubMemberNewsDigestSender struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	blockCh chan struct{}
}

func (s *stubMemberNewsDigestSender) SendWeeklyDigest(ctx context.Context) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()

	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}

	if s.blockCh != nil {
		select {
		case <-s.blockCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (s *stubMemberNewsDigestSender) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestMemberNewsRunNowExecutor_CoalescesConcurrentTriggers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	started := make(chan struct{}, 10)
	blockCh := make(chan struct{})

	sender := &stubMemberNewsDigestSender{
		started: started,
		blockCh: blockCh,
	}
	executor := newMemberNewsRunNowExecutor(context.Background(), sender, 5*time.Second, logger)

	executor.Trigger()
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first run-now invocation did not start")
	}

	for i := 0; i < 5; i++ {
		executor.Trigger()
	}

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, sender.CallCount(), "running 중에는 추가 실행 대신 coalesce 되어야 함")

	close(blockCh)

	require.Eventually(t, func() bool {
		return sender.CallCount() == 2
	}, 2*time.Second, 20*time.Millisecond, "coalesced trigger 1회는 반드시 추가 실행되어야 함")

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 2, sender.CallCount(), "coalescing은 최대 1회 추가 실행으로 제한되어야 함")
}

func TestMemberNewsRunNowExecutor_NilSender(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	executor := newMemberNewsRunNowExecutor(context.Background(), nil, time.Second, logger)

	assert.NotPanics(t, func() {
		executor.Trigger()
		time.Sleep(50 * time.Millisecond)
	})
}

func TestNewLLMSchedulerConfigApplyFn_RunNowOnly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender := &stubMemberNewsDigestSender{}
	applyFn := newLLMSchedulerConfigApplyFn(context.Background(), sender, logger)

	applyFn(configsub.ConfigUpdate{Type: "unknown"})
	applyFn(configsub.ConfigUpdate{Type: contractssettings.UpdateTypeScraperProxy, Payload: []byte(`{"enabled":true}`)})
	applyFn(configsub.ConfigUpdate{Type: contractssettings.UpdateTypeAlarmAdvanceMinutes, Payload: []byte(`{"minutes":30}`)})
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sender.CallCount())

	applyFn(configsub.ConfigUpdate{Type: contractssettings.UpdateTypeMemberNewsRunNow})

	require.Eventually(t, func() bool {
		return sender.CallCount() == 1
	}, time.Second, 20*time.Millisecond)
}
