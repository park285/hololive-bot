package messagestrings

// Key는 message_strings 한 행을 가리키는 타입 있는 key다. Args는 값에 들어 있어야 하는 format 인자 수이며,
// 시드 SQL 기반 테스트가 이 수와 시드 값의 verb 수를 맞춰 본다
// (DEC-20260926-hololive-message-strings-startup-validation).
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

// 시간 표기.
var (
	TimeFmtStreamTimeDays         = key(NamespaceTimeFmt, "stream_time_days", 2)
	TimeFmtStreamTimeHoursMinutes = key(NamespaceTimeFmt, "stream_time_hours_minutes", 3)
	TimeFmtStreamTimeMinutes      = key(NamespaceTimeFmt, "stream_time_minutes", 2)
	TimeFmtRelativeDays           = key(NamespaceTimeFmt, "relative_days", 1)
	TimeFmtRelativeHoursMinutes   = key(NamespaceTimeFmt, "relative_hours_minutes", 2)
	TimeFmtRelativeMinutes        = key(NamespaceTimeFmt, "relative_minutes", 1)
)

// 이름·제목이 비었을 때 보이는 표시값. 코드 대체 문구가 아니라 DB 정본 문구다.
var (
	MiscVTuberFallback     = key(NamespaceMisc, "vtuber_fallback", 0)
	MiscTimeUnknown        = key(NamespaceMisc, "time_unknown", 0)
	MiscAlarmUnknownMember = key(NamespaceMisc, "alarm_unknown_member", 0)
	MiscAlarmNoTitle       = key(NamespaceMisc, "alarm_no_title", 0)
	MiscAlarmNoStream      = key(NamespaceMisc, "alarm_no_stream", 0)
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
		TimeFmtRelativeDays, TimeFmtRelativeHoursMinutes, TimeFmtRelativeMinutes,
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

// AlarmWorkerEgressRequirements는 alarm-worker 알림 발송(알람 dispatch, YouTube outbox)이 기동 때 확인하는
// message_strings 계약이다. Karing 문구(namespace karing)는 DEC-20260926-hololive-karing-egress-disposition에 따라
// 발송 경로와 함께 계약에서 뺐다. DB의 karing 행은 읽는 코드가 없는 보관 데이터다.
func AlarmWorkerEgressRequirements() Requirements {
	return Requirements{Keys: []Key{
		MiscVTuberFallback, MiscAlarmUnknownMember, MiscAlarmNoTitle, MiscAlarmNoStream,
	}}
}

// AllKeys는 이 패키지가 정의한 타입 있는 key 전체다. 시드 SQL 계약 테스트가 사용한다.
func AllKeys() []Key {
	keys := make([]Key, 0, 64)

	keys = append(keys, TimeFmtKeys()...)
	keys = append(keys, CalendarKeys()...)
	keys = append(keys, NotifyKeys()...)
	keys = append(keys, MiscVTuberFallback, MiscTimeUnknown, MiscAlarmUnknownMember, MiscAlarmNoTitle, MiscAlarmNoStream, AlarmTypeAll)

	return keys
}
