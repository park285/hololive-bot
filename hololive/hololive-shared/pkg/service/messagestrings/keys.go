package messagestrings

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

var (
	TimeFmtStreamTimeDays         = key(NamespaceTimeFmt, "stream_time_days", 2)
	TimeFmtStreamTimeHoursMinutes = key(NamespaceTimeFmt, "stream_time_hours_minutes", 3)
	TimeFmtStreamTimeMinutes      = key(NamespaceTimeFmt, "stream_time_minutes", 2)
)

var (
	MiscVTuberFallback = key(NamespaceMisc, "vtuber_fallback", 0)
	MiscTimeUnknown    = key(NamespaceMisc, "time_unknown", 0)
	MiscAlarmNoTitle   = key(NamespaceMisc, "alarm_no_title", 0)
	MiscAlarmNoStream  = key(NamespaceMisc, "alarm_no_stream", 0)
)

var (
	CalendarHeaderMonth      = key(NamespaceCalendar, "header_month", 2)
	CalendarSummary          = key(NamespaceCalendar, "summary", 3)
	CalendarEmpty            = key(NamespaceCalendar, "empty", 0)
	CalendarDay              = key(NamespaceCalendar, "day", 2)
	CalendarBadgeBirthday    = key(NamespaceCalendar, "badge_birthday", 0)
	CalendarBadgeAnniversary = key(NamespaceCalendar, "badge_anniversary", 1)
	CalendarUnknown          = key(NamespaceCalendar, "unknown", 0)
)

var MiscSchedulePosterNotFound = key(NamespaceMisc, "schedule_poster_not_found", 0)

var NotifyGraduatedMemberWarning = key(NamespaceNotify, "graduated_member_warning", 0)

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
	return []Key{NotifyGraduatedMemberWarning}
}

func AlarmWorkerEgressRequirements() Requirements {
	return Requirements{Keys: []Key{
		MiscVTuberFallback, MiscAlarmNoTitle, MiscAlarmNoStream,
	}}
}
