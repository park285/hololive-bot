package messaging

import (
	"slices"
	"strconv"
	"strings"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func (ma *MessageAdapter) tryLiveCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isLiveCommand(command) {
		return nil, false
	}

	params := botcommand.LiveParams{Member: strings.TrimSpace(strings.Join(args, " "))}

	return &ParsedCommand{Type: botcommand.CommandLive, Params: params, RawMessage: raw}, true
}

var liveCommandAliases = []string{"라이브", "live", "방송중", "생방송"}

func (ma *MessageAdapter) isLiveCommand(cmd string) bool {
	return slices.Contains(liveCommandAliases, cmd)
}

func (ma *MessageAdapter) tryUpcomingCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isUpcomingCommand(command) {
		return nil, false
	}

	return &ParsedCommand{
		Type:       botcommand.CommandUpcoming,
		Params:     ma.parseUpcomingArgs(args),
		RawMessage: raw,
	}, true
}

var upcomingCommandAliases = []string{"예정", "upcoming"}

func (ma *MessageAdapter) isUpcomingCommand(cmd string) bool {
	return slices.Contains(upcomingCommandAliases, cmd)
}

func (ma *MessageAdapter) parseUpcomingArgs(args []string) botcommand.UpcomingParams {
	var params botcommand.UpcomingParams

	memberTokens := make([]string, 0, len(args))
	all := false

	for _, arg := range args {
		token := strings.TrimSpace(arg)
		if token == "" {
			continue
		}

		if isAllUpcomingToken(token) {
			all = true
			continue
		}

		if applyUpcomingLimit(&params, token) {
			continue
		}

		memberTokens = append(memberTokens, token)
	}

	if all {
		params.All = true
		params.Limit = 0
	}

	params.Member = strings.TrimSpace(strings.Join(memberTokens, " "))

	return params
}

var allUpcomingTokens = []string{"전체", "전부", "모두", "all"}

func isAllUpcomingToken(token string) bool {
	return slices.Contains(allUpcomingTokens, stringutil.Normalize(token))
}

func applyUpcomingLimit(params *botcommand.UpcomingParams, token string) bool {
	if params.Limit != 0 {
		return false
	}

	n, err := strconv.Atoi(token)
	if err != nil || n <= 0 {
		return false
	}

	params.Limit = n

	return true
}

var scheduleMemberCommandAliases = []string{"멤버", "member"}

func (ma *MessageAdapter) tryScheduleCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isScheduleCommand(command) {
		return nil, false
	}

	if len(args) == 0 && slices.Contains(scheduleMemberCommandAliases, command) {
		return nil, false
	}

	params := ma.parseScheduleArgs(args)

	params.RawCommand = command

	return &ParsedCommand{
		Type:       botcommand.CommandSchedule,
		Params:     params,
		RawMessage: raw,
	}, true
}

var scheduleCommandAliases = []string{"일정", "스케줄", "schedule", "멤버", "member"}

var schedulePosterCommandAliases = []string{"스케쥴"}

func (ma *MessageAdapter) trySchedulePosterCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !slices.Contains(schedulePosterCommandAliases, command) {
		return nil, false
	}

	params := botcommand.SchedulePosterParams{Member: strings.TrimSpace(strings.Join(args, " "))}

	return &ParsedCommand{Type: botcommand.CommandSchedulePoster, Params: params, RawMessage: raw}, true
}

func (ma *MessageAdapter) isScheduleCommand(cmd string) bool {
	return slices.Contains(scheduleCommandAliases, cmd)
}

func (ma *MessageAdapter) parseScheduleArgs(args []string) botcommand.ScheduleParams {
	if len(args) == 0 {
		return botcommand.ScheduleParams{}
	}

	member := args[0]
	days := 7

	if len(args) > 1 {
		if d, err := strconv.Atoi(args[1]); err == nil {
			days = min(max(d, 1), 30)
		}
	}

	return botcommand.ScheduleParams{Member: member, Days: days}
}
