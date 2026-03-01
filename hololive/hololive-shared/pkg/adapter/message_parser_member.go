package adapter

import (
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
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

	action := memberNewsActionStatus
	if len(args) > 0 {
		switch stringutil.Normalize(args[0]) {
		case "켜기", "on", "구독":
			action = memberNewsActionOn
		case "끄기", "off", "해제":
			action = memberNewsActionOff
		case "상태", "목록", "list", memberNewsActionStatus:
			action = memberNewsActionStatus
		default:
			action = memberNewsActionStatus
		}
	}

	return &ParsedCommand{
		Type:       domain.CommandMemberNewsSubscription,
		Params:     map[string]any{"action": action},
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isMemberNewsSubscriptionCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"뉴스알림", "뉴스구독", "newsalert", "newsalerts", "newssubscription"}, cmd)
}

func (ma *MessageAdapter) tryMemberNewsCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMemberNewsCommand(command) {
		return nil, false
	}

	period := "weekly"
	if len(args) > 0 {
		// 공백 포함 입력("이번 달") 대응: args 전체를 결합 후 정규화
		token := stringutil.Normalize(strings.Join(args, ""))
		switch token {
		case "이번주", "주간", "weekly":
			period = "weekly"
		case "이번달", "월간", "monthly":
			period = "monthly"
		}
	}

	return &ParsedCommand{
		Type:       domain.CommandMemberNews,
		Params:     map[string]any{"period": period},
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isMemberNewsCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"뉴스", "news"}, cmd)
}
