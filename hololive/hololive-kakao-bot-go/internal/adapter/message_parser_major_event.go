package adapter

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func (ma *MessageAdapter) isMajorEventCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"이벤트", "행사", "행사알림", "이벤트알림"}, cmd)
}

func (ma *MessageAdapter) tryMajorEventCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMajorEventCommand(command) {
		return nil, false
	}

	params := make(map[string]any)
	if len(args) > 0 {
		action := stringutil.Normalize(args[0])
		switch action {
		case "켜기", "on", "구독":
			params["action"] = "on"
		case "끄기", "off", "해제":
			params["action"] = "off"
		case "목록", "list", "상태":
			params["action"] = actionStatus
		default:
			params["action"] = actionStatus
		}
	} else {
		params["action"] = actionStatus
	}

	return &ParsedCommand{Type: domain.CommandMajorEvent, Params: params, RawMessage: raw}, true
}
