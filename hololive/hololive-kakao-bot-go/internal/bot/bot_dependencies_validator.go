package bot

import (
	"fmt"
	"log/slog"
)

func validateBotDependencies(deps *Dependencies) (streamRuntime, error) {
	if deps == nil {
		return nil, fmt.Errorf("bot dependencies are required")
	}

	if deps.Logger == nil {
		return nil, fmt.Errorf("logger dependency is required")
	}

	deps.Logger.Info("Bot dependency snapshot", slog.Bool("stats_repo", deps.YouTubeStatsRepo != nil))

	if deps.Client == nil {
		return nil, fmt.Errorf("iris client dependency is required")
	}
	if deps.MessageAdapter == nil {
		return nil, fmt.Errorf("message adapter dependency is required")
	}
	if deps.Formatter == nil {
		return nil, fmt.Errorf("response formatter dependency is required")
	}
	if deps.Cache == nil {
		return nil, fmt.Errorf("cache dependency is required")
	}
	if deps.Postgres == nil {
		return nil, fmt.Errorf("postgres dependency is required")
	}
	if deps.Holodex == nil {
		return nil, fmt.Errorf("holodex dependency is required")
	}
	if deps.Profiles == nil {
		return nil, fmt.Errorf("profile service dependency is required")
	}
	if deps.Alarm == nil {
		return nil, fmt.Errorf("alarm service dependency is required")
	}
	if deps.Matcher == nil {
		return nil, fmt.Errorf("matcher dependency is required")
	}
	if deps.MembersData == nil {
		return nil, fmt.Errorf("member data dependency is required")
	}
	if deps.YouTubeStatsRepo == nil {
		return nil, fmt.Errorf("youtube stats repository dependency is required")
	}

	holodexRuntime, ok := deps.Holodex.(streamRuntime)
	if !ok {
		return nil, fmt.Errorf("holodex dependency does not implement stream runtime interface")
	}

	return holodexRuntime, nil
}
