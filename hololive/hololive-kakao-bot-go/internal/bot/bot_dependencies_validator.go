package bot

import (
	"fmt"
	"log/slog"
)

func validateBotDependencies(deps *Dependencies) (streamRuntime, error) {
	if deps == nil {
		return nil, fmt.Errorf("bot dependencies are required")
	}

	core := deps.coreDeps()
	messaging := deps.messagingDeps()
	data := deps.dataDeps()
	stream := deps.streamDeps()

	if core.logger == nil {
		return nil, fmt.Errorf("logger dependency is required")
	}

	core.logger.Info("Bot dependency snapshot", slog.Bool("stats_repo", stream.youTubeStatsRepo != nil))

	if messaging.client == nil {
		return nil, fmt.Errorf("iris client dependency is required")
	}
	if messaging.messageAdapter == nil {
		return nil, fmt.Errorf("message adapter dependency is required")
	}
	if messaging.formatter == nil {
		return nil, fmt.Errorf("response formatter dependency is required")
	}
	if data.cache == nil {
		return nil, fmt.Errorf("cache dependency is required")
	}
	if data.postgres == nil {
		return nil, fmt.Errorf("postgres dependency is required")
	}
	if stream.holodex == nil {
		return nil, fmt.Errorf("holodex dependency is required")
	}
	if stream.profiles == nil {
		return nil, fmt.Errorf("profile service dependency is required")
	}
	if stream.alarm == nil {
		return nil, fmt.Errorf("alarm service dependency is required")
	}
	if stream.matcher == nil {
		return nil, fmt.Errorf("matcher dependency is required")
	}
	if stream.membersData == nil {
		return nil, fmt.Errorf("member data dependency is required")
	}
	if stream.youTubeStatsRepo == nil {
		return nil, fmt.Errorf("youtube stats repository dependency is required")
	}

	holodexRuntime, ok := stream.holodex.(streamRuntime)
	if !ok {
		return nil, fmt.Errorf("holodex dependency does not implement stream runtime interface")
	}

	return holodexRuntime, nil
}
