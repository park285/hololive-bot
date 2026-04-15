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
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	appwiring "github.com/kapu/hololive-kakao-bot-go/internal/app/wiring"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

type botWebhookRuntimeDependencies struct {
	cache cache.Client
}

type botConfigSubscriberDependencies struct {
	cache    cache.Client
	settings settings.ReadWriter
}

type botConfigSubscriberRuntimeDependencies struct {
	youtubeService youtube.Service
	holodexService *holodex.Service
	alarmCRUD      domain.AlarmCRUD
}

type botAdminRuntimeDependencies struct {
	cache            cache.Client
	postgres         database.Client
	memberRepo       *member.Repository
	memberCache      *member.Cache
	profiles         *member.ProfileService
	alarmCRUD        domain.AlarmCRUD
	holodexService   *holodex.Service
	youtubeService   youtube.Service
	statsRepo        stats.StatsDashboardRepository
	activityLogger   *activity.Logger
	settings         settings.ReadWriter
	acl              *acl.Service
	templateAdminSvc *template.AdminService
}

type botServerRuntimeDependencies struct {
	alarmCRUD domain.AlarmCRUD
}

type botRuntimeDependencyViews struct {
	botDeps                 *bot.Dependencies
	webhook                 botWebhookRuntimeDependencies
	configSubscriber        botConfigSubscriberDependencies
	configSubscriberRuntime botConfigSubscriberRuntimeDependencies
	adminRuntime            botAdminRuntimeDependencies
	serverRuntime           botServerRuntimeDependencies
}

func buildBotWebhookRuntimeDependencies(deps *bot.Dependencies) botWebhookRuntimeDependencies {
	return botWebhookRuntimeDependencies{cache: appwiring.BuildBotRuntimeDependencyViews(appwiring.BotRuntimeDependencyViewInputs{BotDependencies: deps}).Webhook.Cache}
}

func buildBotConfigSubscriberDependencies(deps *bot.Dependencies) botConfigSubscriberDependencies {
	views := appwiring.BuildBotRuntimeDependencyViews(appwiring.BotRuntimeDependencyViewInputs{BotDependencies: deps})
	return botConfigSubscriberDependencies{
		cache:    views.ConfigSubscriber.Cache,
		settings: views.ConfigSubscriber.Settings,
	}
}

func buildBotConfigSubscriberRuntimeDependencies(infra *coreInfrastructure) botConfigSubscriberRuntimeDependencies {
	if infra == nil {
		return botConfigSubscriberRuntimeDependencies{}
	}

	views := appwiring.BuildBotRuntimeDependencyViews(appwiring.BotRuntimeDependencyViewInputs{
		BotDependencies: infra.deps,
		AlarmCRUD:       infra.alarmCRUD,
		HolodexService:  infra.holodexService,
	})
	return botConfigSubscriberRuntimeDependencies{
		youtubeService: views.ConfigSubscriberRuntime.YouTubeService,
		holodexService: views.ConfigSubscriberRuntime.HolodexService,
		alarmCRUD:      views.ConfigSubscriberRuntime.AlarmCRUD,
	}
}

func buildBotAdminRuntimeDependencies(infra *coreInfrastructure) botAdminRuntimeDependencies {
	if infra == nil {
		return botAdminRuntimeDependencies{}
	}

	views := appwiring.BuildBotRuntimeDependencyViews(appwiring.BotRuntimeDependencyViewInputs{
		BotDependencies:      infra.deps,
		AlarmCRUD:            infra.alarmCRUD,
		HolodexService:       infra.holodexService,
		YouTubeStack:         infra.ytStack,
		TemplateAdminService: infra.templateAdminSvc,
	})
	return botAdminRuntimeDependencies{
		cache:            views.AdminRuntime.Cache,
		postgres:         views.AdminRuntime.Postgres,
		memberRepo:       views.AdminRuntime.MemberRepo,
		memberCache:      views.AdminRuntime.MemberCache,
		profiles:         views.AdminRuntime.Profiles,
		alarmCRUD:        views.AdminRuntime.AlarmCRUD,
		holodexService:   views.AdminRuntime.HolodexService,
		youtubeService:   views.AdminRuntime.YouTubeService,
		statsRepo:        views.AdminRuntime.StatsRepo,
		activityLogger:   views.AdminRuntime.ActivityLogger,
		settings:         views.AdminRuntime.Settings,
		acl:              views.AdminRuntime.ACL,
		templateAdminSvc: views.AdminRuntime.TemplateAdminService,
	}
}

func buildBotServerRuntimeDependencies(infra *coreInfrastructure) botServerRuntimeDependencies {
	if infra == nil {
		return botServerRuntimeDependencies{}
	}

	return botServerRuntimeDependencies{alarmCRUD: infra.alarmCRUD}
}

func buildBotRuntimeDependencyViews(infra *coreInfrastructure) botRuntimeDependencyViews {
	if infra == nil {
		return botRuntimeDependencyViews{}
	}

	return botRuntimeDependencyViews{
		botDeps:                 infra.deps,
		webhook:                 buildBotWebhookRuntimeDependencies(infra.deps),
		configSubscriber:        buildBotConfigSubscriberDependencies(infra.deps),
		configSubscriberRuntime: buildBotConfigSubscriberRuntimeDependencies(infra),
		adminRuntime:            buildBotAdminRuntimeDependencies(infra),
		serverRuntime:           buildBotServerRuntimeDependencies(infra),
	}
}
