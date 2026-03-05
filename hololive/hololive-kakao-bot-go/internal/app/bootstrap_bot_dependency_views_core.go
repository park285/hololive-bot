package app

import "github.com/kapu/hololive-kakao-bot-go/internal/bot"

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
