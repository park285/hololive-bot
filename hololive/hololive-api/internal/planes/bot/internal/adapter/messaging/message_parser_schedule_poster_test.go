package messaging

import (
	"testing"

	"github.com/park285/iris-client-go/v6/webhook"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestParseMessage_SchedulePosterIsSeparateFromSchedule(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")

	for msg, want := range map[string]*ParsedCommand{
		"!스케쥴 소라시나 소피아": {Type: botcommand.CommandSchedulePoster, Params: botcommand.SchedulePosterParams{Member: "소라시나 소피아"}},
		"!스케쥴":          {Type: botcommand.CommandSchedulePoster, Params: botcommand.SchedulePosterParams{}},
		"!일정 소피아":       {Type: botcommand.CommandSchedule, Params: botcommand.ScheduleParams{Member: "소피아", Days: 7, RawCommand: "일정"}},
	} {
		result := adapter.ParseMessage(&webhook.Message{Msg: msg})
		require.NotNil(t, result, msg)
		require.Equal(t, want.Type, result.Type, msg)
		require.Equal(t, want.Params, result.Params, msg)
	}
}

func TestParseMessage_ScheduleAliasRetired(t *testing.T) {
	t.Parallel()

	result := NewMessageAdapter("!").ParseMessage(&webhook.Message{Msg: "!스케줄 소피아"})
	require.NotNil(t, result)
	require.Equal(t, botcommand.CommandUnknown, result.Type)
}
