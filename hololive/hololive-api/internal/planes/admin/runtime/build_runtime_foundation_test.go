package adminruntime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

func TestBuildAlarmProviderRequiresAlarmProviderURL(t *testing.T) {
	t.Parallel()

	provider, err := buildAlarmProvider(&apiconfig.AdminPlaneConfig{}, slog.New(slog.DiscardHandler))

	require.Nil(t, provider)
	require.Error(t, err)
}
