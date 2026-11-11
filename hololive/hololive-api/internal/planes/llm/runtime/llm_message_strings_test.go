package runtime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func TestLLMMessageStringRequirementsSatisfiedBySeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
	require.NoError(t, store.Load(t.Context()))
	require.NoError(t, store.Validate(llmMessageStringRequirements()))
}
