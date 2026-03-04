package command

// Factory: Command 생성 팩토리.
type Factory func(deps *Dependencies) Command

// DefaultFactories: 기본 내장 명령어 팩토리 목록.
func DefaultFactories() []Factory {
	return []Factory{
		func(deps *Dependencies) Command { return NewHelpCommand(deps) },
		func(deps *Dependencies) Command { return NewLiveCommand(deps) },
		func(deps *Dependencies) Command { return NewUpcomingCommand(deps) },
		func(deps *Dependencies) Command { return NewScheduleCommand(deps) },
		func(deps *Dependencies) Command { return NewAlarmCommand(deps) },
		func(deps *Dependencies) Command { return NewMemberInfoCommand(deps) },
		func(deps *Dependencies) Command { return NewSubscriberCommand(deps) },
		func(deps *Dependencies) Command { return NewStatsCommand(deps) },
	}
}

// NewMajorEventFactory: MajorEvent 명령어 팩토리를 생성한다.
func NewMajorEventFactory(repo MajorEventRepository) Factory {
	return func(deps *Dependencies) Command {
		return NewMajorEventCommand(deps, repo)
	}
}

// MemberNewsFactories: MemberNews 관련 명령어 팩토리 목록.
func MemberNewsFactories() []Factory {
	return []Factory{
		func(deps *Dependencies) Command { return NewMemberNewsCommand(deps) },
		func(deps *Dependencies) Command { return NewMemberNewsSubscriptionCommand(deps) },
	}
}

// BuildCommands: 주어진 팩토리 목록으로 Command 인스턴스를 생성한다.
func BuildCommands(deps *Dependencies, factories ...Factory) []Command {
	commands := make([]Command, 0, len(factories))
	for _, factory := range factories {
		if factory == nil {
			continue
		}

		command := factory(deps)
		if command == nil {
			continue
		}

		commands = append(commands, command)
	}
	return commands
}
