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

package poller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type togglePollerStub struct {
	name    string
	enabled bool
}

func (p *togglePollerStub) Poll(context.Context, string) error { return nil }
func (p *togglePollerStub) Name() string                       { return p.name }
func (p *togglePollerStub) SetProxyEnabled(enabled bool) bool {
	p.enabled = enabled
	return true
}
func (p *togglePollerStub) ProxyEnabled() bool { return p.enabled }

func TestScheduler_ZeroRequestInterval_NoopRL(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0})
	ctx := context.Background()

	// 첫 호출은 초기화 역할을 하고, 두 번째 호출이 실제 무대기 경로를 검증한다.
	require.NoError(t, scheduler.rateLimiter.Wait(ctx))

	start := time.Now()
	err := scheduler.rateLimiter.Wait(ctx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, time.Millisecond, "zero interval rate limiter should not block")
}

func TestScheduler_SetProxyEnabled(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0})
	poller := &togglePollerStub{name: "toggle"}

	scheduler.Register("channel-1", poller, PriorityNormal, time.Minute)
	scheduler.Register("channel-2", poller, PriorityNormal, time.Minute) // 동일 poller 중복 등록

	applied := scheduler.SetProxyEnabled(true)
	assert.Equal(t, 1, applied)

	enabled, known := scheduler.ProxyEnabled()
	assert.True(t, known)
	assert.True(t, enabled)
}
