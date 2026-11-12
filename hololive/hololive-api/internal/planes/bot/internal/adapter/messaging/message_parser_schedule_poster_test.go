package messaging

import (
	"testing"

	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestParseMessage_SchedulePosterIsSeparateFromSchedule(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")

	for msg, want := range map[string]*ParsedCommand{
		"!스케쥴 소라시나 소피아": {Type: botcommand.CommandSchedulePoster, Params: map[string]any{paramMember: "소라시나 소피아"}},
		"!스케쥴":          {Type: botcommand.CommandSchedulePoster, Params: map[string]any{}},
		"!스케줄 소피아":      {Type: botcommand.CommandSchedule, Params: map[string]any{paramMember: "소피아", paramDays: 7, "_raw_command": "스케줄"}},
		"!일정 소피아":       {Type: botcommand.CommandSchedule, Params: map[string]any{paramMember: "소피아", paramDays: 7, "_raw_command": "일정"}},
	} {
		result := adapter.ParseMessage(&webhook.Message{Msg: msg})
		require.NotNil(t, result, msg)
		require.Equal(t, want.Type, result.Type, msg)
		require.Equal(t, want.Params, result.Params, msg)
	}
}
