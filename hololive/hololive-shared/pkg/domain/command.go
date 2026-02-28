package domain

// CommandType: 봇이 인식하고 처리할 수 있는 명령어의 종류 (예: 라이브, 알림 관리 등)
type CommandType string

// CommandType 상수 목록.
// CommandType 상수 목록.
const (
	// CommandLive: 현재 방송 중인 멤버 조회 명령어
	CommandLive CommandType = "live"
	// CommandUpcoming: 방송 예정인 스트림 조회 명령어
	CommandUpcoming CommandType = "upcoming"
	// CommandSchedule: 전체 일정 조회 명령어
	CommandSchedule CommandType = "schedule"
	// CommandHelp: 도움말 보기 명령어
	CommandHelp CommandType = "help"
	// CommandAlarmAdd: 방송 알림 추가 명령어 (예: "페코라 알림 켜줘")
	CommandAlarmAdd CommandType = "alarm_add"
	// CommandAlarmRemove: 방송 알림 삭제 명령어
	CommandAlarmRemove CommandType = "alarm_remove"
	// CommandAlarmList: 현재 설정된 알림 목록 조회 명령어
	CommandAlarmList CommandType = "alarm_list"
	// CommandAlarmClear: 모든 알림 초기화 명령어
	CommandAlarmClear CommandType = "alarm_clear"
	// CommandAlarmInvalid: 알림 관련 불완전하거나 유효하지 않은 명령어
	CommandAlarmInvalid CommandType = "alarm_invalid"
	// CommandMemberInfo: 멤버 프로필 정보 조회 명령어
	CommandMemberInfo CommandType = "member_info"
	// CommandStats: 통계 정보 조회 명령어
	CommandStats CommandType = "stats"
	// CommandSubscriber: 특정 멤버의 구독자 수 조회 명령어
	CommandSubscriber CommandType = "subscriber"
	// CommandMemberNews: 구독 멤버 뉴스 조회 명령어
	CommandMemberNews CommandType = "member_news"
	// CommandMemberNewsSubscription: 구독 멤버 뉴스 알림 구독/해제/상태 명령어
	CommandMemberNewsSubscription CommandType = "news_subscription"
	// CommandMajorEvent: 대형 행사 알림 관리 명령어 (구독/해제/목록)
	CommandMajorEvent CommandType = "major_event"
	// CommandUnknown: 인식할 수 없는 명령어
	CommandUnknown CommandType = "unknown"
)

func (c CommandType) String() string {
	return string(c)
}

// IsValid: 해당 명령어 타입이 유효한지(정의된 목록에 존재하는지) 검증합니다.
func (c CommandType) IsValid() bool {
	switch c {
	case CommandLive, CommandUpcoming, CommandSchedule, CommandHelp,
		CommandAlarmAdd, CommandAlarmRemove, CommandAlarmList, CommandAlarmClear, CommandAlarmInvalid,
		CommandMemberInfo, CommandStats, CommandSubscriber,
		CommandMemberNews, CommandMemberNewsSubscription,
		CommandMajorEvent, CommandUnknown:
		return true
	default:
		return false
	}
}

// ParseResult: 사용자 입력을 파싱하여 도출된 단일 명령어 해석 결과 (신뢰도 포함)
type ParseResult struct {
	Command    CommandType    `json:"command"`
	Params     map[string]any `json:"params"`
	Confidence float64        `json:"confidence"`
	Reasoning  string         `json:"reasoning"`
}

// ParseResults: 파싱 결과의 집합. 단일 명령어일 수도 있고, 중의적일 경우 여러 후보를 포함할 수 있다.
type ParseResults struct {
	Single   *ParseResult
	Multiple []*ParseResult
}

// ChannelSelection: 명령어 대상 채널(멤버)이 불분명할 때, 사용자가 선택할 수 있는 후보 정보
type ChannelSelection struct {
	SelectedIndex int     `json:"selectedIndex"`
	Confidence    float64 `json:"confidence"`
	Reasoning     string  `json:"reasoning"`
}

// IsSingle: 파싱 결과가 단 하나의 명확한 명령어로 해석되었는지 확인합니다.
func (pr *ParseResults) IsSingle() bool {
	return pr.Single != nil
}

// IsMultiple: 파싱 결과가 여러 개의 후보(중의적 해석)를 포함하고 있는지 확인합니다.
func (pr *ParseResults) IsMultiple() bool {
	return len(pr.Multiple) > 0
}

// GetCommands: 해석된 모든 명령어 후보 리스트를 반환합니다.
func (pr *ParseResults) GetCommands() []*ParseResult {
	if pr.IsSingle() {
		return []*ParseResult{pr.Single}
	}
	return pr.Multiple
}
