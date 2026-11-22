package orchcmd

import (
	"strings"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

const CommandKeyAlarm = "alarm"

func NormalizeCommandKey(cmdType botcommand.CommandType, params botcommand.Params) (commandKey string, normalizedParams botcommand.Params) {
	typeStr := stringutil.Normalize(cmdType.String())

	if action, ok := strings.CutPrefix(typeStr, "alarm_"); ok {
		alarm, isAlarm := params.(botcommand.AlarmParams)
		if !isAlarm && params != nil {
			return CommandKeyAlarm, params
		}

		alarm.Action = action

		return CommandKeyAlarm, alarm
	}

	return typeStr, params
}
