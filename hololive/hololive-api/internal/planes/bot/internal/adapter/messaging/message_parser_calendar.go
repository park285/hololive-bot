package messaging

import (
	"strconv"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (ma *MessageAdapter) tryCelebrationCalendarCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isCalendarCommand(command) {
		return nil, false
	}

	params := ma.parseCalendarArgs(args)
	return &ParsedCommand{
		Type:       domain.CommandCalendar,
		Params:     params,
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isCalendarCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"달력", "기념일", "calendar"}, cmd)
}

func (ma *MessageAdapter) parseCalendarArgs(args []string) map[string]any {
	params := make(map[string]any)
	if len(args) == 0 {
		return params
	}
	arg := stringutil.Normalize(args[0])
	if offset, ok := calendarMonthOffset(arg); ok {
		params["monthOffset"] = offset
	}
	if month, ok := calendarMonth(arg); ok {
		params["month"] = month
	}
	return params
}

func calendarMonthOffset(arg string) (int, bool) {
	switch arg {
	case "다음달":
		return 1, true
	case "저번달":
		return -1, true
	default:
		return 0, false
	}
}

func calendarMonth(arg string) (int, bool) {
	m, err := strconv.Atoi(arg)
	if err != nil || m < 1 || m > 12 {
		return 0, false
	}
	return m, true
}
