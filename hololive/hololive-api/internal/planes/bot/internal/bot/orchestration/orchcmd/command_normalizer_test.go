package orchcmd

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestNormalizeCommandKey(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		cmdType    botcommand.CommandType
		params     botcommand.Params
		wantKey    string
		wantParams botcommand.Params
	}{
		{"alarm_list → alarm 키 + action=list", botcommand.CommandAlarmList, botcommand.AlarmParams{}, CommandKeyAlarm, botcommand.AlarmParams{Action: "list"}},
		{"alarm_add → 인자 유지 + action=add", botcommand.CommandAlarmAdd, botcommand.AlarmParams{Member: "pekora", Type: "방송"}, CommandKeyAlarm, botcommand.AlarmParams{Action: "add", Member: "pekora", Type: "방송"}},
		{"alarm_remove → action=remove", botcommand.CommandAlarmRemove, botcommand.AlarmParams{Member: "aqua"}, CommandKeyAlarm, botcommand.AlarmParams{Action: "remove", Member: "aqua"}},
		{"alarm_clear 인자 없음 → action=clear", botcommand.CommandAlarmClear, nil, CommandKeyAlarm, botcommand.AlarmParams{Action: "clear"}},
		{"alarm_invalid → action=invalid", botcommand.CommandAlarmInvalid, botcommand.AlarmParams{}, CommandKeyAlarm, botcommand.AlarmParams{Action: "invalid"}},
		{"live → 변환 없음", botcommand.CommandLive, botcommand.LiveParams{Member: "miko"}, "live", botcommand.LiveParams{Member: "miko"}},
		{"help → 변환 없음", botcommand.CommandHelp, botcommand.NoParams{}, "help", botcommand.NoParams{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotKey, gotParams := NormalizeCommandKey(tc.cmdType, tc.params)
			if gotKey != tc.wantKey || gotParams != tc.wantParams {
				t.Errorf("NormalizeCommandKey(%q) = (%q, %#v), want (%q, %#v)", tc.cmdType, gotKey, gotParams, tc.wantKey, tc.wantParams)
			}
		})
	}
}
