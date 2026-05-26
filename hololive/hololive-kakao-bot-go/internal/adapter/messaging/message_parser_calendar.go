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
	if len(args) > 0 {
		if m, err := strconv.Atoi(args[0]); err == nil && m >= 1 && m <= 12 {
			params["month"] = m
		}
	}
	return params
}
