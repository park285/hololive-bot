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

package botruntime

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

type stubYouTubeService struct{}

func (s *stubYouTubeService) SetScraperProxyEnabled(enabled bool) bool { return enabled }
func (s *stubYouTubeService) ScraperProxyEnabled() bool                { return false }
func (s *stubYouTubeService) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return nil, nil
}
func (s *stubYouTubeService) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return nil, nil
}

type stubSettingsReadWriter struct{}

func (s *stubSettingsReadWriter) Get() settings.Settings { return settings.Settings{} }
func (s *stubSettingsReadWriter) Update(newSettings settings.Settings) error {
	return nil
}

func TestBuildBotWebhookRuntimeDependencies(t *testing.T) {
	t.Run("nil dependencies", func(t *testing.T) {
		view := buildBotWebhookRuntimeDependencies(nil)
		if view.Cache != nil {
			t.Fatal("nil deps must yield zero-value webhook dependency view")
		}
	})

	t.Run("maps cache", func(t *testing.T) {
		cache := &cache.Service{}
		deps := &bot.Dependencies{Cache: cache}
		view := buildBotWebhookRuntimeDependencies(deps)
		if view.Cache != cache {
			t.Fatal("cache mapping mismatch")
		}
	})
}

func TestBuildBotConfigSubscriberDependencies(t *testing.T) {
	t.Run("nil dependencies", func(t *testing.T) {
		view := buildBotConfigSubscriberDependencies(nil)
		if view.Cache != nil || view.Settings != nil {
			t.Fatal("nil deps must yield zero-value config subscriber view")
		}
	})

	t.Run("maps settings and cache", func(t *testing.T) {
		cache := &cache.Service{}
		settingsService := &stubSettingsReadWriter{}
		deps := &bot.Dependencies{Cache: cache, Settings: settingsService}
		view := buildBotConfigSubscriberDependencies(deps)
		if view.Cache != cache {
			t.Fatal("cache mapping mismatch")
		}
		if view.Settings != settingsService {
			t.Fatal("settings mapping mismatch")
		}
	})
}

func TestBuildBotConfigSubscriberRuntimeDependencies(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		view := buildBotConfigSubscriberRuntimeDependencies(nil)
		if view.YouTubeService != nil || view.HolodexService != nil || view.AlarmCRUD != nil {
			t.Fatal("nil infra must yield zero-value config subscriber runtime dependency view")
		}
	})

	t.Run("maps runtime fields", func(t *testing.T) {
		youtubeService := &stubYouTubeService{}
		holodexService := &holodex.Service{}
		var alarmCRUD domain.AlarmCRUD = testAlarmCRUD{}
		infra := &appbootstrap.BotInfrastructure{
			Deps:           &bot.Dependencies{Service: youtubeService},
			HolodexService: holodexService,
			AlarmCRUD:      alarmCRUD,
		}

		view := buildBotConfigSubscriberRuntimeDependencies(infra)
		if view.YouTubeService != youtubeService {
			t.Fatal("youtube service mapping mismatch")
		}
		if view.HolodexService != holodexService {
			t.Fatal("holodex service mapping mismatch")
		}
		if view.AlarmCRUD != alarmCRUD {
			t.Fatal("alarm CRUD mapping mismatch")
		}
	})
}

func TestBuildBotRuntimeDependencyViews(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		views := buildBotRuntimeDependencyViews(nil)
		if views.botDeps != nil {
			t.Fatal("nil infra must yield nil bot deps")
		}
		if views.webhook.Cache != nil || views.configSubscriber.Cache != nil || views.configSubscriberRuntime.AlarmCRUD != nil {
			t.Fatal("nil infra must yield zero-value runtime dependency views")
		}
	})

	t.Run("maps composed runtime views", func(t *testing.T) {
		cache := &cache.Service{}
		settingsService := &stubSettingsReadWriter{}
		youtubeService := &stubYouTubeService{}
		holodexService := &holodex.Service{}
		var alarmCRUD domain.AlarmCRUD = testAlarmCRUD{}
		deps := &bot.Dependencies{Cache: cache, Settings: settingsService, Service: youtubeService}
		infra := &appbootstrap.BotInfrastructure{Deps: deps, AlarmCRUD: alarmCRUD, HolodexService: holodexService}

		views := buildBotRuntimeDependencyViews(infra)
		if views.botDeps != deps {
			t.Fatal("bot deps mapping mismatch")
		}
		if views.webhook.Cache != cache {
			t.Fatal("webhook view mapping mismatch")
		}
		if views.configSubscriber.Cache != cache || views.configSubscriber.Settings != settingsService {
			t.Fatal("config subscriber view mapping mismatch")
		}
		if views.configSubscriberRuntime.AlarmCRUD != alarmCRUD || views.configSubscriberRuntime.HolodexService != holodexService {
			t.Fatal("config subscriber runtime view mapping mismatch")
		}
	})
}

var (
	_ youtube.Service     = (*stubYouTubeService)(nil)
	_ settings.ReadWriter = (*stubSettingsReadWriter)(nil)
)
