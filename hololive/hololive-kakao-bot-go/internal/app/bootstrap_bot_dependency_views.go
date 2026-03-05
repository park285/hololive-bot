package app

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
