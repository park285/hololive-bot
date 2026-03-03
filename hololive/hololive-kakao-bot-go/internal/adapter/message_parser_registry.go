package adapter

// CommandParser: 단일 커맨드 파서 단위를 나타낸다.
type CommandParser interface {
	Parse(command string, args []string, raw string) (*ParsedCommand, bool)
}

type commandParserFunc func(command string, args []string, raw string) (*ParsedCommand, bool)

func (f commandParserFunc) Parse(command string, args []string, raw string) (*ParsedCommand, bool) {
	return f(command, args, raw)
}

func defaultCommandParsers(ma *MessageAdapter) []CommandParser {
	return []CommandParser{
		commandParserFunc(ma.tryLiveCommand),
		commandParserFunc(ma.tryUpcomingCommand),
		commandParserFunc(ma.tryScheduleCommand),
		commandParserFunc(ma.tryAlarmCommand),
		commandParserFunc(func(command string, _ []string, raw string) (*ParsedCommand, bool) {
			return ma.tryHelpCommand(command, raw)
		}),
		commandParserFunc(ma.trySubscriberCommand),
		commandParserFunc(ma.tryStatsCommand),
		commandParserFunc(ma.tryMemberInfoCommand),
		commandParserFunc(ma.tryMemberNewsSubscriptionCommand),
		commandParserFunc(ma.tryMemberNewsCommand),
		commandParserFunc(ma.tryMajorEventCommand),
	}
}
