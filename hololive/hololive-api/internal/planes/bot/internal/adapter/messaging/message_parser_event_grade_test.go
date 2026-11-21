package messaging

import (
	"testing"

	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestEventGradeArguments(t *testing.T) {
	adapter := NewMessageAdapter("!")

	for _, tt := range []struct {
		args   string
		region contract.GradeRegion
		score  int64
	}{
		{"1", "jp", 1}, {"33 일본", "jp", 33}, {"2 미국", "us", 2}, {"1,234 global", "as", 1234}, {"3 글로벌", "as", 3}, {"4 아시아", "as", 4}, {"5 as", "as", 5}, {"9,223,372,036,854,775,807 us", "us", 9223372036854775807},
	} {
		cmd := adapter.ParseMessage(&webhook.Message{Msg: "!이벤트등급 " + tt.args})
		require.NotNil(t, cmd)
		require.Equal(t, botcommand.CommandEventGrade, cmd.Type)
		require.Equal(t, map[string]any{"score": tt.score, "region": tt.region}, cmd.Params)
	}

	for _, args := range []string{"", "0", "-3", "+3", "1,00", "10,0,000", "1234,567", "9,223,372,036,854,775,808", "us 500", "1 jp extra", "1 moon", "a1", "1.5"} {
		cmd := adapter.ParseMessage(&webhook.Message{Msg: "!이벤트등급 " + args})
		require.NotNil(t, cmd)
		require.Equal(t, botcommand.CommandEventGrade, cmd.Type)
		require.Empty(t, cmd.Params)
	}
}
