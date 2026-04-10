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
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

type trackingSettingsApplier struct {
	lastScraperProxyEnabled bool
	lastAlarmMinutes        int
	scraperResult           sharedserver.ScraperProxyApplyResult
	alarmResult             sharedserver.AlarmAdvanceMinutesApplyResult
	runtimeResult           sharedserver.ScraperProxyRuntimeStateResult
}

func (a *trackingSettingsApplier) ApplyScraperProxy(_ context.Context, enabled bool) sharedserver.ScraperProxyApplyResult {
	a.lastScraperProxyEnabled = enabled
	return a.scraperResult
}

func (a *trackingSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) sharedserver.AlarmAdvanceMinutesApplyResult {
	a.lastAlarmMinutes = minutes
	return a.alarmResult
}

func (a *trackingSettingsApplier) ApplyMemberNewsWeeklyRunNow(context.Context) sharedserver.MemberNewsWeeklyRunNowResult {
	return sharedserver.MemberNewsWeeklyRunNowResult{
		Applied: false,
		Reason:  "not used in delegation test",
	}
}

func (a *trackingSettingsApplier) ScraperProxyRuntimeState(_ bool) sharedserver.ScraperProxyRuntimeStateResult {
	return a.runtimeResult
}

type trackingMemberNewsRunNowTrigger struct {
	called int
	err    error
}

func (t *trackingMemberNewsRunNowTrigger) SendMemberNewsWeekly(context.Context) error {
	t.called++
	return t.err
}

type trackingYouTubeSvc struct {
	mu           sync.Mutex
	proxyEnabled bool
}

func (s *trackingYouTubeSvc) SetScraperProxyEnabled(enabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.proxyEnabled = enabled

	return true
}

func (s *trackingYouTubeSvc) ScraperProxyEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.proxyEnabled
}

func (s *trackingYouTubeSvc) isProxyEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.proxyEnabled
}

func (s *trackingYouTubeSvc) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return nil, nil
}

func (s *trackingYouTubeSvc) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return nil, nil
}

type trackingProxyTogglePoller struct {
	mu      sync.Mutex
	enabled bool
}

func (p *trackingProxyTogglePoller) Name() string { return "tracking-proxy-poller" }
func (p *trackingProxyTogglePoller) Poll(context.Context, string) error {
	return nil
}

func (p *trackingProxyTogglePoller) SetProxyEnabled(enabled bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.enabled = enabled

	return true
}

func (p *trackingProxyTogglePoller) ProxyEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.enabled
}

func (p *trackingProxyTogglePoller) isEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.enabled
}

func testAppLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewBotSettingsApplier_DefaultLogger(t *testing.T) {
	t.Parallel()

	base := &trackingSettingsApplier{}

	applier := newBotSettingsApplier(base, nil, nil)
	wrapped, ok := applier.(*botSettingsApplier)
	require.True(t, ok)
	require.NotNil(t, wrapped)
	assert.Same(t, base, wrapped.SettingsApplier)
	assert.NotNil(t, wrapped.logger)
}

func TestBotSettingsApplier_DelegatesToBase(t *testing.T) {
	t.Parallel()

	expectedScraper := sharedserver.ScraperProxyApplyResult{Requested: true}
	expectedAlarm := sharedserver.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: 15,
		AlarmApplied:                 true,
		AlarmTargetMinutes:           []int{5, 15},
	}
	known := true
	expectedRuntime := sharedserver.ScraperProxyRuntimeStateResult{
		Requested: true,
		Known:     &known,
	}
	base := &trackingSettingsApplier{
		scraperResult: expectedScraper,
		alarmResult:   expectedAlarm,
		runtimeResult: expectedRuntime,
	}
	applier := &botSettingsApplier{
		SettingsApplier: base,
		logger:          testAppLogger(),
	}

	assert.Equal(t, expectedScraper, applier.ApplyScraperProxy(t.Context(), true))
	assert.True(t, base.lastScraperProxyEnabled)

	assert.Equal(t, expectedAlarm, applier.ApplyAlarmAdvanceMinutes(t.Context(), 15))
	assert.Equal(t, 15, base.lastAlarmMinutes)

	assert.Equal(t, expectedRuntime, applier.ScraperProxyRuntimeState(true))
}

