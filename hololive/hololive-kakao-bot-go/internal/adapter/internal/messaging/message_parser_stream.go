// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package messaging

import (
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
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
	memberTokens := make([]string, 0, len(args))
	all := false

	for _, arg := range args {
		token := stringutil.TrimSpace(arg)
		if token == "" {
			continue
		}

		if isAllUpcomingToken(token) {
			all = true
			continue
		}

		if applyUpcomingLimit(params, token) {
			continue
		}

		memberTokens = append(memberTokens, token)
	}

	applyUpcomingAll(params, all)
	applyUpcomingMember(params, memberTokens)
	return params
}

func isAllUpcomingToken(token string) bool {
	return stringutil.ContainsString([]string{"전체", "전부", "모두", "all"}, stringutil.Normalize(token))
}

func applyUpcomingLimit(params map[string]any, token string) bool {
	if _, exists := params["limit"]; exists {
		return false
	}

	n, err := strconv.Atoi(token)
	if err != nil || n <= 0 {
		return false
	}

	params["limit"] = n
	return true
}

func applyUpcomingAll(params map[string]any, all bool) {
	if !all {
		return
	}

	params["all"] = true
	delete(params, "limit")
}

func applyUpcomingMember(params map[string]any, memberTokens []string) {
	member := stringutil.TrimSpace(strings.Join(memberTokens, " "))
	if member != "" {
		params["member"] = member
	}
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
