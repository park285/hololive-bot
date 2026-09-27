package runtime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// llm plane 기동 검증 계약은 운영 시드로 통과해야 한다(DEC-20260926-hololive-message-strings-startup-validation).
func TestLLMMessageStringRequirementsSatisfiedBySeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
	require.NoError(t, store.Load(t.Context()))
	require.NoError(t, store.Validate(llmMessageStringRequirements()))
}
