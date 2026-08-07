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
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
)

type botRuntimeDependencyViews struct {
	botDeps                 *orchestration.Dependencies
	webhook                 appbootstrap.BotWebhookRuntimeDependencies
	configSubscriber        appbootstrap.BotConfigSubscriberDependencies
	configSubscriberRuntime appbootstrap.BotConfigSubscriberRuntimeDependencies
}

func buildBotWebhookRuntimeDependencies(deps *orchestration.Dependencies) appbootstrap.BotWebhookRuntimeDependencies {
	if deps == nil {
		return appbootstrap.BotWebhookRuntimeDependencies{}
	}
	return appbootstrap.BotWebhookRuntimeDependencies{Cache: deps.Cache}
}

func buildBotConfigSubscriberDependencies(deps *orchestration.Dependencies) appbootstrap.BotConfigSubscriberDependencies {
	if deps == nil {
		return appbootstrap.BotConfigSubscriberDependencies{}
	}
	return appbootstrap.BotConfigSubscriberDependencies{
		Cache:    deps.Cache,
		Settings: deps.Settings,
	}
}

func buildBotConfigSubscriberRuntimeDependencies(infra *appbootstrap.BotInfrastructure) appbootstrap.BotConfigSubscriberRuntimeDependencies {
	if infra == nil || infra.Deps == nil {
		return appbootstrap.BotConfigSubscriberRuntimeDependencies{}
	}

	return appbootstrap.BotConfigSubscriberRuntimeDependencies{
		YouTubeService: infra.Deps.Service,
		HolodexService: infra.HolodexService,
		AlarmCRUD:      infra.AlarmCRUD,
		ACL:            infra.Deps.ACL,
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
