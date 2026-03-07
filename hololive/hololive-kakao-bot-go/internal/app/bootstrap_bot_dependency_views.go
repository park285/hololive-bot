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

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

// botWebhookRuntimeDependencies: webhook 핸들러 조립에 필요한 최소 의존성 뷰.
type botWebhookRuntimeDependencies struct {
	cache cache.Client
}

// botConfigSubscriberDependencies: 설정 구독 적용에 필요한 최소 의존성 뷰.
type botConfigSubscriberDependencies struct {
	cache    cache.Client
	settings settings.ReadWriter
}

// botConfigSubscriberRuntimeDependencies: 설정 적용 핸들러가 참조하는 런타임 의존성 뷰.
type botConfigSubscriberRuntimeDependencies struct {
	youtubeService youtube.Service
	holodexService *holodex.Service
	alarmCRUD      domain.AlarmCRUD
}

// botAdminRuntimeDependencies: admin API 조립에 필요한 최소 의존성 뷰.
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

// botServerRuntimeDependencies: HTTP 서버 조립에서 필요한 런타임 의존성 뷰.
type botServerRuntimeDependencies struct {
	alarmCRUD domain.AlarmCRUD
}

// botRuntimeDependencyViews: buildBotRuntime에서 소비하는 의존성 뷰 집합.
type botRuntimeDependencyViews struct {
	botDeps                 *bot.Dependencies
	webhook                 botWebhookRuntimeDependencies
	configSubscriber        botConfigSubscriberDependencies
	configSubscriberRuntime botConfigSubscriberRuntimeDependencies
	adminRuntime            botAdminRuntimeDependencies
	serverRuntime           botServerRuntimeDependencies
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

func buildBotWebhookRuntimeDependencies(deps *bot.Dependencies) botWebhookRuntimeDependencies {
	if deps == nil {
		return botWebhookRuntimeDependencies{}
	}
	return botWebhookRuntimeDependencies{
		cache: deps.Cache,
	}
}

func buildBotConfigSubscriberDependencies(deps *bot.Dependencies) botConfigSubscriberDependencies {
	if deps == nil {
		return botConfigSubscriberDependencies{}
	}
	return botConfigSubscriberDependencies{
		cache:    deps.Cache,
		settings: deps.Settings,
	}
}

func buildBotConfigSubscriberRuntimeDependencies(infra *coreInfrastructure) botConfigSubscriberRuntimeDependencies {
	if infra == nil || infra.deps == nil {
		return botConfigSubscriberRuntimeDependencies{}
	}
	return botConfigSubscriberRuntimeDependencies{
		youtubeService: infra.deps.Service,
		holodexService: infra.holodexService,
		alarmCRUD:      infra.alarmCRUD,
	}
}

func buildBotAdminRuntimeDependencies(infra *coreInfrastructure) botAdminRuntimeDependencies {
	if infra == nil || infra.deps == nil {
		return botAdminRuntimeDependencies{}
	}

	var statsRepo stats.StatsDashboardRepository
	if infra.ytStack != nil {
		statsRepo = infra.ytStack.StatsRepo
	}

	return botAdminRuntimeDependencies{
		cache:            infra.deps.Cache,
		postgres:         infra.deps.Postgres,
		memberRepo:       infra.deps.MemberRepo,
		memberCache:      infra.deps.MemberCache,
		profiles:         infra.deps.Profiles,
		alarmCRUD:        infra.alarmCRUD,
		holodexService:   infra.holodexService,
		youtubeService:   infra.deps.Service,
		statsRepo:        statsRepo,
		activityLogger:   infra.deps.Activity,
		settings:         infra.deps.Settings,
		acl:              infra.deps.ACL,
		templateAdminSvc: infra.templateAdminSvc,
	}
}

func buildBotServerRuntimeDependencies(infra *coreInfrastructure) botServerRuntimeDependencies {
	if infra == nil {
		return botServerRuntimeDependencies{}
	}
	return botServerRuntimeDependencies{
		alarmCRUD: infra.alarmCRUD,
	}
}
