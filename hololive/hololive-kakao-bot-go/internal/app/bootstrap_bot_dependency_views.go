package app

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

func buildBotIngestionRuntimeDependencies(deps *bot.Dependencies) botIngestionRuntimeDependencies {
	if deps == nil {
		return botIngestionRuntimeDependencies{}
	}
	return botIngestionRuntimeDependencies{
		cache:      deps.Cache,
		postgres:   deps.Postgres,
		irisClient: deps.Client,
		members:    deps.MembersData,
		scheduler:  deps.Scheduler,
		settings:   deps.Settings,
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

func buildBotYouTubeRuntimeDependencies(infra *coreInfrastructure) botYouTubeRuntimeDependencies {
	if infra == nil {
		return botYouTubeRuntimeDependencies{}
	}
	var youtubeService youtube.Service
	if infra.deps != nil {
		youtubeService = infra.deps.Service
	}
	if youtubeService == nil && infra.ytStack != nil {
		youtubeService = infra.ytStack.Service
	}

	return botYouTubeRuntimeDependencies{
		sharedRateLimiter: infra.sharedRL,
		templateRenderer:  infra.templateRenderer,
		youtubeService:    youtubeService,
		holodexService:    infra.holodexService,
		photoSyncService:  infra.photoSync,
	}
}

func buildBotAdminRuntimeDependencies(infra *coreInfrastructure) botAdminRuntimeDependencies {
	if infra == nil || infra.deps == nil {
		return botAdminRuntimeDependencies{}
	}

	var statsRepo youtube.StatsDashboardRepository
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

func buildBotRuntimeDependencyViews(infra *coreInfrastructure) botRuntimeDependencyViews {
	if infra == nil {
		return botRuntimeDependencyViews{}
	}

	return botRuntimeDependencyViews{
		botDeps:                 infra.deps,
		ingestion:               buildBotIngestionRuntimeDependencies(infra.deps),
		webhook:                 buildBotWebhookRuntimeDependencies(infra.deps),
		configSubscriber:        buildBotConfigSubscriberDependencies(infra.deps),
		configSubscriberRuntime: buildBotConfigSubscriberRuntimeDependencies(infra),
		youtubeRuntime:          buildBotYouTubeRuntimeDependencies(infra),
		adminRuntime:            buildBotAdminRuntimeDependencies(infra),
		serverRuntime:           buildBotServerRuntimeDependencies(infra),
	}
}
