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

package adapter

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

var compactAlarmCommandMapping = map[string]string{
	"알람설정":  "설정",
	"알림설정":  "설정",
	"알람추가":  "추가",
	"알림추가":  "추가",
	"알람목록":  "목록",
	"알림목록":  "목록",
	"알람리스트": "목록",
	"알림리스트": "목록",
	"알람제거":  "제거",
	"알림제거":  "제거",
	"알람삭제":  "삭제",
	"알림삭제":  "삭제",
	"알람초기화": "초기화",
	"알림초기화": "초기화",
	"알람리셋":  "초기화",
	"알림리셋":  "초기화",
	"알람해제":  "제거",
	"알림해제":  "제거",
}

func (ma *MessageAdapter) tryAlarmCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isAlarmCommand(command) {
		return nil, false
	}

	return ma.parseAlarmCommand(command, args, raw), true
}

func (ma *MessageAdapter) isAlarmCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"알람", "알림", "알림설정", "알람설정", "alarm"}, cmd)
}

func (ma *MessageAdapter) parseAlarmCommand(_ string, args []string, rawMessage string) *ParsedCommand {
	if len(args) == 0 {
		return alarmListCommand(rawMessage)
	}

	subCmd := stringutil.Normalize(args[0])
	restArgs := args[1:]

	if stringutil.ContainsString([]string{"추가", "설정", "set", "add"}, subCmd) {
		member, alarmType := ma.extractMemberAndType(restArgs)
		return alarmMemberCommand(domain.CommandAlarmAdd, "add", member, alarmType, rawMessage)
	}

	if stringutil.ContainsString([]string{"제거", "삭제", "remove", "del", "delete"}, subCmd) {
		member, alarmType := ma.extractMemberAndType(restArgs)
		return alarmMemberCommand(domain.CommandAlarmRemove, "remove", member, alarmType, rawMessage)
	}

	if stringutil.ContainsString([]string{"목록", "list", "show"}, subCmd) {
		return alarmListCommand(rawMessage)
	}

	if stringutil.ContainsString([]string{"초기화", "clear", "reset"}, subCmd) {
		return alarmClearCommand(rawMessage)
	}

	return alarmInvalidCommand(subCmd, restArgs, rawMessage)
}

func alarmMemberCommand(commandType domain.CommandType, action string, member string, alarmType string, rawMessage string) *ParsedCommand {
	return &ParsedCommand{
		Type: commandType,
		Params: map[string]any{
			"action": action,
			"member": member,
			"type":   alarmType,
		},
		RawMessage: rawMessage,
	}
}

func alarmListCommand(rawMessage string) *ParsedCommand {
	return &ParsedCommand{
		Type:       domain.CommandAlarmList,
		Params:     map[string]any{"action": "list"},
		RawMessage: rawMessage,
	}
}

func alarmClearCommand(rawMessage string) *ParsedCommand {
	return &ParsedCommand{
		Type:       domain.CommandAlarmClear,
		Params:     map[string]any{"action": "clear"},
		RawMessage: rawMessage,
	}
}

func alarmInvalidCommand(subCmd string, restArgs []string, rawMessage string) *ParsedCommand {
	return &ParsedCommand{
		Type: domain.CommandAlarmInvalid,
		Params: map[string]any{
			"action":      "invalid",
			"sub_command": subCmd,
			"member":      strings.Join(restArgs, " "),
		},
		RawMessage: rawMessage,
	}
}

var alarmTypeKeywords = map[string]string{
	"방송":        "방송",
	"라이브":       "방송",
	"live":      "방송",
	"커뮤니티":      "커뮤니티",
	"community": "커뮤니티",
	"쇼츠":        "쇼츠",
	"shorts":    "쇼츠",
	"전체":        "전체",
	"all":       "전체",
}

func (ma *MessageAdapter) extractMemberAndType(args []string) (member, alarmType string) {
	if len(args) == 0 {
		return "", ""
	}

	lastArg := stringutil.Normalize(args[len(args)-1])
	if typeVal, ok := alarmTypeKeywords[lastArg]; ok && len(args) > 1 {
		return strings.Join(args[:len(args)-1], " "), typeVal
	}

	return strings.Join(args, " "), ""
}

// 알람 명령 정규화.
func normalizeCompactAlarmTokens(command string, args []string) (string, []string, bool) {
	subCmd, ok := compactAlarmCommandMapping[command]
	if !ok {
		return command, args, false
	}

	newArgs := make([]string, 0, 1+len(args))

	newArgs = append(newArgs, subCmd)
	newArgs = append(newArgs, args...)

	return "알람", newArgs, true
}