func TestBotSettingsApplier_ApplyMemberNewsWeeklyRunNow(t *testing.T) {
	t.Parallel()

	t.Run("nil trigger", func(t *testing.T) {
		applier := &botSettingsApplier{
			SettingsApplier:  nil,
			memberNewsRunNow: nil,
			logger:           testAppLogger(),
		}

		result := applier.ApplyMemberNewsWeeklyRunNow(t.Context())
		assert.False(t, result.Applied)
		assert.Equal(t, "member news trigger is not configured", result.Reason)
		assert.Empty(t, result.Error)
	})

	t.Run("trigger failure", func(t *testing.T) {
		trigger := &trackingMemberNewsRunNowTrigger{err: errors.New("request failed")}
		applier := &botSettingsApplier{
			memberNewsRunNow: trigger,
			logger:           testAppLogger(),
		}

		result := applier.ApplyMemberNewsWeeklyRunNow(t.Context())

		assert.Equal(t, 1, trigger.called)
		assert.False(t, result.Applied)
		assert.Equal(t, "member news trigger failed", result.Reason)
		assert.Equal(t, "request failed", result.Error)
	})

	t.Run("success", func(t *testing.T) {
		trigger := &trackingMemberNewsRunNowTrigger{}
		applier := &botSettingsApplier{
			memberNewsRunNow: trigger,
			logger:           testAppLogger(),
		}

		result := applier.ApplyMemberNewsWeeklyRunNow(t.Context())

		assert.Equal(t, 1, trigger.called)
		assert.True(t, result.Applied)
		assert.Equal(t, "member_news_trigger", result.Source)
		assert.Empty(t, result.Error)
	})
}

func TestApplyScraperProxyToggle_UpdatesYouTubeAndScheduler(t *testing.T) {
	t.Parallel()

	logger := testAppLogger()
	youtubeSvc := &trackingYouTubeSvc{}
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: time.Millisecond,
	})
	trackingPoller := &trackingProxyTogglePoller{}
	scheduler.Register("channel-1", trackingPoller, poller.PriorityNormal, time.Minute)

	sharedserver.ApplyScraperProxyToggle(true, youtubeSvc, nil, scheduler, logger)
	assert.True(t, youtubeSvc.isProxyEnabled())
	assert.True(t, trackingPoller.isEnabled())

	enabled, known := scheduler.ProxyEnabled()
	assert.True(t, known)
	assert.True(t, enabled)

	sharedserver.ApplyScraperProxyToggle(false, youtubeSvc, nil, scheduler, logger)
	assert.False(t, youtubeSvc.isProxyEnabled())
	assert.False(t, trackingPoller.isEnabled())

	enabled, known = scheduler.ProxyEnabled()
	assert.True(t, known)
	assert.False(t, enabled)
}

func TestYouTubeStackAndSchedulerAccessors_Defaults(t *testing.T) {
	t.Parallel()

	var stack *providers.YouTubeStack

	assert.Nil(t, stack.GetService())
	assert.Nil(t, ProvideYouTubeScheduler(nil))

	svc := &trackingYouTubeSvc{}
	scheduler := &stubYouTubeScheduler{}
	ytStack := &providers.YouTubeStack{Service: svc}
	deps := &bot.Dependencies{Scheduler: scheduler}

	assert.Same(t, svc, ytStack.GetService())
	assert.Same(t, scheduler, ProvideYouTubeScheduler(deps))
}

var (
	_ sharedserver.SettingsApplier  = (*trackingSettingsApplier)(nil)
	_ memberNewsWeeklyRunNowTrigger = (*trackingMemberNewsRunNowTrigger)(nil)
	_ youtube.Service               = (*trackingYouTubeSvc)(nil)
)
