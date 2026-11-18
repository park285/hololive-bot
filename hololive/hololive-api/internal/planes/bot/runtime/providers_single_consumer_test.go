package botruntime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
)

func TestSingleConsumerProviders_Smoke(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Run("member matcher", func(t *testing.T) {
		matcher := appbootstrap.ProvideMatcher(&stubMemberDataProvider{}, logger)
		require.NotNil(t, matcher)
	})
}
