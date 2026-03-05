package app

import "github.com/kapu/hololive-shared/pkg/service/youtube"

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
