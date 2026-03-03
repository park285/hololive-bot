package adapter

import (
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func (ma *MessageAdapter) tryLiveCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isLiveCommand(command) {
		return nil, false
	}

	params := make(map[string]any)
	if len(args) > 0 {
		member := stringutil.TrimSpace(strings.Join(args, " "))
		if member != "" {
			params["member"] = member
		}
	}

	return &ParsedCommand{Type: domain.CommandLive, Params: params, RawMessage: raw}, true
}

func (ma *MessageAdapter) isLiveCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"라이브", "live", "방송중", "생방송"}, cmd)
}

func (ma *MessageAdapter) tryUpcomingCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isUpcomingCommand(command) {
		return nil, false
	}
	return &ParsedCommand{
		Type:       domain.CommandUpcoming,
		Params:     ma.parseUpcomingArgs(args),
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isUpcomingCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"예정", "upcoming"}, cmd)
}

func (ma *MessageAdapter) parseUpcomingArgs(args []string) map[string]any {
	params := make(map[string]any)
	if len(args) == 0 {
		return params
	}

	memberTokens := make([]string, 0, len(args))
	limitSet := false
	all := false

	for _, arg := range args {
		token := stringutil.TrimSpace(arg)
		if token == "" {
			continue
		}

		normalized := stringutil.Normalize(token)
		if stringutil.ContainsString([]string{"전체", "전부", "모두", "all"}, normalized) {
			all = true
			continue
		}

		if !limitSet {
			if n, err := strconv.Atoi(token); err == nil && n > 0 {
				params["limit"] = n
				limitSet = true
				continue
			}
		}

		memberTokens = append(memberTokens, token)
	}

	if all {
		params["all"] = true
		delete(params, "limit")
	}

	member := stringutil.TrimSpace(strings.Join(memberTokens, " "))
	if member != "" {
		params["member"] = member
	}

	return params
}

func (ma *MessageAdapter) tryScheduleCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isScheduleCommand(command) {
		return nil, false
	}
	if len(args) == 0 && stringutil.ContainsString([]string{"멤버", "member"}, command) {
		return nil, false
	}
	params := ma.parseScheduleArgs(args)
	params["_raw_command"] = command
	return &ParsedCommand{
		Type:       domain.CommandSchedule,
		Params:     params,
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isScheduleCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"일정", "스케줄", "schedule", "멤버", "member"}, cmd)
}

func (ma *MessageAdapter) parseScheduleArgs(args []string) map[string]any {
	if len(args) == 0 {
		return make(map[string]any)
	}

	member := args[0]
	days := 7

	if len(args) > 1 {
		if d, err := strconv.Atoi(args[1]); err == nil {
			days = min(max(d, 1), 30)
		}
	}

	return map[string]any{
		"member": member,
		"days":   days,
	}
}
