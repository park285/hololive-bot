package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvideLLMCostTrackerIsAlwaysNonNil(t *testing.T) {
	require.NotNil(t, ProvideLLMCostTracker())
}
