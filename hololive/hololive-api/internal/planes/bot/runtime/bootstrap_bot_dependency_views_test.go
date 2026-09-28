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
	"testing"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

type stubSettingsReadWriter struct{}

func (s *stubSettingsReadWriter) Get() settings.Settings { return settings.Settings{} }
func (s *stubSettingsReadWriter) Update(settings.Settings) error {
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
		cacheService := &cache.Service{}
		deps := &orchestration.Dependencies{Cache: cacheService}
		view := buildBotWebhookRuntimeDependencies(deps)

		if view.Cache != cacheService {
			t.Fatal("cache mapping mismatch")
		}
	})
}

func TestBuildBotRuntimeDependencyViews(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		views := buildBotRuntimeDependencyViews(nil)
		if views.botDeps != nil {
			t.Fatal("nil infra must yield nil bot deps")
		}

		if views.webhook.Cache != nil {
			t.Fatal("nil infra must yield zero-value runtime dependency views")
		}
	})

	t.Run("maps composed runtime views", func(t *testing.T) {
		cacheService := &cache.Service{}
		settingsService := &stubSettingsReadWriter{}
		holodexService := &holodexprovider.Service{}

		var alarmCRUD domain.AlarmCRUD = testAlarmCRUD{}

		deps := &orchestration.Dependencies{Cache: cacheService, Settings: settingsService}
		infra := &appbootstrap.BotInfrastructure{Deps: deps, AlarmCRUD: alarmCRUD, HolodexService: holodexService}

		views := buildBotRuntimeDependencyViews(infra)
		if views.botDeps != deps {
			t.Fatal("bot deps mapping mismatch")
		}

		if views.webhook.Cache != cacheService {
			t.Fatal("webhook view mapping mismatch")
		}
	})
}

var _ settings.ReadWriter = (*stubSettingsReadWriter)(nil)
