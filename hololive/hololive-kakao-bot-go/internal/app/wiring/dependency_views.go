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

package wiring

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

type BotWebhookRuntimeDependencies struct {
	Cache cache.Client
}

type BotConfigSubscriberDependencies struct {
	Cache    cache.Client
	Settings settings.ReadWriter
}

type BotConfigSubscriberRuntimeDependencies struct {
	YouTubeService youtube.Service
	HolodexService *holodex.Service
	AlarmCRUD      domain.AlarmCRUD
}

type BotRuntimeDependencyViewInputs struct {
	BotDependencies *bot.Dependencies
	AlarmCRUD       domain.AlarmCRUD
	HolodexService  *holodex.Service
}

type BotRuntimeDependencyViews struct {
	BotDeps                 *bot.Dependencies
	Webhook                 BotWebhookRuntimeDependencies
	ConfigSubscriber        BotConfigSubscriberDependencies
	ConfigSubscriberRuntime BotConfigSubscriberRuntimeDependencies
}

func BuildBotRuntimeDependencyViews(inputs BotRuntimeDependencyViewInputs) BotRuntimeDependencyViews {
	if inputs.BotDependencies == nil {
		return BotRuntimeDependencyViews{}
	}

	return BotRuntimeDependencyViews{
		BotDeps: inputs.BotDependencies,
		Webhook: BotWebhookRuntimeDependencies{
			Cache: inputs.BotDependencies.Cache,
		},
		ConfigSubscriber: BotConfigSubscriberDependencies{
			Cache:    inputs.BotDependencies.Cache,
			Settings: inputs.BotDependencies.Settings,
		},
		ConfigSubscriberRuntime: BotConfigSubscriberRuntimeDependencies{
			YouTubeService: inputs.BotDependencies.Service,
			HolodexService: inputs.HolodexService,
			AlarmCRUD:      inputs.AlarmCRUD,
		},
	}
}
