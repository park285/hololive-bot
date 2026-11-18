package bootstrap

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

func TestNewAlarmClientRequiresAlarmProviderURL(t *testing.T) {
	t.Parallel()

	client, err := newAlarmClient(&apiconfig.BotPlaneConfig{}, slog.New(slog.DiscardHandler))

	require.Nil(t, client)
	require.EqualError(t, err, "alarm provider URL (ALARM_INTERNAL_URL) is required")
}

func TestNewAlarmClientUsesAlarmWorkerURL(t *testing.T) {
	t.Parallel()

	client, err := newAlarmClient(
		&apiconfig.BotPlaneConfig{AlarmServiceURL: "http://127.0.0.1:8081"},
		slog.New(slog.DiscardHandler),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
}
