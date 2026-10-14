package messagestrings

// Key는 message_strings 행의 key이며, Args는 값에 필요한 format 인자 수다.
type Key struct {
	Namespace string
	Name      string
	Args      int
}

func (k Key) String() string {
	return k.Namespace + "/" + k.Name
}

func key(namespace, name string, args int) Key {
	return Key{Namespace: namespace, Name: name, Args: args}
}

// 시간 표기. `timefmt/relative_*` 시드는 코드가 조회하지 않으므로 필수 key가 아니다.
var (
	TimeFmtStreamTimeDays         = key(NamespaceTimeFmt, "stream_time_days", 2)
	TimeFmtStreamTimeHoursMinutes = key(NamespaceTimeFmt, "stream_time_hours_minutes", 3)
	TimeFmtStreamTimeMinutes      = key(NamespaceTimeFmt, "stream_time_minutes", 2)
)

// 이름·제목이 비었을 때 보이는 표시값. 코드 대체 문구가 아니라 DB 정본 문구다.
var (
	MiscVTuberFallback = key(NamespaceMisc, "vtuber_fallback", 0)
	MiscTimeUnknown    = key(NamespaceMisc, "time_unknown", 0)
	MiscAlarmNoTitle   = key(NamespaceMisc, "alarm_no_title", 0)
	MiscAlarmNoStream  = key(NamespaceMisc, "alarm_no_stream", 0)
)

// 기념일 카드.
var (
	CalendarHeaderMonth      = key(NamespaceCalendar, "header_month", 2)
	CalendarSummary          = key(NamespaceCalendar, "summary", 3)
	CalendarEmpty            = key(NamespaceCalendar, "empty", 0)
	CalendarDay              = key(NamespaceCalendar, "day", 2)
	CalendarBadgeBirthday    = key(NamespaceCalendar, "badge_birthday", 0)
	CalendarBadgeAnniversary = key(NamespaceCalendar, "badge_anniversary", 1)
	CalendarUnknown          = key(NamespaceCalendar, "unknown", 0)
)

// 멤버 뉴스 알림 응답.
var (
	NotifyMemberNewsNoMembers         = key(NamespaceNotify, "member_news_no_members", 0)
	NotifyMemberNewsSubscribed        = key(NamespaceNotify, "member_news_subscribed", 0)
	NotifyMemberNewsAlreadySubscribed = key(NamespaceNotify, "member_news_already_subscribed", 0)
	NotifyMemberNewsUnsubscribed      = key(NamespaceNotify, "member_news_unsubscribed", 0)
	NotifyMemberNewsNotSubscribed     = key(NamespaceNotify, "member_news_not_subscribed", 0)
	NotifyMemberNewsStatusOn          = key(NamespaceNotify, "member_news_status_on", 0)
	NotifyMemberNewsStatusOff         = key(NamespaceNotify, "member_news_status_off", 0)
	NotifyGraduatedMemberWarning      = key(NamespaceNotify, "graduated_member_warning", 0)
)

// AlarmTypeAll은 모든 알람 종류를 뜻하는 표시값이다. 개별 종류는 domain.AlarmType 문자열로 동적 조회한다.
var AlarmTypeAll = key(NamespaceAlarmType, "ALL", 0)

func TimeFmtKeys() []Key {
	return []Key{
		TimeFmtStreamTimeDays, TimeFmtStreamTimeHoursMinutes, TimeFmtStreamTimeMinutes,
	}
}

func CalendarKeys() []Key {
	return []Key{
		CalendarHeaderMonth, CalendarSummary, CalendarEmpty, CalendarDay,
		CalendarBadgeBirthday, CalendarBadgeAnniversary, CalendarUnknown,
	}
}

func NotifyKeys() []Key {
	return []Key{
		NotifyMemberNewsNoMembers, NotifyMemberNewsSubscribed, NotifyMemberNewsAlreadySubscribed,
		NotifyMemberNewsUnsubscribed, NotifyMemberNewsNotSubscribed, NotifyMemberNewsStatusOn,
		NotifyMemberNewsStatusOff, NotifyGraduatedMemberWarning,
	}
}

// AlarmWorkerEgressRequirements는 alarm-worker가 기동 시 확인하는 알람·YouTube 발송 문구 계약이다.
func AlarmWorkerEgressRequirements() Requirements {
	return Requirements{Keys: []Key{
		MiscVTuberFallback, MiscAlarmNoTitle, MiscAlarmNoStream,
	}}
}
