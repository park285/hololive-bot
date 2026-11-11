package adminruntime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

// alarm provider URL이 없으면 in-process AlarmService로 대신하지 않고 기동을 실패시킨다.
func TestBuildAlarmProviderRequiresAlarmProviderURL(t *testing.T) {
	t.Parallel()

	provider, err := buildAlarmProvider(&apiconfig.AdminPlaneConfig{}, slog.New(slog.DiscardHandler))

	require.Nil(t, provider)
	require.Error(t, err)
}
