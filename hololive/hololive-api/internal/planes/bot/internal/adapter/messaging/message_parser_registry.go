package messaging

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
		commandParserFunc(ma.trySchedulePosterCommand),
		commandParserFunc(ma.tryBroadcastHistoryCommand),
		commandParserFunc(ma.tryBroadcastTimeCommand),
		commandParserFunc(ma.tryAlarmCommand),
		commandParserFunc(func(command string, _ []string, raw string) (*ParsedCommand, bool) {
			return ma.tryHelpCommand(command, raw)
		}),
		commandParserFunc(ma.tryMemberInfoCommand),
		commandParserFunc(ma.tryCelebrationCalendarCommand),
	}
}
