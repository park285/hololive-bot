package adapter

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
)

// MessageAdapter: 카카오톡 메시지를 커맨드로 파싱하는 어댑터입니다.
type MessageAdapter struct {
	prefix string
}

const (
	actionStatus = "status" // 공통: 구독 상태 조회

	memberNewsActionStatus = actionStatus
	memberNewsActionOn     = "on"
	memberNewsActionOff    = "off"
)

var legacyMQPrefixes = []string{"!", "/", "！"}

func normalizeCommandPrefix(prefix string) string {
	trimmed := stringutil.TrimSpace(prefix)
	if trimmed == "" {
		return "!"
	}
	return trimmed
}

func trimLegacyLeading(text string) string {
	return strings.TrimLeftFunc(text, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff':
			return true
		default:
			return false
		}
	})
}

func (ma *MessageAdapter) extractCommandText(raw string) (normalized string, commandText string, ok bool) {
	text := trimLegacyLeading(raw)
	text = stringutil.TrimSpace(text)
	if text == "" {
		return text, "", false
	}

	prefixes := []string{normalizeCommandPrefix(ma.prefix)}
	for _, p := range legacyMQPrefixes {
		if p != prefixes[0] {
			prefixes = append(prefixes, p)
		}
	}

	for _, p := range prefixes {
		if !strings.HasPrefix(text, p) {
			continue
		}
		cmd := stringutil.TrimSpace(text[len(p):])
		if cmd == "" {
			return text, "", false
		}
		return text, cmd, true
	}

	return text, "", false
}

// NewMessageAdapter: MessageAdapter 인스턴스를 생성합니다.
func NewMessageAdapter(prefix string) *MessageAdapter {
	return &MessageAdapter{prefix: prefix}
}

// ParsedCommand: 파싱된 커맨드 정보를 담는 구조체입니다.
type ParsedCommand struct {
	Type       domain.CommandType
	Params     map[string]any
	RawMessage string
}

// ParseMessage: 입력 메시지를 분석하여 커맨드 유형과 파라미터를 추출합니다.
func (ma *MessageAdapter) ParseMessage(message *iris.Message) *ParsedCommand {
	if message == nil || message.Msg == "" {
		return ma.createUnknownCommand("")
	}

	text, commandText, ok := ma.extractCommandText(message.Msg)
	if !ok {
		return ma.createUnknownCommand(text)
	}

	parts := strings.Fields(commandText)
	if len(parts) == 0 {
		return ma.createUnknownCommand(text)
	}

	command := stringutil.Normalize(parts[0])
	args := parts[1:]

	if normalizedCmd, normalizedArgs, ok := normalizeCompactAlarmTokens(command, args); ok {
		command = normalizedCmd
		args = normalizedArgs
	}

	if parsed, ok := ma.tryLiveCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryUpcomingCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryScheduleCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryAlarmCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryHelpCommand(command, text); ok {
		return parsed
	}
	if parsed, ok := ma.trySubscriberCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryStatsCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryMemberInfoCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryMemberNewsSubscriptionCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryMemberNewsCommand(command, args, text); ok {
		return parsed
	}
	if parsed, ok := ma.tryMajorEventCommand(command, args, text); ok {
		return parsed
	}

	return ma.createUnknownCommand(text)
}

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

func (ma *MessageAdapter) tryAlarmCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isAlarmCommand(command, args) {
		return nil, false
	}
	return ma.parseAlarmCommand(command, args, raw), true
}

func (ma *MessageAdapter) tryHelpCommand(command string, raw string) (*ParsedCommand, bool) {
	if !ma.isHelpCommand(command) {
		return nil, false
	}
	return &ParsedCommand{Type: domain.CommandHelp, Params: make(map[string]any), RawMessage: raw}, true
}

func (ma *MessageAdapter) trySubscriberCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isSubscriberCommand(command) {
		return nil, false
	}
	// 멤버 이름이 없으면 에러 처리를 위해 빈 member로 전달
	member := stringutil.TrimSpace(strings.Join(args, " "))
	return &ParsedCommand{
		Type:       domain.CommandSubscriber,
		Params:     map[string]any{"member": member},
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) tryStatsCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isStatsCommand(command) {
		return nil, false
	}
	return &ParsedCommand{
		Type:       domain.CommandStats,
		Params:     ma.parseStatsArgs(args),
		RawMessage: raw,
	}, true
}

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
		}
	}

	return &ParsedCommand{
		Type:       domain.CommandMemberNewsSubscription,
		Params:     map[string]any{"action": action},
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) tryMemberNewsCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMemberNewsCommand(command) {
		return nil, false
	}

	period := "weekly"
	if len(args) > 0 {
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

func (ma *MessageAdapter) isLiveCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"라이브", "live", "방송중", "생방송"}, cmd)
}

func (ma *MessageAdapter) isUpcomingCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"예정", "upcoming"}, cmd)
}

func (ma *MessageAdapter) isScheduleCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"일정", "스케줄", "schedule", "멤버", "member"}, cmd)
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

func (ma *MessageAdapter) isHelpCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"도움말", "도움", "help", "명령어", "commands"}, cmd)
}

func (ma *MessageAdapter) isMemberInfoCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"멤버", "member", "프로필", "profile", "정보", "info"}, cmd)
}

func (ma *MessageAdapter) isSubscriberCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"구독자", "subscriber", "subs"}, cmd)
}

func (ma *MessageAdapter) isStatsCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"구독자순위", "순위", "통계", "stats", "ranking"}, cmd)
}

func (ma *MessageAdapter) isMemberNewsCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"뉴스", "news"}, cmd)
}

func (ma *MessageAdapter) isMemberNewsSubscriptionCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"뉴스알림", "뉴스구독", "newsalert", "newsalerts", "newssubscription"}, cmd)
}

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

func (ma *MessageAdapter) parseStatsArgs(args []string) map[string]any {
	params := map[string]any{"action": "gainers"}
	for _, arg := range args {
		token := stringutil.TrimSpace(arg)
		if token == "" {
			continue
		}

		if strings.Contains(token, "=") {
			parts := strings.SplitN(token, "=", 2)
			if len(parts) != 2 {
				continue
			}

			key := stringutil.TrimSpace(parts[0])
			value := stringutil.TrimSpace(parts[1])
			if key == "" || value == "" {
				continue
			}

			lowerKey := stringutil.Normalize(key)
			if isStatsPeriodKey(lowerKey) {
				if canonical := normalizePeriodToken(value); canonical != "" {
					params["period"] = canonical
				} else {
					params["period"] = value
				}
			} else if canonical := normalizePeriodToken(value); canonical != "" {
				params["period"] = canonical
			}
			continue
		}

		if canonical := normalizePeriodToken(token); canonical != "" {
			params["period"] = canonical
		}
	}

	return params
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

func (ma *MessageAdapter) createUnknownCommand(text string) *ParsedCommand {
	return &ParsedCommand{
		Type:       domain.CommandUnknown,
		Params:     make(map[string]any),
		RawMessage: text,
	}
}

func isStatsPeriodKey(key string) bool {
	switch key {
	case "period", "기간", "주기", "순위", "랭킹", "구독자", "통계":
		return true
	}
	return false
}

func normalizePeriodToken(raw string) string {
	return domain.NormalizeStatsPeriodToken(raw)
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
