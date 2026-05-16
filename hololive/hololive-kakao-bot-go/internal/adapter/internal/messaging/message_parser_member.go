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
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func (ma *MessageAdapter) tryMemberInfoCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMemberInfoCommand(command) {
		return nil, false
	}

	query := stringutil.TrimSpace(strings.Join(args, " "))
	params := make(map[string]any)

	if query != "" {
		params["query"] = query
	}

	return &ParsedCommand{Type: domain.CommandMemberInfo, Params: params, RawMessage: raw}, true
}

func (ma *MessageAdapter) isMemberInfoCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"멤버", "member", "프로필", "profile", "정보", "info"}, cmd)
}

func (ma *MessageAdapter) tryMemberNewsSubscriptionCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMemberNewsSubscriptionCommand(command) {
		return nil, false
	}

	action := memberNewsSubscriptionAction(args)
	return &ParsedCommand{
		Type:       domain.CommandMemberNewsSubscription,
		Params:     map[string]any{"action": action},
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isMemberNewsSubscriptionCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"뉴스알림", "뉴스구독", "newsalert", "newsalerts", "newssubscription"}, cmd)
}

func memberNewsSubscriptionAction(args []string) string {
	if len(args) == 0 {
		return memberNewsActionStatus
	}

	actions := map[string]string{
		"켜기":                   memberNewsActionOn,
		"on":                   memberNewsActionOn,
		"구독":                   memberNewsActionOn,
		"끄기":                   memberNewsActionOff,
		"off":                  memberNewsActionOff,
		"해제":                   memberNewsActionOff,
		"상태":                   memberNewsActionStatus,
		"목록":                   memberNewsActionStatus,
		"list":                 memberNewsActionStatus,
		memberNewsActionStatus: memberNewsActionStatus,
	}
	if action, ok := actions[stringutil.Normalize(args[0])]; ok {
		return action
	}
	return memberNewsActionStatus
}

func (ma *MessageAdapter) tryMemberNewsCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMemberNewsCommand(command) {
		return nil, false
	}

	return &ParsedCommand{
		Type:       domain.CommandMemberNews,
		Params:     map[string]any{"period": memberNewsPeriod(args)},
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isMemberNewsCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"뉴스", "news"}, cmd)
}

func memberNewsPeriod(args []string) string {
	if len(args) == 0 {
		return "weekly"
	}

	periods := map[string]string{
		"이번주":     "weekly",
		"주간":      "weekly",
		"weekly":  "weekly",
		"이번달":     "monthly",
		"월간":      "monthly",
		"monthly": "monthly",
	}
	if period, ok := periods[stringutil.Normalize(strings.Join(args, ""))]; ok {
		return period
	}
	return "weekly"
}
