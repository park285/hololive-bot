package messaging

import (
	"testing"

	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestParseMessage_BroadcastTime(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")

	for msg, want := range map[string]botcommand.BroadcastTimeParams{
		"!방송시간 사쿠라 미코":     {Member: "사쿠라 미코"},
		"!방송시간 사쿠라 미코 30일": {Member: "사쿠라 미코", Days: 30},
		"!방송시간":            {},
	} {
		result := adapter.ParseMessage(&webhook.Message{Msg: msg})
		require.NotNil(t, result, msg)
		require.Equal(t, botcommand.CommandBroadcastTime, result.Type, msg)
		require.Equal(t, botcommand.Params(want), result.Params, msg)
	}
}
