package adapter

import (
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (ma *MessageAdapter) tryAlarmCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isAlarmCommand(command, args) {
		return nil, false
	}
	return ma.parseAlarmCommand(command, args, raw), true
}

func (ma *MessageAdapter) isAlarmCommand(cmd string, args []string) bool {
	if stringutil.ContainsString([]string{"알람", "알림", "알림설정", "알람설정", "alarm"}, cmd) {
		return true
	}

	if len(args) > 0 {
		subCmd := stringutil.Normalize(args[0])
		return stringutil.ContainsString([]string{"추가", "set", "add", "설정", "제거", "remove", "del", "삭제", "목록", "list", "초기화", "clear"}, subCmd)
	}

	return false
}

func (ma *MessageAdapter) parseAlarmCommand(_ string, args []string, rawMessage string) *ParsedCommand {
	if len(args) == 0 {
		return &ParsedCommand{
			Type:       domain.CommandAlarmList,
			Params:     map[string]any{"action": "list"},
			RawMessage: rawMessage,
		}
	}

	subCmd := stringutil.Normalize(args[0])
	restArgs := args[1:]

	if stringutil.ContainsString([]string{"추가", "설정", "set", "add"}, subCmd) {
		member, alarmType := ma.extractMemberAndType(restArgs)
		return &ParsedCommand{
			Type: domain.CommandAlarmAdd,
			Params: map[string]any{
				"action": "add",
				"member": member,
				"type":   alarmType,
			},
			RawMessage: rawMessage,
		}
	}

	if stringutil.ContainsString([]string{"제거", "삭제", "remove", "del", "delete"}, subCmd) {
		member, alarmType := ma.extractMemberAndType(restArgs)
		return &ParsedCommand{
			Type: domain.CommandAlarmRemove,
			Params: map[string]any{
				"action": "remove",
				"member": member,
				"type":   alarmType,
			},
			RawMessage: rawMessage,
		}
	}

	if stringutil.ContainsString([]string{"목록", "list", "show"}, subCmd) {
		return &ParsedCommand{
			Type:       domain.CommandAlarmList,
			Params:     map[string]any{"action": "list"},
			RawMessage: rawMessage,
		}
	}

	if stringutil.ContainsString([]string{"초기화", "clear", "reset"}, subCmd) {
		return &ParsedCommand{
			Type:       domain.CommandAlarmClear,
			Params:     map[string]any{"action": "clear"},
			RawMessage: rawMessage,
		}
	}

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

// 알람 명령 정규화
func normalizeCompactAlarmTokens(command string, args []string) (string, []string, bool) {
	mapping := map[string]string{
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

	subCmd, ok := mapping[command]
	if !ok {
		return command, args, false
	}

	newArgs := make([]string, 0, 1+len(args))
	newArgs = append(newArgs, subCmd)
	newArgs = append(newArgs, args...)

	return "알람", newArgs, true
}
