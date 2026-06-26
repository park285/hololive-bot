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
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	appwiring "github.com/kapu/hololive-api/internal/planes/bot/internal/app/wiring"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot"
)

type botWebhookRuntimeDependencies = appbootstrap.BotWebhookRuntimeDependencies
type botConfigSubscriberDependencies = appbootstrap.BotConfigSubscriberDependencies
type botConfigSubscriberRuntimeDependencies = appbootstrap.BotConfigSubscriberRuntimeDependencies

type botRuntimeDependencyViews struct {
	botDeps                 *bot.Dependencies
	webhook                 botWebhookRuntimeDependencies
	configSubscriber        botConfigSubscriberDependencies
	configSubscriberRuntime botConfigSubscriberRuntimeDependencies
}

func buildBotWebhookRuntimeDependencies(deps *bot.Dependencies) botWebhookRuntimeDependencies {
	return botWebhookRuntimeDependencies{Cache: appwiring.BuildBotRuntimeDependencyViews(appwiring.BotRuntimeDependencyViewInputs{BotDependencies: deps}).Webhook.Cache}
}

func buildBotConfigSubscriberDependencies(deps *bot.Dependencies) botConfigSubscriberDependencies {
	views := appwiring.BuildBotRuntimeDependencyViews(appwiring.BotRuntimeDependencyViewInputs{BotDependencies: deps})
	return botConfigSubscriberDependencies{
		Cache:    views.ConfigSubscriber.Cache,
		Settings: views.ConfigSubscriber.Settings,
	}
}

func buildBotConfigSubscriberRuntimeDependencies(infra *appbootstrap.BotInfrastructure) botConfigSubscriberRuntimeDependencies {
	if infra == nil {
		return botConfigSubscriberRuntimeDependencies{}
	}

	views := appwiring.BuildBotRuntimeDependencyViews(appwiring.BotRuntimeDependencyViewInputs{
		BotDependencies: infra.Deps,
		AlarmCRUD:       infra.AlarmCRUD,
		HolodexService:  infra.HolodexService,
	})
	return botConfigSubscriberRuntimeDependencies{
		YouTubeService: views.ConfigSubscriberRuntime.YouTubeService,
		HolodexService: views.ConfigSubscriberRuntime.HolodexService,
		AlarmCRUD:      views.ConfigSubscriberRuntime.AlarmCRUD,
	}
}

func buildBotRuntimeDependencyViews(infra *appbootstrap.BotInfrastructure) botRuntimeDependencyViews {
	if infra == nil {
		return botRuntimeDependencyViews{}
	}

	return botRuntimeDependencyViews{
		botDeps:                 infra.Deps,
		webhook:                 buildBotWebhookRuntimeDependencies(infra.Deps),
		configSubscriber:        buildBotConfigSubscriberDependencies(infra.Deps),
		configSubscriberRuntime: buildBotConfigSubscriberRuntimeDependencies(infra),
	}
}
